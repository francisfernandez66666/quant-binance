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
//
// §MR-3 规则式 NYSE 日历（stdlib 计算，零新依赖零外部数据腿）：
// 全日休市=元旦、MLK、总统日、耶稣受难日、阵亡将士纪念日、六月节(2021 起)、独立日、
// 劳工节、感恩节、圣诞节；逢周六的惯例按 NYSE 实务（元旦/独立日/圣诞**不顺延**，周五照常；
// 六月节按提前周五）。半交易日 13:00 早收=7/3（次日非周六）、感恩节次日、12/24（圣诞非周六）。
// 不覆盖全国哀悼日/飓风等临时闭市孤例——低频事件靠运维改配置，规则式日历不为孤例买单。

// usMarketClose 纽约当地时刻 t 当日的收市时刻（HHMM：1600 全日 / 1300 半日）与是否交易日。
func usMarketClose(ny time.Time) (int, bool) {
	wd := ny.Weekday()
	if wd == time.Saturday || wd == time.Sunday {
		return 0, false
	}
	if usMarketHoliday(ny) {
		return 0, false
	}
	if usMarketHalfDay(ny) {
		return 1300, true
	}
	return 1600, true
}

// usMarketHoliday 按年度节假日表命中（表按年份惰性构建并缓存，构建=10 次日期算术）。
func usMarketHoliday(ny time.Time) bool {
	y, mo, d := ny.Date()
	for _, h := range usHolidaysCached(y) {
		if _, hm, hd := h.In(time.UTC).Date(); hm == mo && hd == d {
			return true
		}
	}
	return false
}

// usMarketHalfDay 半交易日判定（仅对工作日调用）：7/3、感恩节次日、12/24。
// 7/3 逢周六（=7/4 周日？不可能）与「次日周六」两种例外都落回全日/休市口径。
func usMarketHalfDay(ny time.Time) bool {
	y, mo, d := ny.Date()
	wd := ny.Weekday()
	switch mo {
	case time.July:
		// 7/3 半日：须工作日，且 7/4 不是周六（周六=不观察节日，7/3 全日）。
		if d != 3 || wd == time.Saturday || wd == time.Sunday {
			return false
		}
		return !usFixedHoliday(y, time.July, 4, false).IsZero()
	case time.November:
		// 感恩节次日（恒为周五）半日。
		tg := nthWeekdayDate(y, time.November, time.Thursday, 4)
		return wd == time.Friday && d == tg.AddDate(0, 0, 1).Day()
	case time.December:
		// 12/24 半日：须工作日，且圣诞不是周六（周六则 12/24 全日）。
		if d != 24 || wd == time.Saturday || wd == time.Sunday {
			return false
		}
		return !usFixedHoliday(y, time.December, 25, false).IsZero()
	}
	return false
}

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
// §MR-3 Active/TradingDay 升级到 NYSE 实际：法定节假日闭市 + 半交易日 13:00 早收
// （见下方 usCalendar* 一族），24H 变体不受日历影响。
// English: US session now uses a rule-based NYSE calendar (holidays + 13:00 half-days).
type usSessionRule struct{ window string }

func (u usSessionRule) Active(t time.Time) bool {
	if u.window == "24H" {
		return true
	}
	n := t.In(nyLocation())
	closeHHMM, ok := usMarketClose(n)
	if !ok {
		return false // 周末/节假日
	}
	m := n.Hour()*100 + n.Minute()
	if u.window == "EXTENDED" {
		// 盘前 04:00 起；盘后收：全日 20:00、半日 17:00（NYSE 早收惯例）。
		extClose := 2000
		if closeHHMM == 1300 {
			extClose = 1700
		}
		return m >= 400 && m < extClose
	}
	return m >= 930 && m < closeHHMM // RTH：09:30–收市（16:00/半日 13:00）
}

func (u usSessionRule) TradingDay(t time.Time) bool {
	_, ok := usMarketClose(t.In(nyLocation()))
	return ok
}

func (u usSessionRule) Loc() *time.Location { return nyLocation() }

// CloseSweepHHMM -1：美股收盘清单由 tradingStatus/DAY 过期回报驱动（PLAN §10），不做本地清单。
func (u usSessionRule) CloseSweepHHMM() int { return -1 }

