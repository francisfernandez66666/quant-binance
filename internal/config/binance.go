// 文件职责：rules.binance 配置段（PLAN_BINANCE_MULTI_ASSET §4.1/§4.2/§4.4）——
// BinanceConfig/BinanceMarketProfile 结构体、出厂默认、normalize、validateBinance、
// per-user KV（binance_config_json_v1）读写，以及 BrokerConfig 接口的币安侧实现（BinanceBrokerView）。
// 零回归边界：本文件不触碰 QMTConfig 任何字段/tag；Binance 链路 Phase 1 只到配置层，
// Executor/Router 接线在 Phase 2。发布闸：顶层 enabled 出厂 false + CI 静态锁。
package config

import (
	"encoding/json"
	"fmt"
	"log"
)

// BinanceConfig rules.binance 顶层段：币安双市场（stock=美股 equity、spot=加密货币）共享的
// 连接与开关配置；市场级交易参数放 BinanceMarketProfile。
type BinanceConfig struct {
	// Enabled 交易面总开关（同 qmt.enabled 语义；出厂 false）：开=允许真下单，须凭证齐备。
	Enabled bool `json:"enabled"`
	// DataPlane §MR-1 数据面开关：只装配观测链（行情/状态 feed、FNG、事件腿、K 线读档、
	// 维护节拍），**永不真下**——真执行器构造硬条件仍是 Enabled+凭证（registry.go 装配面）。
	// 依据市场实际：美股/加密货币的行情与历史数据本就不需要钥匙，凭证只属于交易面。
	// English: observation-plane switch — feeds/FNG/events/klines with zero credentials; the live
	// executor still requires Enabled+APIKey, so orders are structurally impossible on this plane.
	DataPlane          bool   `json:"data_plane"`
	Mode               string `json:"mode"`        // auto | manual
	Testnet            bool   `json:"testnet"`     // true=testnet.binance.vision（仅现货有意义）
	APIKey             string `json:"api_key"`     // 行情/交易 API Key
	APISecret          string `json:"api_secret"`  // API Secret（per-user 落库，同 qmt.token 惯例）
	TimeoutSec         int    `json:"timeout_sec"` // REST 超时
	MissHeartbeatSec   int    `json:"miss_heartbeat_sec"`
	Halted             bool   `json:"halted"`                         // kill-switch（即时生效，不入待生效队列）
	CancelStaleSec     int    `json:"cancel_stale_sec"`               // 在途超时撤单（-1=关）
	DisclaimerSignedAt string `json:"disclaimer_signed_at,omitempty"` // 美股披露签署时间（启动探测回填）
	// LeverageAcknowledgedAt §MR-4B 杠杆确认签署时间（设计稿红线"enabled=true 且存在 leverage>1
	// 配置须白名单例外"的落地形态）：本仓不做"账户白名单"这种运行时不可验证的概念，
	// 改为显式人工确认位——交易面开启时任何 profile.leverage>1 都必须先落这个时间戳
	// （与 disclaimer_signed_at 同族语义：人工在币安侧开过合约权限/理解爆仓风险后才签）。
	// English: §MR-4B explicit human acknowledgement replacing the doc's "whitelist" idea —
	// leverage>1 with trading enabled requires this timestamp, same family as disclaimer_signed_at.
	LeverageAcknowledgedAt string               `json:"leverage_acknowledged_at,omitempty"`
	QuoteAsset             string               `json:"quote_asset"` // 美股 quoteAsset 缺省（USDC）
	Stock                  BinanceMarketProfile `json:"stock"`       // —— US 市场档案 ——
	Spot                   BinanceMarketProfile `json:"spot"`        // —— CRYPTO 市场档案 ——
	RiskGate               RiskGateConfig       `json:"risk_gate"`
	// Dispatch §战法批（2026-09-23）xasset 派发总闸：新市场（US/CRYPTO）的
	// 「K线→战法→信号→下单」自动链开关，出厂 false=零行为（信号只进观测面）。
	// 纸面盘（profile.Paper）下派发只落本地账簿、结构上碰不到交易所；真交易面下
	// 派发单与手动单走同一套控制器/风控闸，无旁路。
	// English: §batch-xasset auto-dispatch switch for the new markets; factory-false keeps the
	// chain observation-only, and paper-dispatch can structurally never reach the exchange.
	Dispatch      BinanceDispatchConfig `json:"dispatch,omitempty"`
	PaperSeparate bool                  `json:"paper_separate"` // 新市场纸面盘独立资金池（P2 接 paper）
	// Events §ENH-A4/B8 事件血源腿（SEC EDGAR 8-K / CryptoPanic 热帖）的凭证与范围。
	// 缺省零值=两腿全部不装配（"空配置=不装配"惯例）；token 属密钥，GET 侧只回掩码。
	// English: §ENH-A4/B8 event-leg credentials (EDGAR UA / CryptoPanic key); zero value
	// keeps both legs inert, the token is masked on the read API like api_secret.
	Events BinanceEventsConfig `json:"events,omitempty"`
}

