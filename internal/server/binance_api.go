// 文件职责：§BINANCE-P2（PLAN §6.4/§5）币安实盘运维端点——
// GET /api/binance/state（双市场控制器快照 + 回报接收器三条腿健康度）、
// GET /api/binance/orders?market=（市场本地日的在途/当日委托）、
// POST /api/binance/cancel（按市场撤单）、POST /api/binance/halt（币安链 kill-switch 扇出）、
// GET /api/binance/exchange_info?symbol=（交易规则查询面）、POST /api/binance/disclaimer（披露闩人工复位）。
// 权限口径：全部 adminMiddleware（写端点收权普查 M-14 覆盖面）；CN/QMT 链零改动——
// 本文件只经 BrokerRouter 触达 US/CRYPTO 控制器，halted 持久化走 per-user rules.binance。
package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"quant-trading-v2/internal/data"
	"quant-trading-v2/internal/opslog"
	"quant-trading-v2/internal/store"
	"quant-trading-v2/internal/trading"
)

// binanceLive 解析当前账号的币安实盘路由器（可空=引擎未接入币安通道，端点回 503）。
func (s *Server) binanceLive(r *http.Request) (*trading.BrokerRouter, string, bool) {
	ec := s.liveCtrlFor(userIDFor(r))
	if ec == nil {
		return nil, "", false
	}
	lr := ec.LiveRouter()
	if lr == nil {
		return nil, "", false
	}
	return lr, userIDFor(r), true
}

// SetFNGSource §ENH-A1 注入恐慌贪婪指数（FNG）证据闭包（装配层用 data.FNGClient 的
// 缓存面包装而来：只读内存不触网）。nil=未配置，/api/binance/state 响应不含 "fng" 节
// ——零配置零行为；注入后每请求转读一次缓存，ok=false 呈现为 {"ok":false}（无证据
// ≠中性，与 §9 market_halt 同姿势）。
// English: injects the Fear & Greed evidence closure (cache-only read of data.FNGClient).
// nil keeps the "fng" node absent from /api/binance/state entirely (zero-config,
// zero-behavior); ok=false renders as no-evidence, never as neutral.
func (s *Server) SetFNGSource(fn func() (value int, classification string, ageSec int64, ok bool)) {
	s.fngSource = fn
}

// SetXEventsSource §ENH-A4/B8 注入事件血源快照闭包（装配层=engine.Registry.XEventsJSON，
// 内部只读缓存+异步踢刷新）。nil=未注入时 /api/binance/state 不含 "events" 节
// （零配置零行为）；注入后两市场各成一节，未装配腿如实呈现 {"ok":false}。
// English: injects the §ENH-A4/B8 event-leg snapshot closure; nil omits the "events"
// node entirely, markets without an attached leg report ok=false (no evidence, no fabrication).
func (s *Server) SetXEventsSource(fn func(market string) (events []map[string]any, ageSec int64, ok bool)) {
	s.xeventsSource = fn
}

// SetDispatchSource §战法批-5 注入派发摘要闭包（装配层=main.go，按账号读引擎 DispatchReports）。
// nil=未注入时 /api/binance/state 不含 "dispatch" 节（零配置零行为，同 fng/events 惯例）；
// 注入后每市场各成一小节，从未派发过呈 {"ok":false}——无记录≠在派发，绝不渲染假绿灯。
// English: injects the per-account dispatch-report closure; nil omits the "dispatch" node,
// markets with no tick yet report ok=false.
func (s *Server) SetDispatchSource(fn func(userID, market string) (any, bool)) {
	s.dispatchSource = fn
}

// SetQuoteSource §市场分家-1 注入 US/CRYPTO 单票现价闭包（装配层=main.go 按账号读引擎
// 的 feed 快照+REST 回落现价源）。nil=未注入，/api/binance/quote 恒 ok=false（零配置零行为，
// 同 fng/dispatch 惯例）；CN 现价永不走本端点——那条腿只有 /api/stock/lookup 一家。
// English: injects the per-(account, market, code) Binance quote closure; nil keeps the
// endpoint reporting ok=false. The CN quote chain is untouched and stays on stock/lookup.
func (s *Server) SetQuoteSource(fn func(userID, market, code string) (any, bool)) {
	s.quoteSource = fn
}

