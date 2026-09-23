// 文件职责：§战法批（2026-09-23）币安链纸面成交柜台——无 API 密钥即可把
// 「信号→风控→下单→账簿」整链跑通的模拟执行器（PLAN_MARKET_REALITY §4.1-M1/M2 的
// 派发批实装件）。实现 trading.Executor 接口，与 BinanceExecutor 在装配层同位替换：
// 交易面未激活（无钥匙/总开关关）且 profile.paper=true 时由 registry 注入。
//
// 语义纪律（与真链同口径，绝不另起一本账）：
//  1. 即时成交：受理即按成交价（req.Price，缺价取行情源快照，两处都拿不到=拒单）
//     一次性写 已成 委托 + 成交腿——复用 QMT/币安真链同一套权威写口
//     ApplyRealFill / ApplyOrderReportTx（秩守卫、方向矩阵、判重锚全部原样生效）；
//  2. 资金池：内存现金 + 启动回放 fills 重建，回放用与会话内**同一套公式**（影子开空
//     均价按比例返还+盈亏入账），进程重启不会重置资金，也不会出现两套口径漂移；
//  3. 开空冻结保证金：cash -= amount×marginRate（率取档案 short_margin_rate，
//     0=保守按全额冻结）；平空按持仓行返还冻结本金 + 空单盈亏（含费均价基准，
//     费用开/平各扣一次，与真链双边收费同形）；
//  4. fail-close：市场章不符、方向与方法不匹配、价/量不可得、现金不足、无仓可平、
//     超量平仓一律业务拒单（OK:false），绝不伪造成交；
//  5. 结构上无网络腿：本文件不 import 任何 HTTP 客户端——纸面盘永远碰不到交易所。
//
// English: credential-free paper desk implementing the same Executor interface as the live
// Binance path. Fills apply instantly through the same authoritative store ports; cash is
// rebuilt from the fills ledger with the identical formula used at runtime; every refusal is
// fail-close; there is no network leg — a paper desk structurally cannot reach the exchange.
package trading

import (
	"crypto/rand"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"quant-trading-v2/internal/data"
	"quant-trading-v2/internal/store"
)

// BinancePaperOptions 纸面柜台构造参数。DB/UserID/Market 必填；其余零值走缺省。
type BinancePaperOptions struct {
	DB          *store.DB
	UserID      string
	Market      string  // US | CRYPTO（一个柜台只服务一条市场腿，与执行器分腿惯例一致）
	InitialCash float64 // 初始资金（0=缺省 100000，本市场计价币计）
	FeeRate     float64 // 单边费率（0=缺省 0.001；合约档装配侧给 0.0005）
	// ShortMarginRate 开空冻结比例（0=保守按 1.0 全额冻结——纸面宁可少给火力也不放大隐性杠杆）
	ShortMarginRate float64
	GetPrice        func(code string) float64 // 市价单参考价来源（可空=只认 req.Price）
	Now             func() time.Time
	OnAlert         func(level, title, content string) // 预留告警通道（柜台只到 info 级，拒单不告警）
}

// paperShortLot 空头影子台账（单 code）：累计冻结保证金 + 开空均价与数量的量价对，
// 供平仓按比例返还与盈亏结算；会话内与启动回放共用同一推进公式（口径不漂移的根证）。
// 均价用**未含费**口径：开仓费在成交当刻已从现金扣走、平仓费平仓时再扣一次，
// 盈亏基准若再取含费均价等于把开仓费退给客户——现金池双边收费才是足式的。
// （raw average: the open fee already left cash at open-time; baking it into the PnL base
// would refund it at cover, so the pool would only ever charge one side.）
type paperShortLot struct {
	frozen float64 // 剩余冻结保证金（本市场计价币）
	qty    float64 // 剩余空头数量
	avgPx  float64 // 开空加权均价（未含费）
}

// BinancePaperExecutor 币安链纸面柜台（四方向：买入/卖出/卖出开空/买入平仓）。
type BinancePaperExecutor struct {
	opt BinancePaperOptions

	mu           sync.Mutex
	cash         float64
	shorts       map[string]*paperShortLot // code→空头影子台账
	seq          int64
	salt         string // 实例盐：orderID 跨柜台实例/跨市场/跨重启全局唯一（见 place）
	bootstrapped bool
}

