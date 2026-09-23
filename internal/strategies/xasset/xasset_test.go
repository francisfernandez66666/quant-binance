// xasset_test.go 锁新市场最小战法包的四条契约：
//  1. 市场收口——构造期只收 US/CRYPTO，CN 必拒（本包绝不参与 A 股链路）；
//  2. Market 章等值——凡放行的信号 Signal.Market 必等于构造市场（"信号产出直接带 Market"）；
//  3. 穿越事件语义——金叉只在 fast 上穿 slow 的那一根触发（持续多头不误发），
//     RSI 只在超卖回升那一根触发，且 nodata（K 线不足/全 0 收盘）绝不 Pass；
//  4. 事件适配三关——非个股级/非 Material/中性方向不出信号，代码解析失败跳过。
package xasset

// _T_HELPERS 造盘工具
import (
	"math"
	"testing"

	"quant-trading-v2/internal/data"
	"quant-trading-v2/internal/newsagent"
	"quant-trading-v2/internal/strategy"
	"quant-trading-v2/internal/strategy_engine"
)

// barsK 造收盘序列（open/high/low 同步于 close，Volume=1）。
func barsK(closes []float64) []data.KLine {
	out := make([]data.KLine, 0, len(closes))
	for _, c := range closes {
		out = append(out, data.KLine{Open: c, High: c, Low: c, Close: c, Volume: 1})
	}
	return out
}

// mdOf 造实盘行情（Price=末根收盘，模拟"盘中最新价覆盖"路径）。
func mdOf(closes []float64) *strategy_engine.StockMarketData {
	return &strategy_engine.StockMarketData{Code: "X", KLines: barsK(closes), Price: closes[len(closes)-1]}
}

// _T_MARKET 市场收口锁

// TestXAssetMarketCensusLock 构造期市场收口 + 策略接口三件套自检。
func TestXAssetMarketCensusLock(t *testing.T) {
	if _, err := NewMACross("CN", Params{}); err == nil {
		t.Fatal("CN 必须被 xasset 拒绝")
	}
	if _, err := NewRSIMomentum("", Params{}); err == nil {
		t.Fatal("空 Market（归一 CN）必须被拒")
	}
	if _, err := NewNewsEvents("bad-market"); err == nil {
		t.Fatal("未知市场键必须被拒")
	}
	ma, err := NewMACross("crypto", Params{FastN: 20, SlowN: 5})
	if err == nil || ma != nil {
		t.Fatalf("快窗>=慢窗必须构造失败: %v", err)
	}
	// 接口完备性：三法都可当 strategy.Strategy 用（注册链路零特判）。
	a, _ := NewMACross("US", Params{})
	b, _ := NewRSIMomentum("US", Params{})
	c, _ := NewNewsEvents("US")
	for _, s := range []strategy.Strategy{a, b, c} {
		if s.Name() == "" || s.Type() == "" {
			t.Fatalf("策略 %T 的 Name/Type 不得为空", s)
		}
	}
}

// _T_MA 金叉锁

// TestMACrossGoldenCrossAndMarket MA 金叉：事件触发一根、持续多头不误发、信号必带 US 章。
func TestMACrossGoldenCrossAndMarket(t *testing.T) {
	s, err := NewMACross("US", Params{})
	if err != nil {
		t.Fatal(err)
	}
	// 20 根缓跌 + 4 根急拉（数值解出：上穿事件恰好落在最后一根，且慢线已抬头）。
	base := make([]float64, 0, 24)
	for i := 0; i < 20; i++ {
		base = append(base, 100-float64(i))
	}
	base = append(base, 82, 88, 96, 106)
	ev, err := s.Evaluate("AAPL", mdOf(base))
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil || !ev.Pass || ev.Level != "full_chain" {
		t.Fatalf("金叉应放行: %+v", ev)
	}
	sig, err := s.GenerateSignal("AAPL", ev)
	if err != nil || sig == nil {
		t.Fatalf("放行后必须出信号: %v %+v", err, sig)
	}
	if sig.Market != "US" || sig.Type != strategy.SignalMACross || sig.Action != strategy.ActionBuy {
		t.Fatalf("信号章/类型/方向错: %+v", sig)
	}
	if sig.Confidence <= 0 || sig.Confidence > 0.9 {
		t.Fatalf("置信度越界: %v", sig.Confidence)
	}
	// 持续多头（早已金叉、无上穿事件）：同一根位置关系不再触发。
	up := make([]float64, 0, 23)
	for i := 0; i < 23; i++ {
		up = append(up, 100+float64(i))
	}
	ev2, _ := s.Evaluate("AAPL", mdOf(up))
	if ev2.Pass {
		t.Fatal("无上穿事件的持续多头不得重复放行")
	}
	if sig2, _ := s.GenerateSignal("AAPL", ev2); sig2 != nil {
		t.Fatal("!Pass 必须返回 nil 信号（与 CN 战法同惯例）")
	}
	// nodata：K 线不足 22 根、全 0 收盘、nil 行情，都不得 Pass。
	zeroBars := make([]float64, 22)
	for name, md := range map[string]*strategy_engine.StockMarketData{
		"短序列":  {KLines: barsK([]float64{1, 2, 3}), Price: 3},
		"全零收盘": {KLines: barsK(zeroBars), Price: 0},
		"nil盘": nil,
	} {
		evn, _ := s.Evaluate("X", md)
		if evn == nil || evn.Pass || evn.Level != "nodata" {
			t.Fatalf("%s：必须 nodata 不放行: %+v", name, evn)
		}
	}
}

