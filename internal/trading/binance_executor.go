// Package trading — binance_executor.go：币安双市场（US equity / CRYPTO spot）真实下单执行器。
//
// 职责（PLAN_BINANCE_MULTI_ASSET §6.2/§2.2/§2.6）：
//   - 实现 Executor 接口，供 BinanceController（Market="US"/"CRYPTO" 两个视图各一）装配；
//   - 现货链：POST /api/v3/order（LIMIT GTC / MARKET 按金额 quoteOrderQty），撤单 DELETE /api/v3/order，
//     账户 GET /api/v3/account（对账源），规则缓存 GET /api/v3/exchangeInfo（LOT_SIZE/PRICE_FILTER/
//     MIN_NOTIONAL 本地预检 + 步长截断）；
//   - 美股链：POST /sapi/v1/equity/order 四格矩阵（§2.2 BUY+LIMIT/BUY+MARKET/SELL+LIMIT/SELL+MARKET
//     各自的必填/禁填字段集），撤单 /sapi/v1/equity/cancel，受理回执 status 仅 S/F；
//     频率帽 200 次/分钟（UID 滑窗）；486410（未签披露声明）落闩并 Fatal 分类；
//   - 签名统一走 binanceSigner（HMAC-SHA256 精确 query string）；-1021（时间戳出窗）→ 对时后重试一次；
//   - 错误分类查 binance_errors.go（§15.1 双锁表），486449（幂等命中）→ 查询回填单号。
//
// 零回归边界：本文件只被 rebuildExecutorLocked 在 broker="binance" 分支引用，QMT/CN 链路不 import 任何
// 新符号；OrderRequest 的 US 专用字段全部 omitempty，QMT /order 报文逐字节不变。
//
// English: real order executor for Binance US-equity and spot. Implements the same Executor contract as
// QMTClient so the controller stays broker-agnostic. Spot path uses /api/v3/order with exchangeInfo
// pre-checks; the US path enforces the §2.2 four-cell parameter matrix and the 200/min UID rate cap.
// Signing, time-sync (-1021 retry-once) and error classification live in binance_sign.go /
// binance_errors.go. CN/QMT bytes are untouched: this file is only referenced from the binance branch of
// the controller's executor rebuild.
package trading

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quant-trading-v2/internal/config"
	"quant-trading-v2/internal/risk"
	"quant-trading-v2/internal/store"
)

// 编译期锁：币安执行器必须满足 Executor 接口。
var _ Executor = (*BinanceExecutor)(nil)

// binanceRulesTTL exchangeInfo 规则缓存有效期（§2.6 本地预检数据的新鲜度约定）。
const binanceRulesTTL = 10 * time.Minute

// binanceEquityRateLimit 美股下单频率帽：200 次/分钟/UID（PLAN §2.2 硬约束，超发直接 429 封禁）。
const binanceEquityRateLimit = 200

// binanceEquityRateWindow 滑窗长度（UID 计）。
const binanceEquityRateWindow = time.Minute

// BinanceExecutor 币安真实下单执行器（单实例服务一个市场视图：US 或 CRYPTO）。
// 两个视图各建一个实例（各自的熔断独立），共享同一份凭证配置。
// English: one executor per market view; breaker isolation comes from the per-market controller.
type BinanceExecutor struct {
	view  config.BinanceBrokerView // 市场视图（Market=US/CRYPTO + 凭证/参数）
	httpc *http.Client             // 共享 HTTP 客户端（细粒度超时同 QMTClient）

	spot   *binanceSigner // 现货签名器（base = testnet/主网切换由 BaseURL() 裁决）
	equity *binanceSigner // 美股签名器（base 恒主网，无 testnet，PLAN §2.2）
	// futures §MR-4B 合约签名器（fapi 域；base 空=无凭证，FuturesActive 分叉下 fail-close 拒单）。
	futures *binanceSigner

	// rules exchangeInfo 规则缓存（symbol→步长/最小名义额），mu 保护；惰性拉取 + TTL 刷新。
	mu    sync.Mutex
	rules map[string]*spotRule
	// frules/leverageSet §MR-4B 合约侧同类缓存（fapi exchangeInfo + 已设定的杠杆），共用 mu。
	frules      map[string]*spotRule
	leverageSet map[string]int

	// eqWindow 美股下单滑窗（200/min UID 频率帽的本地闸门），超限时本地拒发不发请求。
	eqMu     sync.Mutex
	eqWindow []time.Time

	// disclaimerUnsigned §2.2 披露声明闩：486410 命中后置位——后续美股下单直接本地拒绝
	// （不再消耗请求配额），直到人工在币安签署后重启/配置变更重建实例。
	disclaimerUnsigned atomic.Bool
}

