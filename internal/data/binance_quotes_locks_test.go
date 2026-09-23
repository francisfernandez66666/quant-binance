// 文件职责：币安行情 feed（binance_quotes_feed.go / _io.go / _snap.go / _merge.go / _fresh.go）
// 与 tick 装配纯函数 applyBinanceTick 的**行为锁**单测。
//
// 六条锁（与源文件一一对应）：
//  1. 构造期校验：Market 只接 CRYPTO/US（其余一律被 NormalizeMarketKey 归一成 CN → 报错，
//     A 股链不动）、Symbols+Streams 同时为空报错、Dial 未注入报错；SourceTag 与订阅 URL 拼装；
//  2. 帧入口 HandlePayload 只写 pending、**不动快照**（首次 Flush 前 Snapshot() 必须为 nil），
//     Flush 才整批装配成 MarketSnapshot（key=symbol 大写、Source=BINANCE-SPOT/BINANCE-STK）；
//  3. 反伪造新鲜度两条：超龄 tick（>MaxTickAge）丢弃并计 dropStale；0 价/空 symbol/解析失败
//     计 dropInvalid；TimeMs=0（源未带事件时间）按落地时间兜底、**不当更旧处理**；
//  4. applyBinanceTick 的字段级合并数学：tick 未携带（=0）的字段保留原值，ChangePct 三档优先
//     （源值 > 按 PrevClose 折算 > 保持既有值，绝不凭空造数）；
//  5. MergeInto 纪律：nil 目标/nil 基线一律 0 且不注入空壳、Name/Sector/资金流保留 CN 原值、
//     0 值用 pickPositive 兜底、超龄票不覆盖既有价、旧共享指针不被原地改写；
//  6. 新鲜度出口：SymbolStalenessMs 的 -1=未知口径与超前收口、Fresh 边界、SpreadBps 无盘口 -1。
//
// 时间全部走注入的虚拟时钟（BinanceQuoteFeedOptions.Now + binanceTestClock），Dial 注入"必然
// 失败"的假拨号且本文件从不调 Start()，因此零网络、零 sleep、无真实时间竞态。
//
// English: behaviour locks for the Binance quote feed and its tick-assembly pure functions —
// construction validation, pending buffering (snapshot untouched until Flush), stale/invalid drops,
// field-level merge math, opt-in MergeInto discipline and the freshness exits. Time comes from an
// injected virtual clock and Dial is a deliberately dead fake, so the file is network/sleep free.
package data

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// ── 测试替身：假拨号 + 可编程解析器 ──

// binanceTestDeadDial 故意失败的拨号接缝：本文件只直调 HandlePayload/Flush，从不 Start()，
// 因此永远不会真的拨号；注入它只为满足 NewBinanceWS 的"Dial 必填"构造期校验。
func binanceTestDeadDial(_ context.Context, url string) (WsTransport, error) {
	return nil, errors.New("binanceTestDeadDial: 单测不该拨号（URL=" + url + "）")
}

// binanceTestParser 可编程解析器：用例把要吐出的 tick 预先塞进盒子，payload 本身不参与断言，
// 从而把"解析层字段口径"与"feed 缓冲/丢弃/装配语义"两件事彻底解耦（同一 feed 可换批次投喂）。
type binanceTestParser struct {
	ticks []binanceTick // 下一次解析要返回的 tick 批次
	err   error         // 非空则模拟"整帧解析失败"（坏帧/订阅回执/心跳）
	calls int           // 被调用次数（验证"每帧都过解析器"）
}

// parse 实现 BinanceQuoteFeedOptions.Parse 注入点。
func (p *binanceTestParser) parse(payload []byte) ([]binanceTick, error) {
	p.calls++
	// 坏帧路径优先：解析失败时 feed 只计数、不注入（下方 Stats 断言会看到 dropInvalid+1）。
	if p.err != nil {
		return nil, p.err
	}
	return p.ticks, nil
}

// set 换一批待吐出的 tick（并清掉错误注入）。
func (p *binanceTestParser) set(ticks ...binanceTick) {
	p.err = nil
	p.ticks = ticks
}

// fail 让后续解析一律失败（模拟不可用帧）。
func (p *binanceTestParser) fail() {
	p.err = errors.New("payload 不是合法行情帧")
}

// newBinanceTestFeed 造一条注入虚拟时钟 + 注入解析器的 feed（不 Start，纯直调帧入口）。
func newBinanceTestFeed(t *testing.T, opt BinanceQuoteFeedOptions, clock *binanceTestClock,
	parser *binanceTestParser) *BinanceQuoteFeed {
	t.Helper()
	// 缺省项在此收口：用例只显式覆盖自己关心的字段，其余走生产缺省值。
	if opt.Now == nil {
		opt.Now = clock.now
	}
	if opt.Parse == nil {
		opt.Parse = parser.parse
	}
	// Dial 必填但永不被调（不 Start）；FlushInterval 放大到小时级，确保没有真实节拍参与断言。
	if opt.Dial == nil {
		opt.Dial = binanceTestDeadDial
	}
	if opt.FlushInterval <= 0 {
		opt.FlushInterval = time.Hour
	}
	f, err := NewBinanceQuoteFeed(opt)
	if err != nil {
		t.Fatalf("构造 feed 失败: %v", err)
	}
	return f
}

// keysOfSnapshot 排障辅助：把快照 key 拼成一行（失败信息里直接看到实际键名）。
func keysOfSnapshot(snap *MarketSnapshot) string {
	if snap == nil {
		return "<nil>"
	}
	out := make([]string, 0, len(snap.Stocks))
	for k := range snap.Stocks {
		out = append(out, k)
	}
	return strings.Join(out, ",")
}

// ── 锁 1：构造期校验与拼装 ──

