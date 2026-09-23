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
	Enabled            bool                 `json:"enabled"`     // 总开关（同 qmt.enabled 语义；出厂 false）
	Mode               string               `json:"mode"`        // auto | manual
	Testnet            bool                 `json:"testnet"`     // true=testnet.binance.vision（仅现货有意义）
	APIKey             string               `json:"api_key"`     // 行情/交易 API Key
	APISecret          string               `json:"api_secret"`  // API Secret（per-user 落库，同 qmt.token 惯例）
	TimeoutSec         int                  `json:"timeout_sec"` // REST 超时
	MissHeartbeatSec   int                  `json:"miss_heartbeat_sec"`
	Halted             bool                 `json:"halted"`                         // kill-switch（即时生效，不入待生效队列）
	CancelStaleSec     int                  `json:"cancel_stale_sec"`               // 在途超时撤单（-1=关）
	DisclaimerSignedAt string               `json:"disclaimer_signed_at,omitempty"` // 美股披露签署时间（启动探测回填）
	QuoteAsset         string               `json:"quote_asset"`                    // 美股 quoteAsset 缺省（USDC）
	Stock              BinanceMarketProfile `json:"stock"`                          // —— US 市场档案 ——
	Spot               BinanceMarketProfile `json:"spot"`                           // —— CRYPTO 市场档案 ——
	RiskGate           RiskGateConfig       `json:"risk_gate"`
	PaperSeparate      bool                 `json:"paper_separate"` // 新市场纸面盘独立资金池（P2 接 paper）
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
	}
	if b.RiskGate.MaxOrderAmount < 0 {
		return fmt.Errorf("binance.risk_gate.max_order_amount 不能为负（%.2f）", b.RiskGate.MaxOrderAmount)
	}
	// §ENH-B7 滑点回灌比例帽同构域校验（0~0.05）。当前消费面=engine 自动挂价链
	// （autoPlace/sellRealPosition 读 ctrl.QMT().RiskGate），binance 侧仅做输入卫生。
	if b.RiskGate.SlippagePassthrough < 0 || b.RiskGate.SlippagePassthrough > 0.05 {
		return fmt.Errorf("binance.risk_gate.slippage_passthrough 超出范围 0~0.05（实际 %.4f）", b.RiskGate.SlippagePassthrough)
	}
	// 启用一致性：总开关开 ⇒ 凭证齐 + 至少一个子市场开
	if b.Enabled {
		if b.APIKey == "" || b.APISecret == "" {
			return fmt.Errorf("binance.enabled=true 但 api_key/api_secret 为空")
		}
		if !b.Stock.Enabled && !b.Spot.Enabled {
			return fmt.Errorf("binance.enabled=true 但 stock/spot 子开关全部关闭")
		}
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

func (v BinanceBrokerView) BrokerEnabled() bool       { return v.Cfg.Enabled && v.profile().Enabled }
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

// BrokerEnforceT1 false：现货与美股均 T+0（币安 equity 结算规则见 disclaimer/API 契约，不走本闸）。
func (v BinanceBrokerView) BrokerEnforceT1() bool { return false }

// BrokerMarket 视图市场优先（装配时已定），空视图兜底按代码形态推断。
func (v BinanceBrokerView) BrokerMarket(code string) string {
	if v.Market != "" {
		return v.Market
	}
	return InferMarketOf(code)
}
