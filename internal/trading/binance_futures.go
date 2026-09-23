// 文件职责：§MR-4B USDT 本位永续合约（umfutures）执行腿——BinanceExecutor 的合约分叉实现。
// 分叉唯一判定点是 config 层的 BinanceBrokerView.FuturesActive()（仅 CRYPTO 档案 +
// product_type=umfutures；美股在 validate 层就不可能是合约），binance_executor.go 只在
// place/Cancel/State/healthOnce 四处各插入一个判定点分支，本文件承接全部分叉后的实现。
// 设计口径（docs/PLAN_MARKET_REALITY_20260923.md §4.2）：
//   - 不复用现货端点：下单 POST /fapi/v1/order、撤单 DELETE /fapi/v1/order、
//     对账 GET /fapi/v2/positionRisk（方向/开仓价/强平价一次拿全）、杠杆预设 POST /fapi/v1/leverage、
//     规则缓存 GET /fapi/v1/exchangeInfo（tickSize/stepSize 截断，与现货同类过滤器名）；
//   - 单向持仓模式（one-way）：不下发 positionSide——BUY/SELL 方向即净头寸增减，
//     四侧向经 binanceSide 折叠（卖出开空→SELL、买入平仓→BUY），平仓侧（卖出/买入平仓）
//     附加 reduceOnly=true 保证"只减不加"——即使本地账本与交易所短暂不同步也绝不会
//     把平仓打成反向开仓（合约爆仓风险最高的错单形态）；
//   - 合约无 quoteOrderQty：市价单两侧都按 quantity 下单，按金额语义的调用方必须
//     先换算数量（信号→交易装配层负责，见战法批 #38）；
//   - 杠杆显式预设：profile.leverage≤0 时代码按最保守 1x 设定（发布闸缺省最保守口径），
//     per-symbol 缓存避免每单一次 leverage POST；设定失败 fail-close 拒单——杠杆与本地
//     账本假设不一致时后续强平/资金费换算全失真，宁可不发。
//
// 零回归边界：本文件所有方法只在 FuturesActive()=true 的实例上被调用；现货视图（product_type
// 空）下构造期只多了一个 base 为空的休眠签名器，运行期零触达，/api/v3 链逐字节不变。
//
// English: USDT-margined perpetual (umfutures) leg of BinanceExecutor. The single fork predicate is
// FuturesActive() (CRYPTO profile only); four call sites in binance_executor.go branch here. One-way
// position mode — no positionSide on the wire; closing sides carry reduceOnly=true so a close can never
// flip into an opposite opening. Leverage is explicitly preset (default 1x, fail-close on mismatch);
// reconcile maps positionRisk into real_positions with direction and entry price. Spot bytes untouched.
package trading

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"quant-trading-v2/internal/risk"
	"quant-trading-v2/internal/store"
)

// futuresPosition 单符号的合约持仓快照（/fapi/v2/positionRisk 的最小投影）。
// 字段全为交易所字符串数值（§15.5 精度惯例：解析走 float 仅用于展示/对账，不回写交易所）。
type futuresPosition struct {
	Symbol      string `json:"symbol"`
	PositionAmt string `json:"positionAmt"` // 带符号：>0 多、<0 空、0 无仓
	EntryPrice  string `json:"entryPrice"`  // 开仓均价（现货链拿不到的成本价这里白送）
	MarkPrice   string `json:"markPrice"`   // 标记价（强平距离分母）
	LiqPrice    string `json:"liquidationPrice"`
}

// placeFutures 合约下单主链路（place() 的 FuturesActive 分支）：构参 → 杠杆预设 → fapi 签名 POST。
// 业务性构参错误按现货同款语义返回 OK:false（不烧请求配额）；杠杆设定失败是传输/交易所错误，
// 返回 error 让 controller 标"发送失败"。
func (e *BinanceExecutor) placeFutures(req OrderRequest) (*OrderResult, error) {
	params, symbol, err := e.futuresParams(req)
	if err != nil {
		return &OrderResult{OK: false, Err: err.Error()}, nil
	}
	// 杠杆先于下单设定（fail-close：设定不成功就不发这一单，见文件头口径）。
	if err := e.ensureFuturesLeverage(symbol); err != nil {
		return nil, fmt.Errorf("合约杠杆预设失败（fail-close 未发单）: %w", err)
	}
	return e.signedRequest(e.futures, http.MethodPost, "/fapi/v1/order", params, "CRYPTO")
}