// TestBinanceQuoteFeedLocksNewValidation 配置错误一律启动失败（宁可失败也不起一条"订阅了个
// 空池"的静默假活通道）：Market 非 CRYPTO/US、Symbols+Streams 双空、Dial 未注入三种都必须
// 报错；合法写法（市场大小写不敏感、Streams 可替代 Symbols）必须构造成功。
func TestBinanceQuoteFeedLocksNewValidation(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	// 反例族：每种错误配置都要有 error，且不得返回半只 feed。
	badCases := []struct {
		name string
		opt  BinanceQuoteFeedOptions
	}{
		{"CN 市场被拒", BinanceQuoteFeedOptions{Market: "CN", Symbols: []string{"600000"}, Dial: binanceTestDeadDial}},
		{"空市场归一成 CN 被拒", BinanceQuoteFeedOptions{Market: "", Symbols: []string{"BTCUSDT"}, Dial: binanceTestDeadDial}},
		{"乱码市场归一成 CN 被拒", BinanceQuoteFeedOptions{Market: "FUTURES", Symbols: []string{"BTCUSDT"}, Dial: binanceTestDeadDial}},
		{"Symbols+Streams 双空", BinanceQuoteFeedOptions{Market: "CRYPTO", Dial: binanceTestDeadDial, Now: clock.now}},
		{"Symbols 全空白等同双空", BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"  ", ""}, Dial: binanceTestDeadDial, Now: clock.now}},
		{"Dial 未注入", BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"BTCUSDT"}}},
	}
	for _, tc := range badCases {
		f, err := NewBinanceQuoteFeed(tc.opt)
		if err == nil {
			t.Fatalf("%s: 应返回 error", tc.name)
		}
		if f != nil {
			t.Fatalf("%s: 出错时不得返回半只 feed", tc.name)
		}
	}
	// 正例族：市场大小写不敏感、Streams 可替代 Symbols、US 全市场单流无需 Symbols。
	okCases := []struct {
		name string
		opt  BinanceQuoteFeedOptions
	}{
		{"CRYPTO+Symbols", BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"BTCUSDT"}}},
		{"小写 crypto", BinanceQuoteFeedOptions{Market: "crypto", Symbols: []string{"btcusdt"}}},
		{"US+显式 Streams", BinanceQuoteFeedOptions{Market: "US", Streams: []string{"price"}}},
		{"US+Symbols（price 单流）", BinanceQuoteFeedOptions{Market: "US", Symbols: []string{"AAPL"}}},
	}
	for _, tc := range okCases {
		tc.opt.Dial = binanceTestDeadDial
		f, err := NewBinanceQuoteFeed(tc.opt)
		if err != nil {
			t.Fatalf("%s: 合法配置不应报错: %v", tc.name, err)
		}
		// 未 Start 的 feed 一律判不健康（§6.9：从未建连不许报绿）。
		if f.Healthy() {
			t.Fatalf("%s: 未启动却报健康=假绿", tc.name)
		}
	}
}

// TestBinanceQuoteFeedLocksSourceTagAndURL 市场标签与订阅 URL 的字节级锁：
// CRYPTO→BINANCE-SPOT（现货流名用**小写** symbol@kind）、US→BINANCE-STK（price 是全市场单流、
// @quote 用**大写** symbol——这是官方口径差异，不是笔误），未知 StreamKind 仍按字面拼流名，
// 但解析器回落该市场缺省型（见 binance_quotes_route.go 的"不猜流型"）。
func TestBinanceQuoteFeedLocksSourceTagAndURL(t *testing.T) {
	cases := []struct {
		name    string
		opt     BinanceQuoteFeedOptions
		wantTag string
		wantURL string
	}{
		{"现货多票组合流", BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"btcusdt", " ethusdt "}},
			"BINANCE-SPOT", "wss://stream.binance.com:9443/stream?streams=btcusdt@miniTicker/ethusdt@miniTicker"},
		{"现货自定义流型", BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"btcusdt"}, StreamKind: "trade"},
			"BINANCE-SPOT", "wss://stream.binance.com:9443/stream?streams=btcusdt@trade"},
		{"美股 price 全市场单流", BinanceQuoteFeedOptions{Market: "US", Symbols: []string{"AAPL", "MSFT"}},
			"BINANCE-STK", "wss://nbstream.binance.com/equity/stream?streams=price"},
		{"美股 quote 逐票大写流名", BinanceQuoteFeedOptions{Market: "US", Symbols: []string{"aapl"}, StreamKind: "quote"},
			"BINANCE-STK", "wss://nbstream.binance.com/equity/stream?streams=AAPL@quote"},
		{"显式 Streams 原样直通", BinanceQuoteFeedOptions{Market: "CRYPTO", Streams: []string{"btcusdt@kline_1m"}},
			"BINANCE-SPOT", "wss://stream.binance.com:9443/stream?streams=btcusdt@kline_1m"},
		{"WsBase 注入覆盖缺省", BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"btcusdt"},
			WsBase: "wss://testnet.binance.vision"}, "BINANCE-SPOT",
			"wss://testnet.binance.vision/stream?streams=btcusdt@miniTicker"},
	}
	for _, tc := range cases {
		tc.opt.Dial = binanceTestDeadDial
		f, err := NewBinanceQuoteFeed(tc.opt)
		if err != nil {
			t.Fatalf("%s: 构造失败: %v", tc.name, err)
		}
		// 源标签即 §WS-C 白名单项：市场判错一个方向就会让下游整条链认错源。
		if got := f.SourceTag(); got != tc.wantTag {
			t.Fatalf("%s: SourceTag got=%q want=%q", tc.name, got, tc.wantTag)
		}
		if got := f.URL(); got != tc.wantURL {
			t.Fatalf("%s: URL got=%q want=%q", tc.name, got, tc.wantURL)
		}
	}
}

