// 文件职责：交易规则查表面（binance_symbol_rules.go / binance_exchange_info.go /
// binance_symbol_rules_cache.go / binance_symbol_rules_source.go）的**行为锁**单测。
//
// 五条锁：
//  1. BinanceSymbolRule.HasEvidence 的"任一字段即有证据"语义（全 0 且无状态=未确认→闸拒单），
//     四个字段逐个点亮各测一次，负数与纯空白状态不算证据（反例同测）；
//  2. RoundQtyDown/RoundPrice 的**向下**取整（step<=0 或值<=0 时原样返回=未知不取整）；
//  3. parseBinanceExchangeInfo 的 filters 认键映射：LOT_SIZE.stepSize（minQty 不得冒充步长）、
//     MARKET_LOT_SIZE 仅在 LOT_SIZE 未给时兜底、PRICE_FILTER.tickSize、
//     NOTIONAL/MIN_NOTIONAL 的 minNotional|notional，认不出的 filterType 一律跳过（不猜）；
//     无 symbol / 全无证据的条目不进表；
//  4. binanceRuleCache 的锁纪律与 TTL：未加载 fresh=false、空表不覆盖旧表、get 不看 TTL、
//     snapshot 是副本、ttl<=0 落 6h 缺省；
//  5. BinanceRuleSource.Rule 的三条出口：空 symbol 直接报错（不发请求）、缓存新鲜即命中
//     （不发请求）、刷新失败且有旧值时**沿用旧规则**（用已取消的 ctx 制造确定性失败，零网络）。
//
// English: behaviour locks for the Binance symbol-rule lookup surface — HasEvidence semantics,
// floor rounding, exchangeInfo filter key mapping, the cache's TTL/empty-table protection and the
// source's three exits (empty symbol, fresh cache hit, stale-on-failure fallback). A cancelled
// context makes the failing-refresh path deterministic and network-free.
package data

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

// binanceTestClose 浮点近似比较（步长取整会带出 1e-14 级误差，用等值断言会假红）。
func binanceTestClose(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9*math.Max(1, math.Abs(b))
}

// ── 锁 1：HasEvidence 的"任一字段"语义 ──

// TestBinanceSymbolRuleHasEvidenceLocksAnyField 全零规则=未确认（false），四个字段任一为
// 正/非空白即为 true；负数（源不该给）与纯空白状态**不**算证据。
func TestBinanceSymbolRuleHasEvidenceLocksAnyField(t *testing.T) {
	cases := []struct {
		name string
		rule BinanceSymbolRule
		want bool
	}{
		{"零值规则", BinanceSymbolRule{}, false},
		{"仅 MinNotional", BinanceSymbolRule{MinNotional: 10}, true},
		{"仅 StepSize", BinanceSymbolRule{StepSize: 0.001}, true},
		{"仅 TickSize", BinanceSymbolRule{TickSize: 0.01}, true},
		{"仅 Status", BinanceSymbolRule{Status: "TRADING"}, true},
		{"只有 Symbol 不算证据", BinanceSymbolRule{Symbol: "BTCUSDT"}, false},
		{"纯空白状态不算证据", BinanceSymbolRule{Status: "   "}, false},
		{"负值字段不算证据", BinanceSymbolRule{MinNotional: -1, StepSize: -2, TickSize: -3}, false},
		{"仅 FetchedAt 不算证据", BinanceSymbolRule{FetchedAt: binanceTestEpoch}, false},
	}
	for _, tc := range cases {
		if got := tc.rule.HasEvidence(); got != tc.want {
			t.Fatalf("%s: HasEvidence got=%v want=%v (%+v)", tc.name, got, tc.want, tc.rule)
		}
	}
}

// ── 锁 2：向下取整（不是四舍五入）──

