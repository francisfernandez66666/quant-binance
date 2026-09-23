// binance_report.go 是币安成交/委托回报链的接收端（PLAN §8.1/§8.2/§8.3，Phase 2 收尾腿）。
//
// 三条腿收敛到单一写入者：
//  1. listenKey 生命周期——现货 /api/v3/userDataStream（X-MBX-APIKEY 免签，TTL 30min，25min 续期）；
//     美股 /sapi/v1/equity/listenKey（HMAC 签名，TTL 60min，45min 续期）。取不到 key 时
//     WS 腿整体停用，只剩 REST 差分兜底（降级不中断）。
//  2. WS 推流——现货 <listenKey>@executionReport / 美股 <listenKey>@orderReport，帧内解析
//     后走 applyReport 统一落库；传输层走注入的 data.WsDialFunc（仓内零 websocket 依赖，
//     dialer 由装配层提供；Dial=nil 时本腿停用）。
//  3. REST 差分兜底——按 PollEvery 周期查 openOrders，对本地仍停在 已报/部成 的行做终态
//     校正（回报可能丢失/乱序，轮询是最终一致性的保底）。
//
// 状态映射按 §8.2 固定表：NEW/ACCEPTED→已报、PARTIALLY_FILLED→部成、FILLED→已成、
// CANCELED→已撤、EXPIRED/REJECTED→废单；未知状态绝不臆造终态，只计数+节流告警。
// 落库复用 QMT 链的同一套权威写口：ApplyOrderReportTx（秩守卫单调推进 + §F5 ext:<order_id>
// 占位键）与 ApplyRealFill（TradeID 精确判重锚、成交腿幂等）。
package trading

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quant-trading-v2/internal/data"
	"quant-trading-v2/internal/store"
)

// BinanceReportEvent 是一条归一化后的回报事件（WS 帧或 REST 差分共同的生产物）。
// 字段口径对齐 store 写口：OrderID 是币安 orderId 的字符串形态（orders 表主键），
// FillQty/FillPx 只在成交腿（executionType=TRADE）非零。
type BinanceReportEvent struct {
	Market  string  `json:"market"`   // US / CRYPTO（本接收器只服务币安链）
	OrderID string  `json:"order_id"` // 币安 orderId（数字→字符串）
	Symbol  string  `json:"symbol"`   // 交易对/标的（AAPL、BTCUSDT）
	Side    string  `json:"side"`     // BUY / SELL
	Status  string  `json:"status"`   // 已映射成中文账本状态（已报/部成/已成/已撤/废单）
	Raw     string  `json:"raw"`      // 原始状态（NEW/FILLED/...，留痕取证）
	Price   float64 `json:"price"`    // 委托价
	Qty     float64 `json:"qty"`      // 委托量
	FillQty float64 `json:"fill_qty"` // 本次成交量（成交腿）
	FillPx  float64 `json:"fill_px"`  // 本次成交价（成交腿）
	Fee     float64 `json:"fee"`      // 手续费腿（现货 n 字段；美股尽力透传）
	TradeID string  `json:"trade_id"` // 成交精确判重锚："symbol:t"（§M4）
	TimeMs  int64   `json:"time_ms"`  // 事件时刻（毫秒；0=用到达时刻）
	Source  string  `json:"source"`   // "ws" / "rest"，用于区分三条腿
}

// binanceStatusMap §8.2 固定映射表（唯一权威，未知键绝不入库）。
var binanceStatusMap = map[string]string{
	"NEW":              "已报",
	"ACCEPTED":         "已报",
	"PARTIALLY_FILLED": "部成",
	"FILLED":           "已成",
	"CANCELED":         "已撤",
	"CANCELLED":        "已撤", // 美股侧拼写变体，同义
	"EXPIRED":          "废单",
	"REJECTED":         "废单",
}

// mapBinanceReportStatus 原始状态→账本状态；第二返回值为 false 时调用方必须忽略该事件
// （不入库、不臆造终态），只做计数与节流告警。
func mapBinanceReportStatus(raw string) (string, bool) {
	s, ok := binanceStatusMap[strings.ToUpper(strings.TrimSpace(raw))]
	return s, ok
}

