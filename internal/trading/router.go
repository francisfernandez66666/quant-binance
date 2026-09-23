// 文件职责：BrokerRouter——按市场键（CN|US|CRYPTO）分发下单请求、按控制器集合扇出维护动作
// （健康探测/撤单闭环/待生效配置/对账/快照）。PLAN_BINANCE_MULTI_ASSET §6.3 Phase 2 接线核心：
// 引擎只持有一个路由器，CN 订单恒派给 QMT 控制器、US/CRYPTO 派给各自的币安控制器，
// 三套控制器熔断互相隔离（一个市场失联不拖死其它市场的下单通道）。
// 零回归边界：路由器未注册币安控制器时（出厂默认 binance.enabled=false），全部行为退化为
// 「只有 CN 控制器」的旧单通道语义，PlaceOrder 对未知市场 fail-close 拒单。
// English: BrokerRouter dispatches orders by market key and fans out maintenance calls across the
// per-market controllers (each with its own circuit breaker). Without binance controllers registered
// it degrades to the legacy single-CN-channel semantics; unknown markets fail closed.
package trading

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"quant-trading-v2/internal/config"
	"quant-trading-v2/internal/data"
)

// BrokerRouter 市场键 → 控制器 的 dispatch 表 + 扇出入口。
type BrokerRouter struct {
	mu        sync.RWMutex
	byMarket  map[string]*Controller      // "CN" | "US" | "CRYPTO" → 该市场唯一控制器
	reporters map[string]*BinanceReporter // §P2 回报接收器（US/CRYPTO 各一；装配层 Start/Stop）
	feeds     map[string]feedEntry        // §P3 行情/状态 feed 观测位（装配层注册，本层只读呈现）
}

// FeedStat §P3 一条 feed 的观测快照（/api/binance/state "feeds" 节的行形状）。
// SilenceMs=-1=从未活动（§M2 语义，消费端禁当 0 用）；Unconfirmed/Miss 仅状态 feed 填。
// English: one market/status feed's observability row for the state endpoint.
type FeedStat struct {
	Name        string   `json:"name"`
	Market      string   `json:"market"`
	Healthy     bool     `json:"healthy"`
	SilenceMs   int64    `json:"silence_ms"`
	Frames      int64    `json:"frames"`
	Unconfirmed []string `json:"unconfirmed,omitempty"`
	MissCount   int      `json:"miss_count,omitempty"`
}

// feedEntry 注册项：stats 闭包由装配层提供（对 feed 本体的读取都在闭包里，路由层零 data 依赖）。
// §ENH-X2 stop 闭包同理由装配层注入（feed 本体归装配层持有，路由层只收纳「怎么停」这一动作；
// nil=该 feed 无停止义务/旧 RegisterFeed 注册）。
type feedEntry struct {
	stats func() FeedStat
	stop  func()
}

// NewBrokerRouter 以 CN（QMT）控制器建路由；cn 可为 nil（无网关部署），币安市场仍可 Register。
func NewBrokerRouter(cn *Controller) *BrokerRouter {
	r := &BrokerRouter{byMarket: map[string]*Controller{}}
	if cn != nil {
		r.byMarket["CN"] = cn
	}
	return r
}

// Register 注册/替换某市场的控制器（幂等覆盖；market 空串按 CN 归一）。
func (r *BrokerRouter) Register(market string, c *Controller) {
	if c == nil {
		return
	}
	if market == "" {
		market = "CN"
	}
	r.mu.Lock()
	r.byMarket[market] = c
	r.mu.Unlock()
}