// TestBinanceSymbolRuleRoundingLocksFloor 数量/价格步长必须**向下**取整：向上会让实际数量
// 超过可用资金/持仓（拒单或超卖）；step<=0（未知）与值<=0 都原样返回，绝不"猜一个步长"。
func TestBinanceSymbolRuleRoundingLocksFloor(t *testing.T) {
	rule := BinanceSymbolRule{Symbol: "BTCUSDT", StepSize: 0.5, TickSize: 0.01}
	// 落在格点上：原值即结果。
	if got := rule.RoundQtyDown(1.5); !binanceTestClose(got, 1.5) {
		t.Fatalf("格点上的数量不该被改动, got=%v", got)
	}
	// 1.49 按 0.5 步长向下 → 1.0（四舍五入会错给 1.5）。
	if got := rule.RoundQtyDown(1.49); !binanceTestClose(got, 1.0) {
		t.Fatalf("RoundQtyDown(1.49) got=%v want=1", got)
	}
	// 价格按 tickSize 向下：100.005 → 100.00。
	if got := rule.RoundPrice(100.005); !binanceTestClose(got, 100.0) {
		t.Fatalf("RoundPrice(100.005) got=%v want=100", got)
	}
	// 步长未知（0/负）→ 原样返回，不取整也不归零。
	unknown := BinanceSymbolRule{Symbol: "X", StepSize: 0, TickSize: -1}
	if got := unknown.RoundQtyDown(1.234567); got != 1.234567 {
		t.Fatalf("step=0 应原样返回, got=%v", got)
	}
	if got := unknown.RoundPrice(9.87); got != 9.87 {
		t.Fatalf("tick<0 应原样返回, got=%v", got)
	}
	// 值<=0（含负数场景）→ 不动原值。
	if got := rule.RoundQtyDown(0); got != 0 {
		t.Fatalf("qty=0 应原样返回, got=%v", got)
	}
	if got := rule.RoundPrice(-3); got != -3 {
		t.Fatalf("负价应原样返回（不适用取整）, got=%v", got)
	}
	// 纯函数层再锁一次：结果永远 <= 原值（向下而非就近）。
	for _, v := range []float64{0.1, 0.49, 0.51, 1.999999, 123.456} {
		if got := roundToStepDown(v, 0.5); got > v+1e-9 {
			t.Fatalf("取整结果不得高于原值: v=%v got=%v", v, got)
		}
	}
}

// ── 锁 3：exchangeInfo → 规则三字段映射（认不出一律跳过，不猜）──