// ── 锁 2：帧入口只写 pending，快照要等 Flush ──

// TestBinanceQuoteFeedLocksPendingBuffering 帧入口/装配分工锁：收帧后 Snapshot() 仍为 nil、
// flushes=0；Flush 才产出快照（大写 key、Source、Time=本轮刷新时刻、字段映射）；空 pending 的
// Flush 返回 0 且快照不替换；后续轮次只更新有帧的票、**绝不删旧票**。
func TestBinanceQuoteFeedLocksPendingBuffering(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	parser := &binanceTestParser{}
	f := newBinanceTestFeed(t, BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"btcusdt", "ethusdt"},
		MaxTickAge: time.Minute}, clock, parser)
	// 首帧之前：快照 nil（与 Fetcher.Snapshot() 行为一致，调用方判空分支不用改）。
	if f.Snapshot() != nil {
		t.Fatal("任何帧之前 Snapshot() 应为 nil")
	}
	// 预置一只现货 BTC tick：量价与涨跌幅全给，验证 tick→StockInfo 的映射口径。
	parser.set(binanceTick{Symbol: "BTCUSDT", Price: 111.5, Open: 100, High: 120, Low: 90,
		Volume: 7, Amount: 800, ChangePct: 11.5, TimeMs: binanceTestEpoch.UnixMilli()})
	if n := f.HandlePayload([]byte(`{"ignored":"injected parser"}`)); n != 1 {
		t.Fatalf("首帧应缓冲 1 只 tick, got=%d", n)
	}
	// 关键锁：帧只进 pending，**不得提前暴露到快照**（读侧要么旧要么新，不看半成品）。
	if f.Snapshot() != nil {
		t.Fatal("HandlePayload 之后、Flush 之前 Snapshot() 仍必须为 nil（整轮一起刷）")
	}
	recv, flushes, stale, invalid := f.Stats()
	if recv != 1 || flushes != 0 || stale != 0 || invalid != 0 {
		t.Fatalf("Stats got recv=%d flushes=%d stale=%d invalid=%d", recv, flushes, stale, invalid)
	}
	if got := f.Flush(); got != 1 {
		t.Fatalf("Flush 应落地 1 票, got=%d", got)
	}
	snap := f.Snapshot()
	if snap == nil {
		t.Fatal("Flush 后快照不应为 nil")
	}
	if snap.Source != BinanceQuoteSourceSpot || snap.Time.IsZero() {
		t.Fatalf("快照头字段错误: source=%q time=%v", snap.Source, snap.Time)
	}
	si := snap.Stocks["BTCUSDT"]
	if si == nil {
		t.Fatalf("快照 key 应为大写 symbol, got=[%s]", keysOfSnapshot(snap))
	}
	// Price/Close 同为最新价（本仓库 Close=最新价口径，昨收看 PrevClose），量价按 tick 原值。
	if si.Code != "BTCUSDT" || si.Price != 111.5 || si.Close != 111.5 || si.Open != 100 ||
		si.High != 120 || si.Low != 90 || si.Volume != 7 || si.Amount != 800 || si.ChangePct != 11.5 {
		t.Fatalf("tick→StockInfo 映射错误: %+v", si)
	}
	if si.PrevClose != 0 {
		t.Fatalf("tick 未给参考价时 PrevClose 必须留 0=未知, got=%v", si.PrevClose)
	}
	// 空 pending 的 Flush：返回 0，快照整体不替换（Time 也不该被推进）。
	before := snap.Time
	if got := f.Flush(); got != 0 {
		t.Fatalf("无新帧时 Flush 应返回 0, got=%d", got)
	}
	if f.Snapshot().Time != before {
		t.Fatal("无新帧时快照时间戳不该被推进（假新鲜）")
	}
	// 第二轮：同票新价覆盖 + 新票入表，旧票不被删（ETH 加入后 BTC 仍在）。
	parser.set(binanceTick{Symbol: "ETHUSDT", Price: 50, TimeMs: clock.now().UnixMilli()},
		binanceTick{Symbol: "BTCUSDT", Price: 120.5, PrevClose: 110, TimeMs: clock.now().UnixMilli()})
	if n := f.HandlePayload([]byte(`{"batch":[...]}`)); n != 2 {
		t.Fatalf("第二轮应缓冲 2 只, got=%d", n)
	}
	clock.advance(5 * time.Second) // 推进虚拟时钟：本轮刷新时刻应体现在新快照上
	if got := f.Flush(); got != 2 {
		t.Fatalf("第二轮 Flush 应落地 2 票, got=%d", got)
	}
	snap2 := f.Snapshot()
	if len(snap2.Stocks) != 2 {
		t.Fatalf("两轮累计应有 2 票, got=[%s]", keysOfSnapshot(snap2))
	}
	btc := snap2.Stocks["BTCUSDT"]
	// 上一轮的量价字段本轮未给 → 保留（不清零）；ChangePct 由新 PrevClose 本地折算。
	if btc.Open != 100 || btc.Volume != 7 {
		t.Fatalf("本轮未携带的字段应保留原值: %+v", btc)
	}
	wantPct := (btc.Price/110 - 1) * 100 // 用 float64 变量算，避免常量折叠成不同结果
	if btc.Price != 120.5 || btc.ChangePct != wantPct {
		t.Fatalf("第二轮 BTC 价/涨跌幅错误 got=%v want=%v", btc.ChangePct, wantPct)
	}
	if snap2.Time != clock.now() {
		t.Fatalf("快照 Time 应为刷新时刻 got=%v want=%v", snap2.Time, clock.now())
	}
	if _, flushes, _, _ := f.Stats(); flushes != 2 {
		t.Fatalf("flushes 应累计 2 轮, got=%d", flushes)
	}
}