// BinanceReporterOptions 回报接收器配置。Exec/DB/UserID 必填；其余零值走缺省。
type BinanceReporterOptions struct {
	Exec       *BinanceExecutor
	DB         *store.DB
	UserID     string
	Market     string          // US 或 CRYPTO（一个接收器只服务一条市场腿）
	Dial       data.WsDialFunc // nil = 停用 WS 腿，只剩 REST 差分
	OnAlert    func(level, title, content string)
	PollEvery  time.Duration // REST 差分周期，缺省 60s
	RenewEvery time.Duration // listenKey 续期周期，缺省 CRYPTO 25m / US 45m
	WsURL      string        // 覆盖 WS 订阅 URL（测试注入用；空=按域拼 listenKey）
	Now        func() time.Time
}

// BinanceReporter 回报接收器生命周期：Start 起 listenKey 续期 + WS + 轮询协程，
// Stop 幂等收摊。Stats 快照供 /api/binance/state 展示三条腿健康度。
type BinanceReporter struct {
	opt BinanceReporterOptions

	mu      sync.Mutex
	ws      *data.BinanceWS
	key     string // 当前 listenKey（空=未取到，WS 腿停用）
	stopped bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	lastEventUnix atomic.Int64 // 最近一条回报事件到达秒（新鲜度展示）
	events        atomic.Int64 // 累计应用事件数
	ignored       atomic.Int64 // 累计忽略（未知状态/无法归一）
	restDiffs     atomic.Int64 // REST 差分补投数
	unknownLog    atomic.Int64 // 未知状态告警节流戳（unix 秒）
}

// NewBinanceReporter 构造接收器；必填项缺失或 Market 非 US/CRYPTO 直接返回 error
// （宁可启动即失败，也不起一个永远不出活的空转协程）。
func NewBinanceReporter(opt BinanceReporterOptions) (*BinanceReporter, error) {
	if opt.Exec == nil || opt.DB == nil || opt.UserID == "" {
		return nil, fmt.Errorf("binance reporter: Exec/DB/UserID 必填")
	}
	m := data.NormalizeMarketKey(opt.Market)
	if m != "US" && m != "CRYPTO" {
		return nil, fmt.Errorf("binance reporter: Market 只支持 US/CRYPTO，收到 %q", opt.Market)
	}
	opt.Market = m
	if opt.PollEvery <= 0 {
		opt.PollEvery = 60 * time.Second
	}
	if opt.RenewEvery <= 0 {
		// 按 TTL 固定比例续期：现货 30min TTL→25min，美股 60min→45min。
		if m == "CRYPTO" {
			opt.RenewEvery = 25 * time.Minute
		} else {
			opt.RenewEvery = 45 * time.Minute
		}
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &BinanceReporter{opt: opt}, nil
}

// Start 启动接收器：非阻塞（重活全在协程里），listenKey 首取失败不致命——
// 自动退化为 REST-only，续期协程会持续重试并在拿到 key 后补起 WS 腿。
func (r *BinanceReporter) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return fmt.Errorf("binance reporter: 已 Stop，不再复用")
	}
	if r.cancel != nil {
		return nil // 幂等：已在跑
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.wg.Add(2)
	go r.keyLoop(ctx)  // 腿1+2：listenKey 取/续 + WS 起停
	go r.pollLoop(ctx) // 腿3：REST 差分兜底
	return nil
}

// Stop 幂等收摊：取消协程树、停 WS、等所有 goroutine 退出。
func (r *BinanceReporter) Stop() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	cancel := r.cancel
	ws := r.ws
	r.cancel, r.ws = nil, nil
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if ws != nil {
		ws.Stop()
	}
	r.wg.Wait()
}

// keyLoop listenKey 生命周期协程：首取→续期循环；key 到手且 Dial 注入时拉起 WS，
// 续期失败则拆掉 WS（旧 key 已死，挂着只会假活）。
func (r *BinanceReporter) keyLoop(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.opt.RenewEvery)
	defer ticker.Stop()
	if !r.ensureKey(ctx, false) {
		// 首取失败：REST-only 降级运行，等下个续期周期重试（不退出，降级≠死亡）。
		r.alert("warn", "币安 listenKey 首取失败", "回报 WS 腿停用，REST 差分兜底运行中")
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.ensureKey(ctx, true)
		}
	}
}