// TestParseBinanceExchangeInfoLocksFilterMapping 逐品种锁 filters 认键口径：
// LOT_SIZE.stepSize（minQty 不得冒充步长）、MARKET_LOT_SIZE 只在 LOT_SIZE 未给时兜底、
// PRICE_FILTER.tickSize、NOTIONAL/MIN_NOTIONAL 的新旧字段名；未知 filterType 必须被忽略；
// 无 symbol 或全无证据的条目不进表；状态大写归一；数字形态（未 quote）也能取到。
func TestParseBinanceExchangeInfoLocksFilterMapping(t *testing.T) {
	raw := map[string]any{
		"symbols": []any{
			// ① 三件套齐全 + 一个不认识的 filter（其 minQty 不得被当成 stepSize）。
			map[string]any{"symbol": "btcusdt", "status": "trading", "filters": []any{
				map[string]any{"filterType": "LOT_SIZE", "stepSize": "0.00100000", "minQty": "0.00001000"},
				map[string]any{"filterType": "PRICE_FILTER", "tickSize": "0.01000000", "minPrice": "0.01"},
				map[string]any{"filterType": "NOTIONAL", "minNotional": "10.00000000"},
				map[string]any{"filterType": "ICEBERG_PARTS", "limit": "100"},
				map[string]any{"filterType": "PERCENT_PRICE", "minQty": "999"},
			}},
			// ② LOT_SIZE 只给 minQty（老字段形态）：minQty 是"最小下单量"不是步长，
			// **不得**冒充 stepSize（缺陷④修复）；stepSize 由 MARKET_LOT_SIZE 兜底为 0.5。
			map[string]any{"symbol": "ETHUSDT", "status": "TRADING", "filters": []any{
				map[string]any{"filterType": "LOT_SIZE", "minQty": "0.00010000"},
				map[string]any{"filterType": "MARKET_LOT_SIZE", "stepSize": "0.50000000"},
			}},
			// ③ 只有 MARKET_LOT_SIZE + 老字段名 notional（数值未 quote 的形态）。
			map[string]any{"symbol": "SOLUSDT", "filters": []any{
				map[string]any{"type": "MARKET_LOT_SIZE", "stepSize": 0.1},
				map[string]any{"filterType": "MIN_NOTIONAL", "notional": 5.5},
				map[string]any{"filterType": "PRICE_FILTER", "tickSize": "-1"}, // 非法值=未知，不猜
			}},
			// ④ 缺 symbol：整条丢弃。
			map[string]any{"status": "TRADING", "filters": []any{
				map[string]any{"filterType": "LOT_SIZE", "stepSize": "1"},
			}},
			// ⑤ 有 symbol 但三字段全 0 且无 status：无证据 → 不进表。
			map[string]any{"symbol": "DOGEUSDT", "filters": []any{
				map[string]any{"filterType": "UNKNOWN_FILTER", "stepSize": "1"},
			}},
			// ⑥ 只有状态（熔断中）：状态本身即证据，必须进表并带大写 Status。
			map[string]any{"symbol": "aapl", "status": " break "},
			// ⑦ filters 形态畸形（字符串而非数组）：不得 panic，按无字段处理。
			map[string]any{"symbol": "XRPUSDT", "status": "TRADING", "filters": "not-a-list"},
		},
	}
	got := parseBinanceExchangeInfo(raw)
	// 应入库 5 条：①②③⑥⑦（④缺 symbol、⑤全无证据）。
	if len(got) != 5 {
		t.Fatalf("入库品种数 got=%d want=5（表=%v）", len(got), keysOfRules(got))
	}
	btc := got["BTCUSDT"] // symbol 一律大写归一进表
	if !binanceTestClose(btc.StepSize, 0.001) || !binanceTestClose(btc.TickSize, 0.01) ||
		!binanceTestClose(btc.MinNotional, 10) {
		t.Fatalf("BTC 三字段映射错误: %+v", btc)
	}
	if btc.Status != "TRADING" {
		t.Fatalf("Status 应大写归一: %+v", btc)
	}
	if btc.Symbol != "BTCUSDT" || !btc.HasEvidence() {
		t.Fatalf("Symbol/证据位错误: %+v", btc)
	}
	// ② LOT_SIZE 无 stepSize 时**不**退到 minQty（minQty≠stepSize，缺陷④修复后的口径）：
	// 步长只由 LOT_SIZE.stepSize 或 MARKET_LOT_SIZE 兜底给出。
	eth := got["ETHUSDT"]
	if !binanceTestClose(eth.StepSize, 0.5) {
		t.Fatalf("ETH 步长应来自 MARKET_LOT_SIZE 兜底（minQty 不得冒充）: %+v", eth)
	}
	// ③ MARKET_LOT_SIZE 兜底 + 老字段名 notional + 非法 tickSize 归 0。
	sol := got["SOLUSDT"]
	if !binanceTestClose(sol.StepSize, 0.1) || !binanceTestClose(sol.MinNotional, 5.5) || sol.TickSize != 0 {
		t.Fatalf("SOL 映射错误: %+v", sol)
	}
	if sol.Status != "" {
		t.Fatalf("未给 status 应留空（未知），不猜 TRADING: %+v", sol)
	}
	// ⑥ 只有状态也是证据（§9 闸自己判状态，规则表不混判）。
	aapl := got["AAPL"]
	if aapl.Status != "BREAK" || aapl.StepSize != 0 || !aapl.HasEvidence() {
		t.Fatalf("仅状态条目应入库且带大写状态: %+v", aapl)
	}
	// ⑦ 畸形 filters 不影响状态证据。
	if got["XRPUSDT"].Status != "TRADING" || got["XRPUSDT"].TickSize != 0 {
		t.Fatalf("畸形 filters 应安全降级: %+v", got["XRPUSDT"])
	}
	// 无 symbols 键 / 空数组：返回空表（store 的空表保护才不会覆盖旧规则）。
	if len(parseBinanceExchangeInfo(map[string]any{})) != 0 {
		t.Fatal("缺 symbols 应返回空表")
	}
	if len(parseBinanceExchangeInfo(map[string]any{"symbols": []any{}})) != 0 {
		t.Fatal("空 symbols 应返回空表")
	}
	if len(parseBinanceExchangeInfo(map[string]any{"symbols": "nope"})) != 0 {
		t.Fatal("symbols 非数组应返回空表而非 panic")
	}
}

