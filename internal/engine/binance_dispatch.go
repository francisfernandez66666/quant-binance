// 文件职责：§战法批-4（2026-09-23）币安链派发核——把 xasset 战法信号（均线/RSI 技术腿
// + 事件情绪腿）折成 trading.OrderRequest 经 BrokerRouter 投给对应市场 Controller，
// 并按档案止盈/止损线扫描持仓产出退出信号（平多=卖出、平空=买入平仓）。
// 这是「战法→信号→下单」在 US/CRYPTO 两腿上的收口件（CN 链的对应件是
// autoPlace/dispatchLive，本文件只镜像其纪律、不共享其代码）。
//
// 准入纪律（全部 fail-close）：
//  1. 有台才开火：派发只在 真交易面（Enabled+双钥匙）或 纸面盘（PaperActive）在位时下单，
//     数据面裸开（NoopExecutor 记账台）一律不投——绝不把信号灌进假受理制造幽灵委托；
//  2. Mode=auto 才实弹：与 CN autoPlace 同闸（manual 档信号只进观测不进委托）；
//  3. 置信闸：sig.Confidence < dispatch.ConfidenceFloor()（0→0.6 缺省）即弃；
//  4. 幂等键：入场 "xbuy/xshort:<code>:<type>:<市场日>"、退出 "xexit:<code>:<side>:<tp|sl>:<市场日>:r<qty>"
//     ——orders 表 (user,signal_id) 唯一键是当日重投的唯一硬闸；
//  5. 轮内限量：每轮新入场 ≤ dispatch.MaxNewOrdersPerRound()（0→5），退出腿不占额
//     （止盈止损是纪律位，不该被买入额度挤掉）；
//  6. 无价不发：市价单也要参考价（行情快照→日K末收盘，两处都拿不到=跳过该票并留痕）。
//
// 节拍：Engine.DispatchBinanceOnce 由 main.go 的 7×24 币安维护 goroutine 驱动
// （与 MaintenanceBinanceOnce 同拍），轮内再按 dispatch.EverySecOr()（0→600s）分市场节流。
//
// English: §XASSET batch-4 — the binance-chain dispatcher. Converts xasset strategy +
// event signals into controller orders (entries) and TP/SL position scans (exits), gated on
// a real or paper desk, auto mode, confidence floor, idempotent signal ids and a
// per-round entry cap; ticked by the 7x24 binance maintenance goroutine.
package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"quant-trading-v2/internal/config"
	"quant-trading-v2/internal/data"
	"quant-trading-v2/internal/store"
	"quant-trading-v2/internal/strategies/xasset"
	"quant-trading-v2/internal/strategy"
	"quant-trading-v2/internal/strategy_engine"
	"quant-trading-v2/internal/trading"
)

// binanceDispatcher 一个引擎账号一份（build 期装配，nil=该引擎没接币安链=零行为）。
type binanceDispatcher struct {
	cfgMgr   *config.Manager
	userID   string
	d1       *store.DB // 研究库：日K（RawBars，键=BTCUSDT / AAPL.US）
	real     *store.DB // 实盘账簿：持仓扫描（退出腿）
	router   *trading.BrokerRouter
	prices   func(market, code string) float64 // 行情快照价（可空=只用日K收盘）
	events   func(market string) []data.XEvent // 事件腿快照（可空=无事件源）
	scorer   *data.XEventScorer                // 方向打分器（关键词基线恒在，LLM 可配）
	llmOpt   config.BinanceEventsConfig        // 大模型四键现值（成器/换器比对用）
	llmCli   *data.XEventLLMClient             // 当前在用的 LLM 客户端
	mu       sync.Mutex
	lastRun  map[string]time.Time
	lastRepo map[string]DispatchReport
}

// DispatchReport 单市场最近一轮派发摘要（观测面 /api/binance/state "dispatch" 节消费）。
type DispatchReport struct {
	At         string   `json:"at"`
	Universe   int      `json:"universe"`    // 参与评估的代号数
	Signals    int      `json:"signals"`     // 过置信闸的信号数
	Placed     int      `json:"placed"`      // 受理成功（含幂等命中）
	Rejected   int      `json:"rejected"`    // 业务拒单
	ExitPlaced int      `json:"exit_placed"` // 止盈止损退出单
	Desk       string   `json:"desk"`        // live|paper|none（none=本轮无台可开火，只出报告不下单）
	Notes      []string `json:"notes,omitempty"`
}