// ensureKey 取/续 listenKey 并同步 WS 腿。renew=true 用 PUT（续期），false 用 POST（新开）。
// 现货走免签 X-MBX-APIKEY 面，美股走 HMAC 签名面（PLAN §8.1 两套形状）。
func (r *BinanceReporter) ensureKey(ctx context.Context, renew bool) bool {
	key, err := r.listenKey(ctx, renew)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped || ctx.Err() != nil {
		return false
	}
	if err != nil {
		// 续期失败：拆掉依赖旧 key 的 WS 腿，等下轮重建。
		if r.key != "" {
			log.Printf("[binance] listenKey %s 失败（%s 腿降级 REST-only）: %v", boolStr(renew, "续期", "开通"), r.opt.Market, err)
			if r.ws != nil {
				r.ws.Stop()
				r.ws = nil
			}
			r.key = ""
		}
		return false
	}
	if key == "" {
		return false
	}
	if r.key == key {
		return true // 同一把 key（幂等续期），WS 腿不动
	}
	r.key = key
	if r.opt.Dial == nil {
		return true // 装配层没给 dialer：WS 腿整体停用（测试/降级形态）
	}
	if r.ws != nil {
		r.ws.Stop()
	}
	ws, werr := data.NewBinanceWS(data.BinanceWSOptions{
		Name:      "binance-" + strings.ToLower(r.opt.Market) + "-reports",
		URL:       r.userStreamURL(key),
		Dial:      r.opt.Dial,
		OnMessage: r.handleFrame,
	})
	if werr != nil {
		log.Printf("[binance] 回报 WS 通道构造失败（REST-only 降级）: %v", werr)
		return true
	}
	ws.Start()
	r.ws = ws
	return true
}

// userStreamURL 按市场拼 user-data 流地址；WsURL 覆写只服务测试注入。
func (r *BinanceReporter) userStreamURL(key string) string {
	if r.opt.WsURL != "" {
		return r.opt.WsURL
	}
	if r.opt.Market == "CRYPTO" {
		base := data.BinanceSpotWSProd
		if r.opt.Exec.view.Cfg.Testnet {
			base = data.BinanceSpotWSTestnet
		}
		return data.UserStreamURLBare(base, key, "@executionReport")
	}
	return data.UserStreamURLBare(data.BinanceEquityWSProd, key, "@orderReport")
}

