// 文件职责：交易状态证据库（binance_trading_status.go + binance_trading_status_io.go）的
// **行为锁**单测——把 §9「未确认即拒」三条纪律钉死在断言里，防止后续改动悄悄放宽闸门口径。
//
// 覆盖面（与源文件一一对应）：
//  1. classifyBinanceState 白名单三分法：明确交易态 / 明确停牌态（含 LULD* 前缀）/ 未知，
//     表驱动**双向**锁（每个取值同时断言 HasEvidence 与 Halted，正反例都跑）；
//  2. TTL 过期：用注入的虚拟时钟（NewBinanceTradingStatus 的 now 形参）拨时间，含边界
//     （恰好等于 ttl 仍算新鲜，>ttl 才失效），过期后进 Unconfirmed；
//  3. 帧入口 HandlePayload：组合流后缀分流 + jsonShapeBinance 对象/数组两种形态 +
//     putStatus 别名键（S/symbol/Symbol/code、tradingStatus/status/state/ts/marketStatus/s、
//     reason 系）+ EventMs 取自 E/eventTime；
//  4. tradingStatus 与 tradability **分表**互不满足（tradability 证据不得冒充 tradingStatus 证据）；
//  5. misses 巡检计数（无证据才记、拿到证据即销账）与 Watch 订阅池。
//
// ⚠ 已知缺陷（本文件按"现状锁 + 明确标注"处理，见 TestBinanceTradingStatusLocksStreamSuffixDispatch）：
// binance_trading_status_io.go:47 的 @tradingStatus 分支用 strings.ToLower(stream) 去后缀匹配
// 常量 "tradingStatus"（含大写 S），永远匹配不上 → 组合流形态的状态帧会被整帧丢弃（返回 0）。
// 修复该大小写缺陷后，对应用例应同步改成断言 n==1（这是刻意留的"改代码必须改测试"的钩子）。
//
// English: behaviour locks for the trading-status evidence store — whitelist classification in both
// directions, TTL expiry on an injected clock, frame-entry tolerance (object/array shapes, alias
// keys, combined-stream suffixes) and the strict separation of tradingStatus vs tradability tables.
// One documented source defect (case-mismatched @tradingStatus suffix) is pinned as-is on purpose.
package data

import (
	"sync"
	"testing"
	"time"
)

// ── 测试替身：虚拟时钟（本包多处 TTL/超龄判定共用，杜绝真实时间抖动）──

// binanceTestClock 可控虚拟时钟：证据 TTL、feed tick 超龄、快照刷新时刻都从它取时间。
type binanceTestClock struct {
	mu sync.Mutex // 保护 at（生产侧可能在协程里回调 now，加锁保证 -race 干净）
	at time.Time
}

// newBinanceTestClock 以 t0 为起点构造虚拟时钟。
func newBinanceTestClock(t0 time.Time) *binanceTestClock {
	return &binanceTestClock{at: t0}
}