// BinanceEventsConfig 事件腿配置（§ENH-A4 EDGAR + §ENH-B8 CryptoPanic）。
// 消费位点=engine Registry 装配面（registry_events.go）：US 分支 EdgarUserAgent 非空才挂
// EDGAR 源、CRYPTO 分支 CryptoPanicToken 非空才挂热帖源；两腿只喂观测面与战法入参，
// 派发链保持 Phase 5 边界（本批不发任何新单）。
// English: event-leg config; consumed at registry assembly — empty key ⇒ leg never built.
type BinanceEventsConfig struct {
	// EdgarUserAgent SEC 自动化访问政策要求的 User-Agent（含联系邮箱）；空=US 事件腿拒发=不装配。
	EdgarUserAgent string `json:"edgar_user_agent,omitempty"`
	// CryptoPanicToken 热帖 API key；空=整腿惰性不装配（客户端 Enabled() 同源判定点）。
	CryptoPanicToken string `json:"cryptopanic_token,omitempty"`
	// CryptoPanicCurrencies 关注的币种列表（如 ["BTC","ETH"]）；空=客户端缺省集。
	CryptoPanicCurrencies []string `json:"cryptopanic_currencies,omitempty"`
	// —— §战法批 事件利多利空大模型打分（OpenAI 兼容 chat 面，DeepSeek/通义/GPT 均可）——
	// 三键齐备才启用；任一缺席=打分器不装配=事件方向退回关键词基线（零配置零行为）。
	// 币安官方无新闻 API（行情/交易面不含资讯），本层打分对象是 EDGAR/CryptoPanic 标题。
	// English: optional LLM scorer for event sentiment (OpenAI-compatible); all three keys
	// required, partial config keeps the keyword baseline. Binance has no news API — the scored
	// corpus is EDGAR/CryptoPanic titles.
	LLMBaseURL string `json:"llm_base_url,omitempty"` // 例 https://api.deepseek.com/v1
	LLMApiKey  string `json:"llm_api_key,omitempty"`  // 密钥（GET 侧只回掩码，同 api_secret 惯例）
	LLMModel   string `json:"llm_model,omitempty"`    // 例 deepseek-chat / qwen-plus / gpt-4o-mini
	// LLMTimeoutSec 单次打分请求超时；0=缺省 15s（派发节拍远大于此，不拖主循环）。
	LLMTimeoutSec int `json:"llm_timeout_sec,omitempty"`
}

// BinanceDispatchConfig §战法批 xasset 自动派发参数（US/CRYPTO 共享一份；标的池取各市场
// 档案的 quote_symbols，纸面/真面同闸）。零值全部=关闭或缺省，出厂态派发链整体静默。
type BinanceDispatchConfig struct {
	Enabled       bool    `json:"enabled"`         // 派发总闸（出厂 false）
	BearEnabled   bool    `json:"bear_enabled"`    // 空头腿开关：开=死叉/过热回落/利空可产 卖出开空 信号（借券闸仍然把关）
	MinConfidence float64 `json:"min_confidence"`  // 信号置信度门槛（0=缺省 0.6；防弱信号烧预算）
	TakeProfitPct float64 `json:"take_profit_pct"` // 持仓止盈 %（0=关退出腿）
	StopLossPct   float64 `json:"stop_loss_pct"`   // 持仓止损 %（0=关）
	EverySec      int     `json:"every_sec"`       // 节拍秒（0=缺省 600；下限 60 防抖）
	MaxLiveOrders int     `json:"max_live_orders"` // 单轮最多派发的新单数（0=缺省 5，防事件风暴刷屏）
}