// listenKey 开通/续期一把 user-data 流密钥。现货：POST|PUT /api/v3/userDataStream，
// 免签、仅带 X-MBX-APIKEY 头；美股：POST|PUT /sapi/v1/equity/listenKey，HMAC 签名
// （复用执行器 rawSigned，两套形状差异是 §8.1 实测前的契约基线）。
func (r *BinanceReporter) listenKey(ctx context.Context, renew bool) (string, error) {
	method := http.MethodPost
	if renew {
		method = http.MethodPut
	}
	if r.opt.Market == "CRYPTO" {
		s := r.opt.Exec.spot
		req, err := http.NewRequestWithContext(ctx, method, s.base+"/api/v3/userDataStream", nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("X-MBX-APIKEY", s.apiKey)
		resp, err := s.httpc.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		body := make([]byte, 0, 256)
		buf := make([]byte, 256)
		for {
			n, rerr := resp.Body.Read(buf)
			body = append(body, buf[:n]...)
			if rerr != nil || len(body) > 8192 {
				break
			}
		}
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("userDataStream %s: HTTP %d: %s", method, resp.StatusCode, truncate(string(body), 160))
		}
		var out struct {
			ListenKey string `json:"listenKey"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return "", fmt.Errorf("userDataStream 解析失败: %w", err)
		}
		return out.ListenKey, nil
	}
	// 美股签名面：响应形状按 {"listenKey":...} 尽力解析（Q5 未实测，容错即可——
	// 解析不出 key 就是错误走降级，不猜形状）。
	body, err := r.opt.Exec.rawSigned(r.opt.Exec.equity, method, "/sapi/v1/equity/listenKey", url.Values{})
	if err != nil {
		return "", err
	}
	var out struct {
		ListenKey string `json:"listenKey"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("equity listenKey 解析失败: %w", err)
	}
	return out.ListenKey, nil
}

// binanceUnmarshal 帧/明细统一解码口：UseNumber 让 orderId/tradeId 这类大整数走
// json.Number（§15.5 int64 精度路径）——标准 Unmarshal 会把它们变 float64，
// jsonString 取不到值，回报主键直接丢失。
func binanceUnmarshal(b []byte, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	return dec.Decode(v)
}

// handleFrame WS 数据帧入口：剥组合流信封（{"stream","data"}）后按市场分发解析，
// 解析出的事件统一交给 applyReport。帧级异常只计数不回抛（通道协程不能因一帧炸掉整腿）。
func (r *BinanceReporter) handleFrame(payload []byte) {
	var envelope struct {
		Stream string          `json:"stream"`
		Data   json.RawMessage `json:"data"`
	}
	raw := payload
	if binanceUnmarshal(payload, &envelope) == nil && len(envelope.Data) > 0 && envelope.Stream != "" {
		raw = envelope.Data // 组合流形态：真身在 data 里
	}
	var evt map[string]any
	if err := binanceUnmarshal(raw, &evt); err != nil {
		r.ignored.Add(1)
		return
	}
	ev, ok := r.parseReportEvent(evt)
	if !ok {
		r.ignored.Add(1)
		return
	}
	r.applyReport(ev)
}

// parseReportEvent 把一帧 JSON 对象归一为 BinanceReportEvent。
// 现货 executionReport 按官方字段表严格解析（e/x/X/l/L/Z/n/i/t/C）；美股 orderReport
//
//	schema 属 Phase0-Q5 未实测项——只做多别名容错取值，取不到必需键即拒（不猜形状，
//
// 由调用方计数留痕，等实测后收紧）。
func (r *BinanceReporter) parseReportEvent(obj map[string]any) (BinanceReportEvent, bool) {
	if r.opt.Market == "CRYPTO" {
		return r.parseSpotExecutionReport(obj)
	}
	return r.parseEquityOrderReport(obj)
}

// parseSpotExecutionReport 现货 executionReport（§8.2/§8.3 契约键）。
func (r *BinanceReporter) parseSpotExecutionReport(obj map[string]any) (BinanceReportEvent, bool) {
	if ev := mapStr(obj, "e"); ev != "" && ev != "executionReport" {
		return BinanceReportEvent{}, false // 非回报事件（如 ACCOUNT_UPDATE），忽略
	}
	rawStatus := mapStr(obj, "X")
	status, ok := mapBinanceReportStatus(rawStatus)
	if !ok {
		r.logUnknown(rawStatus)
		return BinanceReportEvent{}, false
	}
	orderID := jsonString(obj["i"])
	if orderID == "" || orderID == "<nil>" {
		return BinanceReportEvent{}, false // 无主键 order_id 的回报无法幂等落库（对齐 §F5 拒收语义）
	}
	e := BinanceReportEvent{
		Market:  "CRYPTO",
		OrderID: orderID,
		Symbol:  mapStr(obj, "s"),
		Status:  status,
		Raw:     rawStatus,
		Price:   mapFloat(obj, "p"), // 委托价在 executionReport 里是 "p"（"P" 是价格变动百分号字段，勿混）
		Qty:     mapFloat(obj, "Q"),
		TimeMs:  int64(mapFloat(obj, "T")),
		Source:  "ws",
	}
	switch strings.ToUpper(mapStr(obj, "S")) {
	case "BUY":
		e.Side = SideBuy
	case "SELL":
		e.Side = SideSell
	}
	// 成交腿只在 executionType=TRADE 时非零（NEW/CANCELED 帧里的 l/L 是残留字段不可信）。
	if strings.EqualFold(mapStr(obj, "x"), "TRADE") {
		e.FillQty = mapFloat(obj, "l")
		e.FillPx = mapFloat(obj, "L")
		e.Fee = mapFloat(obj, "n")
		if t := jsonString(obj["t"]); t != "" && t != "<nil>" && t != "0" {
			e.TradeID = e.Symbol + ":" + t // §M4 判重精确锚
		}
	}
	return e, e.Side != ""
}

// parseEquityOrderReport 美股 orderReport：Q5 未实测，别名表尽力取值。
// 必需键=订单主键+状态+方向，缺任一即拒（宁缺勿错，错方向的回报绝不允许入库）。
func (r *BinanceReporter) parseEquityOrderReport(obj map[string]any) (BinanceReportEvent, bool) {
	orderID := firstNonEmpty(jsonString(obj["orderId"]), jsonString(obj["order_id"]), mapStr(obj, "i"))
	rawStatus := firstNonEmpty(mapStr(obj, "status"), mapStr(obj, "X"))
	status, ok := mapBinanceReportStatus(rawStatus)
	if !ok {
		r.logUnknown(rawStatus)
		return BinanceReportEvent{}, false
	}
	sideRaw := strings.ToUpper(firstNonEmpty(mapStr(obj, "side"), mapStr(obj, "S")))
	side := ""
	switch sideRaw {
	case "BUY":
		side = SideBuy
	case "SELL":
		side = SideSell
	}
	if orderID == "" || orderID == "<nil>" || side == "" {
		return BinanceReportEvent{}, false
	}
	e := BinanceReportEvent{
		Market:  "US",
		OrderID: orderID,
		Symbol:  firstNonEmpty(mapStr(obj, "symbol"), mapStr(obj, "s")),
		Side:    side,
		Status:  status,
		Raw:     rawStatus,
		Price:   mapFloat(obj, "price", "L"),
		Qty:     mapFloat(obj, "quantity", "origQty", "Q"),
		Source:  "ws",
	}
	// 成交别名族：executedQty/cumQty 都见过拼写，取先命中的非零值。
	if v := mapFloat(obj, "lastQuantity", "l"); v > 0 {
		e.FillQty = v
		e.FillPx = mapFloat(obj, "lastPrice", "L")
	} else {
		e.FillQty = mapFloat(obj, "executedQty", "cumQty", "Z")
	}
	e.Fee = mapFloat(obj, "fee", "n")
	if t := firstNonEmpty(jsonString(obj["tradeId"]), jsonString(obj["t"])); t != "" && t != "<nil>" && t != "0" {
		e.TradeID = e.Symbol + ":" + t
	}
	return e, true
}

// applyReport 三条腿的统一落库口（对齐 server/qmt.go 的 order/trade 接入段语义）：
//  1. 委托状态腿走 ApplyOrderReportTx——秩守卫单调推进，本地无单时补插；signal_id 缺
//     失时用 §F5 的 ext:<order_id> 占位键（orders 表 UNIQUE(user_id,signal_id) 下空串互撞）。
//  2. 成交腿（FillQty>0）再走 ApplyRealFill——fills 幂等判重 + 加权成本持仓更新，
//     TradeID 精确锚防同秒两笔部成误判重放。
//     落库失败只计数留痕不回抛：WS 侧丢一帧还有 REST 差分兜底重投，这里炸协程反而丢更多。
func (r *BinanceReporter) applyReport(e BinanceReportEvent) {
	local := r.localOrderFor(e.OrderID)
	// §F5 占位键：本地无单（回报先于下单回填到达）时用 ext:<order_id>——orders 表
	// UNIQUE(user_id,signal_id) 下空串互撞，ext: 前缀按单号天然唯一且不与 buy:/sell:/pend: 冲突。
	signalID, code := "ext:"+e.OrderID, e.Symbol
	if local != nil {
		signalID, code = local.SignalID, local.Code
	}
	at := r.eventTimeStr(e)
	action, err := r.opt.DB.ApplyOrderReportTx(store.RealOrder{
		OrderID: e.OrderID, SignalID: signalID, Code: code,
		Side: e.Side, Status: e.Status, Price: e.Price, Qty: e.Qty,
		CreatedAt: at, UserID: r.opt.UserID, Market: e.Market,
		// 补插路径（回报先于下单回填）也打计价币章：与成交腿同源 currencyForSymbol，
		// 避免 orders 行留空、fills 行却带 USDT 的口径分裂。
		Currency: currencyForSymbol(e.Market, e.Symbol),
	})
	if err != nil {
		log.Printf("[binance] 回报落库失败(order=%s status=%s 腿=%s): %v（REST 差分兜底重投）", e.OrderID, e.Status, e.Source, err)
		r.ignored.Add(1)
		return
	}
	r.events.Add(1)
	r.lastEventUnix.Store(r.opt.Now().Unix())
	if action == store.OrderReportAdvanced && e.Status != "已报" {
		// 状态有实际推进（成交/撤单/终态）：打日志留痕，腿来源（ws/rest）并进同一时间线便于取证。
		log.Printf("[binance] 委托状态推进 %s %s → %s (order=%s 腿=%s)", e.Market, code, e.Status, e.OrderID, e.Source)
	}
	if e.FillQty <= 0 || e.FillPx <= 0 {
		return
	}
	if err := r.opt.DB.ApplyRealFill(store.RealFill{
		OrderID: e.OrderID, Code: code, Side: e.Side,
		Price: e.FillPx, Qty: e.FillQty, Amount: e.FillQty * e.FillPx,
		TradedAt: at, SignalID: signalID, TradeID: e.TradeID,
		// name=交易对原文：建仓回填腿（positions 表以 code+name 落账），CN 链路由回报
		// 自带证券名，币安回执只有 symbol，故在这里补。
		Name:   e.Symbol,
		UserID: r.opt.UserID, Fee: e.Fee, StampTax: 0, // 币安无印花税腿，恒 0（§15.3）
		Market: e.Market, Currency: currencyForSymbol(e.Market, e.Symbol),
	}); err != nil {
		log.Printf("[binance] 成交腿落库失败(order=%s trade=%s): %v", e.OrderID, e.TradeID, err)
		return
	}
	log.Printf("[binance] 成交入账 %s %s %s %.8f@%.8f fee=%v (order=%s 腿=%s)",
		e.Market, e.Side, code, e.FillQty, e.FillPx, e.Fee, e.OrderID, e.Source)
}

// localOrderFor 按币安 order_id 找本地委托行（取 signal_id/code 归属）；没有返回 nil。
// 回报量级小（每单几条），全表扫用户委托即可，不为它加索引面。
func (r *BinanceReporter) localOrderFor(orderID string) *store.RealOrder {
	orders, err := r.opt.DB.RealOrdersForUser(r.opt.UserID)
	if err != nil {
		return nil
	}
	for i := range orders {
		if orders[i].OrderID == orderID {
			return &orders[i]
		}
	}
	return nil
}

// eventTimeStr 事件时刻 → 市场本地记账时间（CN 北京/US 纽约/CRYPTO UTC，§15.2 日键口径
// 与 marketToday 同源）；TimeMs 缺失（0）退回到达时刻，绝不落零值时间。
func (r *BinanceReporter) eventTimeStr(e BinanceReportEvent) string {
	t := time.UnixMilli(e.TimeMs)
	if e.TimeMs <= 0 {
		t = r.opt.Now() // 帧内无时间戳：退回到达时刻，绝不落零值时间
	}
	return t.In(data.Session(e.Market).Loc()).Format("2006-01-02 15:04:05")
}

// pollLoop REST 差分协程：WS 是快腿但不是可靠腿（静默掉帧/重连窗口），每 PollEvery
// 对本地仍停在 已报/部成 的币安委托做一次交易所侧校正。
func (r *BinanceReporter) pollLoop(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.opt.PollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.pollOnce(ctx)
		}
	}
}