// now 满足 func() time.Time 注入点（生产代码的 Now/now 形参）。
func (c *binanceTestClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

// advance 把虚拟时钟往前拨 d（拨动后所有 TTL 判定立即按新时刻生效）。
func (c *binanceTestClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// binanceTestEpoch 固定起点（2025-06-15T15:06:40Z），避免与真实时钟耦合。
var binanceTestEpoch = time.Unix(1750000000, 0).UTC()

// newBinanceTestStatus 造一个走虚拟时钟的状态证据库（ttl 由用例指定）。
func newBinanceTestStatus(t *testing.T, clock *binanceTestClock, ttl time.Duration) *BinanceTradingStatus {
	t.Helper()
	return NewBinanceTradingStatus(ttl, clock.now)
}

// ── 锁 1：状态取值白名单三分法（正反双向）──

// TestBinanceTradingStatusLocksStateWhitelist 逐个取值锁 (入库条数, HasEvidence, Halted) 三元组：
// 明确交易态→(true,false)、明确停牌态与 LULD*→(true,true)、其余（"filled"、空串、未实测取值）
// →(false,false)。**未知永不等于可交易**是本库第一条纪律，也是 §9 闸的默认拒单姿势。
func TestBinanceTradingStatusLocksStateWhitelist(t *testing.T) {
	cases := []struct {
		state        string // 入库的 tradingStatus 原值
		wantIngested bool   // putStatus 是否收下这条（空串应拒写，不留脏证据）
		wantEvidence bool   // HasEvidence 期望
		wantHalted   bool   // Halted 期望
		desc         string
	}{
		{"NORMAL", true, true, false, "明确交易态：NORMAL"},
		{"TRADING", true, true, false, "明确交易态：TRADING"},
		{"HALTED", true, true, true, "明确停牌态：HALTED"},
		{"BREAK", true, true, true, "明确停牌态：BREAK"},
		{"PAUSED", true, true, true, "明确停牌态：PAUSED"},
		{"SUSPENDED", true, true, true, "明确停牌态：SUSPENDED"},
		{"CLOSE", true, true, true, "明确停牌态：CLOSE"},
		{"CLOSED", true, true, true, "明确停牌态：CLOSED"},
		{"LULD", true, true, true, "LULD 根：熔断语义"},
		{"LULD_AGG_TIER_1", true, true, true, "LULD 变体（未实测也按停牌收）"},
		{"LULD_TRADE_THROUGH", true, true, true, "LULD 变体 2"},
		{"filled", true, false, false, "认不出的取值：未知≠可交易"},
		{"SETTLING", true, false, false, "结算中：未进白名单→未知"},
		{"", false, false, false, "空状态：putStatus 直接拒写"},
	}
	for _, tc := range cases {
		// 每个取值一套独立库：状态表互不污染，失败时能一眼看出是哪个取值崩了。
		clock := newBinanceTestClock(binanceTestEpoch)
		s := newBinanceTestStatus(t, clock, time.Minute)
		// 裸 payload（无 stream 信封）：走 HandlePayload 的"自证"分支，绕开已知信封缺陷。
		raw := []byte(`{"S":"AAPL","tradingStatus":"` + tc.state + `"}`)
		wantN := 0
		if tc.wantIngested {
			wantN = 1
		}
		if got := s.HandlePayload(raw); got != wantN {
			t.Fatalf("%s: 入库条数 got=%d want=%d", tc.desc, got, wantN)
		}
		if got := s.HasEvidence("AAPL"); got != tc.wantEvidence {
			t.Fatalf("%s: HasEvidence got=%v want=%v", tc.desc, got, tc.wantEvidence)
		}
		if got := s.Halted("AAPL"); got != tc.wantHalted {
			t.Fatalf("%s: Halted got=%v want=%v", tc.desc, got, tc.wantHalted)
		}
	}
}

// TestBinanceTradingStatusLocksStateIsUpperCased 状态一律大写原样存：小写输入（源未实测时
// 常见的大小写漂移）必须仍命中白名单；tradability 出口透传的也应是**大写后**的取值。
func TestBinanceTradingStatusLocksStateIsUpperCased(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	s := newBinanceTestStatus(t, clock, time.Minute)
	// tradingStatus 小写形态：putStatus 里 ToUpper 后应判为交易态。
	if n := s.HandlePayload([]byte(`{"S":"aapl","tradingStatus":"trading"}`)); n != 1 {
		t.Fatalf("小写状态帧应入库 1 条, got=%d", n)
	}
	if !s.HasEvidence("AAPL") || s.Halted("AAPL") {
		t.Fatalf("小写 trading 归一后应判交易态: evidence=%v halted=%v", s.HasEvidence("AAPL"), s.Halted("AAPL"))
	}
	// tradability 小写形态：只验"大写存 + 带原因"的透传口径，不做白名单解释（源码就是只透传不解释）。
	if n := s.HandlePayload([]byte(`{"stream":"AAPL@tradability","data":{"S":"AAPL","state":"ssr_restricted","reason":"short-sale"}}`)); n != 1 {
		t.Fatalf("tradability 信封帧应入库 1 条, got=%d", n)
	}
	state, detail, ok := s.Tradability("AAPL")
	if !ok || state != "SSR_RESTRICTED" || detail != "short-sale" {
		t.Fatalf("tradability 应大写原样透传+带原因: state=%q detail=%q ok=%v", state, detail, ok)
	}
}

// ── 锁 2：TTL 过期（含边界）与 Unconfirmed ──

// TestBinanceTradingStatusLocksTTLExpiry 证据会过期：恰好等于 ttl 仍新鲜（源码用严格 `>`），
// 越过 ttl 后 HasEvidence/Halted 一起失效，并重新出现在 Unconfirmed 巡检名单里。
func TestBinanceTradingStatusLocksTTLExpiry(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	const ttl = time.Minute
	s := newBinanceTestStatus(t, clock, ttl)
	s.Watch("AAPL", "MSFT") // 订阅池两票，后面只给 AAPL 证据
	if n := s.HandlePayload([]byte(`{"S":"AAPL","tradingStatus":"HALTED"}`)); n != 1 {
		t.Fatalf("初始帧应入库, got=%d", n)
	}
	// 边界 1：ttl 整点（=ttl，未 `>` ttl）→ 证据仍然作数。
	clock.advance(ttl)
	if !s.HasEvidence("AAPL") || !s.Halted("AAPL") {
		t.Fatalf("恰好到 ttl 应仍新鲜（源码是严格 >）: evidence=%v halted=%v",
			s.HasEvidence("AAPL"), s.Halted("AAPL"))
	}
	// 边界 2：超过 ttl 一毫秒 → 状态过期=未知，Halted 也必须跟着 false。
	clock.advance(time.Millisecond)
	if s.HasEvidence("AAPL") {
		t.Fatal("超过 ttl 后 HasEvidence 必须 false（断流后的旧状态不算数）")
	}
	if s.Halted("AAPL") {
		t.Fatal("超过 ttl 后 Halted 必须 false（不得沿用旧停牌证据）")
	}
	// Unconfirmed 是升序名单：过期的 AAPL 与从未有证据的 MSFT 都应在列。
	got := s.Unconfirmed()
	if len(got) != 2 || got[0] != "AAPL" || got[1] != "MSFT" {
		t.Fatalf("Unconfirmed got=%v want=[AAPL MSFT]", got)
	}
	// 补一帧新证据后该票立刻退出名单（证明过期不是"永久拉黑"，而是"当时无据"）。
	if n := s.HandlePayload([]byte(`{"S":"AAPL","tradingStatus":"NORMAL"}`)); n != 1 {
		t.Fatalf("续期帧应入库, got=%d", n)
	}
	if u := s.Unconfirmed(); len(u) != 1 || u[0] != "MSFT" {
		t.Fatalf("续期后 Unconfirmed got=%v want=[MSFT]", u)
	}
}

// TestBinanceTradingStatusLocksDefaultTTL ttl<=0 走缺省 5min：在 4min59s 仍新鲜、再过 2s 失效，
// 顺带锁 NewBinanceTradingStatus 的缺省值不被"顺手改成长生/不过期"。
func TestBinanceTradingStatusLocksDefaultTTL(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	s := newBinanceTestStatus(t, clock, 0) // 0 → BinanceDefaultStatusTTL
	if s.ttl != BinanceDefaultStatusTTL {
		t.Fatalf("ttl 缺省应为 %v, got=%v", BinanceDefaultStatusTTL, s.ttl)
	}
	if n := s.HandlePayload([]byte(`{"S":"BTCUSDT","tradingStatus":"NORMAL"}`)); n != 1 {
		t.Fatalf("首帧应入库, got=%d", n)
	}
	// 缺省窗口内：新鲜。
	clock.advance(BinanceDefaultStatusTTL - time.Second)
	if !s.HasEvidence("BTCUSDT") {
		t.Fatal("缺省 TTL 前 1 秒仍应新鲜")
	}
	// 越界后：无证据（且不是停牌——两条出口必须同时塌）。
	clock.advance(2 * time.Second)
	if s.HasEvidence("BTCUSDT") || s.Halted("BTCUSDT") {
		t.Fatal("越过缺省 TTL 后应判无证据")
	}
}

// ── 锁 3：帧入口形态（数组批次 / 对象 / 别名键 / EventMs）──

// TestBinanceTradingStatusLocksArrayBatchFrame jsonShapeBinance 的数组形态（批量/合并推送）
// 一次入库多票，与对象形态共用同一条 putStatus 路径；缺 symbol 的脏元素只跳过、不计入条数。
func TestBinanceTradingStatusLocksArrayBatchFrame(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	s := newBinanceTestStatus(t, clock, time.Minute)
	// 三元素：两条合法 + 一条缺 symbol（脏数据必须被拒写，不产生假证据）。
	batch := []byte(`[{"S":"AAPL","tradingStatus":"NORMAL"},{"symbol":"MSFT","status":"HALTED"},{"tradingStatus":"NORMAL"}]`)
	if n := s.HandlePayload(batch); n != 2 {
		t.Fatalf("数组批次应入库 2 条, got=%d", n)
	}
	if !s.HasEvidence("AAPL") || s.Halted("AAPL") {
		t.Fatalf("AAPL 应为交易态: evidence=%v halted=%v", s.HasEvidence("AAPL"), s.Halted("AAPL"))
	}
	if !s.HasEvidence("MSFT") || !s.Halted("MSFT") {
		t.Fatalf("MSFT 应为停牌态: evidence=%v halted=%v", s.HasEvidence("MSFT"), s.Halted("MSFT"))
	}
	// 空数组/空对象/非 JSON 几种畸形输入都应是 0 条且不 panic。
	for _, raw := range []string{`[]`, `{}`, `not-json`, `"a-string"`, `123`} {
		if n := s.HandlePayload([]byte(raw)); n != 0 {
			t.Fatalf("畸形帧 %q 应返回 0, got=%d", raw, n)
		}
	}
}

// TestBinanceTradingStatusLocksAliasKeysAndEventMs putStatus 的键名宽容面：symbol 四别名、
// state 六别名、reason 别名族、EventMs 取 E/eventTime/timestamp/t（数字与字符串两种形态）。
func TestBinanceTradingStatusLocksAliasKeysAndEventMs(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	s := newBinanceTestStatus(t, clock, time.Minute)
	cases := []struct {
		raw       string
		wantSym   string
		wantState string
		wantEvent int64
	}{
		{`{"S":"aapl","tradingStatus":"NORMAL","E":1750000000123}`, "AAPL", "NORMAL", 1750000000123},
		{`{"symbol":"MSFT","status":"HALTED","eventTime":"1750000000456"}`, "MSFT", "HALTED", 1750000000456},
		{`{"Symbol":"TSLA","state":"BREAK","timestamp":1750000000789}`, "TSLA", "BREAK", 1750000000789},
		{`{"code":"nvda","marketStatus":"PAUSED","t":1750000001000}`, "NVDA", "PAUSED", 1750000001000},
		{`{"S":"COIN","ts":"SUSPENDED"}`, "COIN", "SUSPENDED", 0}, // 源未给事件时间=0（未知），不猜
		{`{"S":"DIS","s":"CLOSE"}`, "DIS", "CLOSE", 0},            // 别名 "s" 承载状态（symbol 走 S）
	}
	for _, tc := range cases {
		if n := s.HandlePayload([]byte(tc.raw)); n != 1 {
			t.Fatalf("%s: 应入库 1 条, got=%d", tc.raw, n)
		}
		// 直接读内部证据条目：EventMs/State 没有公开出口，同包测试按最小白盒方式锁口径。
		ev, has := s.trade[tc.wantSym]
		if !has {
			t.Fatalf("%s: 未建到大写键 %s 的证据", tc.raw, tc.wantSym)
		}
		if ev.State != tc.wantState {
			t.Fatalf("%s: State got=%q want=%q", tc.raw, ev.State, tc.wantState)
		}
		if ev.EventMs != tc.wantEvent {
			t.Fatalf("%s: EventMs got=%d want=%d", tc.raw, ev.EventMs, tc.wantEvent)
		}
		if !ev.SeenAt.Equal(clock.now()) {
			t.Fatalf("%s: SeenAt 应取注入时钟时刻, got=%v", tc.raw, ev.SeenAt)
		}
	}
	// reason 别名族：detail 只透传不解释，但要确保取到值。
	if n := s.HandlePayload([]byte(`{"S":"IBM","tradingStatus":"HALTED","limitReason":"LULD"}`)); n != 1 {
		t.Fatalf("IBM 帧应入库, got=%d", n)
	}
	if got := s.trade["IBM"].Detail; got != "LULD" {
		t.Fatalf("limitReason 别名应被取到, got=%q", got)
	}
}

// TestBinanceTradingStatusLocksStreamSuffixDispatch HandlePayload 的组合流后缀分流锁：
// @tradingStatus（驼峰常量）与 @tradability 两类后缀在**大小写两种 stream 名**下都必须命中。
// 历史缺陷①：实现拿 strings.ToLower(stream) 去匹配含大写 S 的常量 "tradingStatus"
// （binance_trading_status_io.go），tradingStatus 分支恒不可达 → 在龄证据永远为零，
// §9 闸被喂成全员"未确认"拒单。修复后两侧统一 lower 口径，本用例按修复后语义锁定。
func TestBinanceTradingStatusLocksStreamSuffixDispatch(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	s := newBinanceTestStatus(t, clock, time.Minute)
	// 状态机信封帧（驼峰 stream 名）：修复后必须入库 1 条。
	env := []byte(`{"stream":"AAPL@tradingStatus","data":{"S":"AAPL","tradingStatus":"HALTED"}}`)
	if n := s.HandlePayload(env); n != 1 {
		t.Fatalf("@tradingStatus 信封帧应入库 1 条（大小写无关分流）；got=%d", n)
	}
	if !s.HasEvidence("AAPL") || !s.Halted("AAPL") {
		t.Fatal("信封帧入库后必须有证据且判停牌")
	}
	// 对照组：同一 data 以裸 payload（无 stream 字段）投喂同样入库。
	if n := s.HandlePayload([]byte(`{"S":"MSFT","tradingStatus":"HALTED"}`)); n != 1 {
		t.Fatalf("裸状态帧应入库 1 条, got=%d", n)
	}
	if !s.HasEvidence("MSFT") || !s.Halted("MSFT") {
		t.Fatal("裸 payload 路径应正常建证据")
	}
	// 小写 stream 名（币安侧真实形态）必须同样命中。
	low := []byte(`{"stream":"ibm@tradingstatus","data":{"S":"IBM","tradingStatus":"NORMAL"}}`)
	if n := s.HandlePayload(low); n != 1 {
		t.Fatalf("小写 stream 名必须命中 tradingStatus 分支；got=%d", n)
	}
	if !s.HasEvidence("IBM") || s.Halted("IBM") {
		t.Fatal("IBM 的 NORMAL 证据应建且不得判停牌")
	}
	// @tradability 全小写、命中正常：与上面的分支形成同一函数内的对照。
	if n := s.HandlePayload([]byte(`{"stream":"tsla@tradability","data":{"S":"TSLA","state":"TRADABLE"}}`)); n != 1 {
		t.Fatalf("@tradability 信封帧应入库 1 条, got=%d", n)
	}
	if st, _, ok := s.Tradability("TSLA"); !ok || st != "TRADABLE" {
		t.Fatalf("tradability 证据应可读出, state=%q ok=%v", st, ok)
	}
	// 行情帧绝不能被状态库误吞（后缀既非状态流也非可交易流 → 0 条）。
	market := []byte(`{"stream":"btcusdt@miniTicker","data":{"e":"24hrMiniTicker","s":"BTCUSDT","c":"100"}}`)
	if n := s.HandlePayload(market); n != 0 {
		t.Fatalf("行情帧必须原样退回 0, got=%d", n)
	}
}

// ── 锁 4：tradingStatus 与 tradability 分表，互不满足 ──

// TestBinanceTradingStatusLocksTradabilitySeparation tradability 证据**不得**满足 tradingStatus
// 的 HasEvidence（否则 §9 闸会把"可交易位/可短卖"当成"正在交易"），反向亦然：两表互不兜底。
func TestBinanceTradingStatusLocksTradabilitySeparation(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	s := newBinanceTestStatus(t, clock, time.Minute)
	// 只给 tradability 证据：状态闸必须仍判"无据可查"。
	raw := []byte(`{"stream":"AAPL@tradability","data":{"S":"AAPL","state":"TRADEABLE"}}`)
	if n := s.HandlePayload(raw); n != 1 {
		t.Fatalf("tradability 帧应入库 1 条, got=%d", n)
	}
	if s.HasEvidence("AAPL") {
		t.Fatal("tradability 证据不得满足 tradingStatus 的 HasEvidence")
	}
	if s.Halted("AAPL") {
		t.Fatal("tradability 证据不得推导出 Halted")
	}
	// 反向：补上 tradingStatus 证据后两条出口各自独立可读（后写的状态不覆盖前写的可交易位）。
	if n := s.HandlePayload([]byte(`{"S":"AAPL","tradingStatus":"HALTED"}`)); n != 1 {
		t.Fatalf("状态帧应入库 1 条, got=%d", n)
	}
	if !s.HasEvidence("AAPL") || !s.Halted("AAPL") {
		t.Fatal("tradingStatus 证据应让 HasEvidence/Halted 生效")
	}
	st, _, ok := s.Tradability("AAPL")
	if !ok || st != "TRADEABLE" {
		t.Fatalf("tradability 原值不得被 tradingStatus 覆盖: state=%q ok=%v", st, ok)
	}
	// tradability 自己也吃 TTL：过期后 ok=false（透传面同样不许沿用旧值）。
	clock.advance(2 * time.Minute)
	if _, _, ok := s.Tradability("AAPL"); ok {
		t.Fatal("tradability 超过 ttl 后应判无证据")
	}
}

// ── 锁 5：misses 巡检计数、Watch 与 symbol 归一 ──

// TestBinanceTradingStatusLocksMissesAndWatch 无证据的查询要留痕（MissCount 按票种计数），
// 拿到可归类证据后该票销账；Halted 这类纯读判定不制造 miss；空白 symbol 不入订阅池。
func TestBinanceTradingStatusLocksMissesAndWatch(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	s := newBinanceTestStatus(t, clock, time.Minute)
	// 空白/纯空格 Watch 归一后为空，不应污染池子（否则 Unconfirmed 永远有噪声项）。
	s.Watch("", "   ", "AAPL", " msft ")
	if u := s.Unconfirmed(); len(u) != 2 || u[0] != "AAPL" || u[1] != "MSFT" {
		t.Fatalf("Watch 应归一并去重, Unconfirmed=%v", u)
	}
	if s.MissCount() != 0 {
		t.Fatalf("未查询前 MissCount 应为 0, got=%d", s.MissCount())
	}
	// 同一票重复查询（含小写形态）只记一次 miss：键是大写归一后的 symbol。
	if s.HasEvidence("AAPL") || s.HasEvidence("aapl") || s.HasEvidence("AAPL") {
		t.Fatal("无证据时 HasEvidence 必须 false")
	}
	if got := s.MissCount(); got != 1 {
		t.Fatalf("同一票重复查询应只记一次 miss, got=%d", got)
	}
	// Halted 纯读不记 miss（否则巡检会把"查过停牌"也算成缺口）。
	if s.Halted("MSFT") {
		t.Fatal("MSFT 无证据不应判停牌")
	}
	if got := s.MissCount(); got != 1 {
		t.Fatalf("Halted 不该制造 miss, got=%d", got)
	}
	// 未知状态同样算 miss（证据存在但不作数：入库由判定侧降级）。
	if n := s.HandlePayload([]byte(`{"S":"MSFT","tradingStatus":"weird-value"}`)); n != 1 {
		t.Fatalf("未知态应入库 1 条, got=%d", n)
	}
	if s.HasEvidence("MSFT") {
		t.Fatal("未知状态不得算证据")
	}
	if got := s.MissCount(); got != 2 {
		t.Fatalf("MSFT 未知态应记 miss, got=%d", got)
	}
	// 拿到可归类证据后销账：AAPL 入库 → misses 里去掉该票。
	if n := s.HandlePayload([]byte(`{"S":"AAPL","tradingStatus":"NORMAL"}`)); n != 1 {
		t.Fatalf("AAPL 帧应入库, got=%d", n)
	}
	if got := s.MissCount(); got != 1 {
		t.Fatalf("入库应删除该票 miss, got=%d", got)
	}
}

// TestBinanceTradingStatusLocksSymbolNormalization 查询侧与入库侧共用同一大写归一：
// 小写查询、带空格查询必须命中同一份证据（否则闸会因大小写误判"无据"而错杀）。
func TestBinanceTradingStatusLocksSymbolNormalization(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	s := newBinanceTestStatus(t, clock, time.Minute)
	if n := s.HandlePayload([]byte(`{"symbol":"  aapl  ","tradingStatus":"HALTED"}`)); n != 1 {
		t.Fatalf("带空格 symbol 帧应入库, got=%d", n)
	}
	for _, q := range []string{"AAPL", "aapl", " aapl ", "AapL"} {
		if !s.HasEvidence(q) || !s.Halted(q) {
			t.Fatalf("查询 %q 应命中同一份证据", q)
		}
	}
	// 不存在的票：两值都 false（区别于"有证据且停牌"的组合）。
	if s.HasEvidence("NVDA") || s.Halted("NVDA") {
		t.Fatal("从未见过的标的应判无证据且不停牌")
	}
}