// keysOfRules 排障辅助：把规则表键拼成一行。
func keysOfRules(m map[string]BinanceSymbolRule) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return strings.Join(out, ",")
}

// TestParseBinanceSymbolRuleLocksRejectShape 单品种入口：symbol 缺失判 false（不进表），
// 全无证据同样判 false；symbol 的别名 code 也要认。
func TestParseBinanceSymbolRuleLocksRejectShape(t *testing.T) {
	if _, ok := parseBinanceSymbolRule(map[string]any{"filters": []any{
		map[string]any{"filterType": "LOT_SIZE", "stepSize": "1"}}}); ok {
		t.Fatal("缺 symbol 的条目必须判 false")
	}
	if _, ok := parseBinanceSymbolRule(map[string]any{"symbol": "X"}); ok {
		t.Fatal("无任何字段且无状态=无证据，必须判 false")
	}
	r, ok := parseBinanceSymbolRule(map[string]any{"code": "nflx", "status": "SETTLING"})
	if !ok || r.Symbol != "NFLX" || r.Status != "SETTLING" {
		t.Fatalf("code 别名 + 仅状态也应成立: %+v ok=%v", r, ok)
	}
}

// ── 锁 4：规则缓存（TTL / 空表保护 / 副本）──

// TestBinanceRuleCacheLocksTTLAndEmptyTableGuard 缓存五条纪律：未加载即无证据、空表不覆盖
// 旧表（一次拉回空 JSON 不该让证据消失）、get 不看 TTL（刷新失败时沿用旧值）、
// snapshot 是副本、ttl<=0 落 6h 缺省。
func TestBinanceRuleCacheLocksTTLAndEmptyTableGuard(t *testing.T) {
	rules := map[string]BinanceSymbolRule{
		"BTCUSDT": {Symbol: "BTCUSDT", StepSize: 0.001, Status: "TRADING"},
	}
	// 缺省 TTL 锁：ttl=0/nil 时钟都走生产缺省。
	def := newBinanceRuleCache(0, nil)
	if def.ttl != 6*time.Hour {
		t.Fatalf("缺省 TTL 应为 6h, got=%v", def.ttl)
	}
	if def.now() == (time.Time{}) {
		t.Fatal("now 缺省应为 time.Now（不得留 nil 让调用方 panic）")
	}
	c := newBinanceRuleCache(time.Hour, func() time.Time { return binanceTestEpoch })
	// 未加载：fresh 与 get 都无值，loadedTime 为零值（=从未成功）。
	if _, ok := c.fresh("BTCUSDT", binanceTestEpoch); ok {
		t.Fatal("未加载时 fresh 必须 false")
	}
	if _, ok := c.get("BTCUSDT"); ok {
		t.Fatal("未加载时 get 必须 false")
	}
	if !c.loadedTime().IsZero() {
		t.Fatal("未加载时 loadedTime 应为零值")
	}
	// 空表保护：store 空 map 不得清掉旧表、也不得推进 loadedAt。
	c.store(rules, binanceTestEpoch)
	c.store(map[string]BinanceSymbolRule{}, binanceTestEpoch.Add(time.Minute))
	if c.loadedTime() != binanceTestEpoch {
		t.Fatalf("空表不得覆盖 loadedAt, got=%v", c.loadedTime())
	}
	if len(c.snapshot()) != 1 {
		t.Fatalf("空表不得清空规则表: %v", keysOfRules(c.snapshot()))
	}
	// TTL 边界：恰好等于 ttl 仍算新鲜（fresh 用严格 `>`），越界即失效但 get 仍可取旧值。
	later := binanceTestEpoch.Add(time.Hour)
	if _, ok := c.fresh("BTCUSDT", later); !ok {
		t.Fatal("恰好到 TTL 应仍新鲜")
	}
	if _, ok := c.fresh("BTCUSDT", later.Add(time.Nanosecond)); ok {
		t.Fatal("越过 TTL 后 fresh 必须 false（触发刷新）")
	}
	if _, ok := c.get("BTCUSDT"); !ok {
		t.Fatal("get 不看 TTL：刷新失败时要能沿用旧规则")
	}
	// snapshot 副本：改返回的 map 不得污染缓存。
	snap := c.snapshot()
	delete(snap, "BTCUSDT")
	if len(c.snapshot()) != 1 {
		t.Fatal("snapshot 泄漏了内部 map")
	}
	// single-flight 令牌：同一时刻只有一个协程真的去刷。
	if !c.tryBeginLoad(later) {
		t.Fatal("首个 tryBeginLoad 应拿到令牌")
	}
	if c.tryBeginLoad(later) {
		t.Fatal("令牌未释放时第二个 tryBeginLoad 必须 false")
	}
	c.finishLoad(later)
	if !c.tryBeginLoad(later) {
		t.Fatal("finishLoad 后令牌应可再取")
	}
	c.finishLoad(later)
	// waitLoad 的脱身口径锁（缺陷③修复后语义）：loading 与 loadDone 同锁读取——
	// 刷新进行中且 ctx 已取消 → 返回 ctx.Err()（不得吞掉取消）；
	// 空闲态（无人刷）→ **立即返回 nil**，不再空等下一条永不关闭的信道。
	if !c.tryBeginLoad(later) {
		t.Fatal("用例前提：应先拿到令牌")
	}
	waitCtx, cancelWait := context.WithCancel(context.Background())
	cancelWait() // 已取消：模拟"请求超时/调用方放弃"
	if err := c.waitLoad(waitCtx); err == nil {
		t.Fatal("刷新进行中且 ctx 已取消时，waitLoad 应返回 ctx.Err()（不得吞掉取消）")
	}
	c.finishLoad(later)
	// 空闲态：修复后 waitLoad 立即返回 nil（历史缺陷是拿到下一条未关闭信道、只能等超时）。
	done := make(chan error, 1)
	go func() { done <- c.waitLoad(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("空闲态 waitLoad 应立即返回 nil, got=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("空闲态 waitLoad 仍在等待（同锁读 loading 的修复失效了）")
	}
	// 加载中且未取消：等待者必须由 finishLoad 唤醒（信道收敛锁，不等 ctx）。
	if !c.tryBeginLoad(later) {
		t.Fatal("用例前提：应再次拿到令牌")
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		c.finishLoad(later)
	}()
	if err := c.waitLoad(context.Background()); err != nil {
		t.Fatalf("刷新收尾后 waitLoad 应被唤醒并返回 nil, got=%v", err)
	}
}

// ── 锁 5：RuleSource 的三条出口（空 symbol / 新鲜命中 / 失败沿用旧值）──

// TestBinanceRuleSourceLocksExits 用"非法协议基址 + 已取消 ctx"把网络路径彻底钉死：
// 因此任何一次真的发请求都会立刻失败——测试由此可反证"新鲜命中路径根本没发请求"。
func TestBinanceRuleSourceLocksExits(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	// 基址故意用不存在的协议：Do 立即返回 unsupported protocol scheme（零 DNS、零出网）。
	client := &BinanceRestClient{Base: "badscheme://unittest.invalid", Timeout: time.Millisecond}
	s := NewBinanceRuleSource(client, time.Hour)
	s.SetClock(clock.now)
	// 出口 1：空 symbol 直接报错，且**不发请求**（错误文案里带归一后的 symbol）。
	if _, err := s.Rule(context.Background(), "   "); err == nil {
		t.Fatal("空 symbol 应直接报错")
	}
	// 未加载过：LoadedAt 零值、All 为空表副本。
	if !s.LoadedAt().IsZero() {
		t.Fatalf("未刷新时 LoadedAt 应为零值, got=%v", s.LoadedAt())
	}
	if len(s.All()) != 0 {
		t.Fatalf("未刷新时 All 应为空, got=%v", keysOfRules(s.All()))
	}
	// 出口 2：缓存新鲜 → 直接命中（走到网络就会因 badscheme 报错，故成功即证明未发请求）。
	s.cache.store(map[string]BinanceSymbolRule{
		"BTCUSDT": {Symbol: "BTCUSDT", MinNotional: 10, StepSize: 0.001, TickSize: 0.01, Status: "TRADING"},
	}, clock.now())
	r, err := s.Rule(context.Background(), "btcusdt")
	if err != nil || r.MinNotional != 10 || r.Status != "TRADING" {
		t.Fatalf("新鲜命中应直接返回规则: %+v err=%v", r, err)
	}
	if s.LoadedAt() != binanceTestEpoch {
		t.Fatalf("新鲜命中不该推进 LoadedAt, got=%v", s.LoadedAt())
	}
	// 出口 3：过期 + 刷新必然失败 → **沿用旧值**（规则宁可旧也不要空），并返回旧规则。
	clock.advance(2 * time.Hour)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel() // 已取消的 ctx：任何 HTTP 路径都立即失败
	got, err := s.Rule(cancelled, "BTCUSDT")
	if err != nil {
		t.Fatalf("刷新失败但有旧值时应返回旧规则, got err=%v", err)
	}
	if got.StepSize != 0.001 || !got.HasEvidence() {
		t.Fatalf("应返回旧规则原值: %+v", got)
	}
	// 失败刷新不得推进 LoadedAt（否则巡检会误以为规则是新的）。
	if s.LoadedAt() != binanceTestEpoch {
		t.Fatalf("刷新失败时 LoadedAt 不该变, got=%v", s.LoadedAt())
	}
	// 表里没有该票且刷新失败 → 明确报错（调用方据此拒单，而不是拿零值规则放行）。
	if _, err := s.Rule(cancelled, "DGEUSDT"); err == nil {
		t.Fatal("无旧值且刷新失败时必须报错")
	}
	// Refresh 直接调用同样失败但保留旧表。
	if err := s.Refresh(cancelled); err == nil {
		t.Fatal("Refresh 应返回错误")
	}
	if len(s.All()) != 1 {
		t.Fatalf("失败刷新后旧表必须还在, got=%v", keysOfRules(s.All()))
	}
	// SetClock(nil) 是空操作（别让注入接口把时钟打成 nil 造成 panic）。
	s.SetClock(nil)
	if _, err := s.Rule(cancelled, "BTCUSDT"); err != nil && got.Symbol == "" {
		t.Fatalf("SetClock(nil) 后仍应可用: %v", err)
	}
}

// TestBinanceRuleSourceDefaults 构造缺省：client=nil 走缺省基址、ttl<=0 落 6h、
// exchangeInfoPath 按市场分前缀（美股 sapi、现货 v3）。
func TestBinanceRuleSourceDefaults(t *testing.T) {
	s := NewBinanceRuleSource(nil, -1)
	if s == nil || s.client == nil || s.cache == nil {
		t.Fatal("nil client 也要造出可用查表器（缺省基址）")
	}
	if s.cache.ttl != 6*time.Hour {
		t.Fatalf("ttl 缺省应为 6h, got=%v", s.cache.ttl)
	}
	if s.client.Base != BinanceSpotRestMirror {
		t.Fatalf("缺省基址应走公共镜像, got=%q", s.client.Base)
	}
	// 端点前缀按市场分档（规则面与行情面同源）。
	if got := s.client.exchangeInfoPath(); got != "/api/v3/exchangeInfo" {
		t.Fatalf("现货规则端点 got=%q", got)
	}
	if got := (&BinanceRestClient{Equity: true}).exchangeInfoPath(); got != "/sapi/v1/equity/market/exchangeInfo" {
		t.Fatalf("美股规则端点 got=%q", got)
	}
}