// spotRule 单 symbol 的现货交易规则快照（exchangeInfo 三过滤器裁剪后的最小集）。
type spotRule struct {
	risk.SymbolRules           // StepSize/TickSize/MinNotional
	fetchedAt        time.Time // 拉取时刻（TTL 判定）
	attemptedAt      time.Time // 最近一次尝试（含失败）——失败也节流，防每单打 exchangeInfo
}

// NewBinanceExecutor 按市场视图创建执行器。凭证缺失时仍返回实例（下单时报错），
// 装配判定（是否用真实执行器）在 controller.rebuildExecutorLocked 已做。
// English: builds the executor for one market view; credential presence is the controller's call.
func NewBinanceExecutor(view config.BinanceBrokerView) *BinanceExecutor {
	timeout := time.Duration(view.Cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	httpc := &http.Client{Timeout: timeout, Transport: transport}
	return &BinanceExecutor{
		view:   view,
		httpc:  httpc,
		spot:   newBinanceSigner(view.Cfg.BaseURL(), view.Cfg.APIKey, view.Cfg.APISecret, httpc),
		equity: newBinanceSigner(view.Cfg.EquityBaseURL(), view.Cfg.APIKey, view.Cfg.APISecret, httpc),
		// futures base 由 FuturesBaseURL() 裁决（无凭证=空串）；分叉只在 FuturesActive() 生效，
		// 现货视图即使构造了此签名器也永远不会被用到（零回归锚点见 binance_futures.go 文件头）。
		futures:     newBinanceSigner(view.Cfg.FuturesBaseURL(), view.Cfg.APIKey, view.Cfg.APISecret, httpc),
		rules:       map[string]*spotRule{},
		frules:      map[string]*spotRule{},
		leverageSet: map[string]int{},
	}
}

// View 返回本执行器的市场视图（装配/诊断用）。
func (e *BinanceExecutor) View() config.BinanceBrokerView { return e.view }

// DisclaimerUnsigned 返回披露声明闩状态（true=曾命中 486410，美股链已自锁）。
func (e *BinanceExecutor) DisclaimerUnsigned() bool { return e.disclaimerUnsigned.Load() }

// ClearDisclaimerUnsigned 复位披露声明闩（仅供 /api/binance/disclaimer 端点使用——语义是
// "人工已在币安侧签署声明，解除本地自锁"。除鉴权管理面外不得调用：无条件清闩等于绕过
// §2.2 的资金安全闸）。
// English: release the §2.2 disclaimer latch — authorized admin endpoints only, after a human has
// actually signed the disclosure on Binance.
func (e *BinanceExecutor) ClearDisclaimerUnsigned() {
	if e.disclaimerUnsigned.Swap(false) {
		log.Printf("[binance] 披露声明闩已人工复位（US 链恢复下单资格，操作留痕见 audit）")
	}
}

// Market 归一后的本实例市场（US | CRYPTO）。
func (e *BinanceExecutor) Market() string {
	if e.view.Market == "US" {
		return "US"
	}
	return "CRYPTO"
}

// —— 下单主链路 ————————————————————————————————————————————————

// PlaceBuy 买入下单（按市场分发 spot/equity）。
func (e *BinanceExecutor) PlaceBuy(req OrderRequest) (*OrderResult, error) { return e.place(req) }

// PlaceSell 卖出下单。
func (e *BinanceExecutor) PlaceSell(req OrderRequest) (*OrderResult, error) { return e.place(req) }

// place 统一下单入口：市场守卫（fail-close）→ 按市场构建参数 → 签名 POST → 分类响应。
// 语义对齐 QMTClient.order：传输失败返回 (nil, err)（controller 标"发送失败"）；
// 交易所业务拒单返回 (OrderResult{OK:false, Err:分类信息}, nil)（单号留空，状态由回报纠偏）。
// English: transport failures return a non-nil error (caller marks the ticket send-failed);
// exchange business rejections return a structured OK:false result with the classified message.
func (e *BinanceExecutor) place(req OrderRequest) (*OrderResult, error) {
	market := req.Market
	if market == "" {
		market = e.Market() // 手动入口未带市场：按实例视图归一
	}
	if market == "CN" {
		return nil, fmt.Errorf("binance executor 收到 CN 订单 %s——路由错误，fail-close 拒绝", req.Code)
	}
	if market != e.Market() {
		return nil, fmt.Errorf("binance executor(%s) 收到 %s 市场订单 %s——路由错误，fail-close 拒绝", e.Market(), market, req.Code)
	}
	if market == "US" {
		if e.disclaimerUnsigned.Load() {
			return &OrderResult{OK: false, Err: "美股披露声明未签署（486410 已闩锁）：请先在 Binance 完成 equity disclaimer 签署"}, nil
		}
		if err := e.equityRateGate(); err != nil {
			return nil, err
		}
		params, err := e.equityParams(req)
		if err != nil {
			return &OrderResult{OK: false, Err: err.Error()}, nil
		}
		return e.signedRequest(e.equity, http.MethodPost, "/sapi/v1/equity/order", params, "US")
	}
	// §MR-4B 合约分叉（唯一判定点 view.FuturesActive()=CRYPTO+product_type=umfutures）：
	// fapi 链独立构参与签名，现货路径保持逐字节原样（分叉在判定点之下、判定点之上零改动）。
	if e.view.FuturesActive() {
		return e.placeFutures(req)
	}
	params, err := e.spotParams(req)
	if err != nil {
		return &OrderResult{OK: false, Err: err.Error()}, nil
	}
	return e.signedRequest(e.spot, http.MethodPost, "/api/v3/order", params, "CRYPTO")
}

// spotParams 构建现货 /api/v3/order 参数（§2.6）：
//   - LIMIT：price+quantity+timeInForce=GTC；
//   - MARKET 买：quoteOrderQty=Amount（按金额买）；MARKET 卖：quantity；
//   - quantity 按 LOT_SIZE.stepSize 截断、price 按 PRICE_FILTER.tickSize 截断（规则未知则原样发出，
//     交给交易所 -1013 兜底拒单——本地预检是优化不是闸）；
//   - MIN_NOTIONAL 本地预检：名义额可算且 < 交易所最小名义额 → 直接拒（省一次往返 + 明确提示）。
func (e *BinanceExecutor) spotParams(req OrderRequest) (url.Values, error) {
	symbol := strings.ToUpper(strings.TrimSpace(req.Code))
	if symbol == "" {
		return nil, errors.New("现货下单缺少交易对代号")
	}
	rules, rulesOK := e.SpotRules(symbol)
	side, err := binanceSide(req.Side)
	if err != nil {
		return nil, err
	}
	p := url.Values{
		"symbol": {symbol},
		"side":   {side},
	}
	if id := req.SignalID; id != "" {
		p.Set("newClientOrderId", spotClientOrderID(id))
	}
	// PriceType=limit → LIMIT（带价格）；其余一律 MARKET。
	if req.PriceType == "limit" {
		if req.Price <= 0 || req.Qty <= 0 {
			return nil, fmt.Errorf("现货限价单缺价格或数量（price=%.8f qty=%.8f）", req.Price, req.Qty)
		}
		qty := req.Qty
		price := req.Price
		if rulesOK {
			qty = floorToStep(qty, rules.StepSize)
			price = floorToStep(price, rules.TickSize)
		}
		if qty <= 0 {
			return nil, fmt.Errorf("数量 %.8f 小于交易对 %s 最小步长，截断后为 0", req.Qty, symbol)
		}
		if rulesOK && rules.MinNotional > 0 && qty*price < rules.MinNotional {
			return nil, fmt.Errorf("名义额 %.4f 低于 %s 最小成交额 MIN_NOTIONAL %.4f", qty*price, symbol, rules.MinNotional)
		}
		p.Set("type", "LIMIT")
		p.Set("timeInForce", "GTC")
		p.Set("quantity", trimNum(qty))
		p.Set("price", trimNum(price))
		return p, nil
	}
	if side == "BUY" {
		amt := firstPositive(req.Notional, req.Amount)
		if amt <= 0 {
			return nil, errors.New("现货市价买单缺少金额（quoteOrderQty 需要正数 Amount/Notional）")
		}
		if rulesOK && rules.MinNotional > 0 && amt < rules.MinNotional {
			return nil, fmt.Errorf("买入金额 %.4f 低于 %s 最小成交额 %.4f", amt, symbol, rules.MinNotional)
		}
		p.Set("type", "MARKET")
		p.Set("quoteOrderQty", trimNum(round2(amt)))
		return p, nil
	}
	if req.Qty <= 0 {
		return nil, errors.New("现货市价卖单缺少数量")
	}
	qty := req.Qty
	if rulesOK {
		qty = floorToStep(qty, rules.StepSize)
	}
	if qty <= 0 {
		return nil, fmt.Errorf("卖出数量 %.8f 小于 %s 最小步长", req.Qty, symbol)
	}
	p.Set("type", "MARKET")
	p.Set("quantity", trimNum(qty))
	return p, nil
}

// equityParams 构建美股 /sapi/v1/equity/order 参数——§2.2 四格矩阵的唯一实现点：
//
//	BUY  + LIMIT  = {price, quantity, tradingSession}   禁 notional
//	BUY  + MARKET = {notional}                           禁 price/quantity/tradingSession
//	SELL + LIMIT  = {price, quantity, tradingSession}    禁 notional
//	SELL + MARKET = {quantity}                           禁 price/notional/tradingSession
//
// 附加规则：price ≤2 位小数（486418 族）、timeInForce DAY|GTC 仅 LIMIT、碎股 GTC 必须配
// EXTENDED/24H（486441/486442）——RTH 下自动降级 DAY 并留日志；quoteAsset 缺省 USDC、
// walletType 缺省 CARD（BUY）/CARD（SELL 恒 CARD，§2.2）。clientOrderId 不下发（Q3 未实测，
// 幂等留在本地 signal_id 唯一键）。
func (e *BinanceExecutor) equityParams(req OrderRequest) (url.Values, error) {
	symbol := strings.ToUpper(strings.TrimSpace(req.Code))
	if symbol == "" {
		return nil, errors.New("美股下单缺少股票代码")
	}
	side, err := binanceSide(req.Side)
	if err != nil {
		return nil, err
	}
	profile := e.view.Cfg.Stock
	p := url.Values{
		"symbol":     {symbol},
		"side":       {side},
		"quoteAsset": {firstNonEmpty(req.QuoteAsset, e.view.Cfg.QuoteAsset, "USDC")},
	}
	limit := req.PriceType == "limit"
	if limit {
		if req.Price <= 0 || req.Qty <= 0 {
			return nil, fmt.Errorf("美股限价单缺价格或数量（price=%.4f qty=%.4f）", req.Price, req.Qty)
		}
		price := round2(req.Price)
		// §2.2 硬约束：price 最多 2 位小数（486418 族）——超精度直接本地拒，不烧请求配额。
		if d := req.Price*100 - math.Round(req.Price*100); d > 1e-6 || d < -1e-6 {
			return nil, fmt.Errorf("美股限价 %.8f 超过 2 位小数精度", req.Price)
		}
		session := firstNonEmpty(req.TradingSession, profile.TradingSession, "RTH")
		switch session {
		case "RTH", "EXTENDED", "24H":
		default:
			return nil, fmt.Errorf("美股 tradingSession 仅允许 RTH/EXTENDED/24H（实际 %q）", session)
		}
		tif := firstNonEmpty(req.TimeInForce, profile.TimeInForce, "DAY")
		if tif != "DAY" && tif != "GTC" {
			return nil, fmt.Errorf("美股 timeInForce 仅允许 DAY/GTC（实际 %q）", tif)
		}
		// 碎股 GTC 必须配 EXTENDED/24H（486441/486442 族）：RTH 下自动降为 DAY，语义等价且不拒用户。
		if tif == "GTC" && req.Qty != float64(int64(req.Qty)) && session == "RTH" {
			log.Printf("[binance] §2.2 碎股 GTC+RTH 非法组合，自动降级 DAY（%s qty=%.6f）", symbol, req.Qty)
			tif = "DAY"
		}
		p.Set("type", "LIMIT")
		p.Set("price", trimNum(price))
		p.Set("quantity", trimNum(req.Qty))
		p.Set("tradingSession", session)
		p.Set("timeInForce", tif)
		return p, nil
	}
	if side == "BUY" {
		amt := firstPositive(req.Notional, req.Amount)
		if amt <= 0 {
			return nil, errors.New("美股市价买单缺少金额（notional 需要正数）")
		}
		p.Set("type", "MARKET")
		p.Set("notional", trimNum(round2(amt)))
		p.Set("walletType", "CARD")
		return p, nil
	}
	if req.Qty <= 0 {
		return nil, errors.New("美股市价卖单缺少数量")
	}
	p.Set("type", "MARKET")
	p.Set("quantity", trimNum(req.Qty))
	p.Set("walletType", "CARD") // §2.2 SELL 恒 CARD
	return p, nil
}

// —— 签名请求与响应分类 ————————————————————————————————————————

// signedRequest 发一次签名下单请求；-1021（时间戳出窗）→ 对时后原地重试一次
// （§15.1 中最特殊的 Retryable：不重试会白拒单，重试前必须 syncTimeOffset 否则必再失败）。
func (e *BinanceExecutor) signedRequest(s *binanceSigner, method, path string, params url.Values, market string) (*OrderResult, error) {
	res, err := e.doSigned(s, method, path, params)
	if err == nil {
		return res, nil
	}
	// 传输层错误（非交易所分类）：不盲重发（下单非幂等，§15.7 5XX=状态未知禁盲重发同族语义）。
	var re *binanceRejected
	if !errors.As(err, &re) {
		return nil, err
	}
	if re.code == -1021 {
		// 现货时间戳错误：对时一次后重发（signQuery 会刷新 timestamp）。
		if syncErr := s.syncTimeOffset(e.spot.base); syncErr != nil {
			log.Printf("[binance] -1021 后对时失败: %v", syncErr)
		}
		res2, err2 := e.doSigned(s, method, path, params)
		if err2 == nil {
			return res2, nil
		}
		var re2 *binanceRejected
		if errors.As(err2, &re2) {
			return e.classifyRejected(re2, market), nil
		}
		return nil, err2
	}
	return e.classifyRejected(re, market), nil
}

// binanceRejected 交易所业务拒单（HTTP 4xx/5xx 带 {"code","msg"}）的分类错误载体。
type binanceRejected struct {
	httpStatus int
	code       int
	msg        string
}

func (r *binanceRejected) Error() string {
	return fmt.Sprintf("binance 拒单 code=%d msg=%s (HTTP %d)", r.code, r.msg, r.httpStatus)
}

// doSigned 执行签名请求并解析下单响应；业务拒单以 *binanceRejected 返回。
// 响应 orderId 兼容 int64 数字（现货 §15.5 UseNumber）与字符串（equity）。
func (e *BinanceExecutor) doSigned(s *binanceSigner, method, path string, params url.Values) (*OrderResult, error) {
	body, err := e.rawSigned(s, method, path, params)
	if err != nil {
		return nil, err
	}
	// §15.5 锁：orderId 是 JSON 数字（int64），Decoder 必须 UseNumber——float64 精度不够。
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var out struct {
		OrderID       any    `json:"orderId"`
		ClientOrderID string `json:"clientOrderId"`
		Status        string `json:"status"`
	}
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("binance %s 响应解析失败: %w: %s", path, err, truncate(string(body), 200))
	}
	orderID := firstNonEmpty(jsonString(out.OrderID), out.ClientOrderID)
	return &OrderResult{OK: true, OrderID: orderID}, nil
}