// handleBinanceQuote GET /api/binance/quote?market=US|CRYPTO&code=：详情抽屉头部现价的
// 非 CN 轨（市场分家：抽屉对美股/加密货币标的不再打 /api/stock/lookup 的 A股四级链）。
// 契约：market 必填且只认 US/CRYPTO（CN→400 两链分轨，与 /api/binance/kline 同姿势）；
// 拿不到有效价（feed 未订阅+REST 失败/未装配）→ 200 {"ok":false}——无证据如实报无，
// 绝不回 0 价伪装、绝不借 CN 数据兜底。authMiddleware（登录态只读展示面，同 kline）。
// English: the non-CN quote leg of the detail drawer. CN is rejected with 400 (track
// separation); a missing quote answers ok=false — never a fabricated zero, never CN data.
func (s *Server) handleBinanceQuote(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	market := data.NormalizeMarketKey(q.Get("market"))
	if market != "US" && market != "CRYPTO" {
		writeError(w, http.StatusBadRequest, "本端点只服务 US/CRYPTO 现价，CN 请走 /api/stock/lookup")
		return
	}
	code := strings.ToUpper(strings.TrimSpace(q.Get("code")))
	if code == "" {
		writeError(w, http.StatusBadRequest, "code 不能为空")
		return
	}
	if s.quoteSource == nil {
		writeJSON(w, 200, map[string]any{"ok": false, "market": market, "code": code,
			"reason": "币安链未装配（无现价源）"})
		return
	}
	view, ok := s.quoteSource(userIDFor(r), market, code)
	if !ok {
		writeJSON(w, 200, map[string]any{"ok": false, "market": market, "code": code,
			"reason": "无现价快照：feed 未订阅该标的且 REST 取价失败"})
		return
	}
	// view 为 engine.BinanceQuoteView 值（自带 json tag）；展平到响应顶层 + ok=true。
	raw, err := json.Marshal(view)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "market": market, "code": code,
			"reason": "现价视图序列化失败"})
		return
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "market": market, "code": code,
			"reason": "现价视图解码失败"})
		return
	}
	out["ok"] = true
	out["market"] = market
	writeJSON(w, 200, out)
}

// handleBinanceState GET /api/binance/state：双市场控制器快照 + 执行器/接收器健康度。
// 前端 BinanceStatusCard 消费；只读、零副作用。
func (s *Server) handleBinanceState(w http.ResponseWriter, r *http.Request) {
	lr, uid, ok := s.binanceLive(r)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "binance live channel not wired")
		return
	}
	cfg := s.cfg.GetBinanceConfigFor(uid)
	snaps := lr.SnapshotAll()
	out := map[string]any{
		"enabled": cfg.Enabled, "mode": cfg.Mode, "testnet": cfg.Testnet,
		"halted":      cfg.Halted,
		"controllers": map[string]any{},
		"reporters":   map[string]any{},
	}
	for m, snap := range snaps {
		if m == "CN" {
			continue // CN 快照走 /api/qmt/state，本端点只讲币安两市场
		}
		entry := map[string]any{"snapshot": snap}
		if exec := lr.BinanceExecutor(m); exec != nil {
			entry["executor"] = true
			if m == "US" {
				entry["disclaimer_unsigned"] = exec.DisclaimerUnsigned()
			}
		} else {
			entry["executor"] = false // Noop（凭证缺失/影子盘）——记账不真下
		}
		out["controllers"].(map[string]any)[m] = entry
	}
	for m, rep := range lr.Reporters() {
		out["reporters"].(map[string]any)[m] = rep.Stats()
	}
	// §P3 feed 观测节：行情双 feed（CRYPTO/US quotes）+ 状态 feed（US tradingStatus）。
	// 未装配（配置缺省/Dial 缺失）时为空数组——前端卡片按"无数据"渲染，绝不缺键崩页。
	out["feeds"] = lr.FeedStats()
	// §ENH-A1 FNG 情绪证据节：仅在装配层注入闭包后出现（nil=响应零变化，前端零特判）。
	// ok=false 只报 ok=false 不造 value——"无证据≠中性"在 JSON 契约层的落点。
	if s.fngSource != nil {
		value, classification, ageSec, ok := s.fngSource()
		node := map[string]any{"ok": ok}
		if ok {
			node["value"] = value
			node["classification"] = classification
			node["age_sec"] = ageSec
		}
		out["fng"] = node
	}
	// §ENH-A4/B8 事件血源节：US=EDGAR 8-K、CRYPTO=CryptoPanic 热帖的最近去重批次。
	// 未装配（凭证空）的市场只有 {"ok":false}——观测面先于派发链（Phase 5 边界），
	// 前端按"无数据"渲染；闭包未注入=整节省略（与 fng 同零配置零行为惯例）。
	if s.xeventsSource != nil {
		node := map[string]any{}
		for _, m := range []string{"US", "CRYPTO"} {
			evs, ageSec, xok := s.xeventsSource(m)
			entry := map[string]any{"ok": xok}
			if xok {
				entry["events"] = evs
				entry["age_sec"] = ageSec
			}
			node[m] = entry
		}
		out["events"] = node
	}
	// §战法批-5 派发摘要节：每市场最近一轮 DispatchReport（universe/signals/placed/rejected/
	// exit_placed/desk/notes）。闭包未注入=整节省略；该市场从未派发过呈 {"ok":false}。
	if s.dispatchSource != nil {
		dnode := map[string]any{}
		for _, m := range []string{"US", "CRYPTO"} {
			rep, ok := s.dispatchSource(uid, m)
			entry := map[string]any{"ok": ok}
			if ok {
				entry["report"] = rep
			}
			dnode[m] = entry
		}
		out["dispatch"] = dnode
	}
	writeJSON(w, 200, out)
}