// BinanceMarketProfile 单市场档案（US/CRYPTO 共用形状）。Enabled 为子开关：
// 总开关开且子开关开 → 该市场 Controller 才装配。
type BinanceMarketProfile struct {
	Enabled           bool     `json:"enabled"`
	TradingSession    string   `json:"trading_session,omitempty"` // US: RTH|EXTENDED|24H
	TimeInForce       string   `json:"time_in_force,omitempty"`   // US: DAY|GTC
	FixedAmount       float64  `json:"fixed_amount"`
	MaxPositions      int      `json:"max_positions"`
	DailyMaxBuys      int      `json:"daily_max_buys"`
	DailyBudgetAmount float64  `json:"daily_budget_amount"`
	Strategies        []string `json:"strategies"`              // 战法白名单（空=全部）
	Blacklist         []string `json:"blacklist,omitempty"`     // 下单黑名单
	QuoteSymbols      []string `json:"quote_symbols,omitempty"` // CRYPTO 监控池
	// StatusSymbols §P3（market_halt 闸数据腿）：需盯 tradingStatus 证据的代码列表（仅 US 装配生效）。
	// 空 = 不启动状态 feed = 第 15 道闸保持未装配态（零配置零行为，与 QuoteSymbols 同族缺省纪律）。
	// English: §P3 symbols whose tradingStatus evidence feeds risk gate 15 (US only; empty = feed
	// never starts and the gate stays unwired/inert).
	StatusSymbols []string `json:"status_symbols,omitempty"`
	// §MR-4A 做空保证金率（融券/合约开空的权益冻结比例，0=禁做空，合法域 0 或 (0,1]）；
	// 消费位点=risk.Gate 第 16 道闸 short_borrow。
	ShortMarginRate float64 `json:"short_margin_rate,omitempty"`
	// ProductType §MR-4B 产品线（仅 CRYPTO 档案有实际意义）："spot"（缺省/空，逐字节兼容
	// 存量配置）| "umfutures"（USDT 本位永续，走 fapi 域名，凭证权限与现货分离）。
	// English: §MR-4B product line for the CRYPTO profile; empty/spot keeps the existing chain
	// byte-identical, umfutures forks orders/positions/streams onto fapi.binance.com.
	ProductType string `json:"product_type,omitempty"`
	// Leverage §MR-4B 名义杠杆（合约档专用；0/1=不加杠杆）。>1 受红线约束：
	// 交易面 enabled=true 时必须先签 leverage_acknowledged_at（validateBinance 拦）。
	Leverage int `json:"leverage,omitempty"`
	// LiqDistMinPct §MR-4B 强平价距离下限（%）：开仓时 markPrice 与强平价距离小于该百分比
	// 即拒单（第 17 道闸 liq_distance 的阈值；0=闸关闭，行为与旧配置一致）。
	LiqDistMinPct float64 `json:"liq_dist_min_pct,omitempty"`
	// Paper §战法批 本市场纸面成交柜台：交易面未激活（无钥匙/总开关关）且本键开时，
	// 控制器执行器落纸面柜台（即时成交、只写本地账簿，结构上碰不到交易所）；
	// 交易面激活时本键被忽略并打日志（真面优先，绝不静默把真单降级成假成交）。
	// 与派发总闸（binance.dispatch.enabled）叠加：纸面盘可先跑通链、后换真枪。
	// English: §batch-xasset per-market paper desk — used only while the trading plane is
	// inactive (structurally impossible to reach the exchange); ignored (with a log) when the
	// live plane is on, so a real profile never silently degrades to fake fills.
	Paper bool `json:"paper,omitempty"`
	// PaperCash 纸面账户初始资金（本市场计价币：US=USD、CRYPTO=USDT）；0=缺省 100000。
	PaperCash float64 `json:"paper_cash,omitempty"`
}

// DefaultBinanceConfig 出厂默认（§4.1 逐键）：总开关关——发布闸要求缺省不接通。
func DefaultBinanceConfig() BinanceConfig {
	return BinanceConfig{
		Enabled:          false,
		Mode:             "manual",
		Testnet:          true,
		TimeoutSec:       10,
		MissHeartbeatSec: 120,
		CancelStaleSec:   120,
		QuoteAsset:       "USDC",
		Stock: BinanceMarketProfile{
			Enabled: true, TradingSession: "RTH", TimeInForce: "DAY",
			FixedAmount: 500, MaxPositions: 10, DailyMaxBuys: 20, DailyBudgetAmount: 2000,
		},
		Spot: BinanceMarketProfile{
			Enabled:     true,
			FixedAmount: 100, MaxPositions: 10, DailyMaxBuys: 30, DailyBudgetAmount: 1000,
			QuoteSymbols: []string{"BTCUSDT", "ETHUSDT"},
		},
		RiskGate: RiskGateConfig{
			StaleQuoteMs:        5000,
			DayLossLimitPct:     3,
			SingleStockValuePct: 20,
			MaxOrderAmount:      5000,
		},
		PaperSeparate: true,
	}
}