// pollOnce 单轮差分。诚实边界：
//   - CRYPTO：openOrders 求不在途集合后，逐单 /api/v3/order?orderId= 查明细拿真实终态
//     （交易所侧唯一权威形状，端点已在执行器契约面实测同域）；
//   - US：openOrders 存在性差分后，消失单**只记日志不猜状态**——明细端点属 Q8 未实测项，
//     臆造成/撤会把账本写脏，宁可等 Phase 0 补测后接真状态。
func (r *BinanceReporter) pollOnce(ctx context.Context) {
	locals, err := r.opt.DB.RealOrdersForUser(r.opt.UserID)
	if err != nil {
		log.Printf("[binance] REST 差分读本地委托失败: %v", err)
		return
	}
	var pending []store.RealOrder
	for _, o := range locals {
		if store.NormalizeMarket(o.Market) == r.opt.Market && (o.Status == "已报" || o.Status == "部成") {
			pending = append(pending, o)
		}
	}
	if len(pending) == 0 {
		return
	}
	open := map[string]bool{}
	if r.opt.Market == "CRYPTO" {
		body, err := r.opt.Exec.rawSigned(r.opt.Exec.spot, http.MethodGet, "/api/v3/openOrders", url.Values{})
		if err != nil {
			log.Printf("[binance] REST 差分拉现货 openOrders 失败: %v", err)
			return
		}
		var arr []map[string]any
		if binanceUnmarshal(body, &arr) != nil {
			return
		}
		for _, o := range arr {
			open[jsonString(o["orderId"])] = true
		}
		for _, o := range pending {
			if open[o.OrderID] {
				continue // 仍在途，无需校正
			}
			r.diffSpotOrder(ctx, o.OrderID)
		}
		return
	}
	arr, err := r.opt.Exec.equityOpenOrders()
	if err != nil {
		log.Printf("[binance] REST 差分拉美股 openOrders 失败: %v", err)
		return
	}
	for _, o := range arr {
		open[o.orderID] = true
	}
	for _, o := range pending {
		if open[o.OrderID] {
			continue
		}
		// Q8 未实测：消失只说明"不再在途"，终态是什么没有可信来源——留痕等人工/补测。
		log.Printf("[binance] ⚠ 美股委托 %s 已不在途但终态不可证（Q8 明细端点未实测），不改本地状态", o.OrderID)
	}
}