// handleBinanceOrders GET /api/binance/orders?market=US|CRYPTO：币安市场委托（按市场章过滤，
// 缺 market 时两市场合并）。账本按 market 章过滤——CN 委托绝不出现在此。
func (s *Server) handleBinanceOrders(w http.ResponseWriter, r *http.Request) {
	db := s.realDB()
	if db == nil {
		writeError(w, http.StatusServiceUnavailable, "real book not available")
		return
	}
	uid := userIDFor(r)
	wantMarket := data.NormalizeMarketKey(r.URL.Query().Get("market"))
	if wantMarket == "CN" {
		wantMarket = "" // 缺省=全市场合并
	}
	orders, err := db.RealOrdersForUser(uid)
	if err != nil {
		writeError(w, 500, "read orders: "+err.Error())
		return
	}
	out := make([]store.RealOrder, 0, len(orders))
	for _, o := range orders {
		m := store.NormalizeMarket(o.Market)
		if m == "CN" {
			continue // 币安端点不回 CN 行（QMT 面板负责）
		}
		if wantMarket != "" && m != wantMarket {
			continue
		}
		out = append(out, o)
	}
	writeJSON(w, 200, out)
}

// handleBinanceCancel POST /api/binance/cancel {"market":"US|CRYPTO","order_id":"..."}：
// 按市场路由撤单（BrokerRouter fail-close——未注册市场拒撤，绝不跨市场误撤）。
func (s *Server) handleBinanceCancel(w http.ResponseWriter, r *http.Request) {
	lr, uid, ok := s.binanceLive(r)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "binance live channel not wired")
		return
	}
	var req struct {
		Market  string `json:"market"`
		OrderID string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderID == "" {
		writeError(w, 400, `invalid body: {"market":"US|CRYPTO","order_id":"..."}`)
		return
	}
	market := data.NormalizeMarketKey(req.Market)
	if market != "US" && market != "CRYPTO" {
		writeError(w, 400, "market 只支持 US/CRYPTO（CN 撤单走 /api/qmt/cancel）")
		return
	}
	if err := lr.CancelOrder(market, req.OrderID); err != nil {
		opslog.Audit("binance_cancel", uid, market+":"+req.OrderID, "fail: "+err.Error())
		writeError(w, 502, "cancel failed: "+err.Error())
		return
	}
	opslog.Audit("binance_cancel", uid, market+":"+req.OrderID, "ok")
	writeJSON(w, 200, map[string]any{"ok": "1", "market": market, "order_id": req.OrderID})
}