// NormalizeBinance 零值缺省回填（"零值=未配置"惯例，与 normalizeD1 同款）：
// 老配置文件没有 binance 段时，Load 后得到的是零值——回填成出厂默认，
// 但绝不开启总开关（Enabled/Halted 保持文件原值，零值即关）。
func NormalizeBinance(b *BinanceConfig) {
	d := DefaultBinanceConfig()
	// 整段缺失（老 config.json 无 binance 键）→ 直接落出厂默认（含 testnet=true 等布尔缺省）。
	if b.Mode == "" && b.TimeoutSec == 0 && b.QuoteAsset == "" && b.APIKey == "" && b.RiskGate == (RiskGateConfig{}) {
		*b = d
		// 出厂默认已含全部键值，此处直接收工不再逐键回填（避免用零值覆盖布尔缺省 testnet=true）。
		return
	}
	if b.Mode == "" {
		b.Mode = d.Mode
	}
	if b.TimeoutSec == 0 {
		b.TimeoutSec = d.TimeoutSec
	}
	if b.MissHeartbeatSec == 0 {
		b.MissHeartbeatSec = d.MissHeartbeatSec
	}
	if b.CancelStaleSec == 0 {
		b.CancelStaleSec = d.CancelStaleSec
	}
	if b.QuoteAsset == "" {
		b.QuoteAsset = d.QuoteAsset
	}
	np := func(p *BinanceMarketProfile, def BinanceMarketProfile) {
		if p.FixedAmount == 0 {
			p.FixedAmount = def.FixedAmount
		}
		if p.MaxPositions == 0 {
			p.MaxPositions = def.MaxPositions
		}
		if p.DailyMaxBuys == 0 {
			p.DailyMaxBuys = def.DailyMaxBuys
		}
		if p.DailyBudgetAmount == 0 {
			p.DailyBudgetAmount = def.DailyBudgetAmount
		}
	}
	np(&b.Stock, d.Stock)
	np(&b.Spot, d.Spot)
	if b.Stock.TradingSession == "" {
		b.Stock.TradingSession = "RTH"
	}
	if b.Stock.TimeInForce == "" {
		b.Stock.TimeInForce = "DAY"
	}
	if b.RiskGate == (RiskGateConfig{}) {
		b.RiskGate = d.RiskGate
	}
}

// ValidateBinance 导出包装：server 层保存 rules.binance 前复校验，避免校验逻辑两处漂移。
func ValidateBinance(b *BinanceConfig) error { return validateBinance(b) }