// ── 锁 3：超龄/非法帧丢弃与计数 ──

// TestBinanceQuoteFeedLocksStaleAndInvalidDrops 两条反伪造新鲜度纪律（照抄 §ENH-5/§M2 姿势）：
//   - 超龄 tick（now-事件时间 > MaxTickAge）丢弃并计 dropStale，快照里绝不允许出现该票；
//   - 0 价 / 空 symbol 的 tick 丢弃并计 dropInvalid；整帧解析失败同样计 dropInvalid；
//   - TimeMs=0（源未带事件时间）按落地时间兜底、**不当更旧处理**（作为对照正例）。
func TestBinanceQuoteFeedLocksStaleAndInvalidDrops(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	cases := []struct {
		name        string
		parser      *binanceTestParser
		wantHits    int
		wantStale   int64
		wantInvalid int64
	}{
		// 断流重连后币安会回放旧帧：旧价盖新价比没价更危险，必须丢。
		{"超龄帧被丢", &binanceTestParser{ticks: []binanceTick{{Symbol: "BTCUSDT", Price: 10,
			TimeMs: binanceTestEpoch.Add(-time.Hour).UnixMilli()}}}, 0, 1, 0},
		// 0 价=停牌/无成交：宁可不注入。
		{"0 价被丢", &binanceTestParser{ticks: []binanceTick{{Symbol: "BTCUSDT", Price: 0}}}, 0, 0, 1},
		// 无 symbol 无法定位键，同样只计数。
		{"空 symbol 被丢", &binanceTestParser{ticks: []binanceTick{{Symbol: "   ", Price: 10}}}, 0, 0, 1},
		// 坏帧（订阅回执/心跳/脏 JSON）不该让通道掉线，只丢弃计数。
		{"解析失败被丢", &binanceTestParser{err: errors.New("bad frame")}, 0, 0, 1},
		// 对照正例：源未带事件时间 → 用落地时间兜底，判为新鲜。
		{"无事件时间按落地兜底", &binanceTestParser{ticks: []binanceTick{{Symbol: "BTCUSDT", Price: 10}}}, 1, 0, 0},
	}
	for _, tc := range cases {
		// 每个用例一条独立 feed：计数不串味，Stats 一眼对得上。
		f := newBinanceTestFeed(t, BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"btcusdt"},
			MaxTickAge: 10 * time.Second}, clock, tc.parser)
		if got := f.HandlePayload([]byte("payload")); got != tc.wantHits {
			t.Fatalf("%s: 命中数 got=%d want=%d", tc.name, got, tc.wantHits)
		}
		recv, _, stale, invalid := f.Stats()
		if recv != 1 {
			t.Fatalf("%s: 收帧计数应为 1, got=%d", tc.name, recv)
		}
		if stale != tc.wantStale || invalid != tc.wantInvalid {
			t.Fatalf("%s: 丢弃计数 got stale=%d invalid=%d want stale=%d invalid=%d",
				tc.name, stale, invalid, tc.wantStale, tc.wantInvalid)
		}
		// 被丢弃的票绝不能进快照（pending 为空时 Flush 返回 0、快照保持 nil）。
		if got := f.Flush(); got != tc.wantHits {
			t.Fatalf("%s: Flush 落地数应等于命中数 got=%d want=%d", tc.name, got, tc.wantHits)
		}
		snap := f.Snapshot()
		if tc.wantHits == 0 && snap != nil {
			t.Fatalf("%s: 全丢弃时快照应仍为 nil, got=[%s]", tc.name, keysOfSnapshot(snap))
		}
	}
	// 边界：恰好等于 MaxTickAge 不算超龄（源码用严格 `>`），越过 1ms 才丢。
	f := newBinanceTestFeed(t, BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"btcusdt"},
		MaxTickAge: 10 * time.Second}, clock, &binanceTestParser{ticks: []binanceTick{{Symbol: "BTCUSDT",
		Price: 10, TimeMs: binanceTestEpoch.Add(-10 * time.Second).UnixMilli()}}})
	if got := f.HandlePayload([]byte("edge")); got != 1 {
		t.Fatalf("恰好到 MaxTickAge 应收下, got=%d", got)
	}
	// 再投一帧晚 1ms 的旧帧：此时应判超龄丢弃。
	f2 := newBinanceTestFeed(t, BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"btcusdt"},
		MaxTickAge: 10 * time.Second}, clock, &binanceTestParser{ticks: []binanceTick{{Symbol: "BTCUSDT",
		Price: 10, TimeMs: binanceTestEpoch.Add(-10*time.Second - time.Millisecond).UnixMilli()}}})
	if got := f2.HandlePayload([]byte("edge2")); got != 0 {
		t.Fatalf("越过 MaxTickAge 1ms 应丢弃, got=%d", got)
	}
	if _, _, stale, _ := f2.Stats(); stale != 1 {
		t.Fatalf("超龄计数应为 1, got=%d", stale)
	}
}

// ── 锁 4：applyBinanceTick 的合并数学（纯函数，不经通道）──