// rawSigned 发签名请求并返回响应体；非 2xx 解析 {"code","msg"} 为 *binanceRejected。
func (e *BinanceExecutor) rawSigned(s *binanceSigner, method, path string, params url.Values) ([]byte, error) {
	q := s.signQuery(params)
	httpReq, err := http.NewRequest(method, s.base+path+"?"+q, strings.NewReader(""))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("X-MBX-APIKEY", s.apiKey)
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("binance %s %s 传输失败: %w", method, path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		var rej struct {
			Code any    `json:"code"` // 现货 -1021 族为负数 / equity 486xxx 族
			Msg  string `json:"msg"`
		}
		_ = json.Unmarshal(body, &rej) // 解析失败则 code=0，仍按拒单处理
		return nil, &binanceRejected{httpStatus: resp.StatusCode, code: jsonInt(rej.Code), msg: rej.Msg}
	}
	return body, nil
}

// classifyRejected 把交易所拒单翻译成 OrderResult：486410 落闩；486449 幂等命中→查单回填；
// 其余按 §15.1 分类表拼提示语。全部返回 OK:false、error 留空（业务拒单不是链路故障）。
func (e *BinanceExecutor) classifyRejected(re *binanceRejected, market string) *OrderResult {
	if re.code == 486410 {
		e.disclaimerUnsigned.Store(true)
		return &OrderResult{OK: false, Err: binanceErrMessage(re.code, re.msg) + "——美股链已自锁，签署披露声明后重建执行器"}
	}
	if re.code == 486449 && market == "US" {
		// 幂等命中：委托可能已存在，查在途委托取最近一笔回填（Q3 clientOrderId 未实测，
		// 查不到仍报幂等冲突交人工）。
		if id, ok := e.recentEquityOrderID(); ok {
			return &OrderResult{OK: true, OrderID: id}
		}
	}
	_ = classifyBinanceError(re.code) // 分类表命中审计锚点：未知码走 binanceErrUnknown 兜底
	return &OrderResult{OK: false, Err: binanceErrMessage(re.code, re.msg)}
}