func newBinanceDispatcher(cfgMgr *config.Manager, userID string, d1, real *store.DB, router *trading.BrokerRouter,
	prices func(string, string) float64, events func(string) []data.XEvent, scorer *data.XEventScorer) *binanceDispatcher {
	return &binanceDispatcher{
		cfgMgr: cfgMgr, userID: userID, d1: d1, real: real, router: router,
		prices: prices, events: events, scorer: scorer,
		lastRun: map[string]time.Time{}, lastRepo: map[string]DispatchReport{},
	}
}

// bnQuotePrice 从在位行情 feed 取该代号实时快照价（feed 未装配/快照未见=0，调用方按
// 无价不发 fail-close）。market 键用归一章（US/CRYPTO），与 registry 装配期的写入键一致。
func bnQuotePrice(feeds map[string]*data.BinanceQuoteFeed, market, code string) float64 {
	f := feeds[market]
	if f == nil {
		return 0
	}
	if si := f.Quote(code); si != nil {
		return si.Price
	}
	return 0
}

// DispatchBinanceOnce 7×24 拍上的单次派发（无派发器=零行为；节流/闸全在 tick 内）。
func (e *Engine) DispatchBinanceOnce(now time.Time) {
	e.mu.RLock()
	d := e.bnDispatch
	e.mu.RUnlock()
	if d == nil {
		return
	}
	d.tickAll(now)
}

// tickAll 两市场各走一轮（顺序跑：单轮秒级、且事件打分有共享外呼车道，并发无增益）。
func (d *binanceDispatcher) tickAll(now time.Time) {
	for _, mkt := range []string{"US", "CRYPTO"} {
		repo := d.tickMarket(mkt, now)
		d.mu.Lock()
		if repo.Desk != "" || repo.Signals > 0 {
			d.lastRepo[mkt] = repo
		}
		d.mu.Unlock()
	}
}

// shouldRun 节流判定+登记（锁内完成）：距上轮 ≥ EverySecOr 才放行。
func (d *binanceDispatcher) shouldRun(market string, every time.Duration, now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !now.IsZero() && every > 0 && now.Sub(d.lastRun[market]) < every {
		return false
	}
	d.lastRun[market] = now
	return true
}

