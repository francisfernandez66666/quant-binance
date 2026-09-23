// 文件职责：候选事件容器 XEvent 的去重键锁（xasset_events.go）。
// 三条边界：① 同 URL+同 UTC 日 → 只留首条（URL 相同但一条缺时间=零值日键，与
// 有时间的行不同键，天然保留——本锁如实钉死该口径）；② 同日不同 URL 全保留；
// ③ 同 URL 跨日（含时区偏移折到不同 UTC 日）全保留。日键=PublishedAt.UTC() YYYY-MM-DD。
package data

import (
	"testing"
	"time"
)

func TestXEventDedupeKeys(t *testing.T) {
	day1 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	day1LateUTC := day1.Add(2 * time.Hour)
	// 东八区的 "9月2日 07:00" = UTC 9月1日 23:00 → 与 day1 同「UTC 日键」。
	day1CST := time.Date(2026, 9, 2, 7, 0, 0, 0, time.FixedZone("CST", 8*3600))
	day2 := day1.Add(24 * time.Hour)

	in := []XEvent{
		{Market: "US", Ticker: "AAPL", URL: "https://x/1", PublishedAt: day1},
		{Market: "US", Ticker: "AAPL", URL: "https://x/1", PublishedAt: day1LateUTC},        // 同 URL 同日 → 去
		{Market: "US", Ticker: "AAPL", URL: "https://x/1", PublishedAt: day1CST},            // 时区折回同 UTC 日 → 去
		{Market: "US", Ticker: "MSFT", URL: "https://x/2", PublishedAt: day1LateUTC},        // 同日不同 URL → 留
		{Market: "CRYPTO", Symbol: "BTCUSDT", URL: "https://x/1", PublishedAt: day2},        // 同 URL 跨日 → 留
		{Market: "CRYPTO", Symbol: "ETHUSDT", URL: "https://x/3", PublishedAt: time.Time{}}, // 零值日键独立一档
		{Market: "CRYPTO", Symbol: "ETHUSDT", URL: "https://x/3", PublishedAt: time.Time{}}, // 零值+同 URL 重复 → 去
	}
	out := DedupeEvents(in)
	want := []string{"https://x/1|day1", "https://x/2|day1", "https://x/1|day2", "https://x/3|zero"}
	if len(out) != len(want) {
		t.Fatalf("去重结果 want=%d got=%d (%+v)", len(want), len(out), out)
	}
	gotKeys := []string{}
	for _, ev := range out {
		key := ev.URL
		switch {
		case ev.PublishedAt.IsZero():
			key += "|zero"
		case ev.PublishedAt.UTC().Equal(day1.UTC()) || ev.PublishedAt.UTC().Equal(day1LateUTC.UTC()) || ev.PublishedAt.UTC().Equal(day1CST.UTC()):
			key += "|day1"
		default:
			key += "|day2"
		}
		gotKeys = append(gotKeys, key)
	}
	for i := range want {
		if gotKeys[i] != want[i] {
			t.Fatalf("第 %d 条 want=%s got=%s（顺序须保持首现序）", i, want[i], gotKeys[i])
		}
	}
	// 首条保留的是最早出现的那条（AAPL/day1 原值未被后两条覆盖）。
	if out[0].Ticker != "AAPL" || !out[0].PublishedAt.Equal(day1) {
		t.Fatalf("去重应留首现: %+v", out[0])
	}
	// 空输入/nil 幂等。
	if len(DedupeEvents(nil)) != 0 {
		t.Fatal("nil 输入应得 nil/空")
	}
}
