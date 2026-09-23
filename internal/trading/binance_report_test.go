// binance_report_test.go 锁 Phase 2 回报接收器（binance_report.go）的四条契约：
//  1. §8.2 状态映射表——8 个原始状态的等值映射 + 未知状态必须被忽略（绝不臆造终态）；
//  2. 构造期收口——必填项/市场白名单/缺省周期（CRYPTO 25m、US 45m、轮询 60s）；
//  3. 帧→账本端到端——executionReport/orderReport 帧经 handleFrame 后，orders 秩推进、
//     fills 成交腿入账（TradeID 精确锚）、重放不重复入账、§F5 ext: 占位键、大整数
//     orderId 走 json.Number 不丢精度（§15.5）；
//  4. REST 差分兜底——CRYPTO 消失单查明细补投终态；US 消失单只留痕不改状态（Q8 诚实边界）。
package trading

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"quant-trading-v2/internal/data"
	"quant-trading-v2/internal/store"
)

// reportDB 独立临时库（与 advice_test 的 testDB 同形，回报测试专用命名）。
func reportDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "report.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestBinanceStatusMapLock §8.2 映射表等值锁：每个原始状态→唯一中文终态，未知必拒。
func TestBinanceStatusMapLock(t *testing.T) {
	want := map[string]string{
		"NEW": "已报", "ACCEPTED": "已报", "PARTIALLY_FILLED": "部成",
		"FILLED": "已成", "CANCELED": "已撤", "CANCELLED": "已撤",
		"EXPIRED": "废单", "REJECTED": "废单",
	}
	for raw, zh := range want {
		got, ok := mapBinanceReportStatus(raw)
		if !ok || got != zh {
			t.Fatalf("映射 %s → %q/%v，期望 %q", raw, got, ok, zh)
		}
	}
	// 大小写/空白宽容 + 未知键（含 PENDING_NEW 这类见过的变体）一律拒绝。
	if got, ok := mapBinanceReportStatus(" filled "); !ok || got != "已成" {
		t.Fatalf("大小写/空白形态应可解析: %q %v", got, ok)
	}
	for _, raw := range []string{"", "PENDING_NEW", "S", "EXPIRED_IN_FUTURE", "已撤"} {
		if _, ok := mapBinanceReportStatus(raw); ok {
			t.Fatalf("未知状态 %q 不得入库", raw)
		}
	}
}

// TestBinanceReporterConstructor 构造期收口：必填三件套 + 市场白名单 + 缺省周期。
func TestBinanceReporterConstructor(t *testing.T) {
	db := reportDB(t)
	exec := executorFor(t, newMockBinance(t), "CRYPTO")
	base := BinanceReporterOptions{Exec: exec, DB: db, UserID: "u1"}
	if _, err := NewBinanceReporter(BinanceReporterOptions{DB: db, UserID: "u1"}); err == nil {
		t.Fatal("缺 Exec 必须构造失败")
	}
	if _, err := NewBinanceReporter(base); err == nil {
		t.Fatal("Market 非 US/CRYPTO 必须构造失败")
	}
	for m, wantRenew := range map[string]time.Duration{"CRYPTO": 25 * time.Minute, "US": 45 * time.Minute} {
		opt := base
		opt.Market = m
		r, err := NewBinanceReporter(opt)
		if err != nil {
			t.Fatalf("%s 构造失败: %v", m, err)
		}
		if r.opt.PollEvery != 60*time.Second || r.opt.RenewEvery != wantRenew {
			t.Fatalf("%s 缺省周期错: poll=%v renew=%v", m, r.opt.PollEvery, r.opt.RenewEvery)
		}
	}
	// 空 Market 归一为 CN → 白名单外（"加市场不换市场"：CN 链不走币安回报器）。
	if _, err := NewBinanceReporter(BinanceReporterOptions{Exec: exec, DB: db, UserID: "u1", Market: ""}); err == nil {
		t.Fatal("空 Market 归一 CN 后必须被白名单拒绝")
	}
}