// TestApplyBinanceTickLocksMergeMath tick 不携带（=0）的字段一律保留原值（0=源未给，绝不抹零），
// ChangePct 三档优先级：源值 > 按可信 PrevClose 本地折算 > 保持既有值（不凭空造数）。
func TestApplyBinanceTickLocksMergeMath(t *testing.T) {
	// 基线：一只已有 OHLC/量额/名称/资金流的票（模拟币安帧不携带或本帧未给的字段）。
	base := func() *StockInfo {
		return &StockInfo{Code: "600000", Name: "浦发银行", Sector: "银行", Price: 9, Close: 9,
			Open: 9, High: 12, Low: 8, PrevClose: 10, Volume: 100, Amount: 1000,
			ChangePct: 7.7, NetInflow: 55, HasFlow: true}
	}
	// 情形 1：tick 只带价格 → 其余字段原样保留，ChangePct 按已有 PrevClose 折算。
	si := base()
	applyBinanceTick(si, binanceTick{Symbol: "600000", Price: 10.5})
	if si.Price != 10.5 || si.Close != 10.5 {
		t.Fatalf("Price/Close 应同为最新价: price=%v close=%v", si.Price, si.Close)
	}
	if si.Open != 9 || si.High != 12 || si.Low != 8 || si.Volume != 100 || si.Amount != 1000 {
		t.Fatalf("tick 未携带（=0）的字段必须保留原值: %+v", si)
	}
	if si.Name != "浦发银行" || si.Sector != "银行" || !si.HasFlow || si.NetInflow != 55 {
		t.Fatalf("Name/Sector/资金流不该被行情帧抹掉: %+v", si)
	}
	if want := (si.Price/10 - 1) * 100; si.ChangePct != want { // 用 float64 变量算，避开常量折叠
		t.Fatalf("ChangePct 应按 PrevClose 折算 got=%v want=%v", si.ChangePct, want)
	}
	// 情形 2：tick 带源涨跌幅 → 源值优先，不再本地折算。
	si2 := base()
	applyBinanceTick(si2, binanceTick{Symbol: "600000", Price: 10.5, ChangePct: -3.25})
	if si2.ChangePct != -3.25 {
		t.Fatalf("源涨跌幅应优先: got=%v", si2.ChangePct)
	}
	// 情形 3：tick 带新参考价 → PrevClose 更新，且涨跌幅按**新**参考价折算。
	si3 := base()
	applyBinanceTick(si3, binanceTick{Symbol: "600000", Price: 10.5, PrevClose: 11})
	if si3.PrevClose != 11 {
		t.Fatalf("PrevClose 只在源真给参考价时更新: got=%v", si3.PrevClose)
	}
	if want := (si3.Price/11 - 1) * 100; si3.ChangePct != want { // 同上：运行时浮点序一致
		t.Fatalf("应按新 PrevClose 折算 got=%v want=%v", si3.ChangePct, want)
	}
	// 情形 4：既无源涨跌幅又无可信 PrevClose（0=未知）→ 保持既有值（不造数）。
	si4 := &StockInfo{Code: "X", ChangePct: 4.4}
	applyBinanceTick(si4, binanceTick{Symbol: "X", Price: 3})
	if si4.ChangePct != 4.4 {
		t.Fatalf("无可信参考价时不得改涨跌幅: got=%v", si4.ChangePct)
	}
	// 情形 5：tick 携带全部量价字段（>0）→ 逐项覆盖。
	si5 := base()
	applyBinanceTick(si5, binanceTick{Symbol: "600000", Price: 10, Open: 9.5, High: 10.2, Low: 9.4,
		Volume: 200, Amount: 2000})
	if si5.Open != 9.5 || si5.High != 10.2 || si5.Low != 9.4 || si5.Volume != 200 || si5.Amount != 2000 {
		t.Fatalf("tick 携带（>0）的字段应覆盖: %+v", si5)
	}
	// pickPositive 本体锁：新值为 0 才回退旧值，负数也算"未给"（源不该给负价）。
	if got := pickPositive(0, 9.8); got != 9.8 {
		t.Fatalf("pickPositive(0,9.8) got=%v", got)
	}
	if got := pickPositive(11, 9.8); got != 11 {
		t.Fatalf("pickPositive(11,9.8) got=%v", got)
	}
	if got := pickPositive(-1, 9.8); got != 9.8 {
		t.Fatalf("pickPositive(-1,9.8) got=%v", got)
	}
}

// ── 锁 5：MergeInto 的守卫与字段保留（显式并池）──

// binanceTestBaseFetcher 造一个带基线快照的 CN Fetcher（沿用 qmt 链的测试姿势：600000 已有
// Name/板块/资金流，300750 只在监控池里、本轮快照尚未采到）。
func binanceTestBaseFetcher(t *testing.T) *Fetcher {
	t.Helper()
	// api 传零值、dc 传 nil：本用例只走 IngestSnapshot/Snapshot，不触发任何采集网络路径。
	f := NewFetcher([]string{"600000", "300750"}, &MarketAPI{}, nil)
	f.IngestSnapshot(&MarketSnapshot{
		Stocks: map[string]*StockInfo{
			"600000": {Code: "600000", Name: "浦发银行", Sector: "银行", Price: 9.9, Close: 9.9,
				Open: 9.8, High: 10.1, Low: 9.7, PrevClose: 10, Volume: 300000, Amount: 300000,
				NetInflow: 1234.5, HasFlow: true},
		},
		Time: binanceTestEpoch, Source: "新浪",
	})
	return f
}

