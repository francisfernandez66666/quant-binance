// handlers_fix_pure_test.go — §AUDIT1019-M6 第二批（2026-10-21）：
// handlers_fix.go 里**零覆盖的纯函数**分支补强。
//
// 背景：handlers_fix.go 是「前端兼容层」的处理器集合（/api/signals、/api/holdings、
// /api/news、/api/alerts… 全在这），是前端主链路的出口；但 server 包 154 个零覆盖函数里，
// 语句量最大的前 8 个全在本文件（handleFixNews 82 / handleFixMinute 56 / …）。这些端点
// handler 需要真引擎 + 真数据源，单测层面硬啃会退化成「为凑数写无用断言」，故本批先啃
// **不依赖外部世界的纯函数**——它们是 handler 的组装零件，改坏了同样会让所有前端列算错，
// 而此前一条断言都没有（改坏不红）。
//
// 选题口径：按「未覆盖语句数 × 是否在前端主链路上」挑，全部零外部依赖（无网络/无 IO/无落盘）。
// 纪律：每条断言对应一个「改坏即红」的具体分支（阈值边界、类型分支、兜底分支），不写恒真断言。
// English: branch tests for previously uncovered pure helpers in handlers_fix.go — the assembly
// parts of the frontend-facing handlers (formatting, dimension mapping, news resolution, lot
// synthesis). No network, no IO, no fixtures.

package server

import (
	"testing"
	"time"

	"quant-trading-v2/internal/combat_agent"
	"quant-trading-v2/internal/display"
	"quant-trading-v2/internal/newsagent"
	"quant-trading-v2/internal/report"
)