// handleBinanceHalt POST /api/binance/halt {"halted":true|false}：币安链专用 kill-switch。
// 与 /api/qmt/halt 同语义（即时生效绕过待生效队列 + 撤销在途委托 + SSE/audit/opslog 留痕），
// 但作用域只圈 binance 控制器（SetHaltedBinance 扇出）——紧急停止美股/加密货币不牵连 A 股。
func (s *Server) handleBinanceHalt(w http.ResponseWriter, r *http.Request) {
	lr, uid, ok := s.binanceLive(r)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "binance live channel not wired")
		return
	}
	var req struct {
		Halted *bool `json:"halted"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Halted == nil {
		writeError(w, 400, "invalid request body: 需要 {\"halted\": true|false}")
		return
	}
	cfg := *(s.cfg.GetBinanceConfigFor(uid)) // 值拷贝单字段覆盖，防读到一半被并发改写
	cfg.Halted = *req.Halted
	s.cfg.SetBinanceConfigFor(uid, &cfg) // 持久化（跨重启保留）
	cancelled := lr.SetHaltedBinance(cfg, *req.Halted)
	word := map[bool]string{true: "置位(紧急停止)", false: "解除"}[*req.Halted]
	log.Printf("[binance] ⚠ kill-switch %s (用户=%s): 同步撤销币安在途委托 %d 笔", word, uid, cancelled)
	opslog.Logf("quant", "binance kill-switch %s 用户=%s 撤销未成交委托=%d", word, uid, cancelled)
	result := "clear"
	if *req.Halted {
		result = "set"
	}
	opslog.Audit("kill_switch", uid, "binance:endpoint", result)
	if s.sse != nil {
		s.sse.BroadcastTo(uid, map[string]any{
			"type": "binance_halt", "halted": *req.Halted, "cancelled": cancelled,
			"time": time.Now().Format("15:04:05"),
		})
	}
	writeJSON(w, 200, map[string]any{"ok": "1", "halted": *req.Halted, "cancelled": cancelled})
}

// handleBinanceExchangeInfo GET /api/binance/exchange_info?symbol=BTCUSDT：交易规则查询面
// （步长/最小名义额，执行器 10min 缓存）。查不到回 found=false——前端据此提示而非静默。
func (s *Server) handleBinanceExchangeInfo(w http.ResponseWriter, r *http.Request) {
	lr, _, ok := s.binanceLive(r)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "binance live channel not wired")
		return
	}
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		writeError(w, 400, "缺少 symbol 参数")
		return
	}
	exec := lr.BinanceExecutor("CRYPTO")
	if exec == nil {
		writeError(w, http.StatusServiceUnavailable, "CRYPTO 执行器未装配（凭证缺失/未启用）")
		return
	}
	rules, found := exec.SpotRules(symbol)
	if !found {
		writeJSON(w, 200, map[string]any{"symbol": symbol, "found": false})
		return
	}
	writeJSON(w, 200, map[string]any{
		"symbol": symbol, "found": true,
		"step_size": rules.StepSize, "tick_size": rules.TickSize, "min_notional": rules.MinNotional,
	})
}

// handleBinanceDisclaimerPost POST /api/binance/disclaimer {"confirmed":true}：美股披露声明闩
// 人工复位。这是资金安全闸的解套入口——必须显式 confirmed 且 admin 权限，操作全程 audit；
// 前提是人工已在币安侧完成签署（系统无法代签，端点只解除本地自锁）。
func (s *Server) handleBinanceDisclaimerPost(w http.ResponseWriter, r *http.Request) {
	lr, uid, ok := s.binanceLive(r)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "binance live channel not wired")
		return
	}
	var req struct {
		Confirmed bool `json:"confirmed"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if !req.Confirmed {
		writeError(w, 400, "必须显式 {\"confirmed\": true}：确认已在币安侧完成披露声明签署")
		return
	}
	exec := lr.BinanceExecutor("US")
	if exec == nil {
		writeError(w, http.StatusServiceUnavailable, "US 执行器未装配（凭证缺失/未启用）")
		return
	}
	had := exec.DisclaimerUnsigned()
	exec.ClearDisclaimerUnsigned()
	cfg := *(s.cfg.GetBinanceConfigFor(uid))
	cfg.DisclaimerSignedAt = time.Now().UTC().Format(time.RFC3339)
	s.cfg.SetBinanceConfigFor(uid, &cfg)
	opslog.Logf("quant", "binance 披露声明闩复位 用户=%s 此前自锁=%v", uid, had)
	opslog.Audit("binance_disclaimer", uid, "US", "clear")
	writeJSON(w, 200, map[string]any{"ok": "1", "was_unsigned": had})
}