// TestBinanceQuoteFeedLocksMergeInto 并池五条锁：
//  1. 目标为 nil → 0（不 panic）；
//  2. CN 基线快照为 nil → 0 且**不注入空壳**（否则给断流造假新鲜、误解除 §WS-C 闸）；
//  3. feed 自己还没 Flush 出任何在龄票 → 0，且不触碰 Fetcher 快照；
//  4. 命中注入：Name/Sector/资金流保留 CN 原值，tick 未给（=0）的量价用 pickPositive 兜底，
//     币安独有标的直接入表，注入后 Source=本源标签，旧共享指针不被原地改写；
//  5. 超龄票不覆盖既有价（时钟越过 MaxTickAge 后一票不注；再刷的新票单独注入）。
func TestBinanceQuoteFeedLocksMergeInto(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	parser := &binanceTestParser{}
	feed := newBinanceTestFeed(t, BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"600000", "AAPL"},
		MaxTickAge: time.Minute}, clock, parser)
	// 守卫 1：nil 目标。
	if got := feed.MergeInto(nil); got != 0 {
		t.Fatalf("nil 目标应返回 0, got=%d", got)
	}
	// 守卫 2：基线快照 nil（CN 首轮未采）→ 0 且内部快照仍为 nil。
	empty := NewFetcher([]string{"600000"}, &MarketAPI{}, nil)
	if got := feed.MergeInto(empty); got != 0 {
		t.Fatalf("无基线快照应返回 0, got=%d", got)
	}
	if empty.Snapshot() != nil {
		t.Fatal("无基线时不得注入空壳快照")
	}
	// 守卫 3：feed 尚未 Flush 出任何票（stamp 表为空）→ 0，CN 快照原样。
	base := binanceTestBaseFetcher(t)
	old := base.Snapshot().Stocks["600000"] // 与内部快照共享指针的旧对象，锁"不被原地改写"
	if got := feed.MergeInto(base); got != 0 {
		t.Fatalf("feed 未产快照时应返回 0, got=%d", got)
	}
	if got := base.Snapshot(); got.Source != "新浪" || len(got.Stocks) != 1 {
		t.Fatalf("未命中不得覆盖 CN 快照: source=%q keys=[%s]", got.Source, keysOfSnapshot(got))
	}
	// 正式注入：两票，600000 刻意不带 Open/High/Volume（验证 pickPositive 保留原值）。
	parser.set(binanceTick{Symbol: "600000", Price: 11.1, ChangePct: 11, TimeMs: clock.now().UnixMilli()},
		binanceTick{Symbol: "AAPL", Price: 200.5, Open: 199, Volume: 10, TimeMs: clock.now().UnixMilli()})
	if n := feed.HandlePayload([]byte("frame")); n != 2 {
		t.Fatalf("应缓冲 2 票, got=%d", n)
	}
	feed.Flush()
	if got := feed.MergeInto(base); got != 2 {
		t.Fatalf("并池应注入 2 票, got=%d", got)
	}
	snap := base.Snapshot()
	if snap.Source != BinanceQuoteSourceSpot {
		t.Fatalf("并池后 Source 应为本源标签, got=%q", snap.Source)
	}
	merged := snap.Stocks["600000"]
	if merged == nil || merged.Price != 11.1 || merged.Close != 11.1 || merged.ChangePct != 11 {
		t.Fatalf("价格/涨跌幅应被币安覆盖: %+v", merged)
	}
	if merged.Name != "浦发银行" || merged.Sector != "银行" || merged.NetInflow != 1234.5 || !merged.HasFlow {
		t.Fatalf("Name/Sector/资金流必须保留 CN 原值: %+v", merged)
	}
	// tick 未给（0）的四个量价字段 + PrevClose 全部走 pickPositive 兜底，绝不抹零。
	if merged.Open != 9.8 || merged.High != 10.1 || merged.Low != 9.7 || merged.Volume != 300000 ||
		merged.Amount != 300000 || merged.PrevClose != 10 {
		t.Fatalf("0 值字段应保留 CN 原值: %+v", merged)
	}
	// 币安独有标的直接入表（CN 池里没有的键）。
	if got := snap.Stocks["AAPL"]; got == nil || got.Price != 200.5 || got.Open != 199 || got.Volume != 10 {
		t.Fatalf("币安独有标的应入表且字段齐: %+v", got)
	}
	// 指针隔离：注入前的旧 *StockInfo 不得被原地改写（Snapshot 是浅拷贝）。
	if old.Price != 9.9 || old.Open != 9.8 || old.Name != "浦发银行" {
		t.Fatalf("旧 StockInfo 被原地改写（并发竞态隐患）: %+v", old)
	}
	// 锁 5：时钟越过 MaxTickAge → 一票不注、CN 快照不动。
	clock.advance(2 * time.Minute)
	base2 := binanceTestBaseFetcher(t)
	if got := feed.MergeInto(base2); got != 0 {
		t.Fatalf("全超龄时不应并池, got=%d", got)
	}
	if got := base2.Snapshot(); got.Source != "新浪" || got.Stocks["600000"].Price != 9.9 {
		t.Fatalf("超龄票不得覆盖既有价: source=%q price=%v", got.Source, got.Stocks["600000"].Price)
	}
	// 只刷新 600000 一帧：AAPL 仍在超龄窗口外 → 命中数 1，AAPL 不进 CN 池。
	parser.set(binanceTick{Symbol: "600000", Price: 12.3, TimeMs: clock.now().UnixMilli()})
	feed.HandlePayload([]byte("frame2"))
	feed.Flush()
	if got := feed.MergeInto(base2); got != 1 {
		t.Fatalf("只有 600000 在龄, got=%d", got)
	}
	after := base2.Snapshot()
	if after.Stocks["600000"].Price != 12.3 {
		t.Fatalf("在龄票应覆盖价格: %+v", after.Stocks["600000"])
	}
	if _, has := after.Stocks["AAPL"]; has {
		t.Fatal("超龄的 AAPL 不得进池")
	}
}

// ── 锁 6：新鲜度与价差出口（-1=未知，绝不与"新鲜 0"混同）──