// diffSpotOrder 拉单笔现货明细并补投成 REST 事件（WS 丢帧的最终一致性保底）。
func (r *BinanceReporter) diffSpotOrder(ctx context.Context, orderID string) {
	params := url.Values{"orderId": {orderID}}
	body, err := r.opt.Exec.rawSigned(r.opt.Exec.spot, http.MethodGet, "/api/v3/order", params)
	if err != nil {
		log.Printf("[binance] REST 差分查明细 %s 失败: %v", orderID, err)
		return
	}
	var obj map[string]any
	if binanceUnmarshal(body, &obj) != nil {
		return
	}
	rawStatus := mapStr(obj, "status")
	status, ok := mapBinanceReportStatus(rawStatus)
	if !ok {
		r.logUnknown(rawStatus)
		return
	}
	ev := BinanceReportEvent{
		Market:  "CRYPTO",
		OrderID: firstNonEmpty(jsonString(obj["orderId"]), orderID),
		Symbol:  mapStr(obj, "symbol"),
		Status:  status,
		Raw:     rawStatus,
		Price:   mapFloat(obj, "price"),
		Qty:     mapFloat(obj, "origQty"),
		TimeMs:  int64(mapFloat(obj, "updateTime")),
		Source:  "rest",
	}
	switch strings.ToUpper(mapStr(obj, "side")) {
	case "BUY":
		ev.Side = SideBuy
	case "SELL":
		ev.Side = SideSell
	}
	// 明细给的是累计成交量额：差分补投只在"累计量比本地行多"时才有信息量，
	// 但 ApplyOrderReportTx 秩守卫会自行吸收重复状态，这里按累计额平摊出单笔近似成交
	// 只用于兜底重建（WS 正常时本函数极少触发）。
	if q := mapFloat(obj, "executedQty"); q > 0 {
		ev.FillQty = q
		if pv := mapFloat(obj, "cummulativeQuoteQty"); pv > 0 && q > 0 {
			ev.FillPx = pv / q
		} else {
			ev.FillPx = ev.Price
		}
	}
	r.restDiffs.Add(1)
	r.applyReport(ev)
}

