// 文件职责：§MR-3 规则式 NYSE 日历行为锁——2026/2027 已知休市日与半交易日逐点核打
// （节假日全谱+周六不观察惯例+六月节周末位移），以及 RTH/EXTENDED 在早收日的窗口边界。
package data

import (
	"testing"
	"time"
)

func nyDate(y int, mo time.Month, d int) time.Time {
	return time.Date(y, mo, d, 12, 0, 0, 0, nyLocation())
}

func TestUSMarketCalendar(t *testing.T) {
	closed := []struct {
		name string
		at   time.Time
	}{
		{"元旦2026(周四)", nyDate(2026, time.January, 1)},
		{"MLK2026(1月第3周一)", nyDate(2026, time.January, 19)},
		{"总统日2026", nyDate(2026, time.February, 16)},
		{"耶稣受难日2026(复活节4/5前2天)", nyDate(2026, time.April, 3)},
		{"阵亡将士2026(5月最后周一)", nyDate(2026, time.May, 25)},
		{"六月节2026(周五)", nyDate(2026, time.June, 19)},
		{"独立日2026(周六本体)", nyDate(2026, time.July, 4)},
		{"劳工节2026", nyDate(2026, time.September, 7)},
		{"感恩节2026(11月第4周四)", nyDate(2026, time.November, 26)},
		{"圣诞2026(周五)", nyDate(2026, time.December, 25)},
		{"六月节2027位移(6/19周六→6/18周五休)", nyDate(2027, time.June, 18)},
		{"独立日2027位移(6/4? 7/4周日→7/5周一休)", nyDate(2027, time.July, 5)},
	}
	for _, c := range closed {
		if _, ok := usMarketClose(c.at); ok {
			t.Fatalf("%s 应休市，却判为交易日", c.name)
		}
	}
	open := []struct {
		name      string
		at        time.Time
		closeHHMM int
	}{
		{"7/3惯例日2026(7/4周六→不观察,全日)", nyDate(2026, time.July, 3), 1600},
		{"7/6补假不观察2026(周一全日)", nyDate(2026, time.July, 6), 1600},
		{"感恩节次日2026(半日13:00)", nyDate(2026, time.November, 27), 1300},
		{"平安夜2026(半日13:00)", nyDate(2026, time.December, 24), 1300},
		{"7/3惯例日2025(7/4周五→7/3半日)", nyDate(2025, time.July, 3), 1300},
		{"普通周三2026-09-23", nyDate(2026, time.September, 23), 1600},
	}
	for _, c := range open {
		got, ok := usMarketClose(c.at)
		if !ok {
			t.Fatalf("%s 应交易日，却判休市", c.name)
		}
		if got != c.closeHHMM {
			t.Fatalf("%s 收市口径 want=%d got=%d", c.name, c.closeHHMM, got)
		}
	}
}

func TestUSSessionActiveHalfDay(t *testing.T) {
	rth := SessionUS("RTH")
	ext := SessionUS("EXTENDED")
	half := nyDate(2026, time.November, 27) // 感恩节次日 半日
	at := func(hh, mm int) time.Time {
		return time.Date(2026, time.November, 27, hh, mm, 0, 0, nyLocation())
	}
	if !rth.Active(at(12, 45)) {
		t.Fatal("半日 12:45 RTH 应活跃")
	}
	if rth.Active(at(13, 5)) {
		t.Fatal("半日 13:05 RTH 应已收市")
	}
	if !ext.Active(at(16, 30)) {
		t.Fatal("半日 16:30 EXTENDED 应活跃（盘后至 17:00）")
	}
	if ext.Active(at(17, 30)) {
		t.Fatal("半日 17:30 EXTENDED 应已收")
	}
	full := time.Date(2026, time.November, 25, 15, 30, 0, 0, nyLocation())
	if !rth.Active(full) {
		t.Fatal("全日 15:30 RTH 应活跃")
	}
	if rth.Active(time.Date(2026, time.November, 26, 10, 0, 0, 0, nyLocation())) {
		t.Fatal("感恩节当日任何时刻不得活跃")
	}
	if !SessionUS("24H").Active(nyDate(2026, time.July, 4)) {
		t.Fatal("24H 变体不受日历影响")
	}
	_ = half
}