// validateBinance rules.binance 段校验（PLAN §6.1）：枚举/范围/启用一致性。
// 缺省（Enabled=false）只查枚举与数值域——保证不启用也不阻断启动。
func validateBinance(b *BinanceConfig) error {
	if b.Mode != "" && b.Mode != "auto" && b.Mode != "manual" {
		return fmt.Errorf("binance.mode 仅允许 auto/manual（实际 %q）", b.Mode)
	}
	if b.TimeoutSec < 0 || b.TimeoutSec > 120 {
		return fmt.Errorf("binance.timeout_sec 超出范围 0-120（实际 %d）", b.TimeoutSec)
	}
	if b.MissHeartbeatSec != 0 && (b.MissHeartbeatSec < 30 || b.MissHeartbeatSec > 3600) {
		return fmt.Errorf("binance.miss_heartbeat_sec 超出范围 30-3600（实际 %d）", b.MissHeartbeatSec)
	}
	// 0=未配置（NormalizeBinance 回填 120）；-1=显式关闭；其余必须 ≥30。
	if b.CancelStaleSec != 0 && b.CancelStaleSec != -1 && b.CancelStaleSec < 30 {
		return fmt.Errorf("binance.cancel_stale_sec 仅允许 -1（关）或 ≥30（实际 %d）", b.CancelStaleSec)
	}
	// 子档案枚举（US 特有字段；CRYPTO 侧留空即跳过）
	switch b.Stock.TradingSession {
	case "", "RTH", "EXTENDED", "24H":
	default:
		return fmt.Errorf("binance.stock.trading_session 仅允许 RTH/EXTENDED/24H（实际 %q）", b.Stock.TradingSession)
	}
	switch b.Stock.TimeInForce {
	case "", "DAY", "GTC":
	default:
		return fmt.Errorf("binance.stock.time_in_force 仅允许 DAY/GTC（实际 %q）", b.Stock.TimeInForce)
	}
	// 数值域（两子档案同规则；0=不设限沿用现状语义，负值=配置错误）
	for _, e := range []struct {
		name string
		p    *BinanceMarketProfile
	}{{"stock", &b.Stock}, {"spot", &b.Spot}} {
		if e.p.FixedAmount < 0 {
			return fmt.Errorf("binance.%s.fixed_amount 不能为负（%.2f）", e.name, e.p.FixedAmount)
		}
		if e.p.MaxPositions < 0 || e.p.MaxPositions > 50 {
			return fmt.Errorf("binance.%s.max_positions 超出范围 0-50（实际 %d）", e.name, e.p.MaxPositions)
		}
		if e.p.DailyMaxBuys < 0 {
			return fmt.Errorf("binance.%s.daily_max_buys 不能为负（%d）", e.name, e.p.DailyMaxBuys)
		}
		if e.p.DailyBudgetAmount < 0 {
			return fmt.Errorf("binance.%s.daily_budget_amount 不能为负（%.2f）", e.name, e.p.DailyBudgetAmount)
		}
		// §MR-4A 做空保证金率域校验：0=禁做空（出厂缺省）；启用必须落在 (0,1]。
		// 负值=配置错误；>1=名义杠杆低于 1 倍（开 1 元首寸冻结超过 1 元权益），超出档案表达能力，拒。
		if e.p.ShortMarginRate < 0 || e.p.ShortMarginRate > 1 {
			return fmt.Errorf("binance.%s.short_margin_rate 仅允许 0（禁做空）或 0-1 之间的保证金率（实际 %.4f）", e.name, e.p.ShortMarginRate)
		}
		// §MR-4B 产品线枚举：空/spot=现货（存量配置逐字节兼容）；umfutures 仅 CRYPTO 档案可用
		// （美股没有币安永续产品线，US 侧填 umfutures 属配置错位，直接拒）。
		switch e.p.ProductType {
		case "", "spot":
		case "umfutures":
			if e.name == "stock" {
				return fmt.Errorf("binance.stock.product_type 不支持 umfutures（永续仅 CRYPTO 档案）")
			}
		default:
			return fmt.Errorf("binance.%s.product_type 仅允许 spot/umfutures（实际 %q）", e.name, e.p.ProductType)
		}
		// §MR-4B 杠杆域：0/1=不加杠杆；上限 20 为本地硬帽（设计稿"白名单例外"的替代收口——
		// 更高杠杆档位在币安侧本就有分级保证金限制，本仓不做超限配置面）。
		if e.p.Leverage < 0 || e.p.Leverage > 20 {
			return fmt.Errorf("binance.%s.leverage 超出范围 0-20（实际 %d）", e.name, e.p.Leverage)
		}
		if e.p.Leverage > 1 && e.p.ProductType != "umfutures" {
			return fmt.Errorf("binance.%s.leverage>1 仅允许 product_type=umfutures（现货档无杠杆语义）", e.name)
		}
		// §MR-4B 强平距离闸阈值域（0=闸关；>100 无意义）。
		if e.p.LiqDistMinPct < 0 || e.p.LiqDistMinPct > 100 {
			return fmt.Errorf("binance.%s.liq_dist_min_pct 超出范围 0-100（实际 %.2f）", e.name, e.p.LiqDistMinPct)
		}
		// §战法批 纸面盘卫生：初始资金非负；paper=true 但市场子开关关=空配置（柜台根本不会装配）。
		if e.p.PaperCash < 0 {
			return fmt.Errorf("binance.%s.paper_cash 不能为负（%.2f）", e.name, e.p.PaperCash)
		}
		if e.p.Paper && !e.p.Enabled {
			return fmt.Errorf("binance.%s.paper=true 但该市场子开关关闭（纸面柜台根本不会装配，配置错位）", e.name)
		}
		// §战法批-4 死配置闸：币安链装配门是 Enabled||DataPlane（registry），两平面全关时
		// paper 配了也建不出柜台——与其静默无效，不如启动期就报错指点开数据面。
		if e.p.Paper && !b.Enabled && !b.DataPlane {
			return fmt.Errorf("binance.%s.paper=true 需开启币安链平面（data_plane=true 或 enabled=true）——平面全关时纸面柜台不会装配，本配置无效", e.name)
		}
	}
	// §MR-4B 红线（设计稿 §4.2-M5）：交易面开启且存在 leverage>1 档案时，必须有显式人工确认
	// （leverage_acknowledged_at 时间戳，替代不可运行时验证的"白名单"概念）。
	// 关闭交易面允许预配杠杆参数（数据面/离线演练形态），但结构上不可能真下单。
	// English: §MR-4B red line — enabled trading with any leverage>1 profile requires the
	// explicit human acknowledgement timestamp; configuring leverage while disabled is allowed
	// (orders are structurally impossible without Enabled).
	if b.Enabled && b.LeverageAcknowledgedAt == "" && (b.Stock.Leverage > 1 || b.Spot.Leverage > 1) {
		return fmt.Errorf("binance.enabled=true 且存在 leverage>1 档案，但未签 leverage_acknowledged_at（合约杠杆须人工确认后才能开交易面，§MR-4B 红线）")
	}
	if b.RiskGate.MaxOrderAmount < 0 {
		return fmt.Errorf("binance.risk_gate.max_order_amount 不能为负（%.2f）", b.RiskGate.MaxOrderAmount)
	}
	// —— §战法批 派发与事件打分的域校验（缺省全零=链静默，不启用也不阻断启动）——
	d := b.Dispatch
	if d.MinConfidence < 0 || d.MinConfidence > 1 {
		return fmt.Errorf("binance.dispatch.min_confidence 超出范围 0-1（实际 %.2f）", d.MinConfidence)
	}
	if d.TakeProfitPct < 0 || d.TakeProfitPct > 100 || d.StopLossPct < 0 || d.StopLossPct > 100 {
		return fmt.Errorf("binance.dispatch 止盈/止损幅度超出范围 0-100（tp=%.2f sl=%.2f）", d.TakeProfitPct, d.StopLossPct)
	}
	if d.EverySec != 0 && d.EverySec < 60 {
		return fmt.Errorf("binance.dispatch.every_sec 仅允许 0（缺省 600s）或 ≥60（实际 %d）", d.EverySec)
	}
	if d.MaxLiveOrders < 0 || d.MaxLiveOrders > 50 {
		return fmt.Errorf("binance.dispatch.max_live_orders 超出范围 0-50（实际 %d）", d.MaxLiveOrders)
	}
	// 派发一致性（fail-loud）：开派发但既无真交易面（Enabled+凭证）也无任何纸面盘——
	// 派发单会全部撞在 Noop 执行器上白烧信号，属配置错位，启动即拒而不是静默空转。
	if d.Enabled {
		live := b.Enabled && b.APIKey != "" && b.APISecret != ""
		if !live && !(b.Stock.Enabled && b.Stock.Paper) && !(b.Spot.Enabled && b.Spot.Paper) {
			return fmt.Errorf("binance.dispatch.enabled=true 但既无交易面（enabled+凭证）也无纸面盘（paper=true）——派发链无处成交")
		}
	}
	// 事件 LLM 打分：三键部分配置=半截接线（打分器根本装不起来），拒；全空=关键词基线。
	ev := b.Events
	if ev.LLMBaseURL != "" || ev.LLMApiKey != "" || ev.LLMModel != "" {
		if ev.LLMBaseURL == "" || ev.LLMApiKey == "" || ev.LLMModel == "" {
			return fmt.Errorf("binance.events.llm_* 需 base_url/api_key/model 三键齐备（缺席键=打分器不装配，应整体留空回关键词基线）")
		}
	}
	if ev.LLMTimeoutSec != 0 && (ev.LLMTimeoutSec < 5 || ev.LLMTimeoutSec > 120) {
		return fmt.Errorf("binance.events.llm_timeout_sec 仅允许 0（缺省 15s）或 5-120（实际 %d）", ev.LLMTimeoutSec)
	}
	// §ENH-B7 滑点回灌比例帽同构域校验（0~0.05）。当前消费面=engine 自动挂价链
	// （autoPlace/sellRealPosition 读 ctrl.QMT().RiskGate），binance 侧仅做输入卫生。
	if b.RiskGate.SlippagePassthrough < 0 || b.RiskGate.SlippagePassthrough > 0.05 {
		return fmt.Errorf("binance.risk_gate.slippage_passthrough 超出范围 0~0.05（实际 %.4f）", b.RiskGate.SlippagePassthrough)
	}
	// 启用一致性：交易面开 ⇒ 凭证齐 + 至少一个子市场开；数据面开 ⇒ 至少一个子市场开（**不查凭证**——
	// §MR-1 行情/事件/K线是公开数据腿，钥匙闸只属于交易面，这正是美股/加密货币的实际）。
	if b.Enabled {
		if b.APIKey == "" || b.APISecret == "" {
			return fmt.Errorf("binance.enabled=true 但 api_key/api_secret 为空")
		}
		if !b.Stock.Enabled && !b.Spot.Enabled {
			return fmt.Errorf("binance.enabled=true 但 stock/spot 子开关全部关闭")
		}
	}
	if b.DataPlane && !b.Stock.Enabled && !b.Spot.Enabled {
		return fmt.Errorf("binance.data_plane=true 但 stock/spot 子开关全部关闭")
	}
	return nil
}

