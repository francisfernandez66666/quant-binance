// 文件职责：§P1-e 市场时段抽象（PLAN_BINANCE_MULTI_ASSET §6.10/§10）——
// Session(market) 返回按市场的「时区 + 交易时段 + 交易日 + 收盘清单」规则集：
//
//	CN     → 完全包装现有 trade_time/cntime（A 股链字节级零漂移）；
//	CRYPTO → 7×24 恒真，时区 UTC（评分循环不再被 session 门控跳过）；
//	US     → America/New_York 周一至五 09:30–16:00（RTH 缺省），EXTENDED=04:00–20:00、24H=恒真。
//
// 边界（加市场不换市场）：本文件只新增抽象，不改任何现有 CN 调用点——Controller/评分循环
// 切到 Session(market) 在 Phase 2 Router 接线时执行；美股节假日 P1 为 weekday 粗判，
// calendar WS 流校正在 Phase 3。
//
// English: §P1-e per-market session abstraction. CN delegates to the existing trade_time
// (byte-identical), CRYPTO is always-active in UTC, US uses America/New_York with RTH/EXTENDED/24H
// variants. No existing CN call site changes here — the switch to Session(market) happens with the
// Phase 2 router wiring.
package data

import (
	"log"
	"strings"
	"sync"
	"time"

	"quant-trading-v2/internal/cntime"
)

// SessionRule 单市场的时段规则接口（PLAN §6.10 方法集）。
// English: the per-market session rule set (PLAN §6.10).
type SessionRule interface {
	// Active 该时刻是否处于活跃交易时段（各市场口径见实现注释）。
	Active(t time.Time) bool
	// TradingDay 该日期是否交易日（CN 含法定节假日日历；US P1 为 weekday 粗判）。
	TradingDay(t time.Time) bool
	// Loc 该市场的判定/记账时区（CN=北京、CRYPTO=UTC、US=纽约）。
	Loc() *time.Location
	// CloseSweepHHMM 收盘清单时刻（HHMM）；CN=1452，CRYPTO/US=-1（无收盘清单，跳过）。
	CloseSweepHHMM() int
}

// NormalizeMarketKey 市场键归一化：" " /未知值一律落到 "CN"（与 store 侧空值缺省同口径——
// 加市场不换市场，存量链路不带 market 时行为不变）。
// English: normalizes a market key; anything unknown or empty folds into "CN".
func NormalizeMarketKey(market string) string {
	switch strings.ToUpper(strings.TrimSpace(market)) {
	case "CRYPTO":
		return "CRYPTO"
	case "US":
		return "US"
	default:
		return "CN"
	}
}

// Session 按市场返回时段规则（未知/空=CN，与 NormalizeMarketKey 同源）。
// English: Session returns the rule for a market key (empty/unknown → CN).
func Session(market string) SessionRule {
	switch NormalizeMarketKey(market) {
	case "CRYPTO":
		return cryptoSessionRule{}
	case "US":
		return usSessionRule{window: "RTH"}
	default:
		return cnSessionRule{}
	}
}

// SessionUS 美股时段规则的 tradingSession 变体（PLAN §10）：
// RTH=09:30–16:00、EXTENDED=04:00–20:00、24H=恒真；未知值回落 RTH（缺省最保守）。
// English: US session variants keyed by the order's tradingSession (unknown → RTH).
func SessionUS(tradingSession string) SessionRule {
	w := strings.ToUpper(strings.TrimSpace(tradingSession))
	switch w {
	case "RTH", "EXTENDED", "24H":
	default:
		w = "RTH"
	}
	return usSessionRule{window: w}
}

// —— CN：全部委托现有实现，零复制零漂移 ——

// cnSessionRule A 股规则：包装 trade_time 的活跃时段/交易日判定，时区取 cntime 北京单源。
type cnSessionRule struct{}

func (cnSessionRule) Active(t time.Time) bool     { return IsActiveSession(t) }
func (cnSessionRule) TradingDay(t time.Time) bool { return IsTradingDay(t) }
func (cnSessionRule) Loc() *time.Location         { return cntime.Loc }

// CloseSweepHHMM 1452 与 config.QMTConfig.CloseSweepAt 出厂默认同值；运行时覆盖以配置层为准
// （Controller 现读 cfg.CloseSweepAt，本方法是矩阵/文档口径的缺省权威）。
func (cnSessionRule) CloseSweepHHMM() int { return 1452 }

// —— CRYPTO：7×24 ——

// cryptoSessionRule 加密现货恒开市（币安现货无休市概念），记账时区 UTC。
type cryptoSessionRule struct{}

func (cryptoSessionRule) Active(time.Time) bool     { return true }
func (cryptoSessionRule) TradingDay(time.Time) bool { return true }
func (cryptoSessionRule) Loc() *time.Location       { return time.UTC }
func (cryptoSessionRule) CloseSweepHHMM() int       { return -1 }

// —— US：America/New_York ——

// nyLocOnce 纽约时区惰性加载（裁剪镜像 tzdata 缺失时退回固定 UTC-5——与 cst 同族惯例，
// 代价是夏令时周界判偏移 1 小时，仅影响盘前后 1h 的边缘时刻，启动留痕便于运维发现）。
var (
	nyLocOnce sync.Once
	nyLocVal  *time.Location
)

func nyLocation() *time.Location {
	nyLocOnce.Do(func() {
		l, err := time.LoadLocation("America/New_York")
		if err != nil {
			log.Printf("[market-session] America/New_York 时区库缺失，退回固定 UTC-5（无夏令时）: %v", err)
			l = time.FixedZone("EST-5", -5*3600)
		}
		nyLocVal = l
	})
	return nyLocVal
}

// usSessionRule 美股规则：window 取 RTH/EXTENDED/24H（SessionUS 已做枚举兜底）。
// Active：24H 恒真；其余按纽约当地钟点，周末恒假。
// TradingDay：P1 weekday 粗判（周一至五），正式节假日数据 Phase 2+ 接 calendar 流校正。
type usSessionRule struct{ window string }

func (u usSessionRule) Active(t time.Time) bool {
	if u.window == "24H" {
		return true
	}
	n := t.In(nyLocation())
	wd := n.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return false
	}
	m := n.Hour()*100 + n.Minute()
	if u.window == "EXTENDED" {
		return m >= 400 && m < 2000
	}
	return m >= 930 && m < 1600 // RTH 缺省：09:30–16:00
}

func (u usSessionRule) TradingDay(t time.Time) bool {
	wd := t.In(nyLocation()).Weekday()
	return wd != time.Saturday && wd != time.Sunday
}

func (u usSessionRule) Loc() *time.Location { return nyLocation() }

// CloseSweepHHMM -1：美股收盘清单由 tradingStatus/DAY 过期回报驱动（PLAN §10），不做本地清单。
func (u usSessionRule) CloseSweepHHMM() int { return -1 }
