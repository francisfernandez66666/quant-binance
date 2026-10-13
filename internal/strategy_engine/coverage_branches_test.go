// 文件职责：§FIX1012-M7 核心包分支覆盖补强（strategy_engine 最高 ROI 包，行数最少、提升最快）。
// 覆盖 engine.go 里「无外部数据源依赖」即可触达的分支——纯函数与归因逻辑，不依赖 marketAPI/scanner/ths
// （Evaluate 的行情拉取链路需真实数据源，留给集成层，本文件只锁「不依赖网络也能验证」的分支）。
//
// 选取依据（按 plan M7「分支密度×出错代价」排序）：
//
//	· normalizeCode / absScore / tradingDaysBefore / livePrevClose：纯转换，输入边界多；
//	· cacheReusable（§KLINE-CHAIN-3 昨收锚点判过期）：四态分支，错判会让历史基准混进今日涨跌幅；
//	· countSuccess / collectBearStocks / enrichSectorData：聚合逻辑，去重/空 scanner 早退；
//	· attribution / BuildHotSectors：事件→板块分流（个股级跳过、按 Score 符号分池、同名合并取最大 |score|）。
//
// English: branch tests for strategy_engine covering dependency-free logic only (no marketAPI/scanner needed).
package strategy_engine

import (
	"testing"
	"time"

	"quant-trading-v2/internal/data"
	"quant-trading-v2/internal/newsagent"
)