// BaseURL 返回 REST 根地址：testnet=true 时现货走 https://testnet.binance.vision（PLAN §2.1，
// 美股 equity 无独立 testnet，仍走正式 base 由 Key 权限隔离）。空串=未配置凭证（上层拒绝探活）。
// English: BaseURL resolves the REST root (spot sandbox on testnet; equity stays on prod base —
// there is no equity sandbox). Empty means credentials are not configured.
func (b BinanceConfig) BaseURL() string {
	if b.APIKey == "" || b.APISecret == "" {
		return ""
	}
	if b.Testnet {
		return "https://testnet.binance.vision"
	}
	return "https://api.binance.com"
}

// EquityBaseURL 美股 equity 正式根地址（无 testnet 变体，§2.1）。
func (b BinanceConfig) EquityBaseURL() string {
	if b.APIKey == "" || b.APISecret == "" {
		return ""
	}
	return "https://api.binance.com"
}

// FuturesBaseURL §MR-4B USDT 本位永续（umfutures）REST 根地址：
// 主网 fapi.binance.com；testnet=true 时走 testnet.binancefuture.com（现货 testnet 域名
// 不承载合约 API）。凭证缺失返回空串（上层拒绝探活/装配，同 BaseURL 惯例）。
// 域名与现货分离是市场实际：合约 API 需要在币安侧单独授权，Key 建议分开申请（§5 备忘）。
// English: §MR-4B UM-futures REST root (fapi mainnet / testnet.binancefuture.com); empty
// without credentials, mirroring BaseURL's refusal semantics.
func (b BinanceConfig) FuturesBaseURL() string {
	if b.APIKey == "" || b.APISecret == "" {
		return ""
	}
	if b.Testnet {
		return "https://testnet.binancefuture.com"
	}
	return "https://fapi.binance.com"
}