// tickMarket 单市场一轮：闸→票源采集→入场折单→退出扫描。
func (d *binanceDispatcher) tickMarket(market string, now time.Time) DispatchReport {
	repo := DispatchReport{}
	cfg := *d.cfgMgr.GetBinanceConfigFor(d.userID)
	view := config.BinanceBrokerView{Cfg: cfg, Market: market}
	disp := cfg.Dispatch
	if !disp.Enabled || !view.BrokerEnabled() {
		return repo // 开关关/市场腿关：零行为零留痕（零配置零行为总纪律）
	}
	desk := ""
	switch {
	case view.TradingActive() && cfg.APIKey != "" && cfg.APISecret != "":
		desk = "live"
	case view.PaperActive():
		desk = "paper"
	}
	repo.Desk = desk
	if !d.shouldRun(market, time.Duration(disp.EverySecOr())*time.Second, now) {
		repo.Desk = "" // 未到期：报告不更新（Desk 空=本轮没跑）
		return repo
	}
	if d.router == nil || d.d1 == nil {
		repo.Notes = append(repo.Notes, "路由/研究库未接线")
		return repo
	}
	ctrl := d.router.ControllerFor(market)
	if ctrl == nil {
		repo.Notes = append(repo.Notes, "该市场 Controller 未装配")
		return repo
	}
	prof := cfg.Stock
	if market == "CRYPTO" {
		prof = cfg.Spot
	}
	codes := make([]string, 0, len(prof.QuoteSymbols))
	for _, c := range prof.QuoteSymbols {
		c = strings.ToUpper(strings.TrimSpace(c))
		if c == "" || containsCI(prof.Blacklist, c) {
			continue
		}
		codes = append(codes, c)
	}
	repo.Universe = len(codes)
	if len(codes) == 0 {
		repo.Notes = append(repo.Notes, "监控池为空（quote_symbols）")
		return repo
	}

	// —— 技术腿：均线交叉 + RSI 动量（BearEnabled 由派发配置统一控制）——
	ma, err1 := xasset.NewMACross(market, xasset.Params{BearEnabled: disp.BearEnabled})
	rsi, err2 := xasset.NewRSIMomentum(market, xasset.Params{BearEnabled: disp.BearEnabled})
	if err1 != nil || err2 != nil {
		repo.Notes = append(repo.Notes, fmt.Sprintf("技术腿构造失败: %v %v", err1, err2))
		return repo
	}
	day := dispatchDay(market, now)
	floor := disp.ConfidenceFloor()
	var sigs []strategy.Signal
	// refPx：技术腿评估时用过的参考价（快照缺席=末根收盘）——入场折单同价复用，
	// 绝不让"评估用 A 价、下单查不到价被弃单"的口径裂缝溜进来。
	refPx := map[string]float64{}
	for _, code := range codes {
		md := d.marketData(market, code, now)
		if md == nil {
			continue
		}
		if md.Price > 0 {
			refPx[code] = md.Price
		}
		// 逐票评估：两腿共用 collect（评估→过闸→出信号），nodata 静默跳过（缺数据不是错误）。
		collect := func(st strategy.Strategy) {
			ev, err := st.Evaluate(code, md)
			if err != nil {
				repo.Notes = append(repo.Notes, fmt.Sprintf("%s %s 评估出错: %v", st.Name(), code, err))
				return
			}
			if ev == nil || !ev.Pass {
				return
			}
			sig, err := st.GenerateSignal(code, ev)
			if err != nil || sig == nil {
				return
			}
			sigs = append(sigs, *sig)
		}
		collect(ma)
		collect(rsi)
	}

	// —— 事件腿：快照→打分→信号（打分器缺位时本轮跳过，不拦技术腿）——
	if d.events != nil && d.scorer != nil {
		if evs := d.events(market); len(evs) > 0 {
			d.ensureLLM(cfg.Events)
			senti := d.scorer.ScoreEvents(context.Background(), evs)
			xs, err := xasset.SignalsFromXEvents(evs, senti, market, xasset.XEventSignalOptions{
				BearEnabled: disp.BearEnabled, MinConfidence: floor,
			})
			if err == nil {
				sigs = append(sigs, xs...)
			}
		}
	}

	// —— 入场折单（live/paper 台在位才投；MaxNewOrdersPerRound 限新入场）——
	marketReady := desk != "" && cfg.Mode == "auto"
	if desk != "" && cfg.Mode != "auto" {
		repo.Notes = append(repo.Notes, "mode=manual：信号只观测不下单")
	}
	if !marketReady && len(sigs) > 0 {
		repo.Notes = append(repo.Notes, "无台/非自动档：本轮信号未投单（desk="+desk+" mode="+cfg.Mode+"）")
	}
	capN := disp.MaxNewOrdersPerRound()
	for i := range sigs {
		sig := sigs[i]
		if sig.Confidence < floor {
			continue // 技术腿补闸（事件腿已在 SignalsFromXEvents 内过同值闸）
		}
		repo.Signals++
		if !marketReady || repo.Placed >= capN {
			continue
		}
		req, ok := d.entryRequest(market, sig, prof.FixedAmount, day, refPx[sig.Code])
		if !ok {
			continue
		}
		if d.place(ctrl, req, &repo, "入场") {
			repo.Placed++
		}
	}

	// —— 退出腿：止盈/止损扫描（不占入场限额；台不在位同样只观测）——
	if disp.TakeProfitPct > 0 || disp.StopLossPct > 0 {
		positions, err := d.real.RealPositionsForUser(d.userID)
		if err != nil {
			repo.Notes = append(repo.Notes, "持仓扫描失败: "+err.Error())
		} else {
			for _, p := range positions {
				if data.NormalizeMarketKey(p.Market) != market || p.Qty <= 0 || p.CostPrice <= 0 {
					continue
				}
				px := d.priceOf(market, p.TsCode, p.CostPrice)
				if px <= 0 {
					continue // 无价不判盈亏（宁漏不错平）
				}
				isShort := p.Side == "short"
				pnl := (px - p.CostPrice) / p.CostPrice * 100
				if isShort {
					pnl = (p.CostPrice - px) / p.CostPrice * 100
				}
				class := ""
				switch {
				case disp.TakeProfitPct > 0 && pnl >= disp.TakeProfitPct:
					class = "tp"
				case disp.StopLossPct > 0 && pnl <= -disp.StopLossPct:
					class = "sl"
				}
				if class == "" || !marketReady {
					continue
				}
				side := trading.SideSell
				if isShort {
					side = trading.SideShortCover
				}
				qty := roundQty(market, p.Qty)
				if qty <= 0 {
					continue
				}
				req := trading.OrderRequest{
					SignalID: fmt.Sprintf("xexit:%s:%s:%s:%s:r%s", p.TsCode, sideClass(isShort), class, day, store.QtyString(qty)),
					Code:     p.TsCode, Name: p.Name, Side: side, PriceType: "market",
					Price: px, Qty: qty, Amount: px * qty, CurrentPrice: px, Market: market,
				}
				if d.place(ctrl, req, &repo, "退出") {
					repo.ExitPlaced++
				}
			}
		}
	}
	repo.At = now.Format("2006-01-02 15:04:05")
	return repo
}