// ── normalizeCode：前缀/后缀剥离 + 边界 ──────────────────────────────────────
func TestNormalizeCode(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"SH600000", "600000"},
		{"SZ000001", "000001"},
		{"BJ123456", "123456"},
		{"600000.SH", "600000"},
		{"000001.SZ", "000001"},
		{"123456.BJ", "123456"},
		{"600000", "600000"},       // 已干净
		{"  SH600000  ", "600000"}, // 先 trim 再剥离
		{"AB", "AB"},               // 长度 ≤2，不动
		{"SH", "SH"},               // 长度恰好 2，前缀判定不触发
	}
	for _, c := range cases {
		if got := normalizeCode(c.in); got != c.want {
			t.Errorf("normalizeCode(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

// ── absScore：正负零 ──────────────────────────────────────────────────────────
func TestAbsScore(t *testing.T) {
	if v := absScore(5); v != 5 {
		t.Errorf("absScore(5)=%v", v)
	}
	if v := absScore(-5); v != 5 {
		t.Errorf("absScore(-5)=%v", v)
	}
	if v := absScore(0); v != 0 {
		t.Errorf("absScore(0)=%v", v)
	}
}

// ── tradingDaysBefore：交易日回退 + 非法输入兜底 ───────────────────────────────
func TestTradingDaysBefore(t *testing.T) {
	// 2026-10-12 为周一交易日；n=0 不动
	if got := tradingDaysBefore("20261012", 0); got != "20261012" {
		t.Errorf("n=0 应原样返回，got %s", got)
	}
	// n>0：结果应是交易日且不等于输入
	prev := tradingDaysBefore("20261012", 1)
	if prev == "20261012" {
		t.Errorf("n=1 不应等于输入")
	}
	tp, err := time.ParseInLocation("20060102", prev, time.Local)
	if err != nil {
		t.Fatalf("返回非日期：%s", prev)
	}
	if !data.IsTradingDay(tp) {
		t.Errorf("tradingDaysBefore 结果 %s 不是交易日", prev)
	}
	// 非法输入：解析失败原样返回
	if got := tradingDaysBefore("not-a-date", 3); got != "not-a-date" {
		t.Errorf("非法输入应原样返回，got %s", got)
	}
}

// ── cacheReusable（§KLINE-CHAIN-3）：四态 ─────────────────────────────────────
func TestCacheReusable(t *testing.T) {
	// 非库内源（腾讯/东财/新浪等）：不受昨收锚点约束，一律可复用
	if !(&klineCacheEntry{src: "tencent"}).cacheReusable(11) {
		t.Errorf("非库内源应可复用")
	}
	// 库内源 + 昨收缺失（prevClose<=0）：不重写已归一结果，照用
	if !(&klineCacheEntry{src: kLineSrcStore, anchor: 9}).cacheReusable(0) {
		t.Errorf("库内源且 prevClose<=0 应可复用")
	}
	// 库内源 + 锚点一致：可复用
	if !(&klineCacheEntry{src: kLineSrcStore, anchor: 10}).cacheReusable(10) {
		t.Errorf("库内源锚点一致应可复用")
	}
	// 库内源 + 锚点漂移（昨收变了 scale 变）：必须重取
	if (&klineCacheEntry{src: kLineSrcStore, anchor: 10}).cacheReusable(11) {
		t.Errorf("库内源锚点漂移应判不可复用（否则今日涨跌幅混进历史基准）")
	}
}

// ── countSuccess：Price>0 且无 Error 才算成功 ──────────────────────────────────
func TestCountSuccess(t *testing.T) {
	m := map[string]*StockMarketData{
		"a": {Code: "a", Price: 10, Error: ""},       // 成功
		"b": {Code: "b", Price: 0, Error: ""},        // 价 0 不算
		"c": {Code: "c", Price: 5, Error: "timeout"}, // 有错不算
	}
	if n := countSuccess(m); n != 1 {
		t.Errorf("countSuccess=%d, want 1", n)
	}
}

// ── collectBearStocks：跨板块去重 ──────────────────────────────────────────────
func TestCollectBearStocks(t *testing.T) {
	e := New(nil)
	bears := []SectorHot{
		{LeadStocks: []string{"600000", "000001"}},
		{LeadStocks: []string{"000001", "600519"}}, // 000001 重复
	}
	got := e.collectBearStocks(bears)
	seen := map[string]int{}
	for _, c := range got {
		seen[c]++
	}
	if len(got) != 3 {
		t.Errorf("去重后应 3 只，got %d: %v", len(got), got)
	}
	for c, n := range seen {
		if n != 1 {
			t.Errorf("个股 %s 重复出现 %d 次", c, n)
		}
	}
}

// ── enrichSectorData：scanner 为 nil 早退（不 panic、不改 map）─────────────────
func TestEnrichSectorDataNilScanner(t *testing.T) {
	sectors := map[string]*SectorHot{
		"半导体": {Name: "半导体"},
	}
	enrichSectorData(sectors, nil) // 必须安全返回
	if len(sectors) != 1 {
		t.Errorf("nil scanner 不应改动 map，got len=%d", len(sectors))
	}
}

// ── livePrevClose：从实时快照读昨收，取不到返 0 ───────────────────────────────
func TestLivePrevClose(t *testing.T) {
	if v := livePrevClose(nil); v != 0 {
		t.Errorf("nil md 应返 0，got %v", v)
	}
	if v := livePrevClose(&StockMarketData{}); v != 0 {
		t.Errorf("nil Quote 应返 0，got %v", v)
	}
	if v := livePrevClose(&StockMarketData{Quote: &data.StockInfo{PrevClose: 0}}); v != 0 {
		t.Errorf("PrevClose=0 应返 0，got %v", v)
	}
	if v := livePrevClose(&StockMarketData{Quote: &data.StockInfo{PrevClose: 12.5}}); v != 12.5 {
		t.Errorf("PrevClose=12.5 应返 12.5，got %v", v)
	}
}

// ── attribution / BuildHotSectors：事件→板块分流 ─────────────────────────────
func TestAttribution(t *testing.T) {
	e := New(nil) // 不依赖 marketAPI（attribution 仅用 scanner，nil 时 enrichSectorData 早退）

	// 空事件
	bull, bear := e.attribution(nil)
	if len(bull) != 0 || len(bear) != 0 {
		t.Errorf("空事件应无板块，got bull=%d bear=%d", len(bull), len(bear))
	}

	// 个股级事件：顶层直接跳过（不进板块池）
	bull, bear = e.attribution([]newsagent.NewsEvent{
		{Level: "个股", Score: 5, Sectors: []string{"半导体"}, Title: "x"},
	})
	if len(bull) != 0 || len(bear) != 0 {
		t.Errorf("个股级事件应被跳过，got bull=%d bear=%d", len(bull), len(bear))
	}

	// 板块级事件：按 Score 符号分池
	bull, bear = e.attribution([]newsagent.NewsEvent{
		{Level: "板块", Score: 3, Sectors: []string{"半导体"}, Title: "利好"},
		{Level: "板块", Score: -2, Sectors: []string{"白酒"}, Title: "利空"},
	})
	if len(bull) != 1 || bull[0].Name != "半导体" {
		t.Errorf("利好板块应为半导体，got %+v", bull)
	}
	if len(bear) != 1 || bear[0].Name != "白酒" {
		t.Errorf("利空板块应为白酒，got %+v", bear)
	}

	// 同名板块重复事件：合并标题，Score 取最大 |score|
	bull, _ = e.attribution([]newsagent.NewsEvent{
		{Level: "板块", Score: 1, Sectors: []string{"半导体"}, Title: "弱利好"},
		{Level: "板块", Score: 4, Sectors: []string{"半导体"}, Title: "强利好"},
	})
	if len(bull) != 1 {
		t.Fatalf("同名板块应合并为 1，got %d", len(bull))
	}
	if bull[0].Score != 4 {
		t.Errorf("合并后应保留最大 |score|=4，got %v", bull[0].Score)
	}
	if len(bull[0].NewsTitles) != 2 {
		t.Errorf("合并应累计 2 条标题，got %d", len(bull[0].NewsTitles))
	}

	// 空 Sector 字符串：跳过
	bull, bear = e.attribution([]newsagent.NewsEvent{
		{Level: "板块", Score: 2, Sectors: []string{""}, Title: "空板块"},
	})
	if len(bull) != 0 || len(bear) != 0 {
		t.Errorf("空板块名应跳过，got bull=%d bear=%d", len(bull), len(bear))
	}

	// BuildHotSectors 与 attribution 同实现（幂等包装）
	b2, _ := e.BuildHotSectors([]newsagent.NewsEvent{
		{Level: "板块", Score: 3, Sectors: []string{"半导体"}, Title: "t"},
	})
	if len(b2) != 1 || b2[0].Name != "半导体" {
		t.Errorf("BuildHotSectors 应与 attribution 一致，got %+v", b2)
	}
}