// —— per-user KV（PLAN §4.4）：账号级隔离，命名空间 binance_config_json_v1 ——
// 与 SetQMTConfigFor 的差异说明：QMT 配置搭在全量 Rules 快照里（quant_config_json_v1），
// binance 凭证独立成键——避免前端每存一次币安配置就整体翻转 Rules 快照的副作用。

// perUserBinanceKey 每账号 Binance 配置在 KVStore 中的键。
const perUserBinanceKey = "binance_config_json_v1"

// GetBinanceConfigFor 返回指定账号的币安配置。解析优先级与 GetQMTConfigFor 一致：
// ① 账号自身覆盖 → ② 运营账号覆盖 → ③ 全局 rules.binance。
func (m *Manager) GetBinanceConfigFor(userID string) *BinanceConfig {
	if m.store == nil || userID == "" {
		return &m.Rules.Binance
	}
	if c, ok := m.storedUserBinance(userID); ok {
		return c
	}
	if oid := m.ownerOf(userID); oid != "" && oid != userID {
		if c, ok := m.storedUserBinance(oid); ok {
			return c
		}
	}
	return &m.Rules.Binance
}

// storedUserBinance 读取某账号 KV 键中的 BinanceConfig（无键/坏 JSON → false）。
func (m *Manager) storedUserBinance(userID string) (*BinanceConfig, bool) {
	m.mu.RLock()
	raw, ok := m.store.GetConfig(userID, perUserBinanceKey)
	m.mu.RUnlock()
	if !ok || raw == "" {
		return nil, false
	}
	var c BinanceConfig
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		log.Printf("[config] 账号 %s binance 配置反序列化失败, 回退全局: %v", userID, err)
		return nil, false
	}
	NormalizeBinance(&c)
	return &c, true
}

// SetBinanceConfigFor 落库指定账号的币安配置（独立 KV 键；5s 热加载由消费方轮询此入口）。
// 调用方负责取值合法性（枚举/范围），这里只做序列化落库。store 缺席时落全局快照。
func (m *Manager) SetBinanceConfigFor(userID string, cfg *BinanceConfig) {
	if cfg == nil {
		return
	}
	if m.store == nil || userID == "" {
		m.Rules.Binance = *cfg
		m.Save()
		return
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		log.Printf("[config] 账号 %s binance 配置序列化失败: %v", userID, err)
		return
	}
	if err := m.store.SetConfig(userID, perUserBinanceKey, string(data)); err != nil {
		log.Printf("[config] 账号 %s binance 配置落库失败: %v", userID, err)
	}
}

// —— BrokerConfig 的币安侧实现（Phase 2 Controller/Gate 泛型化时消费）——

// BinanceBrokerView 把 BinanceConfig 的"单市场档案"适配为 BrokerConfig：
// 一个视图 = 一个市场的下单侧配置（Market="US"→Stock 档案，"CRYPTO"→Spot 档案）。
// US/CRYPTO 各建一个 Controller 时注入各自视图，凭证/开关等共享字段留在顶层语义里。
type BinanceBrokerView struct {
	Cfg    BinanceConfig
	Market string // "US" | "CRYPTO"
}

// 编译期锁：币安侧视图必须满足 BrokerConfig（接口增删方法即编译失败）。
var _ BrokerConfig = BinanceBrokerView{}

// profile 返回本视图对应市场的子档案；未知 Market 一律按 CRYPTO（Spot）处理。
func (v BinanceBrokerView) profile() BinanceMarketProfile {
	if v.Market == "US" {
		return v.Cfg.Stock
	}
	return v.Cfg.Spot
}

// BrokerEnabled 视图活跃判定＝（交易面 || 数据面）且子开关开。§MR-1 起本方法的语义是
// 「该市场控制器是否装配/维护」，不再等价于「允许交易」——交易判定用 TradingActive()。
// English: plane-active (trading OR data-plane) — use TradingActive() for "may place live orders".
func (v BinanceBrokerView) BrokerEnabled() bool {
	return (v.Cfg.Enabled || v.Cfg.DataPlane) && v.profile().Enabled
}

// TradingActive §MR-1 交易面活跃：仅 Enabled（数据面永不满足）且子开关开。真执行器/回报链
// 的构造闸用它——数据面模式下即使误配了凭证也不会出现真单通道。
// English: trading plane requires Cfg.Enabled; the data plane can never satisfy it, so a live
// executor/reporter channel is structurally impossible without the trading switch.
func (v BinanceBrokerView) TradingActive() bool       { return v.Cfg.Enabled && v.profile().Enabled }
func (v BinanceBrokerView) BrokerMode() string        { return v.Cfg.Mode }
func (v BinanceBrokerView) BrokerHalted() bool        { return v.Cfg.Halted }
func (v BinanceBrokerView) BrokerCancelStaleSec() int { return v.Cfg.CancelStaleSec }

// BrokerCloseSweepAt -1：币安侧无收盘清单（CRYPTO 7×24；美股清单由 tradingStatus 流驱动，Phase 1 不做）。
func (v BinanceBrokerView) BrokerCloseSweepAt() int { return -1 }