// entryRequest 入场信号→市价单请求：动作折方向、金额折数量、参考价兜底
// （fallbackPx=本轮评估该票的日K末收盘，0=评估链没碰过这只票，即事件腿新面孔）。
// 返回 ok=false=该信号不可投（无价/无动作），已在 notes 留痕。
func (d *binanceDispatcher) entryRequest(market string, sig strategy.Signal, fixedAmount float64, day string, fallbackPx float64) (trading.OrderRequest, bool) {
	side := ""
	switch sig.Action {
	case strategy.ActionBuy:
		side = trading.SideBuy
	case strategy.ActionSell:
		side = trading.SideSell
	case strategy.ActionShortOpen:
		side = trading.SideShortOpen
	case strategy.ActionShortCover:
		side = trading.SideShortCover
	default:
		return trading.OrderRequest{}, false
	}
	px := d.priceOf(market, sig.Code, fallbackPx)
	if px <= 0 {
		return trading.OrderRequest{}, false
	}
	if fixedAmount <= 0 {
		fixedAmount = 10000 // 与 CN autoPlace 同缺省（档案没给仓位尺寸就不开火改为保守常量仅护航纸面演示）
	}
	qty := roundQty(market, fixedAmount/px)
	if qty <= 0 {
		return trading.OrderRequest{}, false
	}
	prefix := map[string]string{trading.SideBuy: "xbuy", trading.SideSell: "xsell", trading.SideShortOpen: "xshort", trading.SideShortCover: "xcover"}[side]
	req := trading.OrderRequest{
		SignalID:     fmt.Sprintf("%s:%s:%s:%s", prefix, sig.Code, sig.Type, day),
		Code:         sig.Code,
		Name:         firstNonEmptyStr(sig.Name, sig.Code),
		Side:         side,
		PriceType:    "market",
		Price:        px,
		Qty:          qty,
		Amount:       px * qty,
		Notional:     px * qty, // 市价买腿的执行器契约（US Notional / CRYPTO quoteOrderQty 取 Amount|Notional）
		CurrentPrice: px,
		Market:       market,
	}
	return req, true
}

// place 投单并归类留痕（幂等命中=控制器既有单，计成功不双计）。
func (d *binanceDispatcher) place(ctrl *trading.Controller, req trading.OrderRequest, repo *DispatchReport, what string) bool {
	res, err := ctrl.PlaceOrder(req)
	if err != nil {
		repo.Rejected++
		if len(repo.Notes) < 12 {
			repo.Notes = append(repo.Notes, what+"失败 "+req.Code+": "+err.Error())
		}
		return false
	}
	if res == nil {
		repo.Rejected++
		if len(repo.Notes) < 12 {
			repo.Notes = append(repo.Notes, what+"空回执 "+req.Code)
		}
		return false
	}
	if !res.OK {
		// 幂等命中（同 signal_id 当日内已有单）＝目标状态已达成，计成功不污染拒单数；
		// 其余业务拒单（风控闸/资金不足）计入 Rejected 并留痕。
		if strings.Contains(res.Err, "duplicate signal_id") {
			return true
		}
		repo.Rejected++
		msg := "业务拒单"
		if res.Err != "" {
			msg = res.Err
		}
		if len(repo.Notes) < 12 {
			repo.Notes = append(repo.Notes, what+"拒单 "+req.Code+": "+msg)
		}
		return false
	}
	return true
}