// Stats 三条腿健康度快照（/api/binance/state 消费；只读计数，不加写路径锁）。
func (r *BinanceReporter) Stats() map[string]any {
	r.mu.Lock()
	ws, key := r.ws, r.key
	r.mu.Unlock()
	out := map[string]any{
		"market":        r.opt.Market,
		"listen_key":    key != "",          // listenKey 是否在握（false=REST-only 降级）
		"ws":            ws != nil,          // WS 腿是否装配
		"events":        r.events.Load(),    // 累计应用
		"ignored":       r.ignored.Load(),   // 累计忽略（未知状态/坏帧/落库失败）
		"rest_diffs":    r.restDiffs.Load(), // REST 差分补投数
		"last_event":    r.lastEventUnix.Load(),
		"ws_reconnects": int64(0),
		"ws_messages":   int64(0),
		"ws_healthy":    false,
		"ws_connected":  false,
	}
	if ws != nil {
		out["ws_healthy"] = ws.Healthy()
		out["ws_connected"] = ws.Connected()
		out["ws_reconnects"] = ws.Reconnects()
		out["ws_messages"] = ws.MessageCount()
	}
	return out
}

// alert 运维告警出口（OnAlert 注入；nil 时退化为日志），回报链的任何"活不正常"都走这里。
func (r *BinanceReporter) alert(level, title, content string) {
	log.Printf("[binance] %s: %s — %s", strings.ToUpper(level), title, content)
	if r.opt.OnAlert != nil {
		r.opt.OnAlert(level, title, content)
	}
}