// recentEquityOrderID 486449 回填路径：拉美股在途委托取最近一笔单号。
func (e *BinanceExecutor) recentEquityOrderID() (string, bool) {
	orders, err := e.equityOpenOrders()
	if err != nil || len(orders) == 0 {
		return "", false
	}
	return orders[len(orders)-1].orderID, true
}

// —— 撤单 / 状态 / 健康 ————————————————————————————————————————

// Cancel 撤单。执行器按市场实例化，路由由实例决定：
//   - US：纯数字 → orderId；否则 clientOrderId（POST /sapi/v1/equity/cancel）；
//   - CRYPTO：DELETE /api/v3/order 需要 symbol+orderId，单号约定 "SYMBOL:orderId" 复合格式
//     （controller.CancelOrder 在调用前装配；裸数字无法反查 symbol 时拒撤并留提示）。
func (e *BinanceExecutor) Cancel(orderID string) error {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return errors.New("撤单缺少单号")
	}
	if e.Market() == "US" {
		params := url.Values{}
		if _, err := strconv.ParseInt(orderID, 10, 64); err == nil {
			params.Set("orderId", orderID)
		} else {
			params.Set("clientOrderId", orderID)
		}
		_, err := e.rawSigned(e.equity, http.MethodPost, "/sapi/v1/equity/cancel", params)
		return err
	}
	// §MR-4B 合约撤单分叉：同 "SYMBOL:orderId" 约定，端点换 fapi（binance_futures.go）。
	if e.view.FuturesActive() {
		return e.cancelFutures(orderID)
	}
	symbol, id, ok := strings.Cut(orderID, ":")
	if !ok {
		return fmt.Errorf("现货撤单需要 SYMBOL:orderId 复合格式（收到 %q）", orderID)
	}
	_, err := e.rawSigned(e.spot, http.MethodDelete, "/api/v3/order",
		url.Values{"symbol": {strings.ToUpper(symbol)}, "orderId": {id}})
	return err
}