// futuresParams 构建 /fapi/v1/order 参数：
//   - 四侧向经 binanceSide 折叠（买入/买入平仓→BUY，卖出/卖出开空→SELL）；
//   - 平仓侧（卖出、买入平仓）带 reduceOnly=true——单向模式下这是"只平不开"的唯一线上表达；
//   - LIMIT：price+quantity+timeInForce=GTC，按 fapi exchangeInfo 步长截断；
//   - MARKET：两侧一律 quantity（合约没有 quoteOrderQty）。
func (e *BinanceExecutor) futuresParams(req OrderRequest) (url.Values, string, error) {
	symbol := strings.ToUpper(strings.TrimSpace(req.Code))
	if symbol == "" {
		return nil, "", errors.New("合约下单缺少交易对代号")
	}
	if e.futures == nil || e.futures.base == "" {
		return nil, "", errors.New("合约链未配置 fapi 域名（FuturesActive 与凭证不一致，fail-close）")
	}
	side, err := binanceSide(req.Side)
	if err != nil {
		return nil, "", err
	}
	rules, rulesOK := e.FuturesRules(symbol)
	p := url.Values{"symbol": {symbol}, "side": {side}}
	if id := req.SignalID; id != "" {
		p.Set("newClientOrderId", spotClientOrderID(id)) // 与现货同派生：qt+sha1(signal) 截 36
	}
	// 平仓侧标记 reduceOnly：判据是本地侧向（binanceSide 已折叠，这里必须用折叠前的原值）。
	if req.Side == risk.SideSell || req.Side == risk.SideShortCover {
		p.Set("reduceOnly", "true")
	}
	if req.PriceType == "limit" {
		if req.Price <= 0 || req.Qty <= 0 {
			return nil, "", fmt.Errorf("合约限价单缺价格或数量（price=%.8f qty=%.8f）", req.Price, req.Qty)
		}
		qty, price := req.Qty, req.Price
		if rulesOK {
			qty = floorToStep(qty, rules.StepSize)
			price = floorToStep(price, rules.TickSize)
		}
		if qty <= 0 {
			return nil, "", fmt.Errorf("数量 %.8f 小于 %s 最小步长，截断后为 0", req.Qty, symbol)
		}
		p.Set("type", "LIMIT")
		p.Set("timeInForce", "GTC")
		p.Set("quantity", trimNum(qty))
		p.Set("price", trimNum(price))
		return p, symbol, nil
	}
	if req.Qty <= 0 {
		// 合约无按金额市价：给出明确换算指引（装配层负责金额→数量，见战法批接线）。
		return nil, "", fmt.Errorf("合股市价单需要数量 quantity（合约不支持 quoteOrderQty 按金额，收到 amount=%.2f notional=%.2f）", req.Amount, req.Notional)
	}
	qty := req.Qty
	if rulesOK {
		qty = floorToStep(qty, rules.StepSize)
	}
	if qty <= 0 {
		return nil, "", fmt.Errorf("下单数量 %.8f 小于 %s 最小步长", req.Qty, symbol)
	}
	p.Set("type", "MARKET")
	p.Set("quantity", trimNum(qty))
	return p, symbol, nil
}

// ensureFuturesLeverage 把 symbol 的起始杠杆设为档案值（≤0 时按最保守 1x），成功后缓存
// 不再重设（杠杆是账户级持久状态，重设幂等但白烧配额）。缓存与 rules 同用 e.mu。
func (e *BinanceExecutor) ensureFuturesLeverage(symbol string) error {
	want := e.view.BrokerLeverage()
	if want <= 0 {
		want = 1 // 发布闸缺省最保守：未显式配置杠杆=1x，绝不继承交易所 20x 默认
	}
	e.mu.Lock()
	set := e.leverageSet[symbol]
	e.mu.Unlock()
	if set == want {
		return nil
	}
	// POST /fapi/v1/leverage：交易所侧成功即代表后续订单按此杠杆计保证金/强平价。
	_, err := e.rawSigned(e.futures, http.MethodPost, "/fapi/v1/leverage",
		url.Values{"symbol": {symbol}, "leverage": {strconv.Itoa(want)}})
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.leverageSet[symbol] = want
	e.mu.Unlock()
	log.Printf("[binance] 合约杠杆已设定 %s leverage=%d（per-symbol 缓存生效，重设幂等不重发）", symbol, want)
	return nil
}