// marketData 日K→策略输入（研究库键：US 点分 AAPL.US、CRYPTO 裸符号）；不足/无档=nil。
func (d *binanceDispatcher) marketData(market, code string, now time.Time) *strategy_engine.StockMarketData {
	key := code
	if market == "US" {
		key = code + ".US"
	}
	bars, err := d.d1.RawBars(key, "", "99999999")
	if err != nil || len(bars) == 0 {
		return nil
	}
	if n := len(bars); n > 120 {
		bars = bars[n-120:] // 只喂近半年，控每轮内存/耗时（minBars=22，余量充足）
	}
	ks := make([]data.KLine, 0, len(bars))
	for _, b := range bars {
		t, _ := time.ParseInLocation("20060102", b.Date, data.Session(market).Loc())
		ks = append(ks, data.KLine{Date: t, Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, Volume: b.Vol, Amount: b.Amount})
	}
	last := ks[len(ks)-1].Close
	px := d.priceOf(market, code, last)
	return &strategy_engine.StockMarketData{Code: code, KLines: ks, Price: px}
}

// priceOf 参考价三级：行情快照 → 传入回退（日K收盘）→ 0（调用方 fail-close）。
func (d *binanceDispatcher) priceOf(market, code string, fallback float64) float64 {
	if d.prices != nil {
		if p := d.prices(market, code); p > 0 {
			return p
		}
	}
	return fallback
}

// ensureLLM 大模型四键热轮换：配置与在用客户端不一致就换器（空键=纯关键词器）。
// 逐键比对而非整struct相等——BinanceEventsConfig 含切片字段（currencies），不可 ==。
func (d *binanceDispatcher) ensureLLM(ev config.BinanceEventsConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.scorer == nil ||
		(d.llmOpt.LLMBaseURL == ev.LLMBaseURL && d.llmOpt.LLMApiKey == ev.LLMApiKey &&
			d.llmOpt.LLMModel == ev.LLMModel && d.llmOpt.LLMTimeoutSec == ev.LLMTimeoutSec) {
		return
	}
	d.llmOpt = ev
	if (config.BinanceConfig{Events: ev}).DispatchLLMActive() {
		cli, ok := data.NewXEventLLMClient(data.XEventLLMOptions{
			BaseURL: ev.LLMBaseURL, APIKey: ev.LLMApiKey, Model: ev.LLMModel, TimeoutSec: ev.LLMTimeoutSec,
		})
		if ok {
			d.llmCli = cli
			d.scorer = data.NewXEventScorer(cli)
			return
		}
	}
	d.llmCli = nil
	d.scorer = data.NewXEventScorer(nil)
}

// dispatchDay 市场本地日键（幂等键的"当日"以交易市场的日历为准，CRYPTO=UTC 日、US=美东日）。
func dispatchDay(market string, now time.Time) string {
	return now.In(data.Session(market).Loc()).Format("2006-01-02")
}

// sideClass 幂等键里的持仓方向短标（long/short）。
func sideClass(isShort bool) string {
	if isShort {
		return "short"
	}
	return "long"
}

// containsCI 黑名单大小写不敏感包含判定。
func containsCI(list []string, code string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), code) {
			return true
		}
	}
	return false
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// reports 派发器内部快照（Engine.DispatchReports 的取数口；测试直连本方法）。
func (d *binanceDispatcher) reports() map[string]DispatchReport {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]DispatchReport, len(d.lastRepo))
	for k, v := range d.lastRepo {
		out[k] = v
	}
	return out
}

// DispatchReports 观测面快照（server /api/binance/state 的 dispatch 节消费）。
func (e *Engine) DispatchReports() map[string]DispatchReport {
	e.mu.RLock()
	d := e.bnDispatch
	e.mu.RUnlock()
	if d == nil {
		return nil
	}
	return d.reports()
}