// State 对账源快照。
//   - CRYPTO：GET /api/v3/account 余额 → 持仓行（free+locked>0 的资产；Currency=资产代号，
//     CostPrice=0——币安不提供成本，Controller.Reconcile 前置合并本地成本价）。
//     Connected 以账户查询成功为准。
//   - US：positions 权威端点未实测（Phase 0 Q4）——恒返回 Connected:false + 空持仓，
//     Controller.Reconcile 的「未连接且空快照禁止清账」守卫因此永远跳过 US 对账，
//     绝不会把本地美股账本清空。Q4 实链后再接入。
//
// English: spot account balances become the reconcile source (cost basis merged locally first);
// the US path reports Connected=false until the positions endpoint is verified (Q4), so reconcile
// can never wipe the book from an empty snapshot.
func (e *BinanceExecutor) State() (*GatewayState, error) {
	if e.Market() == "US" {
		return &GatewayState{Connected: false, Account: maskKey(e.view.Cfg.APIKey)}, nil
	}
	// §MR-4B 合约对账分叉：positionRisk 直给方向/开仓价（现货余额链没有的信息），
	// 空快照禁止清账守卫沿用 Controller 侧同款语义（详见 stateFutures）。
	if e.view.FuturesActive() {
		return e.stateFutures()
	}
	body, err := e.rawSigned(e.spot, http.MethodGet, "/api/v3/account", url.Values{})
	if err != nil {
		return nil, err
	}
	var acc struct {
		Balances []struct {
			Asset  string `json:"asset"`
			Free   string `json:"free"`
			Locked string `json:"locked"`
		} `json:"balances"`
	}
	if err := json.Unmarshal(body, &acc); err != nil {
		return nil, fmt.Errorf("现货账户响应解析失败: %w", err)
	}
	positions := make([]store.RealPosition, 0, len(acc.Balances))
	for _, b := range acc.Balances {
		free, _ := strconv.ParseFloat(b.Free, 64)
		locked, _ := strconv.ParseFloat(b.Locked, 64)
		qty := free + locked
		if qty <= 0 {
			continue
		}
		positions = append(positions, store.RealPosition{
			Market:    "CRYPTO",
			Currency:  b.Asset,
			TsCode:    b.Asset, // 现货余额以资产代号为持仓键（BTC，而非 BTCUSDT 交易对）
			Qty:       qty,
			CostPrice: 0, // 交易所不提供成本价——Controller.Reconcile 合并本地成本
			UpdatedAt: time.Now().Format("2006-01-02 15:04:05"),
		})
	}
	return &GatewayState{Connected: true, Account: maskKey(e.view.Cfg.APIKey), Positions: positions}, nil
}