// TestCurrencyForSymbol 计价币推断：US 恒 USD；现货按长后缀优先，推断不出留空（错标比空更糟）。
func TestCurrencyForSymbol(t *testing.T) {
	cases := []struct{ market, symbol, want string }{
		{"US", "AAPL", "USD"},
		{"CRYPTO", "BTCUSDT", "USDT"},
		{"CRYPTO", "ETHFDUSD", "FDUSD"}, // FDUSD 必须先于 USD 匹配
		{"CRYPTO", "BNBUSDC", "USDC"},
		{"CRYPTO", "BTCUP", ""}, // 非计价币后缀：不猜
		{"CRYPTO", "USD", ""},   // 整串=后缀本身不算交易对
	}
	for _, c := range cases {
		if got := currencyForSymbol(c.market, c.symbol); got != c.want {
			t.Fatalf("currencyForSymbol(%s,%s)=%q，期望 %q", c.market, c.symbol, got, c.want)
		}
	}
}

// reportFixture 构造不外发的接收器（直接喂帧测解析+落库链）。
func reportFixture(t *testing.T, market string) (*BinanceReporter, *store.DB, *mockBinance) {
	t.Helper()
	m := newMockBinance(t)
	db := reportDB(t)
	r, err := NewBinanceReporter(BinanceReporterOptions{
		Exec: executorFor(t, m, market), DB: db, UserID: "u1", Market: market,
	})
	if err != nil {
		t.Fatalf("reporter: %v", err)
	}
	return r, db, m
}

// seedReportOrder 预置一条本地在途委托（回报落库的先验：orders 行存在才能"推进"而非"补插"）。
func seedReportOrder(t *testing.T, db *store.DB, market, orderID, signalID, side, status string) {
	t.Helper()
	if _, err := db.UpsertRealOrder(store.RealOrder{
		OrderID: orderID, SignalID: signalID, Code: "BTCUSDT", Side: side, Status: status,
		Price: 65000, Qty: 0.5, CreatedAt: "2026-09-23 01:00:00", UserID: "u1", Market: market,
	}); err != nil {
		t.Fatalf("seed order: %v", err)
	}
}

func orderRow(t *testing.T, db *store.DB, orderID string) store.RealOrder {
	t.Helper()
	for _, o := range ordersOf(t, db) {
		if o.OrderID == orderID {
			return o
		}
	}
	t.Fatalf("orders 里找不到 %s", orderID)
	return store.RealOrder{}
}

// ordersOf/fillsOf 拉账本行：Go 不允许多返回值函数调用与其他实参在同一个调用里混用，
// 单独收口一层（错误直接 Fatal，测试体保持单表达式读感）。
func ordersOf(t *testing.T, db *store.DB) []store.RealOrder {
	t.Helper()
	os, err := db.RealOrdersForUser("u1")
	if err != nil {
		t.Fatal(err)
	}
	return os
}