// _T_RSI 动量锁

// TestRSIMomentumOversoldRebound RSI：超卖回升一根触发、过热不追、nodata 拒评。
func TestRSIMomentumOversoldRebound(t *testing.T) {
	s, err := NewRSIMomentum("CRYPTO", Params{})
	if err != nil {
		t.Fatal(err)
	}
	// 深跌入超卖后逐根复利急拉：末根 RSI 由 16.5 上穿 30 且 <70（数值解出的形状）。
	seq := make([]float64, 0, 37)
	v := 100.0
	for i := 0; i < 34; i++ {
		v *= 0.97
		seq = append(seq, v)
	}
	for _, m := range []float64{1.05, 1.06, 1.12} {
		v *= m
		seq = append(seq, v)
	}
	ev, err := s.Evaluate("BTCUSDT", mdOf(seq))
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil || !ev.Pass {
		t.Fatalf("超卖回升应放行: %+v", ev)
	}
	sig, _ := s.GenerateSignal("BTCUSDT", ev)
	if sig == nil || sig.Market != "CRYPTO" || sig.Type != strategy.SignalRSI {
		t.Fatalf("信号章错: %+v", sig)
	}
	// 单边大涨：RSI 长期过热，任何一根都不构成"回升事件"。
	mo := make([]float64, 0, 40)
	m := 50.0
	for i := 0; i < 40; i++ {
		m *= 1.05
		mo = append(mo, m)
	}
	if ev2, _ := s.Evaluate("BTCUSDT", mdOf(mo)); ev2.Pass {
		t.Fatal("过热单边不得放行（不追顶）")
	}
	// nodata：序列短于 RSI 预热窗。
	if ev3, _ := s.Evaluate("BTCUSDT", mdOf([]float64{1, 2, 3, 4, 5})); ev3.Pass || ev3.Level != "nodata" {
		t.Fatalf("短序列必须 nodata: %+v", ev3)
	}
}

// _T_NEWS 事件锁

// TestSignalsFromEventsThreeGates 事件适配三关 + Market 章 + 解析失败跳过。
func TestSignalsFromEventsThreeGates(t *testing.T) {
	evs := []newsagent.NewsEvent{
		{IsMaterial: true, Level: "个股", Direction: "利好", Score: 8, Title: "财报超预期", CleanedStocks: []string{"苹果|AAPL"}},
		{IsMaterial: true, Level: "个股", Direction: "利空", Score: -6, Title: "集体诉讼", CleanedStocks: []string{"某币|ETHUSDT"}},
		{IsMaterial: false, Level: "个股", Direction: "利好", Score: 9, Title: "未过初筛", CleanedStocks: []string{"跳过|X1"}},
		{IsMaterial: true, Level: "板块", Direction: "利好", Score: 7, Title: "板块级不下单票", CleanedStocks: []string{"跳过|X2"}},
		{IsMaterial: true, Level: "个股", Direction: "中性", Score: 1, Title: "中性", CleanedStocks: []string{"跳过|X3"}},
		{IsMaterial: true, Level: "个股", Direction: "利好", Score: 5, Title: "解析不出代码", CleanedStocks: []string{"没有竖线"}},
	}
	sigs, err := SignalsFromEvents(evs, "US")
	if err != nil {
		t.Fatal(err)
	}
	if len(sigs) != 2 {
		t.Fatalf("三关后应剩 2 条信号，实际 %d: %+v", len(sigs), sigs)
	}
	buy, sell := sigs[0], sigs[1]
	if buy.Code != "AAPL" || buy.Name != "苹果" || buy.Action != strategy.ActionBuy || buy.Market != "US" {
		t.Fatalf("利好信号错: %+v", buy)
	}
	if math.Abs(buy.Confidence-0.8) > 1e-9 {
		t.Fatalf("置信度应=|8|/10: %v", buy.Confidence)
	}
	if sell.Code != "ETHUSDT" || sell.Action != strategy.ActionSell || sell.Market != "US" {
		t.Fatalf("利空信号错: %+v", sell)
	}
	if math.Abs(sell.Confidence-0.6) > 1e-9 {
		t.Fatalf("负分方向置信度取绝对值: %v", sell.Confidence)
	}
	if _, err := SignalsFromEvents(evs, "CN"); err == nil {
		t.Fatal("事件适配同样拒 CN")
	}
}