// Health 探测：CRYPTO 走签名 /api/v3/account；US 走在途委托查询（最轻的签名读端点）。
// 失败自动重探一次（跨网抖动缓冲，同 QMTClient.Health 惯例）。
func (e *BinanceExecutor) Health() (bool, error) {
	ok, err := e.healthOnce()
	if err != nil {
		time.Sleep(400 * time.Millisecond)
		return e.healthOnce()
	}
	return ok, nil
}

func (e *BinanceExecutor) healthOnce() (bool, error) {
	if e.Market() == "US" {
		_, err := e.equityOpenOrders()
		return err == nil, err
	}
	// §MR-4B 健康探测分叉：合约走在 /fapi/v1/account（最轻的签名读端点）。
	if e.view.FuturesActive() {
		_, err := e.rawSigned(e.futures, http.MethodGet, "/fapi/v1/account", url.Values{})
		return err == nil, err
	}
	_, err := e.rawSigned(e.spot, http.MethodGet, "/api/v3/account", url.Values{})
	return err == nil, err
}

// equityOpenOrder 美股在途委托的最小投影（health 探测 + 486449 回填共用）。
type equityOpenOrder struct {
	orderID string
}

// equityOpenOrders 拉美股在途委托（查询面；端点属 Q 未实测项，失败仅影响回填/探测路径）。
func (e *BinanceExecutor) equityOpenOrders() ([]equityOpenOrder, error) {
	body, err := e.rawSigned(e.equity, http.MethodGet, "/sapi/v1/equity/openOrders", url.Values{})
	if err != nil {
		return nil, err
	}
	var arr []struct {
		OrderID       any    `json:"orderId"`
		ClientOrderID string `json:"clientOrderId"`
	}
	if err := json.Unmarshal(body, &arr); err != nil {
		// 对象形响应（单数端点差异）兜底：按单笔解析。
		var one struct {
			OrderID       any    `json:"orderId"`
			ClientOrderID string `json:"clientOrderId"`
		}
		if err2 := json.Unmarshal(body, &one); err2 != nil {
			return nil, fmt.Errorf("美股在途委托解析失败: %w", err)
		}
		arr = append(arr, one)
	}
	out := make([]equityOpenOrder, 0, len(arr))
	for _, o := range arr {
		out = append(out, equityOpenOrder{orderID: firstNonEmpty(jsonString(o.OrderID), o.ClientOrderID)})
	}
	return out, nil
}

