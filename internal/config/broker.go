// 文件职责：BrokerConfig 多券商配置接口（PLAN_BINANCE_MULTI_ASSET §4.3）+ QMTConfig 的接口实现。
// Phase 1 只做"接口 + 方法集"：QMTConfig 零字段、零 JSON tag 变化（CN 链零回归铁律），
// Controller/Gate 的消费方泛型化在 Phase 2（Router 接线 + Controller broker 参数化）执行。
package config

import (
	"regexp"
	"strings"
)

// BrokerConfig 抽象"一个实盘券商的下单侧配置"：CN=QMTConfig，US/CRYPTO=BinanceConfig（P1-b）。
// 设计原则：只暴露 Controller/Gate 实际消费的标量访问器，复合结构（Money/Advice/Discipline/Settle）
// 暂不入接口——QMT 专属语义，Binance 侧对应物在 Phase 2 按需要扩。
type BrokerConfig interface {
	BrokerEnabled() bool             // ← Enabled 总开关
	BrokerMode() string              // ← Mode（auto/manual）
	BrokerHalted() bool              // ← Halted kill-switch
	BrokerCancelStaleSec() int       // ← CancelStaleSec 未成交自动撤单阈值
	BrokerCloseSweepAt() int         // ← CloseSweepAt 收盘清单时刻；Binance 返回 -1（无收盘清单）
	BrokerMissHeartbeat() int        // ← MissHeartbeatSec 心跳超时熔断
	BrokerFixedAmount() float64      // ← FixedAmount 单票买入金额
	BrokerMaxPositions() int         // ← MaxPositions
	BrokerDailyMaxBuys() int         // ← DailyMaxBuys
	BrokerDailyBudget() float64      // ← DailyBudgetAmount
	BrokerInitialCapital() float64   // ← InitialCapital
	BrokerStrategies() []string      // ← Strategies 策略白名单
	BrokerBlacklist() []string       // ← Blacklist 下单黑名单
	BrokerRiskGate() RiskGateConfig  // ← RiskGate 风控闸参数
	BrokerEnforceT1() bool           // T+1 守卫：CN=true；CRYPTO/US=false（T+0）
	BrokerMarket(code string) string // 由代码形态推断市场（Router 用 req.Market 直判，此为兜底）
}

// 编译期锁：QMTConfig 必须满足 BrokerConfig（方法集遗漏即编译失败）。
var _ BrokerConfig = QMTConfig{}

// —— QMTConfig 接口实现：一行包装现有字段，零行为变化 ——

func (q QMTConfig) BrokerEnabled() bool            { return q.Enabled }
func (q QMTConfig) BrokerMode() string             { return q.Mode }
func (q QMTConfig) BrokerHalted() bool             { return q.Halted }
func (q QMTConfig) BrokerCancelStaleSec() int      { return q.CancelStaleSec }
func (q QMTConfig) BrokerCloseSweepAt() int        { return q.CloseSweepAt }
func (q QMTConfig) BrokerMissHeartbeat() int       { return q.MissHeartbeatSec }
func (q QMTConfig) BrokerFixedAmount() float64     { return q.FixedAmount }
func (q QMTConfig) BrokerMaxPositions() int        { return q.MaxPositions }
func (q QMTConfig) BrokerDailyMaxBuys() int        { return q.DailyMaxBuys }
func (q QMTConfig) BrokerDailyBudget() float64     { return q.DailyBudgetAmount }
func (q QMTConfig) BrokerInitialCapital() float64  { return q.InitialCapital }
func (q QMTConfig) BrokerStrategies() []string     { return q.Strategies }
func (q QMTConfig) BrokerBlacklist() []string      { return q.Blacklist }
func (q QMTConfig) BrokerRiskGate() RiskGateConfig { return q.RiskGate }
func (q QMTConfig) BrokerEnforceT1() bool          { return q.EnforceT1Enabled() }

// BrokerMarket QMT 只服务 A 股账户：识别出非 CN 形态的代码也返回 "CN"（下游 validTsCode 会拒），
// 推断逻辑仅用于日志/兜底展示。
func (q QMTConfig) BrokerMarket(code string) string { return InferMarketOf(code) }

// —— 代码形态 → 市场 推断（PLAN §5.3 三正则的共享实现；store 侧 validTsCode 在 P1-c 复用）——

var (
	brokerCnTsCodeRe  = regexp.MustCompile(`^[0-9]{6}\.(SH|SZ|BJ)$`)
	brokerQuotePairRe = regexp.MustCompile(`(USDT|USDC|BUSD|FDUSD|TUSD|USDD|DAI|BTC|ETH|BNB|EUR|TRY|JPY|AUD)$`)
	brokerUsTickerRe  = regexp.MustCompile(`^[A-Z][A-Z0-9.]{0,9}$`)
)

// InferMarketOf 由代码形态推断市场："600519.SH"→CN、"BTCUSDT"→CRYPTO、"AAPL"/"BRK.B"→US。
// 判定顺序即消歧顺序：
//  1. A 股带后缀码（唯一 6 位数字形态）优先；
//  2. 含 "." 且非 CN → US 点分代码（BRK.B）；
//  3. 币安计价币对尾缀（USDT/BTC/…）→ CRYPTO；
//  4. 纯大写字母数字且 ≤10 位 → US（AAPL）；
//  5. 缺省 CN——与存量链路默认市场一致（加市场不换市场）。
//
// 注意纯字母冲突（BTCUSDT 与 AAPL 同形）：靠尾缀白名单区分，universe 配置仍显式带 market 段，
// 本函数只做兜底，不参与交易对合法性判定（合法性在 validTsCode(market, code)）。
func InferMarketOf(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return "CN"
	}
	if brokerCnTsCodeRe.MatchString(code) {
		return "CN"
	}
	if strings.Contains(code, ".") {
		return "US"
	}
	if brokerQuotePairRe.MatchString(code) {
		return "CRYPTO"
	}
	if brokerUsTickerRe.MatchString(code) {
		return "US"
	}
	return "CN"
}