// —— §MR-3 NYSE 规则式日历 helpers（stdlib 算术，年度惰性构建缓存） ——

var usHolidayCache sync.Map // year(int) -> []time.Time（UTC 正午锚点，仅比月/日）

// usHolidaysCached 年份节假日表缓存入口（10 年内热路径只做一次 map 查找）。
func usHolidaysCached(year int) []time.Time {
	if v, ok := usHolidayCache.Load(year); ok {
		return v.([]time.Time)
	}
	hs := usHolidays(year)
	usHolidayCache.Store(year, hs)
	return hs
}

// usHolidays 构建某年 NYSE 全日休市表。零点值=该年不观察（周六惯例），由 push 过滤。
func usHolidays(year int) []time.Time {
	hs := make([]time.Time, 0, 11)
	push := func(t time.Time) {
		if !t.IsZero() {
			hs = append(hs, t)
		}
	}
	push(usFixedHoliday(year, time.January, 1, false))                     // 元旦（周六不观察）
	hs = append(hs, nthWeekdayDate(year, time.January, time.Monday, 3))    // MLK
	hs = append(hs, nthWeekdayDate(year, time.February, time.Monday, 3))   // 总统日
	hs = append(hs, easterSunday(year).AddDate(0, 0, -2))                  // 耶稣受难日
	hs = append(hs, lastWeekdayDate(year, time.May, time.Monday))          // 阵亡将士纪念日
	push(usFixedHoliday(year, time.June, 19, true))                        // 六月节（2021 起观察）
	push(usFixedHoliday(year, time.July, 4, false))                        // 独立日（周六不观察）
	hs = append(hs, nthWeekdayDate(year, time.September, time.Monday, 1))  // 劳工节
	hs = append(hs, nthWeekdayDate(year, time.November, time.Thursday, 4)) // 感恩节
	push(usFixedHoliday(year, time.December, 25, false))                   // 圣诞（周六不观察）
	return hs
}

// usFixedHoliday 固定日期节的实际休市日：周日→顺延周一；周六→satShift 决定提前周五或
// 返回零点值（NYSE 对元旦/独立日/圣诞不补假）。六月节 2021 才存在，早于此返回零点值。
func usFixedHoliday(year int, m time.Month, dom int, satShift bool) time.Time {
	if m == time.June && dom == 19 && year < 2021 {
		return time.Time{}
	}
	t := time.Date(year, m, dom, 12, 0, 0, 0, time.UTC)
	switch t.Weekday() {
	case time.Saturday:
		if satShift {
			return t.AddDate(0, 0, -1)
		}
		return time.Time{}
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	}
	return t
}

// nthWeekdayDate year 月 month 的第 n 个 wd（UTC 正午锚点，只比日期用）。
func nthWeekdayDate(year int, month time.Month, wd time.Weekday, n int) time.Time {
	first := time.Date(year, month, 1, 12, 0, 0, 0, time.UTC)
	offset := (int(wd) - int(first.Weekday()) + 7) % 7
	return first.AddDate(0, 0, offset+7*(n-1))
}

// lastWeekdayDate year 月 month 的最后一个 wd。
func lastWeekdayDate(year int, month time.Month, wd time.Weekday) time.Time {
	// 下月 1 日回退一天=月末，再从月末向前找 wd。
	last := time.Date(year, month+1, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	diff := (int(last.Weekday()) - int(wd) + 7) % 7
	return last.AddDate(0, 0, -diff)
}

// easterSunday 复活节周日（Anonymous Gregorian 算法，1900–2199 均有效）。
func easterSunday(year int) time.Time {
	a := year % 19
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 7*h + 4*l + 15) / 400
	// 结果月序是"三月系"（0=3月、1=4月，temp 恒 ≤56），Go 的 Month 从 1 起算故 +3；
	// 早前直接 time.Month(0/1) 会把复活节打到上一年 12 月/1 月（探针测出后修正）。
	month, day := (h+l-7*m+21)/31+3, ((h+l-7*m+21)%31)+1
	return time.Date(year, time.Month(month), day, 12, 0, 0, 0, time.UTC)
}