// TestNormImpactLevel 影响级别归一：中英双语 + 大小写/空白容忍；未知值原样透出（不吞字）。
// 覆盖点：三个 case 分支各两条输入 + default 兜底。
// English: impact-level normalization — Chinese/English, case & space tolerant, unknown passthrough.
func TestNormImpactLevel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"high", "高"}, {"HIGH", "高"}, {" 高 ", "高"}, // TrimSpace + ToLower
		{"medium", "中"}, {"mid", "中"}, {"中", "中"},
		{"low", "低"}, {"LOW", "低"}, {"低", "低"},
		{"unknown", "unknown"}, // default：原样返回，便于发现新来源而不是静默变"低"
		{"", ""},
	}
	for _, c := range cases {
		if got := normImpactLevel(c.in); got != c.want {
			t.Errorf("normImpactLevel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRoundHelpers r2/r0 的价格/分数取整：正数、负数、不需要进位三种。
// 覆盖点：math.Round 的「半数远离零」语义（负数 -1.234 → -1.23，不是 -1.24）。
// English: rounding helpers — positive, negative, and no-carry cases.
func TestRoundHelpers(t *testing.T) {
	if got := r2(1.234); got != 1.23 {
		t.Errorf("r2(1.234) = %v, want 1.23", got)
	}
	if got := r2(1.236); got != 1.24 {
		t.Errorf("r2(1.236) = %v, want 1.24", got)
	}
	if got := r2(-1.234); got != -1.23 {
		t.Errorf("r2(-1.234) = %v, want -1.23", got)
	}
	if got := r2(0); got != 0 {
		t.Errorf("r2(0) = %v, want 0", got)
	}
	if got := r0(2.6); got != 3 {
		t.Errorf("r0(2.6) = %v, want 3", got)
	}
	if got := r0(2.4); got != 2 {
		t.Errorf("r0(2.4) = %v, want 2", got)
	}
	if got := r0(-2.6); got != -3 {
		t.Errorf("r0(-2.6) = %v, want -3", got)
	}
}

// TestScoreToRemindLevel 提醒级别三档的**边界值**（0.7 / 0.4 取等号侧）：
// 阈值写反（>= 写成 >）或档位顺序调错都会在这里红。
// English: reminder level boundaries (>=0.7 strong, >=0.4 observe, else mute).
func TestScoreToRemindLevel(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{0.7, "strong"},   // 下边界（含等号）
		{0.69, "observe"}, // 差 0.01 即掉档
		{1.0, "strong"},
		{0.4, "observe"}, // 下边界（含等号）
		{0.39, "mute"},
		{0, "mute"},
		{-0.5, "mute"}, // 负分（异常数据）不得判成 strong
	}
	for _, c := range cases {
		if got := scoreToRemindLevel(c.score); got != c.want {
			t.Errorf("scoreToRemindLevel(%v) = %q, want %q", c.score, got, c.want)
		}
	}
}

// TestSignalGeneratedAtText 信号产生时间文本：零值必须返回空串（前端据此显示 '-'），
// 非零值必须是 "2006-01-02 15:04:05" 固定布局（换布局会让前端列宽/排序错乱）。
// English: zero time ⇒ empty (frontend renders '-'); otherwise the fixed layout.
func TestSignalGeneratedAtText(t *testing.T) {
	if got := signalGeneratedAtText(time.Time{}); got != "" {
		t.Errorf("zero time = %q, want empty (frontend shows '-')", got)
	}
	ts := time.Date(2026, 3, 5, 14, 30, 9, 0, time.Local)
	if got, want := signalGeneratedAtText(ts), "2026-03-05 14:30:09"; got != want {
		t.Errorf("signalGeneratedAtText = %q, want %q", got, want)
	}
}

// TestDimScores 各战法 Meta 键 → 统一 d1/d2/d3/d4 的映射（§寻优修复的产物）。
// 覆盖点：空 Meta 早退、dragon / double_bump / dragon_return 三套专属键、default 的 d1..d4。
// 这条断言的价值：此前 toFixSignals 一律读 Meta["d1"]，除 N形外前端 D1~D4 整列为 0；
// 任何一套键名写回旧形态，这里立刻红。
// English: per-strategy Meta key mapping onto d1..d4; guards the §寻优 fix from regressing.
func TestDimScores(t *testing.T) {
	// 空 Meta：必须早退返回全 0，而不是 panic 或读出脏值
	if d1, d2, d3, d4 := dimScores(combat_agent.Signal{}); d1 != 0 || d2 != 0 || d3 != 0 || d4 != 0 {
		t.Errorf("empty meta = (%v,%v,%v,%v), want all 0", d1, d2, d3, d4)
	}
	dragon := combat_agent.Signal{StrategyType: "dragon", Meta: map[string]float64{
		"f1_seal": 1, "f2_resonance": 2, "f3_premium": 3, "f4_rs": 4, "d1": 99,
	}}
	if d1, d2, d3, d4 := dimScores(dragon); d1 != 1 || d2 != 2 || d3 != 3 || d4 != 4 {
		t.Errorf("dragon = (%v,%v,%v,%v), want (1,2,3,4)（不得误读 d1=99）", d1, d2, d3, d4)
	}
	db := combat_agent.Signal{StrategyType: "double_bump", Meta: map[string]float64{
		"vol_score": 5, "adjust_score": 6, "ma_score": 7, "adjust_depth": 8,
	}}
	if d1, d2, d3, d4 := dimScores(db); d1 != 5 || d2 != 6 || d3 != 7 || d4 != 8 {
		t.Errorf("double_bump = (%v,%v,%v,%v), want (5,6,7,8)", d1, d2, d3, d4)
	}
	dr := combat_agent.Signal{StrategyType: "dragon_return", Meta: map[string]float64{
		"dragon_score": 9, "pullback_score": 10, "duck_score": 11, "confirm_score": 12,
	}}
	if d1, d2, d3, d4 := dimScores(dr); d1 != 9 || d2 != 10 || d3 != 11 || d4 != 12 {
		t.Errorf("dragon_return = (%v,%v,%v,%v), want (9,10,11,12)", d1, d2, d3, d4)
	}
	// default：n_shape 原生 d1..d4；未知战法类型同样走 default（新增战法不该读出 0）
	ns := combat_agent.Signal{StrategyType: "n_shape", Meta: map[string]float64{"d1": 13, "d2": 14, "d3": 15, "d4": 16}}
	if d1, d2, d3, d4 := dimScores(ns); d1 != 13 || d2 != 14 || d3 != 15 || d4 != 16 {
		t.Errorf("n_shape = (%v,%v,%v,%v), want (13,14,15,16)", d1, d2, d3, d4)
	}
	unknown := combat_agent.Signal{StrategyType: "some_new_tactic", Meta: map[string]float64{"d1": 17}}
	if d1, _, _, _ := dimScores(unknown); d1 != 17 {
		t.Errorf("unknown strategy d1 = %v, want 17（新战法应落 default 而非 0）", d1)
	}
}

// TestToFixSignals 内部 Signal → 前端 fixSignal 的字段组装（信号页整表的数据源）。
// 覆盖点：空输入返回非 nil 空切片（前端 `out.map` 不炸）、CanOpen 的**双条件**、
// 置信度→总分/提醒级别换算、零值 GeneratedAt 省略。
// English: Signal → fixSignal assembly; guards CanOpen's two-condition rule and score mapping.
func TestToFixSignals(t *testing.T) {
	if out := toFixSignals(nil); out == nil || len(out) != 0 {
		t.Errorf("toFixSignals(nil) = %#v, want non-nil empty slice", out)
	}
	ts := time.Date(2026, 3, 5, 9, 31, 0, 0, time.Local)
	sigs := []combat_agent.Signal{
		// 可开仓：置信度 ≥0.7 且 action == "buy"
		{Code: "600000", Name: "浦发", Strategy: "N形", StrategyType: "n_shape", Action: "buy",
			Confidence: 0.72, Price: 10.5, Sector: "银行", Reason: "放量突破", GeneratedAt: ts,
			Meta: map[string]float64{"d1": 8, "d2": 7, "d3": 6, "d4": 5}},
		// 置信度够但动作是 watch：不可开仓
		{Code: "000001", Name: "平安", Action: "watch", Confidence: 0.8},
		// 动作是 buy 但置信度不足：不可开仓（两个条件缺一不可）
		{Code: "000002", Name: "万科", Action: "buy", Confidence: 0.65},
		// 零值时间：generated_at 必须省略
		{Code: "000003", Name: "国寿", Action: "buy", Confidence: 0.4},
	}
	out := toFixSignals(sigs)
	if len(out) != 4 {
		t.Fatalf("len = %d, want 4", len(out))
	}
	a := out[0]
	if !a.CanOpen {
		t.Error("conf 0.72 + buy ⇒ CanOpen must be true")
	}
	if a.TotalScore < 71.9 || a.TotalScore > 72.1 {
		t.Errorf("TotalScore = %v, want ~72", a.TotalScore)
	}
	if a.RemindLevel != "strong" || a.Level != "交易" || !a.SignalActive {
		t.Errorf("level fields = (%q,%q,%v), want (strong,交易,true)", a.RemindLevel, a.Level, a.SignalActive)
	}
	if a.D1 != 8 || a.D2 != 7 || a.D3 != 6 || a.D4 != 5 {
		t.Errorf("dim scores = (%v,%v,%v,%v), want (8,7,6,5)", a.D1, a.D2, a.D3, a.D4)
	}
	if a.D1Desc != "放量突破" || a.D2Desc != "银行" {
		t.Errorf("desc = (%q,%q), want (放量突破,银行)", a.D1Desc, a.D2Desc)
	}
	if a.GeneratedAt != "2026-03-05 09:31:00" {
		t.Errorf("GeneratedAt = %q, want 2026-03-05 09:31:00", a.GeneratedAt)
	}
	if out[1].CanOpen || out[2].CanOpen {
		t.Error("watch 或 conf<0.7 均不得 CanOpen（双条件缺一即 false）")
	}
	if out[3].GeneratedAt != "" {
		t.Errorf("zero GeneratedAt must be omitted, got %q", out[3].GeneratedAt)
	}
	if out[3].RemindLevel != "observe" {
		t.Errorf("conf 0.4 ⇒ observe, got %q", out[3].RemindLevel)
	}
}

// TestAlertGeneratedAtKey 消息时间键的类型分支：ctrl 路径存 string、看板兜底存 time.Time，
// 其余类型（含 nil）必须返回空串排最后，而不是 panic 或拼出乱序键。
// English: type switch for the message time key — string/time.Time, unknown ⇒ empty (sorts last).
func TestAlertGeneratedAtKey(t *testing.T) {
	if got := alertGeneratedAtKey("2026-03-05T09:31:00Z"); got != "2026-03-05T09:31:00Z" {
		t.Errorf("string = %q", got)
	}
	ts := time.Date(2026, 3, 5, 9, 31, 0, 0, time.UTC)
	want := ts.Format("2006-01-02T15:04:05.000000000Z07:00")
	if got := alertGeneratedAtKey(ts); got != want {
		t.Errorf("time.Time = %q, want %q", got, want)
	}
	if got := alertGeneratedAtKey(nil); got != "" {
		t.Errorf("nil = %q, want empty", got)
	}
	if got := alertGeneratedAtKey(123); got != "" {
		t.Errorf("int = %q, want empty（未知类型不参与排序）", got)
	}
}

// TestNormalizeTitle 标题归一（去空白/标点 + 转小写），用于新闻溯源的模糊匹配。
// 覆盖点：半角/全角空格、中英文标点、大小写；这是一个 strings.NewReplacer 表，
// 表里少一项就会匹配不上，故逐项断言。
// English: title normalization for fuzzy news matching — spaces, punctuation, case.
func TestNormalizeTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{" 你好，世界 ", "你好世界"},    // 半角空格 + 中文逗号
		{"你　好", "你好"},          // 全角空格
		{"A-B—C", "abc"},       // 连字符 + 破折号 + 转小写
		{"（标题）", "标题"},         // 全角括号
		{"(title)", "title"},   // 半角括号
		{"a：b、c。d", "abcd"},    // 全角冒号/顿号/句号
		{"A·B", "ab"},          // 间隔号
		{`"q"'`, "q"},          // 引号
		{"新能源补贴政策", "新能源补贴政策"}, // 无变化
	}
	for _, c := range cases {
		if got := normalizeTitle(c.in); got != c.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestPickBestEvent 同标题候选取正文最长的一条（溯源弹窗要的是可读正文）；
// 空列表必须返回 nil（调用方靠 nil 判"没找到"，返回零值会让弹窗显示空标题）。
// English: pick the longest-content candidate; nil for empty input.
func TestPickBestEvent(t *testing.T) {
	if got := pickBestEvent(nil); got != nil {
		t.Errorf("pickBestEvent(nil) = %#v, want nil", got)
	}
	if got := pickBestEvent([]newsagent.NewsEvent{}); got != nil {
		t.Errorf("pickBestEvent(empty) = %#v, want nil", got)
	}
	list := []newsagent.NewsEvent{
		{Title: "T", Content: "短"},
		{Title: "T", Content: "这是一条明显更长的正文"},
		{Title: "T", Content: "中长度正文"},
	}
	best := pickBestEvent(list)
	if best == nil || best.Content != "这是一条明显更长的正文" {
		t.Fatalf("pickBestEvent = %#v, want the longest content", best)
	}
	// 单元素：直接返回首条
	one := []newsagent.NewsEvent{{Title: "X", Content: "c"}}
	if got := pickBestEvent(one); got == nil || got.Title != "X" {
		t.Errorf("single element = %#v, want X", got)
	}
}

// TestResolveSectorNewsItems 板块 LLM 改写标题 → 真实新闻事件的反查（溯源弹窗数据源）。
// 覆盖点（每条都是一个真实分支）：
//  1. 空标题列表 ⇒ 非 nil 空切片（前端拿到 [] 而不是 null）；
//  2. 查不到事件 ⇒ 仍保留改写标题且 source=llm（弹窗至少显示 LLM 引用的标题，不留白）；
//  3. 精确归一匹配；4. 子串包含匹配；5. 同事件去重；6. 空标题跳过。
//
// English: resolve rewritten titles back to real events — empty/llm-fallback/exact/substring/dedupe.
func TestResolveSectorNewsItems(t *testing.T) {
	s := &Server{} // 无 registry/ctrl ⇒ ctrlFor 返回 nil，走纯 dash 数据源（不碰任何外部依赖）

	// 1) 空列表
	if got := resolveSectorNewsItems(s, "u1", nil, nil); got == nil || len(got) != 0 {
		t.Errorf("empty titles = %#v, want non-nil empty slice", got)
	}

	dash := &display.DashboardData{NewsEvents: []newsagent.NewsEvent{
		{Title: "新能源补贴政策落地", Content: "正文-补贴", Source: "财联社", Direction: "利好", Sectors: []string{"新能源"}},
		// 同标题，正文更长（pickBestEvent 应命中这条）——source 故意与上面不同，
		// 便于断言"取的是正文最长那条"而不是"第一条"。
		{Title: "新能源补贴政策落地", Content: "更完整的正文-补贴公告", Source: "新华社", Direction: "利好", Sectors: []string{"新能源"}},
	}}

	// 2) 查不到 ⇒ 保留改写标题 + source=llm
	miss := resolveSectorNewsItems(s, "u1", dash, []string{"完全无关的标题"})
	if len(miss) != 1 {
		t.Fatalf("miss len = %d, want 1", len(miss))
	}
	if miss[0]["title"] != "完全无关的标题" || miss[0]["source"] != "llm" || miss[0]["content"] != "" {
		t.Errorf("miss item = %#v, want title preserved + source=llm + empty content", miss[0])
	}

	// 3) 精确归一匹配（且取正文更长的那条）
	exact := resolveSectorNewsItems(s, "u1", dash, []string{"新能源补贴政策落地"})
	if len(exact) != 1 {
		t.Fatalf("exact len = %d, want 1", len(exact))
	}
	if exact[0]["content"] != "更完整的正文-补贴公告" {
		t.Errorf("exact content = %v, want the longer one", exact[0]["content"])
	}
	if exact[0]["source"] != "新华社" || exact[0]["direction"] != "利好" {
		t.Errorf("exact fields = %#v, want the longest-content event's fields", exact[0])
	}

	// 4) 子串包含匹配（LLM 改写后的标题被截断）
	sub := resolveSectorNewsItems(s, "u1", dash, []string{"新能源补贴政策"})
	if len(sub) != 1 || sub[0]["title"] != "新能源补贴政策落地" {
		t.Errorf("substring match = %#v, want hit on 新能源补贴政策落地", sub)
	}

	// 5) 两条改写标题命中同一事件 ⇒ 只出一条（去重）
	dedupe := resolveSectorNewsItems(s, "u1", dash, []string{"新能源补贴政策落地", "新能源补贴政策"})
	if len(dedupe) != 1 {
		t.Errorf("dedupe len = %d, want 1（同一事件只展示一次）", len(dedupe))
	}

	// 6) 空标题 / 纯空白标题跳过，不得产出空壳条目，更不得误命中任意事件。
	//    ★ 这是 2026-10-21 修掉的真实缺陷：空白标题归一后是空串，而子串匹配
	//    `strings.Contains(n, "")` 恒为真 ⇒ 会随机命中一条无关新闻（map 遍历顺序不定）。
	//    本条断言即该缺陷的回归锁：把 handlers_fix.go 里的 `if nt == ""` 守卫删掉必红。
	//    English: regression lock — a whitespace-only title must not match an arbitrary event.
	skip := resolveSectorNewsItems(s, "u1", dash, []string{"", "  ", "，。、"})
	if len(skip) != 0 {
		t.Errorf("blank/punctuation-only titles = %#v, want empty", skip)
	}
}

// TestNormalizeNewsTime 资讯时间的多类型归一（数据源给的 datetime 类型不一：
// 有的给 epoch 秒的 float64/int64/int，有的给已格式化的字符串）。
// 覆盖点：nil / float64 / int64 / int / 数字串 / 长串截断到 "MM-DD HH:MM" / 短串原样 /
// 空串 / 未知类型走 fmt.Sprint。任一分支改错都会让资讯页时间列显示错乱或脏数据。
// English: news time normalization across the heterogeneous source types.
func TestNormalizeNewsTime(t *testing.T) {
	sec := int64(1772688000) // 一个固定的 epoch 秒
	want := time.Unix(sec, 0).Format("2006-01-02 15:04")

	if got := normalizeNewsTime(nil); got != "" {
		t.Errorf("nil = %q, want empty", got)
	}
	if got := normalizeNewsTime(float64(sec)); got != want {
		t.Errorf("float64 = %q, want %q", got, want)
	}
	if got := normalizeNewsTime(sec); got != want {
		t.Errorf("int64 = %q, want %q", got, want)
	}
	if got := normalizeNewsTime(int(sec)); got != want {
		t.Errorf("int = %q, want %q", got, want)
	}
	// 数字字符串按 epoch 解析（不可当成已格式化文本直接截断）
	if got := normalizeNewsTime("1772688000"); got != want {
		t.Errorf("numeric string = %q, want %q", got, want)
	}
	// 已格式化：长度 ≥16 时截到分钟（去掉秒）
	if got := normalizeNewsTime("2026-03-05 09:31:00"); got != "2026-03-05 09:31" {
		t.Errorf("formatted = %q, want 2026-03-05 09:31", got)
	}
	// 短串原样返回
	if got := normalizeNewsTime("03-05 09:31"); got != "03-05 09:31" {
		t.Errorf("short string = %q, want unchanged", got)
	}
	if got := normalizeNewsTime("   "); got != "" {
		t.Errorf("blank string = %q, want empty", got)
	}
	// 未知类型：退化成 fmt.Sprint，不得 panic
	if got := normalizeNewsTime(true); got != "true" {
		t.Errorf("bool = %q, want true", got)
	}
}

// TestNewsTimeFromEpoch epoch → 展示文本；非正数必须返回空串，
// 否则 0 值会被格式化成 1970-01-01 08:00（脏数据直接上前端）。
// English: epoch → display text; non-positive ⇒ empty (never render 1970-01-01).
func TestNewsTimeFromEpoch(t *testing.T) {
	if got := newsTimeFromEpoch(0); got != "" {
		t.Errorf("0 = %q, want empty", got)
	}
	if got := newsTimeFromEpoch(-1); got != "" {
		t.Errorf("-1 = %q, want empty", got)
	}
	sec := int64(1772688000)
	if got, want := newsTimeFromEpoch(sec), time.Unix(sec, 0).Format("2006-01-02 15:04"); got != want {
		t.Errorf("valid epoch = %q, want %q", got, want)
	}
}

// TestTruncateTitle 按 rune 截断（中文安全）：多字节字符不得被切半产生乱码；
// 长度不足时原样返回；maxLen=0 返回空串。
// English: rune-safe truncation — no broken UTF-8, unchanged when short.
func TestTruncateTitle(t *testing.T) {
	if got := truncateTitle("你好世界", 10); got != "你好世界" {
		t.Errorf("short = %q, want unchanged", got)
	}
	if got := truncateTitle("你好世界", 2); got != "你好" {
		t.Errorf("truncate 2 runes = %q, want 你好", got)
	}
	if got := truncateTitle("你好世界", 0); got != "" {
		t.Errorf("maxLen 0 = %q, want empty", got)
	}
	if got := truncateTitle("abcdef", 3); got != "abc" {
		t.Errorf("ascii = %q, want abc", got)
	}
}

// TestHoldingLots 加仓批次明细：有批次记录时原样返回；
// 旧数据（无批次）用「开仓价 + 数量」合成一条，数量为 0 的脏数据兜底成 1
// ——前端明细表要有数据可展示，且不能出现 0 数量的行。
// English: lot details — passthrough when present, synthetic fallback for legacy rows.
func TestHoldingLots(t *testing.T) {
	lots := []report.Lot{{Price: 10, Quantity: 100}}
	if got := holdingLots(report.ExecLog{Lots: lots}); len(got) != 1 || got[0].Quantity != 100 {
		t.Errorf("with lots = %#v, want passthrough", got)
	}
	// 无批次：合成一条（以开仓价/数量为准）
	got := holdingLots(report.ExecLog{EntryPrice: 8.5, Quantity: 200, EntryAt: time.Unix(1772688000, 0)})
	if len(got) != 1 || got[0].Price != 8.5 || got[0].Quantity != 200 {
		t.Errorf("synthetic lot = %#v, want {8.5, 200}", got)
	}
	// 数量 ≤0（脏数据）：兜底成 1，前端不出现 0 数量行
	got = holdingLots(report.ExecLog{EntryPrice: 8.5})
	if len(got) != 1 || got[0].Quantity != 1 {
		t.Errorf("zero qty = %#v, want quantity 1", got)
	}
}

// TestBuildHoldingFallback 持仓组装的**行情缺失兜底路径**：
// 未注入行情源（fetcher/dc/market 全空）时，现价必须回退到开仓价、盈亏为 0，
// 而不是 panic 或算出 NaN（此前 GetRealtimeQuote 内访问 nil market 会 SIGSEGV 整链崩溃）。
// 看板缺失（agg 零值）时评分段整体跳过，其余字段照常组装。
// English: buildHolding with no quote source ⇒ fall back to entry price, pnl 0, no panic.
func TestBuildHoldingFallback(t *testing.T) {
	// agg 用零值聚合器：Current() 返回 nil ⇒ dashFor 走缓存回退（cacheDir 空 ⇒ 直接 nil）
	s := &Server{agg: &display.Aggregator{}}
	h := s.buildHolding(report.ExecLog{
		SignalID:      "sig-1",
		Code:          "600000",
		Name:          "浦发银行",
		EntryPrice:    10,
		Quantity:      100,
		TakeProfitPct: 10,
		StopLossPct:   5,
		HighestPrice:  11,
	}, "u1")
	if h.Code != "600000" || h.Name != "浦发银行" {
		t.Errorf("code/name = (%q,%q)", h.Code, h.Name)
	}
	if h.CurPrice != 10 || h.CostPrice != 10 {
		t.Errorf("no quote ⇒ cur=cost=10, got cur=%v cost=%v", h.CurPrice, h.CostPrice)
	}
	if h.ChangePct != 0 || h.PnlPct != 0 {
		t.Errorf("no quote ⇒ change/pnl = 0, got %v / %v", h.ChangePct, h.PnlPct)
	}
	if h.Quantity != 100 {
		t.Errorf("quantity = %v, want 100", h.Quantity)
	}
	// 止盈/止损目标价由开仓价推导（不依赖行情）
	if h.TakeProfit != 11 || h.StopLoss != 9.5 {
		t.Errorf("take/stop = (%v,%v), want (11,9.5)", h.TakeProfit, h.StopLoss)
	}
	if h.HighestPrice != 11 {
		t.Errorf("highest = %v, want 11", h.HighestPrice)
	}
	// 无看板 ⇒ 评分全 0 且 signal_active=false，不得凭空置 true
	if h.SignalActive || h.NSscore != 0 || h.DragonScore != 0 {
		t.Errorf("no dash ⇒ scores 0 & inactive, got %#v", h)
	}
	// 无批次记录 ⇒ 合成一条（与 holdingLots 同口径）
	if len(h.Lots) != 1 || h.Lots[0].Quantity != 100 {
		t.Errorf("lots = %#v, want one synthetic lot with qty 100", h.Lots)
	}
	// 数量 ≤0 兜底 1
	h2 := s.buildHolding(report.ExecLog{Code: "000001", EntryPrice: 5}, "u1")
	if h2.Quantity != 1 {
		t.Errorf("zero quantity ⇒ 1, got %v", h2.Quantity)
	}
}