// NewBinancePaperExecutor 构造纸面柜台。Market 归一后必须是 US/CRYPTO（CN 链自有 paper 引擎，
// 本柜台不参与——与 xasset 构造期市场收口同姿势）。
func NewBinancePaperExecutor(opt BinancePaperOptions) (*BinancePaperExecutor, error) {
	if opt.DB == nil || opt.UserID == "" {
		return nil, fmt.Errorf("binance paper: DB/UserID 必填")
	}
	m := data.NormalizeMarketKey(opt.Market)
	if m != "US" && m != "CRYPTO" {
		return nil, fmt.Errorf("binance paper: Market 只支持 US/CRYPTO，收到 %q", opt.Market)
	}
	opt.Market = m
	if opt.InitialCash <= 0 {
		opt.InitialCash = 100000
	}
	if opt.FeeRate <= 0 {
		opt.FeeRate = 0.001
	}
	if opt.ShortMarginRate <= 0 {
		opt.ShortMarginRate = 1.0 // 保守缺省：开空按全额冻结（真保证金率由档案显式给出）
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	// 实例盐：同秒构造的多腿柜台（US+CRYPTO 双盘）与重启后的新实例共享 fills 判重锚
	// （TradeID="paper:"+orderID+":1"），纯 seq+unix 会撞车、撞车即成交被幂等静默吞掉
	// （单测实测复现）。4 字节随机盐保证 orderID 全局唯一。
	var sb [4]byte
	if _, err := rand.Read(sb[:]); err != nil {
		return nil, fmt.Errorf("binance paper: 实例盐生成失败: %w", err)
	}
	return &BinancePaperExecutor{opt: opt, cash: opt.InitialCash, shorts: map[string]*paperShortLot{}, salt: fmt.Sprintf("%x", sb[:])}, nil
}

// applyCashEventLocked 单笔成交对现金池与空头影子台账的推进（会话内与启动回放的唯一
// 共用公式——两处若各写各的，重启就造出第二个资金口径，这是纸面资金最容易烂掉的地方）。
func (e *BinancePaperExecutor) applyCashEventLocked(code, side string, price, qty, fee float64) {
	switch side {
	case SideBuy:
		e.cash -= qty*price + fee
	case SideSell:
		e.cash += qty*price - fee
	case SideShortOpen:
		freeze := qty * price * e.opt.ShortMarginRate
		e.cash -= freeze + fee
		lot := e.shorts[code]
		if lot == nil {
			lot = &paperShortLot{}
			e.shorts[code] = lot
		}
		newQty := lot.qty + qty
		if newQty > 0 {
			lot.avgPx = (lot.avgPx*lot.qty + qty*price) / newQty
		}
		lot.qty = newQty
		lot.frozen += freeze
	case SideShortCover:
		lot := e.shorts[code]
		if lot == nil || lot.qty <= 0 {
			// 影子簿里没有这个空头=回放起点缺账（理论上 ApplyRealFill 已拦下平空无行，
			// 走到这里只剩脏数据）：只扣费、绝不凭空返还本金——宁可少还不注水。
			e.cash -= fee
			return
		}
		cq := qty
		if cq > lot.qty {
			cq = lot.qty
		}
		back := lot.frozen * cq / lot.qty
		e.cash += back + (lot.avgPx-price)*cq - fee
		lot.frozen -= back
		if lot.frozen < 0 {
			lot.frozen = 0
		}
		lot.qty -= cq
	}
}

// ensureBootstrapLocked 首轮使用前回放本市场 fills 重建现金与空头影子台账（公式与会话内
// 同源，见 applyCashEventLocked）。读库失败降级为"初始资金起算"+日志——纸面语义，
// 绝不因重建失败拒掉全部成交。
func (e *BinancePaperExecutor) ensureBootstrapLocked() {
	if e.bootstrapped {
		return
	}
	e.bootstrapped = true
	fills, err := e.opt.DB.RealFills()
	if err != nil {
		log.Printf("[binance-paper] 成交回放失败（现金按初始值起算）: %v", err)
		return
	}
	for _, f := range fills {
		if data.NormalizeMarketKey(f.Market) != e.opt.Market || (f.UserID != "" && f.UserID != e.opt.UserID) {
			continue // 跨市场/跨账号行不改写本柜台资金（市场章+账号双过滤）
		}
		if f.Qty <= 0 || f.Price <= 0 {
			continue // 脏行（无量/无价）不进资金推进，宁可少动账
		}
		e.applyCashEventLocked(f.Code, f.Side, f.Price, f.Qty, f.Fee)
	}
	e.syncAccountLocked()
}

// syncAccountLocked 把柜台资金池映射到 real_account 市场行（available=现金、frozen=空头冻结合计）。
// 为什么必须写：集中度闸（checkConcentration→TotalAssetsForMarket）的总资产分母=该账户行现金+
// 持仓市值，QMT 链靠网关上报、纸面盘没有上报方——不回填则首笔建仓后分母只剩持仓市值本身，
// 第二笔新开仓必然 99%+ 集中度误拒（本批实测）。柜台即纸面盘的"网关"，成交后如实反映权益。
// 调用方必须已持 e.mu；回写失败只留日志，绝不翻转已落账的成交结果。
func (e *BinancePaperExecutor) syncAccountLocked() {
	frozen := 0.0
	for _, lot := range e.shorts {
		frozen += lot.frozen
	}
	if err := e.opt.DB.UpsertRealAccount(store.RealAccount{
		UserID: e.opt.UserID, Market: e.opt.Market,
		AvailableCash: e.cash, FrozenCash: frozen,
	}); err != nil {
		e.info("账户资产回写失败（集中度分母将滞留旧值）: %v", err)
	}
}

// PlaceBuy 买入腿（买入 / 买入平仓）。req.Side 缺席时按方法语义补齐。
func (e *BinancePaperExecutor) PlaceBuy(req OrderRequest) (*OrderResult, error) {
	if req.Side == "" {
		req.Side = SideBuy
	}
	if req.Side != SideBuy && req.Side != SideShortCover {
		return &OrderResult{OK: false, Err: fmt.Sprintf("binance paper: PlaceBuy 只接受 %s/%s（收到 %q）", SideBuy, SideShortCover, req.Side)}, nil
	}
	return e.place(req)
}

// PlaceSell 卖出腿（卖出平多 / 卖出开空）。
func (e *BinancePaperExecutor) PlaceSell(req OrderRequest) (*OrderResult, error) {
	if req.Side == "" {
		req.Side = SideSell
	}
	if req.Side != SideSell && req.Side != SideShortOpen {
		return &OrderResult{OK: false, Err: fmt.Sprintf("binance paper: PlaceSell 只接受 %s/%s（收到 %q）", SideSell, SideShortOpen, req.Side)}, nil
	}
	return e.place(req)
}

// Cancel 撤单：纸面盘即时成交、没有在途单，受理即成功（幂等 nil，不炸编排层）。
func (e *BinancePaperExecutor) Cancel(orderID string) error { return nil }

// State 对账源：连接恒真 + 账簿持仓/委托按市场过滤回读（与控制器 Reconcile 的读面兼容）。
func (e *BinancePaperExecutor) State() (*GatewayState, error) {
	positions, err := e.opt.DB.RealPositionsForUser(e.opt.UserID)
	if err != nil {
		return nil, err
	}
	orders, err := e.opt.DB.RealOrdersForUser(e.opt.UserID)
	if err != nil {
		return nil, err
	}
	st := &GatewayState{Connected: true, Account: "paper:" + e.opt.Market}
	for _, p := range positions {
		if data.NormalizeMarketKey(p.Market) == e.opt.Market {
			st.Positions = append(st.Positions, p)
		}
	}
	for _, o := range orders {
		if data.NormalizeMarketKey(o.Market) == e.opt.Market {
			st.Orders = append(st.Orders, o)
		}
	}
	return st, nil
}

// Health 柜台健康：纸面盘无外呼，恒真（真链健康腿的 401/网络抖动形态在这里结构性不存在）。
func (e *BinancePaperExecutor) Health() (bool, error) { return true, nil }

// Cash 当前现金快照（本市场计价币）——state 观测面消费。
func (e *BinancePaperExecutor) Cash() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureBootstrapLocked()
	return e.cash
}