// logUnknown 未知状态节流告警（60s 一条）：绝不入库、绝不臆造终态（§8.2 收口约定）。
func (r *BinanceReporter) logUnknown(raw string) {
	now := r.opt.Now().Unix()
	last := r.unknownLog.Load()
	if now-last >= 60 && r.unknownLog.CompareAndSwap(last, now) {
		r.alert("warn", "币安回报出现未知状态", fmt.Sprintf("market=%s raw=%q 已忽略（不入库），请核对 §8.2 映射表", r.opt.Market, raw))
	}
}

// —— 帧解析小工具 ————————————————————————————————————————————————

// mapStr 按别名顺序取第一个非空字符串键（美股 schema 未实测，多别名容错）。
func mapStr(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := obj[k]; ok {
			if s := strings.TrimSpace(jsonString(v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

// mapFloat 按别名顺序取第一个可解析数值（JSON 数字与数字串双形态；解析失败继续下一键）。
// 绝不猜价：全部键都拿不到时返回 0，由上层判缺（与 binance_tick.go 数值规则同源）。
func mapFloat(obj map[string]any, keys ...string) float64 {
	for _, k := range keys {
		v, ok := obj[k]
		if !ok {
			continue
		}
		switch t := v.(type) {
		case float64:
			if t != 0 {
				return t
			}
		case json.Number: // UseNumber 解码后数值键的真身（binanceUnmarshal 约定）
			if f, err := t.Float64(); err == nil && f != 0 {
				return f
			}
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
				return f
			}
		}
	}
	return 0
}

// boolStr 布尔→中文短语（日志形状）。
func boolStr(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}

// cryptoQuoteAssets 现货计价币后缀表（长后缀在前，防 "USD" 抢先命中 "USDT" 尾部）。
var cryptoQuoteAssets = []string{"FDUSD", "USDP", "USDT", "USDC", "TUSD", "BUSD", "EUR", "USD"}

// currencyForSymbol 推断计价币（fills/orders currency 腿）：美股恒 USD；现货按已知
// 计价后缀匹配，匹配不上留空——错标计价币比留空危害大（§15.2 币种隔离的入账锚）。
func currencyForSymbol(market, symbol string) string {
	if market == "US" {
		return "USD"
	}
	s := strings.ToUpper(strings.TrimSpace(symbol))
	for _, q := range cryptoQuoteAssets {
		if strings.HasSuffix(s, q) && len(s) > len(q) {
			return q
		}
	}
	return ""
}