// fillsOf 读全库成交流水（回报落账的唯一物证），失败即 Fatal。
func fillsOf(t *testing.T, db *store.DB) []store.RealFill {
	t.Helper()
	fs, err := db.RealFills()
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// TestReporterSpotFrameToLedger 现货帧端到端：NEW→部成→已成 逐级秩推进、成交腿入账
// （TradeID 大整数走 json.Number 不丢精度）、重放幂等、未知状态只计数。
func TestReporterSpotFrameToLedger(t *testing.T) {
	r, db, _ := reportFixture(t, "CRYPTO")
	// 9007199254740993 = 2^53+1：任何 float64 中转都会把它掰弯，落库必须保持原串。
	const bigID = "9007199254740993"
	seedReportOrder(t, db, "CRYPTO", bigID, "buy:BTCUSDT:g1", "买入", "已报")

	frame := func(x, X, extra string) []byte {
		return []byte(`{"e":"executionReport","s":"BTCUSDT","S":"BUY","x":"` + x + `","X":"` + X +
			`","Q":"0.5","Z":"0.2","l":"0.2","L":"65100","n":"0.0002","N":"BTC","i":` + bigID +
			`,"t":4455667788,"T":1759123456789,"C":"qt-abc"` + extra + `}`)
	}
	r.handleFrame(frame("NEW", "NEW", ""))
	if got := orderRow(t, db, bigID); got.Status != "已报" {
		t.Fatalf("NEW 帧后状态=%s", got.Status)
	}
	r.handleFrame(frame("TRADE", "PARTIALLY_FILLED", ""))
	if got := orderRow(t, db, bigID); got.Status != "部成" {
		t.Fatalf("TRADE 帧后状态=%s", got.Status)
	}
	fills := fillsOf(t, db)
	if len(fills) != 1 {
		t.Fatalf("成交腿应入账 1 条，实际 %d", len(fills))
	}
	f := fills[0]
	if f.TradeID != "BTCUSDT:4455667788" || f.SignalID != "buy:BTCUSDT:g1" ||
		f.OrderID != bigID || f.Market != "CRYPTO" || f.Currency != "USDT" ||
		f.Qty != 0.2 || f.Price != 65100 || f.Fee != 0.0002 {
		t.Fatalf("成交行字段错: %+v", f)
	}
	// 同一帧重放：orders 秩守卫吸收、fills 幂等回滚，两条腿都不许长第二行。
	r.handleFrame(frame("TRADE", "PARTIALLY_FILLED", ""))
	if n := len(fillsOf(t, db)); n != 1 {
		t.Fatalf("重放后 fills 仍应 1 条，实际 %d", n)
	}
	// 未知状态：忽略计数 +1，账本零扰动。
	before := r.ignored.Load()
	r.handleFrame(frame("TRADE", "PENDING_NEW", ""))
	if r.ignored.Load() != before+1 {
		t.Fatal("未知状态必须走 ignored 计数")
	}
	if got := orderRow(t, db, bigID); got.Status != "部成" {
		t.Fatalf("未知状态不得扰动账本，状态=%s", got.Status)
	}
	// 非回报事件帧（ACCOUNT_UPDATE）同样静默忽略。
	r.handleFrame([]byte(`{"e":"accountUpdate","a":"BTCUSDT"}`))
	// 成交流水入账后，组合流信封形态（{"stream":...,"data":...}）也要能剥壳应用。
	env := []byte(`{"stream":"abc@executionReport","data":{"e":"executionReport","s":"BTCUSDT","S":"BUY","x":"CANCELED","X":"CANCELED","Q":"0.5","Z":"0.2","i":` + bigID + `}}`)
	r.handleFrame(env)
	if got := orderRow(t, db, bigID); got.Status != "已撤" {
		t.Fatalf("信封帧剥壳后应推进到已撤，实际 %s", got.Status)
	}
}

// TestReporterExtPlaceholderAndEquity 本地无单→§F5 ext: 占位键补插；美股别名帧容错解析。
func TestReporterExtPlaceholderAndEquity(t *testing.T) {
	r, db, _ := reportFixture(t, "CRYPTO")
	r.handleFrame([]byte(`{"e":"executionReport","s":"ETHUSDT","S":"SELL","x":"NEW","X":"NEW","Q":"2","i":777,"T":1759123456789}`))
	got := orderRow(t, db, "777")
	if !strings.HasPrefix(got.SignalID, "ext:") || got.SignalID != "ext:777" {
		t.Fatalf("本地无单应以 ext:<order_id> 占位，实际 %q", got.SignalID)
	}
	if got.Status != "已报" || got.Market != "CRYPTO" || got.Side != "卖出" {
		t.Fatalf("补插行字段错: %+v", got)
	}
	// 美股别名族（Q5 未实测的形状容错）：orderId 字符串 + side/status 小写键。
	ru, dbu, _ := reportFixture(t, "US")
	seedReportOrder(t, dbu, "US", "E-777", "buy:AAPL:u1", "买入", "已报")
	ru.handleFrame([]byte(`{"orderId":"E-777","symbol":"AAPL","side":"SELL","status":"FILLED","price":"231.5","quantity":"10","executedQty":"10","lastPrice":"232","fee":"0.5"}`))
	if o := orderRow(t, dbu, "E-777"); o.Status != "已成" {
		t.Fatalf("美股别名帧应推进已成，实际 %s", o.Status)
	}
	// 缺方向的美股帧必须拒（错方向入库=反向清仓风险，宁缺勿错）。
	ignored := ru.ignored.Load()
	ru.handleFrame([]byte(`{"orderId":"E-777","status":"PARTIALLY_FILLED"}`))
	if ru.ignored.Load() != ignored+1 {
		t.Fatal("缺方向的回报帧必须忽略")
	}
}

// TestReporterRestDiffSpotTerminal REST 差分兜底：本地在途单已不在 openOrders——
// 查 /api/v3/order 明细补投终态+成交；仍在途的单绝不误动。
func TestReporterRestDiffSpotTerminal(t *testing.T) {
	r, db, m := reportFixture(t, "CRYPTO")
	seedReportOrder(t, db, "CRYPTO", "555", "sell:BTCUSDT:d1", "卖出", "已报")
	seedReportOrder(t, db, "CRYPTO", "556", "sell:BTCUSDT:d2", "卖出", "已报")
	m.spotOpenOrders = `[{"orderId":556}]` // 555 消失、556 在途
	m.spotResp = func(path, rawQuery string) (int, string) {
		return 200, `{"orderId":555,"status":"FILLED","symbol":"BTCUSDT","side":"SELL",
			"price":"65000","origQty":"0.5","executedQty":"0.5","cummulativeQuoteQty":"32500",
			"updateTime":1759123456789}`
	}
	r.pollOnce(context.Background())
	if got := orderRow(t, db, "555"); got.Status != "已成" {
		t.Fatalf("消失单应经明细补投成已成，实际 %s", got.Status)
	}
	if got := orderRow(t, db, "556"); got.Status != "已报" {
		t.Fatalf("在途单不得被动，实际 %s", got.Status)
	}
	fills := fillsOf(t, db)
	if len(fills) != 1 || fills[0].Qty != 0.5 || fills[0].Price != 65000 ||
		fills[0].SignalID != "sell:BTCUSDT:d1" {
		t.Fatalf("差分成交腿入账错: %+v", fills)
	}
	if r.restDiffs.Load() != 1 {
		t.Fatalf("restDiffs 计数=%d", r.restDiffs.Load())
	}
}

// TestReporterRestDiffUSNoGuess 美股消失单诚实边界：Q8 明细未实测——只留痕，不猜终态。
func TestReporterRestDiffUSNoGuess(t *testing.T) {
	r, db, m := reportFixture(t, "US")
	seedReportOrder(t, db, "US", "E-9", "buy:AAPL:x1", "买入", "已报")
	m.openOrders = `[]` // 不在途
	r.pollOnce(context.Background())
	if got := orderRow(t, db, "E-9"); got.Status != "已报" {
		t.Fatalf("美股消失单状态必须保持已报（不许臆造成/撤），实际 %s", got.Status)
	}
	if n := len(fillsOf(t, db)); n != 0 {
		t.Fatalf("美股差分路径不得入账成交，实际 %d 条", n)
	}
}

// fakeWsTransport 内存管道版 WsTransport：测试经 in 通道投帧，close 后读侧报错触发重连语义。
type fakeWsTransport struct{ in chan []byte }

// ReadMessage 阻塞取 in 通道帧；通道关闭即返回 Canceled——让"WS 断线"在内存管道上可脚本化。
func (f *fakeWsTransport) ReadMessage() ([]byte, error) {
	b, ok := <-f.in
	if !ok {
		return nil, context.Canceled
	}
	return b, nil
}
func (f *fakeWsTransport) WriteMessage([]byte) error { return nil }
func (f *fakeWsTransport) Close() error              { return nil }

// TestReporterListenKeyAndWSLeg Start 端到端（假柜台+假 dialer）：listenKey 用 POST 开通、
// WS URL 携带 key+@executionReport 后缀、帧经通道协程真实落库；Stop 幂等且不再复用。
func TestReporterListenKeyAndWSLeg(t *testing.T) {
	m := newMockBinance(t)
	db := reportDB(t)
	tr := &fakeWsTransport{in: make(chan []byte, 8)}
	var mu sync.Mutex
	var dialed []string
	exec := executorFor(t, m, "CRYPTO")
	r, err := NewBinanceReporter(BinanceReporterOptions{
		Exec: exec, DB: db, UserID: "u1", Market: "CRYPTO",
		Dial: func(ctx context.Context, url string) (data.WsTransport, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err() // 停摆时不泄漏拨号
			default:
			}
			mu.Lock()
			dialed = append(dialed, url)
			mu.Unlock()
			return tr, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	seedReportOrder(t, db, "CRYPTO", "3001", "buy:BTCUSDT:w1", "买入", "已报")
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(dialed)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	url := strings.Join(dialed, ",")
	mu.Unlock()
	if !strings.Contains(url, "LK-TEST@executionReport") {
		t.Fatalf("WS URL 未携带 listenKey/后缀: %s", url)
	}
	if len(m.queries("/api/v3/userDataStream POST")) == 0 {
		t.Fatal("listenKey 必须走 POST /api/v3/userDataStream")
	}
	tr.in <- []byte(`{"e":"executionReport","s":"BTCUSDT","S":"BUY","x":"TRADE","X":"FILLED",
		"Q":"0.5","Z":"0.5","l":"0.5","L":"65200","n":"0.001","i":3001,"t":987,"T":1759123456789}`)
	deadline = time.Now().Add(3 * time.Second)
	for r.events.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := orderRow(t, db, "3001"); got.Status != "已成" {
		t.Fatalf("WS 帧未落库: %s events=%d", got.Status, r.events.Load())
	}
	if st := r.Stats(); st["ws"] != true || st["listen_key"] != true {
		t.Fatalf("Stats 双腿状态错: %v", st)
	}
	r.Stop()
	r.Stop() // 幂等
	if err := r.Start(); err == nil {
		t.Fatal("Stop 后不得复用")
	}
	close(tr.in) // 放掉残留读侧，防 goroutine 卡死
}

// TestReporterListenKeyFailureDegrades listenKey 取不到=降级不死亡：WS 腿不起、
// REST-only 照常轮询、告警发到 OnAlert（三条腿的降级契约）。
func TestReporterListenKeyFailureDegrades(t *testing.T) {
	m := newMockBinance(t)
	m.listenKeyBody = `not-a-json`
	var mu sync.Mutex
	var alerts int
	dialed := 0
	r, err := NewBinanceReporter(BinanceReporterOptions{
		Exec: executorFor(t, m, "CRYPTO"), DB: reportDB(t), UserID: "u1", Market: "CRYPTO",
		OnAlert: func(_, _, _ string) { mu.Lock(); alerts++; mu.Unlock() },
		Dial: func(ctx context.Context, url string) (data.WsTransport, error) {
			mu.Lock()
			dialed++
			mu.Unlock()
			return nil, context.Canceled
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		a := alerts
		mu.Unlock()
		if a > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.Stop()
	mu.Lock()
	defer mu.Unlock()
	if alerts == 0 {
		t.Fatal("listenKey 首取失败必须告警")
	}
	if dialed != 0 {
		t.Fatal("没有 listenKey 不得拨 WS（假活腿）")
	}
	if st := r.Stats(); st["listen_key"] != false || st["ws"] != false {
		t.Fatalf("降级态 Stats 错: %v", st)
	}
}

// TestReporterUSListenKeySignedLeg 美股腿形状差异：listenKey 走签名面
// （/sapi/v1/equity/listenKey 带 signature），WS URL 挂 nbstream/equity + @orderReport。
func TestReporterUSListenKeySignedLeg(t *testing.T) {
	m := newMockBinance(t)
	db := reportDB(t)
	var mu sync.Mutex
	var urls []string
	r, err := NewBinanceReporter(BinanceReporterOptions{
		Exec: executorFor(t, m, "US"), DB: db, UserID: "u1", Market: "US",
		Dial: func(ctx context.Context, u string) (data.WsTransport, error) {
			mu.Lock()
			urls = append(urls, u)
			mu.Unlock()
			return &fakeWsTransport{in: make(chan []byte)}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(urls)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.Stop()
	mu.Lock()
	defer mu.Unlock()
	if len(urls) == 0 || !strings.Contains(urls[0], "LK-US@orderReport") ||
		!strings.HasPrefix(urls[0], "wss://nbstream.binance.com/equity/ws/") {
		t.Fatalf("美股 WS URL 形状错: %v", urls)
	}
	q := m.queries("/sapi/v1/equity/listenKey POST")
	if len(q) == 0 || !strings.Contains(q[0], "signature=") {
		t.Fatalf("美股 listenKey 必须走签名面，实际 queries=%v", q)
	}
}