// paperPositionRow 读本账号本市场某 code 的持仓行（qty>0 才算有仓；nil=无行）。
func (e *BinancePaperExecutor) paperPositionRow(code string) (*store.RealPosition, error) {
	positions, err := e.opt.DB.RealPositionsForUser(e.opt.UserID)
	if err != nil {
		return nil, err
	}
	for i := range positions {
		p := positions[i]
		if data.NormalizeMarketKey(p.Market) == e.opt.Market && p.TsCode == code && p.Qty > 0 {
			return &p, nil
		}
	}
	return nil, nil
}

// place 四方向统一受理口：定价→定量→方向前置校验（现金/可平量）→成交腿→委托秩推进→
// 现金结算。返回 error 仅代表链路故障（读库失败等），业务拒单一律 OK:false+Err、
// error=nil——与 BinanceExecutor 分类语义同构，控制器降级/重试判定共用。
func (e *BinancePaperExecutor) place(req OrderRequest) (*OrderResult, error) {
	if data.NormalizeMarketKey(req.Market) != e.opt.Market {
		return &OrderResult{OK: false, Err: fmt.Sprintf("binance paper: 市场章不符（柜台 %s，请求 %q）", e.opt.Market, req.Market)}, nil
	}
	price := req.Price
	if price <= 0 && e.opt.GetPrice != nil {
		price = e.opt.GetPrice(req.Code)
	}
	if price <= 0 {
		return &OrderResult{OK: false, Err: "binance paper: 无可用参考价（req.Price 与行情源都拿不到），拒单不造成交价"}, nil
	}
	qty := req.Qty
	if qty <= 0 {
		// 金额单换量：市价按金额受理的形态（US Notional / 通用 Amount）在这里折叠成数量。
		amt := req.Amount
		if amt <= 0 && req.Notional > 0 {
			amt = req.Notional
		}
		if amt <= 0 {
			return &OrderResult{OK: false, Err: "binance paper: 数量与金额均缺失，拒单"}, nil
		}
		qty = amt / price
	}
	amount := qty * price
	fee := amount * e.opt.FeeRate
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensureBootstrapLocked()

	// —— 方向前置校验（fail-close，全部在落库前完成）——
	switch req.Side {
	case SideBuy:
		if e.cash < amount+fee {
			return &OrderResult{OK: false, Err: fmt.Sprintf("binance paper: 现金不足（需 %.2f 含费，剩 %.2f）", amount+fee, e.cash)}, nil
		}
	case SideShortOpen:
		need := amount*e.opt.ShortMarginRate + fee
		if e.cash < need {
			return &OrderResult{OK: false, Err: fmt.Sprintf("binance paper: 开空保证金不足（需冻结 %.2f，现金 %.2f）", need, e.cash)}, nil
		}
	case SideSell, SideShortCover:
		row, err := e.paperPositionRow(req.Code)
		if err != nil {
			return nil, err
		}
		wantShort := req.Side == SideShortCover
		if row == nil || (row.Side == "short") != wantShort {
			have := "无持仓行"
			if row != nil {
				have = "行方向 " + row.Side
			}
			return &OrderResult{OK: false, Err: fmt.Sprintf("binance paper: %s %s 无对应可平仓位（%s），拒单（不伪造成交）", req.Side, req.Code, have)}, nil
		}
		if row.Qty+1e-9 < qty {
			return &OrderResult{OK: false, Err: fmt.Sprintf("binance paper: 平仓量 %.8f 超过持仓 %.8f（%s），拒单", qty, row.Qty, req.Side)}, nil
		}
	}

	e.seq++
	// orderID 含实例盐（生成见构造函数注释）：跨实例/跨重启唯一，成交判重锚不误吞新单。
	orderID := "paper-" + e.salt + "-" + strconv.FormatInt(e.seq, 10) + "-" + strconv.FormatInt(e.opt.Now().Unix(), 10)
	at := e.opt.Now().In(data.Session(e.opt.Market).Loc()).Format("2006-01-02 15:04:05")
	currency := currencyForSymbol(e.opt.Market, req.Code)

	// 成交腿先落（ApplyRealFill 自带 TradeID 判重与方向矩阵——万一拒收，委托行停留 已报，
	// 清扫腿兜底降级，账面不会出现"已成但无成交"的幽灵）。
	if err := e.opt.DB.ApplyRealFill(store.RealFill{
		OrderID: orderID, Code: req.Code, Name: req.Name, Side: req.Side,
		Price: price, Qty: qty, Amount: amount, TradedAt: at, SignalID: req.SignalID,
		TradeID: "paper:" + orderID + ":1", UserID: e.opt.UserID, Fee: fee,
		Market: e.opt.Market, Currency: currency,
	}); err != nil {
		return &OrderResult{OK: false, Err: "binance paper: 成交腿落库被拒: " + err.Error()}, nil
	}
	// 委托行推进到 已成（按 signal_id 命中控制器占位行；秩守卫幂等）。
	if _, err := e.opt.DB.ApplyOrderReportTx(store.RealOrder{
		OrderID: orderID, SignalID: req.SignalID, Code: req.Code, Side: req.Side,
		Status: "已成", Price: price, Qty: qty, CreatedAt: at,
		UserID: e.opt.UserID, Market: e.opt.Market, Currency: currency,
	}); err != nil {
		// 成交已落、状态推进失败：如实报错交对账腿收敛，绝不回滚成交。
		return nil, fmt.Errorf("binance paper: 成交已入账但委托状态推进失败(%s): %w", orderID, err)
	}

	// 现金结算（与前置校验同锁内完成，杜绝并发双花）：
	e.applyCashEventLocked(req.Code, req.Side, price, qty, fee)
	e.syncAccountLocked()
	e.info("成交 %s %s %.8f@%.2f 费 %.4f (order=%s)", req.Side, req.Code, qty, price, fee, orderID)
	return &OrderResult{OK: true, OrderID: orderID}, nil
}

// info 统一日志（拒单只回因不告警；成交 info 留痕取证）。
func (e *BinancePaperExecutor) info(format string, args ...any) {
	log.Printf("[binance-paper] "+format, args...)
}
