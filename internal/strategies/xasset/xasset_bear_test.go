// xasset_bear_test.go §战法批 空头腿行为锁：
//  1. 出厂态（BearEnabled=false）逐字节等价——死叉/破位序列 Pass=false 且 gate 原因串
//     与本批前完全同文（负向锁：空头章绝不出现在关态 Details）；
//  2. 开态镜像语义——死叉只在穿越那一根触发（持续空头不误发新空）、慢线走低才放行；
//     RSI 过热破位同事件形；信号必带 ActionShortOpen + 构造市场章；
//  3. XEvent 适配闸——市场章过滤/无代码跳过/中性跳过/置信闸/同标的取最高置信；
//     利空在 BearEnabled 开/关下分别产 short_open/sell；
//  4. newsagent fork——关=旧信号逐字段不动，开=利空卖出改开空，利好不动。
package xasset

import (
	"testing"

	"quant-trading-v2/internal/data"
	"quant-trading-v2/internal/newsagent"
	"quant-trading-v2/internal/strategy"
	"time"
)

// deathCrossBars 死叉序列：20 根缓涨（100..119）+ 4 根急杀（118,112,104,94）——
// 数值解出穿越恰好落在最后一根，且慢线已走低。
func deathCrossBars() []float64 {
	base := make([]float64, 0, 24)
	for i := 0; i < 20; i++ {
		base = append(base, 100+float64(i))
	}
	return append(base, 118, 112, 104, 94)
}

// rsiBreakDownBars 过热破位序列：34 根 ×1.03 单边拉到 RSI=100，再 4 根 ×0.97——
// 末根 RSI 74.8→68.6 恰好跌破上沿（预热区误差已收敛，判定只看末两根）。
func rsiBreakDownBars() []float64 {
	seq := make([]float64, 0, 39)
	v := 100.0
	seq = append(seq, v)
	for i := 0; i < 34; i++ {
		v *= 1.03
		seq = append(seq, v)
	}
	for i := 0; i < 4; i++ {
		v *= 0.97
		seq = append(seq, v)
	}
	return seq
}

// TestMACrossBearLegOffEquivalence 关态逐字节等价：死叉序列在 BearEnabled=false 下
// 不放行、Level=fail、gate 原因串沿用本批前原文、Details 无空头章、不出信号。
func TestMACrossBearLegOffEquivalence(t *testing.T) {
	s, err := NewMACross("US", Params{})
	if err != nil {
		t.Fatal(err)
	}
	ev, _ := s.Evaluate("AAPL", mdOf(deathCrossBars()))
	if ev == nil || ev.Pass {
		t.Fatalf("关态死叉必须不放行: %+v", ev)
	}
	if ev.Level != "fail" || ev.Reasons["gate"] != "本根无金叉事件（位置关系不等于穿越）" {
		t.Fatalf("关态原因串漂移: %+v", ev)
	}
	if ev.Details[evalSideShort] != 0 {
		t.Fatalf("关态不得出现空头章: %+v", ev.Details)
	}
	if sig, _ := s.GenerateSignal("AAPL", ev); sig != nil {
		t.Fatalf("关态 !Pass 必须无信号: %+v", sig)
	}
}