// —— exchangeInfo 规则缓存（现货本地预检数据源）————————————————————————

// SpotRules 返回交易对的规则快照（缓存命中且未过期直接返回；否则惰性拉取 exchangeInfo 单 symbol）。
// 拉取失败返回 ok=false——调用方（spotParams）退化为"交易所侧兜底"，风控闸侧 fail-open。
// English: lazy per-symbol rules cache with TTL; failures degrade to exchange-side enforcement.
func (e *BinanceExecutor) SpotRules(symbol string) (risk.SymbolRules, bool) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return risk.SymbolRules{}, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if r, ok := e.rules[symbol]; ok {
		if time.Since(r.fetchedAt) < binanceRulesTTL {
			return r.SymbolRules, r.MinNotional+r.StepSize+r.TickSize > 0
		}
		if time.Since(r.attemptedAt) < time.Minute { // 失败节流：60s 内不重拉，防每单打 exchangeInfo
			return risk.SymbolRules{}, false
		}
	}
	rules, err := e.fetchSpotRules(symbol)
	now := time.Now()
	if err != nil {
		e.rules[symbol] = &spotRule{attemptedAt: now}
		log.Printf("[binance] exchangeInfo(%s) 拉取失败，本地预检降级: %v", symbol, err)
		return risk.SymbolRules{}, false
	}
	e.rules[symbol] = &spotRule{SymbolRules: rules, fetchedAt: now, attemptedAt: now}
	return rules, true
}