// TestBinanceQuoteFeedLocksFreshness SymbolStalenessMs 三态：-1=从未见过、按事件时刻的毫秒差、
// 源时间戳超前时按 0 收口；Fresh 按 maxAge 边界且未知一律 false；SpreadBps 无盘口返回 -1。
func TestBinanceQuoteFeedLocksFreshness(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	parser := &binanceTestParser{}
	f := newBinanceTestFeed(t, BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"btcusdt"},
		MaxTickAge: time.Minute}, clock, parser)
	// 未见过该票：-1（未知），Fresh 必须 false（§M2 反伪造新鲜度：禁止把 -1 当 0 用）。
	if got := f.SymbolStalenessMs("BTCUSDT"); got != -1 {
		t.Fatalf("未见过该票应返回 -1, got=%d", got)
	}
	if f.Fresh("BTCUSDT", time.Hour) {
		t.Fatal("陈旧度未知（-1）时 Fresh 必须 false")
	}
	if got := f.SpreadBps("BTCUSDT"); got != -1 {
		t.Fatalf("无盘口数据应返回 -1, got=%v", got)
	}
	// Flush 后按事件时刻算陈旧度：事件比当前时刻早 2s → 2000ms。
	parser.set(binanceTick{Symbol: "BTCUSDT", Price: 101, Bid: 100, Ask: 102,
		TimeMs: binanceTestEpoch.Add(-2 * time.Second).UnixMilli()})
	f.HandlePayload([]byte("frame"))
	f.Flush()
	if got := f.SymbolStalenessMs("btcusdt"); got != 2000 {
		t.Fatalf("陈旧度应按事件时刻算, got=%d want=2000", got)
	}
	if !f.Fresh("BTCUSDT", 3*time.Second) || f.Fresh("BTCUSDT", time.Second) {
		t.Fatal("Fresh 应按 maxAge 边界判定")
	}
	// 价差从 lastTick 取：(ask-bid)/mid*10000 bp。
	want := (102.0 - 100.0) / 101.0 * 10000
	if got := f.SpreadBps("BTCUSDT"); got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("SpreadBps got=%v want≈%v", got, want)
	}
	// 只有 bid 无 ask（残缺盘口）→ 仍返回 -1（未知），不猜中价。
	parser.set(binanceTick{Symbol: "BTCUSDT", Price: 101, Bid: 100, TimeMs: clock.now().UnixMilli()})
	f.HandlePayload([]byte("frame2"))
	f.Flush()
	if got := f.SpreadBps("BTCUSDT"); got != -1 {
		t.Fatalf("缺 ask 时应返回 -1, got=%v", got)
	}
	// 源时间戳超前（时钟漂移）：按 0 收口，绝不返回负数。
	ahead := &binanceTestParser{ticks: []binanceTick{{Symbol: "ETHUSDT", Price: 10,
		TimeMs: binanceTestEpoch.Add(time.Hour).UnixMilli()}}}
	g := newBinanceTestFeed(t, BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"ethusdt"},
		MaxTickAge: time.Hour}, clock, ahead)
	g.HandlePayload([]byte("frame"))
	g.Flush()
	if got := g.SymbolStalenessMs("ETHUSDT"); got != 0 {
		t.Fatalf("源时间超前应按 0 处理, got=%d", got)
	}
}

// TestBinanceQuoteFeedLocksQuoteAndWatchSymbols 单票出口与"池子真的活了"出口：
// Quote 返回副本指针（改它不得污染内部快照），未见过的票返回 nil；
// WatchSymbols 只列已装配进快照的票（大写）。
func TestBinanceQuoteFeedLocksQuoteAndWatchSymbols(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	parser := &binanceTestParser{}
	f := newBinanceTestFeed(t, BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"btcusdt"},
		MaxTickAge: time.Minute}, clock, parser)
	// 未 Flush 前：单票出口 nil、池子为空（与 Snapshot 同口径，别一半可见一半不可见）。
	if f.Quote("BTCUSDT") != nil {
		t.Fatal("未 Flush 前单票出口应为 nil")
	}
	if got := f.WatchSymbols(); len(got) != 0 {
		t.Fatalf("未 Flush 前 WatchSymbols 应为空, got=%v", got)
	}
	parser.set(binanceTick{Symbol: "BTCUSDT", Price: 42, TimeMs: clock.now().UnixMilli()})
	f.HandlePayload([]byte("frame"))
	f.Flush()
	// 小写查询也要命中（归一口径与状态库一致）。
	q := f.Quote("btcusdt")
	if q == nil || q.Price != 42 {
		t.Fatalf("Quote 应命中并返回副本: %+v", q)
	}
	q.Price = 0 // 改副本不得污染内部快照
	if got := f.Quote("BTCUSDT").Price; got != 42 {
		t.Fatalf("内部快照被外部副本改写: %v", got)
	}
	if f.Quote("NOPE") != nil {
		t.Fatal("未见过的票应返回 nil")
	}
	if got := f.WatchSymbols(); len(got) != 1 || got[0] != "BTCUSDT" {
		t.Fatalf("WatchSymbols got=%v", got)
	}
	// Snapshot 是浅拷贝：改返回的 map 不影响内部快照。
	snap := f.Snapshot()
	delete(snap.Stocks, "BTCUSDT")
	if f.Snapshot().Stocks["BTCUSDT"] == nil {
		t.Fatal("外部 map 改动泄漏进了内部快照")
	}
}

// ── 锁 7：市场标签、回调出口与"解析层必须自己大写 symbol"的键名口径 ──