func (v BinanceBrokerView) BrokerMissHeartbeat() int   { return v.Cfg.MissHeartbeatSec }
func (v BinanceBrokerView) BrokerFixedAmount() float64 { return v.profile().FixedAmount }
func (v BinanceBrokerView) BrokerMaxPositions() int    { return v.profile().MaxPositions }
func (v BinanceBrokerView) BrokerDailyMaxBuys() int    { return v.profile().DailyMaxBuys }
func (v BinanceBrokerView) BrokerDailyBudget() float64 { return v.profile().DailyBudgetAmount }

// BrokerInitialCapital 0=不设资金基线（币安侧初始资金由账户实时权益驱动，P2 接 paper 后补）。
func (v BinanceBrokerView) BrokerInitialCapital() float64 { return 0 }

func (v BinanceBrokerView) BrokerStrategies() []string     { return v.profile().Strategies }
func (v BinanceBrokerView) BrokerBlacklist() []string      { return v.profile().Blacklist }
func (v BinanceBrokerView) BrokerRiskGate() RiskGateConfig { return v.Cfg.RiskGate }

// BrokerShortMarginRate §MR-4A 市场档案做空保证金率（0=禁做空）。刻意不入 BrokerConfig 接口：
// 这是币安侧档案专属字段，风控闸内用 moneyOf 同姿势的类型断言取用，QMT 视图恒落 0=禁用。
// English: per-market short margin rate (0=disabled); kept off the BrokerConfig interface
// (binance-profile-only field, consumed via type assertion with the moneyOf pattern).
func (v BinanceBrokerView) BrokerShortMarginRate() float64 { return v.profile().ShortMarginRate }

// FuturesActive §MR-4B 本视图产品线是否为 USDT 本位永续：仅 CRYPTO 档案 + product_type=umfutures
// 同时成立（US 档案 validate 已拒 umfutures，这里双保险再收一次）。执行器/回报/维护节拍
// 的全部合约分叉以本方法为唯一判定点。
// English: §MR-4B single decision point of the futures fork — CRYPTO view with the umfutures
// product line; US views can never be futures (validate rejects it upstream).
func (v BinanceBrokerView) FuturesActive() bool {
	return v.Market == "CRYPTO" && v.profile().ProductType == "umfutures"
}

// BrokerLeverage / BrokerLiqDistMinPct §MR-4B 档案杠杆与强平距离阈值的对外读取口。
func (v BinanceBrokerView) BrokerLeverage() int          { return v.profile().Leverage }
func (v BinanceBrokerView) BrokerLiqDistMinPct() float64 { return v.profile().LiqDistMinPct }

// PaperActive §战法批 纸面成交柜台装配判定：交易面未激活（总开关关）且市场子开关开且
// profile.paper=true。交易面激活时恒 false——真面永远优先，配置了 paper 也只打日志不生效，
// 绝不出现「开着真交易却静默假成交」的危险形态。
// English: §batch-xasset paper desk predicate — only while the live trading plane is off;
// with Enabled=true this always returns false so a real profile can never fake-fill.
func (v BinanceBrokerView) PaperActive() bool {
	return !v.Cfg.Enabled && v.profile().Enabled && v.profile().Paper
}

// DispatchLLMActive 事件打分器启用判定：三键齐备（validate 已保证半截配置进不来）。
func (b BinanceConfig) DispatchLLMActive() bool {
	return b.Events.LLMBaseURL != "" && b.Events.LLMApiKey != "" && b.Events.LLMModel != ""
}

// —— §战法批 派发参数缺省回填（消费侧使用；validate 已做域校验，这里只做 0→缺省）——

// ConfidenceFloor 信号置信度门槛：0=缺省 0.6。
func (d BinanceDispatchConfig) ConfidenceFloor() float64 {
	if d.MinConfidence <= 0 {
		return 0.6
	}
	return d.MinConfidence
}

// EverySecOr 派发节拍秒：0=缺省 600；validate 已挡 <60。
func (d BinanceDispatchConfig) EverySecOr() int {
	if d.EverySec == 0 {
		return 600
	}
	return d.EverySec
}

// MaxNewOrdersPerRound 单轮新单上限：0=缺省 5。
func (d BinanceDispatchConfig) MaxNewOrdersPerRound() int {
	if d.MaxLiveOrders <= 0 {
		return 5
	}
	return d.MaxLiveOrders
}

// BrokerEnforceT1 false：现货与美股均 T+0（币安 equity 结算规则见 disclaimer/API 契约，不走本闸）。
func (v BinanceBrokerView) BrokerEnforceT1() bool { return false }

// BrokerMarket 视图市场优先（装配时已定），空视图兜底按代码形态推断。
func (v BinanceBrokerView) BrokerMarket(code string) string {
	if v.Market != "" {
		return v.Market
	}
	return InferMarketOf(code)
}