// fetchSpotRules 拉取 /api/v3/exchangeInfo?symbol=X 并裁剪三过滤器（LOT_SIZE/PRICE_FILTER/
// MIN_NOTIONAL，新版交易对的 NOTIONAL 同键归并）。
func (e *BinanceExecutor) fetchSpotRules(symbol string) (risk.SymbolRules, error) {
	req, err := http.NewRequest(http.MethodGet,
		e.spot.base+"/api/v3/exchangeInfo?symbol="+url.QueryEscape(symbol), nil)
	if err != nil {
		return risk.SymbolRules{}, err
	}
	if e.spot.apiKey != "" {
		req.Header.Set("X-MBX-APIKEY", e.spot.apiKey) // testnet 要求带 key；主网匿名可访
	}
	resp, err := e.spot.httpc.Do(req)
	if err != nil {
		return risk.SymbolRules{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return risk.SymbolRules{}, fmt.Errorf("exchangeInfo %s: HTTP %d: %s", symbol, resp.StatusCode, truncate(string(body), 160))
	}
	var info struct {
		Filters []struct {
			FilterType  string `json:"filterType"`
			TickSize    string `json:"tickSize"`
			MinNotional string `json:"minNotional"`
			Notional    string `json:"notional"`
			StepSize    string `json:"stepSize"`
		} `json:"filters"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return risk.SymbolRules{}, fmt.Errorf("exchangeInfo 解析失败: %w", err)
	}
	var r risk.SymbolRules
	for _, f := range info.Filters {
		switch strings.ToUpper(f.FilterType) {
		case "PRICE_FILTER":
			r.TickSize = parseFloatOr(f.TickSize, 0)
		case "LOT_SIZE":
			r.StepSize = parseFloatOr(f.StepSize, 0)
		case "MIN_NOTIONAL":
			r.MinNotional = parseFloatOr(f.MinNotional, 0)
		case "NOTIONAL": // 新版现货交易对用 NOTIONAL.minNotional
			if v := parseFloatOr(f.MinNotional, 0); v > r.MinNotional {
				r.MinNotional = v
			}
		}
	}
	return r, nil
}

// —— 工具函数 ————————————————————————————————————————————————

// spotClientOrderID 现货幂等键：newClientOrderId = "qt" + sha1(signal_id) hex 前 34 字符（总长 36 ≤ 上限）。
// English: deterministic spot client order id derived from the local signal id (§2.6, 36-char cap).
func spotClientOrderID(signalID string) string {
	sum := sha1.Sum([]byte(signalID))
	return "qt" + hex.EncodeToString(sum[:])[:34]
}

// binanceSide 中文方向 → Binance BUY/SELL。
// §MR-4A：币安现货链上没有独立的"借券"动作——开空在交易所侧就是一笔真实 SELL、平空是一笔 BUY，
// 空头语义由账本侧承载（占位委托行存真实方向 + ApplyRealFill 按方向落空头行）。本映射把空头
// 两个中文方向折叠到同名 REST 值；请求的真实方向沿 SignalID→委托行链路保留，不在此处丢失。
// English: §MR-4A — on the wire a spot short-open IS a SELL and a cover IS a BUY; short semantics
// live in the ledger (placeholder order row keeps the true side), so this map folds the short sides
// onto the same REST values without losing the request's real direction upstream.
func binanceSide(side string) (string, error) {
	switch side {
	case SideBuy, SideShortCover, "buy", "BUY":
		return "BUY", nil
	case SideSell, SideShortOpen, "sell", "SELL":
		return "SELL", nil
	}
	return "", fmt.Errorf("未知下单方向 %q", side)
}

// equityRateGate 美股 200/min UID 滑窗本地闸：超限返回错误（不发请求），窗口内记一次发出。
func (e *BinanceExecutor) equityRateGate() error {
	e.eqMu.Lock()
	defer e.eqMu.Unlock()
	now := time.Now()
	cut := now.Add(-binanceEquityRateWindow)
	kept := e.eqWindow[:0]
	for _, t := range e.eqWindow {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= binanceEquityRateLimit {
		e.eqWindow = kept
		earliest := kept[0]
		for _, t := range kept[1:] {
			if t.Before(earliest) {
				earliest = t
			}
		}
		wait := binanceEquityRateWindow - now.Sub(earliest)
		return fmt.Errorf("美股下单频率达到 200 次/分钟上限，约 %ds 后重试", int(wait.Seconds())+1)
	}
	e.eqWindow = append(kept, now)
	return nil
}

// floorToStep 数量/价格按交易所步长向下截断（stepSize/tickSize ≤0 时原样返回）。
func floorToStep(v, step float64) float64 {
	if step <= 0 || v <= 0 {
		return v
	}
	steps := int64(v/step + 1e-9)
	return float64(steps) * step
}

// trimNum 去掉浮点尾零输出交易所友好数字（0.00100000 → "0.001"；避免科学计数）。
func trimNum(v float64) string {
	s := strconv.FormatFloat(v, 'f', 8, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return "0"
	}
	return s
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// firstNonEmpty 返回首个去空白后非空的字符串（全空返回空串），供"多源字段取首个可用值"场景。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// firstPositive 返回首个严格大于 0 的数值（全非正返回 0），供交易所回写字段的"缺数回落"链。
func firstPositive(vals ...float64) float64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

func parseFloatOr(s string, def float64) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}

// jsonInt any（json.Number/float64/string）→ int 错误码。
func jsonInt(v any) int {
	switch t := v.(type) {
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}

// jsonString any（json.Number/string）→ 单号字符串（§15.5 int64 精度路径）。
func jsonString(v any) string {
	switch t := v.(type) {
	case json.Number:
		return t.String()
	case string:
		return t
	}
	return ""
}