// TestMACrossBearLegOn 开态死叉做空：方向章/信号动作/持续空头不误发。
func TestMACrossBearLegOn(t *testing.T) {
	s, err := NewMACross("CRYPTO", Params{BearEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	ev, _ := s.Evaluate("BTCUSDT", mdOf(deathCrossBars()))
	if ev == nil || !ev.Pass || ev.Level != "full_chain_short" {
		t.Fatalf("开态死叉应放行为空头链: %+v", ev)
	}
	if ev.Details[evalSideShort] != 1 {
		t.Fatalf("空头章缺失: %+v", ev.Details)
	}
	if ev.Confidence <= 0.6 || ev.Confidence > 0.9 {
		t.Fatalf("死叉置信度区间错位: %v", ev.Confidence)
	}
	sig, _ := s.GenerateSignal("BTCUSDT", ev)
	if sig == nil || sig.Action != strategy.ActionShortOpen || sig.Market != "CRYPTO" || sig.Type != strategy.SignalMACross {
		t.Fatalf("空头信号错: %+v", sig)
	}
	// 持续空头（早已死叉、无新穿越事件）：不得重复放行。
	down := make([]float64, 0, 24)
	for i := 0; i < 24; i++ {
		down = append(down, 200-float64(i)*3)
	}
	if ev2, _ := s.Evaluate("BTCUSDT", mdOf(down)); ev2.Pass {
		t.Fatalf("无穿越事件的持续空头不得再放行: %+v", ev2)
	}
	// 开态下金叉多头链不受污染：仍产买入信号、无空头章。
	up := make([]float64, 0, 24)
	for i := 0; i < 20; i++ {
		up = append(up, 100-float64(i))
	}
	up = append(up, 82, 88, 96, 106)
	ev3, _ := s.Evaluate("BTCUSDT", mdOf(up))
	if ev3 == nil || !ev3.Pass || ev3.Level != "full_chain" || ev3.Details[evalSideShort] != 0 {
		t.Fatalf("开态金叉链漂移: %+v", ev3)
	}
	sig3, _ := s.GenerateSignal("BTCUSDT", ev3)
	if sig3 == nil || sig3.Action != strategy.ActionBuy {
		t.Fatalf("金叉仍须产买入: %+v", sig3)
	}
}

// TestRSIMomentumBearLeg 破位腿：关态等价（原因串原文）、开态事件形做空且不重复触发。
func TestRSIMomentumBearLeg(t *testing.T) {
	bars := rsiBreakDownBars()
	off, _ := NewRSIMomentum("US", Params{})
	evOff, _ := off.Evaluate("AAPL", mdOf(bars))
	if evOff == nil || evOff.Pass || evOff.Level != "fail" {
		t.Fatalf("关态破位必须不放行: %+v", evOff)
	}
	if evOff.Reasons["gate"] != "上根未处超卖区：不构成回升事件" || evOff.Details[evalSideShort] != 0 {
		t.Fatalf("关态原因/章漂移: %+v", evOff)
	}
	on, _ := NewRSIMomentum("US", Params{BearEnabled: true})
	evOn, _ := on.Evaluate("AAPL", mdOf(bars))
	if evOn == nil || !evOn.Pass || evOn.Level != "full_chain_short" || evOn.Details[evalSideShort] != 1 {
		t.Fatalf("开态破位应放行: %+v", evOn)
	}
	sig, _ := on.GenerateSignal("AAPL", evOn)
	if sig == nil || sig.Action != strategy.ActionShortOpen || sig.Type != strategy.SignalRSI {
		t.Fatalf("破位信号错: %+v", sig)
	}
	// 破位后再下一根（prev 已 <70）：事件不重复。
	more := append(append([]float64{}, bars...), bars[len(bars)-1]*0.97)
	if ev2, _ := on.Evaluate("AAPL", mdOf(more)); ev2.Pass {
		t.Fatalf("破位事件不得连续重复触发: %+v", ev2)
	}
}

// TestSignalsFromXEvents XEvent 适配闸全矩阵。
func TestSignalsFromXEvents(t *testing.T) {
	now := time.Unix(1700000000, 0)
	evs := []data.XEvent{
		{Market: "US", Ticker: "AAPL", Title: "Apple surges", URL: "u1", PublishedAt: now},
		{Market: "US", Ticker: "MSFT", Title: "MSFT hacked", URL: "u2", PublishedAt: now},
		{Market: "US", Ticker: "TSLA", Title: "nothing read", URL: "u3", PublishedAt: now},
		{Market: "US", Title: "no ticker at all", URL: "u4", PublishedAt: now},
		{Market: "CRYPTO", Symbol: "BTCUSDT", Ticker: "", Title: "btc pumps", URL: "u5", PublishedAt: now},
		{Market: "US", Ticker: "AAPL", Title: "Apple record high again", URL: "u6", PublishedAt: now},
		{Market: "US", Ticker: "NVDA", Title: "weak bullish whisper", URL: "u7", PublishedAt: now},
	}
	senti := map[string]data.XEventSentiment{
		"u1": {Direction: "利好", Confidence: 0.9, Source: "llm"},
		"u2": {Direction: "利空", Confidence: 0.85, Source: "llm"},
		"u3": {Direction: "中性", Confidence: 0, Source: "keyword"},
		"u4": {Direction: "利好", Confidence: 0.9, Source: "llm"},
		"u5": {Direction: "利好", Confidence: 0.95, Source: "llm"},
		"u6": {Direction: "利好", Confidence: 0.7, Source: "keyword"},
		"u7": {Direction: "利空", Confidence: 0.4, Source: "keyword"},
	}
	// 关态（利空→sell）+ 市场章过滤 + 中性/无代码跳过 + 同标的取最高置信。
	sigs, err := SignalsFromXEvents(evs, senti, "US", XEventSignalOptions{NowFn: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	// 默认 MinConfidence=0=不再加闸（闸值归派发腿管），本批 3 条有向票全出。
	if len(sigs) != 3 {
		t.Fatalf("US 关态应剩 AAPL/MSFT/NVDA 三条: %+v", sigs)
	}
	if sigs[0].Code != "AAPL" || sigs[0].Action != strategy.ActionBuy || sigs[0].Confidence != 0.9 {
		t.Fatalf("AAPL 应取 u1 高票买入: %+v", sigs[0])
	}
	if sigs[1].Code != "MSFT" || sigs[1].Action != strategy.ActionSell || sigs[1].Market != "US" {
		t.Fatalf("MSFT 利空关态必须平多卖出: %+v", sigs[1])
	}
	// 开态：利空→开空（两条利空都翻）。
	bear, err := SignalsFromXEvents(evs, senti, "US", XEventSignalOptions{BearEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(bear) != 3 || bear[1].Action != strategy.ActionShortOpen || bear[2].Action != strategy.ActionShortOpen || bear[0].Action != strategy.ActionBuy {
		t.Fatalf("开态利空必须产 short_open、利好不动: %+v", bear)
	}
	// 置信闸：MinConfidence=0.5 挡掉 u7 弱票、且不影响本批其余（NVDA 本就不在结果里）；
	// 单独投喂 NVDA 验证闸门真拦。
	weak, _ := SignalsFromXEvents(evs[6:], senti, "US", XEventSignalOptions{MinConfidence: 0.5})
	if len(weak) != 0 {
		t.Fatalf("低于闸值的事件票必须被拦: %+v", weak)
	}
	weak2, _ := SignalsFromXEvents(evs[6:], senti, "US", XEventSignalOptions{MinConfidence: 0.3})
	if len(weak2) != 1 || weak2[0].Code != "NVDA" {
		t.Fatalf("闸值放到 0.3 应放行弱票: %+v", weak2)
	}
	// CRYPTO 腿：代码取 Symbol，US 事件被市场章挡下。
	crypto, err := SignalsFromXEvents(evs, senti, "CRYPTO", XEventSignalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(crypto) != 1 || crypto[0].Code != "BTCUSDT" || crypto[0].Action != strategy.ActionBuy {
		t.Fatalf("CRYPTO 腿应只收币对事件: %+v", crypto)
	}
	if _, err := SignalsFromXEvents(evs, senti, "CN", XEventSignalOptions{}); err == nil {
		t.Fatal("CN 市场键必须拒（市场收口同三法）")
	}
}

// TestSignalsFromEventsBearFork newsagent 链 fork：关=逐字段不动，开=利空卖出→开空、利好不动。
func TestSignalsFromEventsBearFork(t *testing.T) {
	evs := []newsagent.NewsEvent{
		{IsMaterial: true, Level: "个股", Direction: "利好", Score: 8, Title: "财报超预期", CleanedStocks: []string{"苹果|AAPL"}},
		{IsMaterial: true, Level: "个股", Direction: "利空", Score: -6, Title: "集体诉讼", CleanedStocks: []string{"微软|MSFT"}},
	}
	off, err := SignalsFromEventsBear(evs, "US", false)
	if err != nil {
		t.Fatal(err)
	}
	onSig, err := SignalsFromEvents(evs, "US")
	if err != nil {
		t.Fatal(err)
	}
	if len(off) != len(onSig) {
		t.Fatalf("关态 fork 必须与老口子等长")
	}
	for i := range off {
		if off[i].Action != onSig[i].Action || off[i].Confidence != onSig[i].Confidence || off[i].Reason != onSig[i].Reason {
			t.Fatalf("关态 fork 改动了旧字段: %+v vs %+v", off[i], onSig[i])
		}
	}
	on, err := SignalsFromEventsBear(evs, "US", true)
	if err != nil {
		t.Fatal(err)
	}
	if on[0].Action != strategy.ActionBuy {
		t.Fatalf("利好信号不得被 fork: %+v", on[0])
	}
	if on[1].Action != strategy.ActionShortOpen || on[1].Code != "MSFT" {
		t.Fatalf("利空开态必须改产开空: %+v", on[1])
	}
}