// ControllerFor 返回某市场的控制器（""→CN；未知市场返回 nil）。
func (r *BrokerRouter) ControllerFor(market string) *Controller {
	if market == "" {
		market = "CN"
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byMarket[market]
}

// All 返回全部控制器（市场键字典序的稳定快照：CN < CRYPTO < US）。
func (r *BrokerRouter) All() []*Controller {
	r.mu.RLock()
	keys := make([]string, 0, len(r.byMarket))
	for k := range r.byMarket {
		keys = append(keys, k)
	}
	mkts := make([]*Controller, 0, len(keys))
	sort.Strings(keys)
	for _, k := range keys {
		mkts = append(mkts, r.byMarket[k])
	}
	r.mu.RUnlock()
	return mkts
}

// PlaceOrder 按 req.Market 分发下单。未注册的市场 fail-close 拒单——绝不把 US/CRYPTO 单
// 误派给 CN 通道（反之亦然）；这是"加市场不换市场"的路由级保证。
// English: dispatch by market key; an unregistered market is rejected fail-closed, never rerouted.
func (r *BrokerRouter) PlaceOrder(req OrderRequest) (*OrderResult, error) {
	market := req.Market
	if market == "" {
		market = "CN"
	}
	c := r.ControllerFor(market)
	if c == nil {
		return nil, fmt.Errorf("市场 %s 未接入实盘通道（控制器未注册，fail-close 拒单）", market)
	}
	return c.PlaceOrder(req)
}

// CancelOrder 按市场撤单（手动入口带市场键）。
func (r *BrokerRouter) CancelOrder(market, orderID string) error {
	c := r.ControllerFor(market)
	if c == nil {
		return fmt.Errorf("市场 %s 未接入实盘通道，无法撤单", market)
	}
	return c.CancelOrder(orderID)
}

// HealthCheckAll 扇出健康探测（引擎 5s 循环调用；各控制器内部自节流）。
func (r *BrokerRouter) HealthCheckAll() {
	for _, c := range r.All() {
		c.HealthCheck()
	}
}

// SweepAll 扇出撤单闭环（30s 内部节流）。
func (r *BrokerRouter) SweepAll(now time.Time) {
	for _, c := range r.All() {
		c.SweepOrders(now)
	}
}

// ApplyPendingAll 扇出待生效配置消费（交易时段调用；返回是否有任一控制器实际应用）。
func (r *BrokerRouter) ApplyPendingAll() bool {
	applied := false
	for _, c := range r.All() {
		if c.ApplyPendingConfig() {
			applied = true
		}
	}
	return applied
}

// MaybeReconcileAll 扇出周期对账（各控制器自带节流戳；US 控制器快照 Connected=false 恒跳过清账）。
func (r *BrokerRouter) MaybeReconcileAll(interval time.Duration) {
	for _, c := range r.All() {
		c.MaybeReconcile(interval)
	}
}

// SnapshotAll 返回各市场互通健康快照（前端系统行按市场展示；键=市场键 CN/US/CRYPTO）。
func (r *BrokerRouter) SnapshotAll() map[string]StateSnapshot {
	ctrls := r.All()
	out := make(map[string]StateSnapshot, len(ctrls))
	for _, c := range ctrls {
		out[c.MarketKey()] = c.Snapshot()
	}
	return out
}

// BinanceExecutor 返回某市场控制器的币安执行器（非币安市场/Noop/未装配返回 nil）。
// /api/binance/exchange_info、disclaimer 等直连查询面的唯一入口。
func (r *BrokerRouter) BinanceExecutor(market string) *BinanceExecutor {
	if c := r.ControllerFor(market); c != nil {
		return c.BinanceExecutor()
	}
	return nil
}

// RegisterReporter 注册/替换某市场的回报接收器（装配层建好 Dial/配置后注入；重复注册覆盖）。
func (r *BrokerRouter) RegisterReporter(market string, rep *BinanceReporter) {
	if rep == nil {
		return
	}
	r.mu.Lock()
	if r.reporters == nil {
		r.reporters = map[string]*BinanceReporter{}
	}
	r.reporters[data.NormalizeMarketKey(market)] = rep
	r.mu.Unlock()
}

// Reporters 返回市场键→接收器快照（拷贝，防调用方持有期间被替换）。
func (r *BrokerRouter) Reporters() map[string]*BinanceReporter {
	r.mu.RLock()
	out := make(map[string]*BinanceReporter, len(r.reporters))
	for k, v := range r.reporters {
		out[k] = v
	}
	r.mu.RUnlock()
	return out
}

// StartReporters 引擎装配尾调用：逐市场 Start（单腿失败不拖死其它腿——Start 内部自带降级）。
func (r *BrokerRouter) StartReporters() {
	for m, rep := range r.Reporters() {
		if err := rep.Start(); err != nil {
			log.Printf("[trading] 币安 %s 回报接收器启动失败: %v", m, err)
		}
	}
}

// StopReporters 收摊幂等（进程/引擎退出路径）。
func (r *BrokerRouter) StopReporters() {
	for _, rep := range r.Reporters() {
		rep.Stop()
	}
}

// ShutdownLive §ENH-X2 币安侧全生命周期收摊：先停回报接收器（WS+listenKey 腿），再逐一调用
// 各 feed 的 stop 闭包（quotes/status feed 的 WS close 帧与读协程退出）。设计为优雅停机链
// 的显式一步；重复调用安全（reporter/feed 内部各自幂等），单腿 stop panic 不影响其余腿。
// English: shuts down every binance-side reporter and registered feed; safe to call twice.
func (r *BrokerRouter) ShutdownLive() {
	r.StopReporters()
	r.mu.RLock()
	stops := make([]func(), 0, len(r.feeds))
	for _, e := range r.feeds {
		if e.stop != nil {
			stops = append(stops, e.stop)
		}
	}
	r.mu.RUnlock()
	for _, s := range stops {
		func() {
			// 单腿 stop panic 隔离：一扇门炸不许带走整条停机链（与 safeFeedStats 同姿势）。
			defer func() { _ = recover() }()
			s()
		}()
	}
}

// RegisterFeed §P3 注册/替换一条行情/状态 feed 的观测位（name 幂等覆盖）。
// 路由层不持有 feed 本体、不启停其生命周期（装配层负责），这里只做 stats 闭包收纳，
// 供 /api/binance/state 的 "feeds" 节统一呈现（观测不干扰：stats 闭包按名升序调用）。
// 无停止义务的 feed 走本入口；需随 ShutdownLive 关停的用 RegisterFeedWithStop。
// English: registers a read-only observability closure for a quote/status feed; the router
// never owns the feed lifecycle.
func (r *BrokerRouter) RegisterFeed(name, market string, stats func() FeedStat) {
	r.RegisterFeedWithStop(name, market, stats, nil)
}

// RegisterFeedWithStop §ENH-X2 观测位 + 停止闭包一起注册。stop 由装配层给出（形如 feed.Stop），
// ShutdownLive 时逐一调用——补齐「Start 有主、Stop 无门」的进程生命周期不对称：以前 feed 只能
// 靠进程退出连带收摊（WS 无 close 帧、对端半开连接要等超时），现在优雅停机链可显式关停。
// stop=nil 与 RegisterFeed 同语义。重复注册同 name 覆盖旧 stop（幂等约定同 stats）。
func (r *BrokerRouter) RegisterFeedWithStop(name, market string, stats func() FeedStat, stop func()) {
	if name == "" || stats == nil {
		return
	}
	r.mu.Lock()
	if r.feeds == nil {
		r.feeds = map[string]feedEntry{}
	}
	r.feeds[name] = feedEntry{stats: stats, stop: stop}
	r.mu.Unlock()
}

// FeedStats 全部 feed 观测快照（按 name 升序，输出稳定可直接上 UI；stats 闭包在锁外调用，
// 防 feed 内部再取锁引发死锁）。闭包 panic 视为该 feed 失活（Healthy=false + 负数哨兵），
// 观测面绝不让状态端点跟着崩。
func (r *BrokerRouter) FeedStats() []FeedStat {
	r.mu.RLock()
	names := make([]string, 0, len(r.feeds))
	fns := make(map[string]func() FeedStat, len(r.feeds))
	for n, e := range r.feeds {
		names = append(names, n)
		fns[n] = e.stats
	}
	r.mu.RUnlock()
	sort.Strings(names) // name 升序：状态端点行序稳定，UI 不随 map 迭代抖动
	out := make([]FeedStat, 0, len(names))
	for _, n := range names {
		st, ok := safeFeedStats(fns[n])
		if !ok {
			st = FeedStat{Name: n, Healthy: false, SilenceMs: -1, Frames: -1} // panic 降级：只报失活，不编数
		}
		if st.Name == "" {
			st.Name = n
		}
		out = append(out, st)
	}
	return out
}

// safeFeedStats 执行一次 stats 闭包并捕获 panic（观测链的隔离舱）。
func safeFeedStats(fn func() FeedStat) (st FeedStat, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return fn(), true
}

// SetHaltedBinance 币安链 kill-switch 扇出（镜像 applyKillSwitchNow 语义，只作用币安控制器）：
// 以新 halted 重建各市场视图并 UpdateConfig 立即生效（不入待生效队列——紧急停止必须同步），
// 置位时逐控制器 HaltAll 撤销本市场在途委托。返回同步撤销笔数。
// English: kill-switch fan-out across binance controllers only — immediate UpdateConfig (bypassing the
// pending queue) + HaltAll on engagement; CN/QMT tickets are deliberately untouched.
func (r *BrokerRouter) SetHaltedBinance(base config.BinanceConfig, halted bool) int {
	base.Halted = halted
	cancelled := 0
	for _, c := range r.All() {
		if c.Broker() != "binance" {
			continue
		}
		c.UpdateConfig(config.BinanceBrokerView{Cfg: base, Market: c.MarketKey()})
		if halted {
			cancelled += c.HaltAll()
		}
	}
	return cancelled
}

// MaintenanceBinance §P2 币安控制器维护扇出（引擎主循环每轮调用，各子步内部自节流）：
//   - ApplyPendingConfig：按市场会话门控（CRYPTO 恒真 7×24；US 走 RTH/EXTENDED 窗口）——
//     复刻 §QMT-PENDING "休市不翻转实盘行为"语义，但用各自市场的会话日历而非 A 股日历；
//   - HealthCheck/SweepOrders/MaybeReconcile：币安 REST 端点 7×24 可用（行情/交易时段
//     限制在下单链与回报侧），维护动作不设市场门；未启用的控制器在各方法内自然早退。
//
// CN 控制器不在此列——其维护仍由既有 5s 打分循环驱动（§CB-TICKWINDOW 的 A 股窗口口径不变），
// 零回归边界。English: maintenance fan-out for binance controllers only (CN keeps its existing,
// A-share-session-throttled loop); pending config applies are gated by each market's own session.
func (r *BrokerRouter) MaintenanceBinance(now time.Time) {
	for _, c := range r.All() {
		if c.Broker() != "binance" {
			continue
		}
		if data.Session(c.MarketKey()).Active(now) {
			c.ApplyPendingConfig()
		}
		c.HealthCheck()
		c.SweepOrders(now)
		c.MaybeReconcile(5 * time.Minute)
	}
}