// TestBinanceQuoteFeedLocksUSTagAndCallback US feed 的快照标签必须是 BINANCE-STK，
// 且 OnSnapshot 回调每轮刷新拿到**同一份**已发布快照（回调在锁外，读侧不会自锁死）。
func TestBinanceQuoteFeedLocksUSTagAndCallback(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	parser := &binanceTestParser{}
	var gotSnapshots []*MarketSnapshot
	f := newBinanceTestFeed(t, BinanceQuoteFeedOptions{Market: "US", Symbols: []string{"AAPL"},
		MaxTickAge: time.Minute, OnSnapshot: func(s *MarketSnapshot) { gotSnapshots = append(gotSnapshots, s) }},
		clock, parser)
	parser.set(binanceTick{Symbol: "AAPL", Price: 201.25, PrevClose: 200, TimeMs: clock.now().UnixMilli()})
	f.HandlePayload([]byte("frame"))
	if got := f.Flush(); got != 1 {
		t.Fatalf("美股帧应落地 1 票, got=%d", got)
	}
	snap := f.Snapshot()
	// 源标签是 §WS-C 白名单项：美股写成 BINANCE-STK，不能与现货混用。
	if snap.Source != BinanceQuoteSourceUS {
		t.Fatalf("美股 Source got=%q want=%q", snap.Source, BinanceQuoteSourceUS)
	}
	// 回调拿到的是**已发布的那一份**快照本体（Snapshot() 出口才是浅拷贝，两者指针不同是设计）。
	if len(gotSnapshots) != 1 || gotSnapshots[0] != f.snap {
		t.Fatalf("OnSnapshot 应各轮收到一次本轮快照本体: calls=%d", len(gotSnapshots))
	}
	// 无新帧的 Flush 不回调（hits=0 时快照未换，别推假节拍）。
	if got := f.Flush(); got != 0 || len(gotSnapshots) != 1 {
		t.Fatalf("空轮不应触发回调: hits=%d calls=%d", got, len(gotSnapshots))
	}
	if aapl := snap.Stocks["AAPL"]; aapl == nil || aapl.Price != 201.25 ||
		aapl.ChangePct != (aapl.Price/200-1)*100 {
		t.Fatalf("美股 tick 映射/折算错误: %+v", aapl)
	}
}

// TestBinanceQuoteFeedLocksSymbolKeyCase 键名口径锁（修复后语义）：feed 的 pending/快照/lastTick
// 一律以 **normalizeBinanceSymbol(t.Symbol)** 为键——写入端归一（binance_quotes_io.go 缺陷②
// 修复），自定义 Parse（Options.Parse 是公开注入面）吐出小写 symbol 也不会在快照里造出小写键，
// 读侧大写查询必须命中。历史缺口是"写小写键、读大写键"的不对称——tick 永远查不到。
func TestBinanceQuoteFeedLocksSymbolKeyCase(t *testing.T) {
	clock := newBinanceTestClock(binanceTestEpoch)
	lower := &binanceTestParser{ticks: []binanceTick{{Symbol: "adausdt", Price: 1.5,
		TimeMs: clock.now().UnixMilli()}}}
	f := newBinanceTestFeed(t, BinanceQuoteFeedOptions{Market: "CRYPTO", Symbols: []string{"adausdt"},
		MaxTickAge: time.Minute}, clock, lower)
	if got := f.HandlePayload([]byte("frame")); got != 1 {
		t.Fatalf("小写 symbol 的 tick 仍应被收下（valid 只判非空/正价）, got=%d", got)
	}
	f.Flush()
	snap := f.Snapshot()
	// 写侧已归一：快照只许有大写键，小写键不得出现。
	if _, has := snap.Stocks["adausdt"]; has {
		t.Fatalf("快照键必须经 normalizeBinanceSymbol 归一，小写键不得入表, got=[%s]", keysOfSnapshot(snap))
	}
	if _, has := snap.Stocks["ADAUSDT"]; !has {
		t.Fatalf("大写归一键必须命中, got=[%s]", keysOfSnapshot(snap))
	}
	q := f.Quote("ADAUSDT")
	if q == nil || q.Price != 1.5 {
		t.Fatalf("大写查询必须命中归一后的 tick: %+v", q)
	}
	if got := f.SymbolStalenessMs("adausdt"); got < 0 {
		t.Fatalf("归一口径下小写查询也应命中（staleness ≥0）, got=%d", got)
	}
}

// ── 锁 8：显式 Streams 的"空订阅"拒收闸（缺陷⑤修复锁）──

// TestBinanceQuoteFeedLocksBlankExplicitStreams 构造期的"流名结果为空"守卫（缺陷⑤修复后语义）：
// 显式传 Streams 时同样先归一再判空——全空白列表必须在构造期报错，绝不产出
// `streams=` 空订阅 URL（feed 文件头禁止的"静默假活通道"）。Symbols 拼装分支口径不变。
func TestBinanceQuoteFeedLocksBlankExplicitStreams(t *testing.T) {
	if _, err := NewBinanceQuoteFeed(BinanceQuoteFeedOptions{
		Market: "CRYPTO", Streams: []string{"  ", "\t"}, Dial: binanceTestDeadDial}); err == nil {
		t.Fatal("显式 Streams 全空白必须构造期报错（不得订阅空池）")
	}
	// 半空白：有效条目保留，且入表流名已 TrimSpace（对照 URL 断言归一生效）。
	f, err := NewBinanceQuoteFeed(BinanceQuoteFeedOptions{
		Market: "CRYPTO", Streams: []string{" btcusdt@miniTicker ", ""}, Dial: binanceTestDeadDial})
	if err != nil {
		t.Fatalf("含有效条目应放行, got=%v", err)
	}
	want := "wss://stream.binance.com:9443/stream?streams=btcusdt@miniTicker"
	if got := f.URL(); got != want {
		t.Fatalf("订阅 URL got=%q want=%q", got, want)
	}
	// Symbols 全空白才会被守卫拦下（走 binanceStreamNames 拼装分支）。
	if _, err := NewBinanceQuoteFeed(BinanceQuoteFeedOptions{Market: "CRYPTO",
		Symbols: []string{"  ", ""}, Dial: binanceTestDeadDial}); err == nil {
		t.Fatal("Symbols 拼装结果为空时应报错")
	}
}