// FuturesRules fapi exchangeInfo 规则缓存（与 SpotRules 同构：TTL 命中直返、失败 60s 节流、
// 拉取失败退化"交易所侧兜底"——本地预检是优化不是闸）。独立 map：现货步长与合约步长
// 同 symbol 可以不同，绝不能共用缓存条目。
func (e *BinanceExecutor) FuturesRules(symbol string) (risk.SymbolRules, bool) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return risk.SymbolRules{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if r, ok := e.frules[symbol]; ok {
		if time.Since(r.fetchedAt) < binanceRulesTTL {
			return r.SymbolRules, r.MinNotional+r.StepSize+r.TickSize > 0
		}
		if time.Since(r.attemptedAt) < time.Minute { // 失败节流：防每单打 exchangeInfo
			return risk.SymbolRules{}, false
		}
	}
	rules, err := e.fetchFuturesRules(symbol)
	now := time.Now()
	if err != nil {
		e.frules[symbol] = &spotRule{attemptedAt: now}
		return risk.SymbolRules{}, false
	}
	e.frules[symbol] = &spotRule{SymbolRules: rules, fetchedAt: now, attemptedAt: now}
	return rules, true
}

// fetchFuturesRules 拉 GET /fapi/v1/exchangeInfo?symbol=X（公开端点，带 key 无害），
// 裁剪 PRICE_FILTER.tickSize 与 LOT_SIZE.stepSize（合约最小名义额过滤器族形态多变，
// 交给交易所 -4164 兜底，本地不预检）。
func (e *BinanceExecutor) fetchFuturesRules(symbol string) (risk.SymbolRules, error) {
	req, err := http.NewRequest(http.MethodGet,
		e.futures.base+"/fapi/v1/exchangeInfo?symbol="+url.QueryEscape(symbol), nil)
	if err != nil {
		return risk.SymbolRules{}, err
	}
	if e.futures.apiKey != "" {
		req.Header.Set("X-MBX-APIKEY", e.futures.apiKey)
	}
	resp, err := e.futures.httpc.Do(req)
	if err != nil {
		return risk.SymbolRules{}, err
	}
	defer resp.Body.Close()
	// 响应体 1MB 上限（同现货 exchangeInfo 惯例，防异常大响应拖垮装配线程）。
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return risk.SymbolRules{}, fmt.Errorf("fapi exchangeInfo %s: HTTP %d: %s", symbol, resp.StatusCode, truncate(string(body), 160))
	}
	// 响应形状与现货 exchangeInfo 同族（symbols[0].filters），复用裁剪逻辑。
	var info struct {
		Symbols []struct {
			Filters []struct {
				FilterType string `json:"filterType"`
				TickSize   string `json:"tickSize"`
				StepSize   string `json:"stepSize"`
			} `json:"filters"`
		} `json:"symbols"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return risk.SymbolRules{}, fmt.Errorf("fapi exchangeInfo 解析失败: %w", err)
	}
	var r risk.SymbolRules
	if len(info.Symbols) == 0 {
		return risk.SymbolRules{}, fmt.Errorf("fapi exchangeInfo(%s) 无匹配交易对", symbol)
	}
	for _, f := range info.Symbols[0].Filters {
		switch strings.ToUpper(f.FilterType) {
		case "PRICE_FILTER":
			r.TickSize = parseFloatOr(f.TickSize, 0)
		case "LOT_SIZE":
			r.StepSize = parseFloatOr(f.StepSize, 0)
		}
	}
	return r, nil
}

// cancelFutures 合约撤单：与现货同款 "SYMBOL:orderId" 复合单号约定（controller 装配），
// 端点 DELETE /fapi/v1/order。
func (e *BinanceExecutor) cancelFutures(orderID string) error {
	symbol, id, ok := strings.Cut(orderID, ":")
	if !ok {
		return fmt.Errorf("合约撤单需要 SYMBOL:orderId 复合格式（收到 %q）", orderID)
	}
	_, err := e.rawSigned(e.futures, http.MethodDelete, "/fapi/v1/order",
		url.Values{"symbol": {strings.ToUpper(symbol)}, "orderId": {id}})
	return err
}

// stateFutures 合约对账源：positionRisk → real_positions 行。与现货链的三点差异：
//
//   - 方向白送：positionAmt 带符号，<0 落 short 行（§MR-4A side 列的交易所侧对应物）；
//
//   - 成本白送：entryPrice 直接进 CostPrice（现货链 CostPrice=0 靠本地成本合并，这里不覆盖）；
//
//   - 计价币恒 USDT（本位保证金），TsCode 用交易对代号（BTCUSDT，与现货的资产代号 BTC 区分——
//     同账户两条产品线主键 (market,ts_code) 因此天然不碰撞）。
//
//     空持仓（amt=0）跳过；查询失败返回 error（Connected 语义由 controller 的空快照守卫兜底）。
func (e *BinanceExecutor) stateFutures() (*GatewayState, error) {
	positions, err := e.futuresPositions()
	if err != nil {
		return nil, err
	}
	out := make([]store.RealPosition, 0, len(positions))
	for _, fp := range positions {
		amt := parseFloatOr(fp.PositionAmt, 0)
		if amt == 0 {
			continue // 无仓符号不进对账源（与现货 free+locked<=0 跳过同族）
		}
		side := "long"
		if amt < 0 {
			side = "short"
		}
		out = append(out, store.RealPosition{
			Market:    "CRYPTO",
			Currency:  "USDT", // USDT 本位：保证金/盈亏计价币
			Side:      side,
			TsCode:    fp.Symbol,
			Name:      fp.Symbol,
			Qty:       qtyAbs(amt),
			CostPrice: parseFloatOr(fp.EntryPrice, 0),
			UpdatedAt: time.Now().Format("2006-01-02 15:04:05"),
		})
	}
	return &GatewayState{Connected: true, Account: maskKey(e.view.Cfg.APIKey), Positions: out}, nil
}

// qtyAbs 数量取绝对值（空头 positionAmt 为负，账本 qty 恒正——§MR-4A 簿口径）。
func qtyAbs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// futuresPositions 拉 /fapi/v2/positionRisk 全符号持仓（State 与强平证据源共用）。
func (e *BinanceExecutor) futuresPositions() ([]futuresPosition, error) {
	body, err := e.rawSigned(e.futures, http.MethodGet, "/fapi/v2/positionRisk", url.Values{})
	if err != nil {
		return nil, err
	}
	var arr []futuresPosition
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, fmt.Errorf("fapi positionRisk 解析失败: %w", err)
	}
	return arr, nil
}

// LiqEvidence 第 17 闸 liq_distance 的证据源形态：返回该 code 当前空头持仓距强平价的
// 百分比距离是否 ≥ 档案阈值（LiqDistMinPct）。数据门 fail-open 口径（同 crossPrice/haltEvidence
// 惯例的反向利用）：阈值未配（≤0）/无既有仓位（新开仓尚无强平价）/查询失败 → 放行并给
// 出原因；只有"确有仓位且距离跌破阈值"才拦。拦截理由必须带数字（回报面可读）。
func (e *BinanceExecutor) LiqEvidence(market, code string) (bool, string) {
	threshold := e.view.BrokerLiqDistMinPct()
	if threshold <= 0 {
		return true, "liq_dist_min_pct 未配置（0=闸不启用）"
	}
	if market == "" {
		market = e.Market()
	}
	positions, err := e.futuresPositions()
	if err != nil {
		return true, fmt.Sprintf("强平价数据暂不可得（按数据门缺省放行）: %v", err)
	}
	symbol := strings.ToUpper(strings.TrimSpace(code))
	for _, fp := range positions {
		if fp.Symbol != symbol {
			continue
		}
		amt := parseFloatOr(fp.PositionAmt, 0)
		if amt >= 0 {
			return true, "无空头持仓（liq_distance 只约束空头）"
		}
		mark, liq := parseFloatOr(fp.MarkPrice, 0), parseFloatOr(fp.LiqPrice, 0)
		if mark <= 0 || liq <= 0 {
			return true, "标记价/强平价缺失（按数据门缺省放行）"
		}
		dist := (liq - mark) / mark * 100 // 空头强平价恒高于标记价：距离=上涨多少触发强平
		if dist < threshold {
			return false, fmt.Sprintf("空头距强平 %.2f%% < 阈值 %.2f%%（mark=%.2f liq=%.2f）", dist, threshold, mark, liq)
		}
		return true, fmt.Sprintf("距强平 %.2f%% ≥ 阈值 %.2f%%", dist, threshold)
	}
	return true, "该代号无既有仓位（新开空仓尚无强平价，放行交由成交后的下一单约束）"
}
