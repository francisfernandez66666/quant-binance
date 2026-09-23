// 文件职责：§战法批-4 派发链行为锁——在测试内手工搭出与 registry 同构的
// 「纸面柜台 + 控制器 + 风控闸 + 派发核」栈，用研究库日K/注入行情/注入事件
// 驱动四条链跑通并逐条落账：
//
//	US 做多（AAPL 金叉→买入）、US 做空（NVDA 死叉→卖出开空）、
//	CRYPTO 做多（BTCUSDT 金叉→买入）、CRYPTO 做空（ETHUSDT 利空事件→卖出开空）；
//
// 外加合约腿（umfutures 档案下同四向跑通）、止盈/止损退出腿、幂等重投、
// 关闭态零行为矩阵（dispatch 关/manual/无台/置信闸/轮内帽）。
// English: §XASSET batch-4 dispatch-chain e2e locks — a test-local stack mirroring the
// registry wiring (paper desk + controller + gates + dispatcher), proving all four
// long/short × spot/futures chains land orders and positions, plus TP/SL exits,
// same-day idempotency and the factory-off zero-behavior matrix.
package engine

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quant-trading-v2/internal/config"
	"quant-trading-v2/internal/data"
	"quant-trading-v2/internal/store"
	"quant-trading-v2/internal/trading"
)

// dspSeedBars 往研究库 daily 表灌一段收盘序列（open=high=low=close，日期从 end 往前排）。
func dspSeedBars(t *testing.T, db *store.DB, key string, closes []float64, end time.Time) {
	t.Helper()
	cols := []string{"ts_code", "trade_date", "open", "high", "low", "close", "vol", "amount"}
	rows := make([]map[string]any, 0, len(closes))
	n := len(closes)
	for i, c := range closes {
		d := end.AddDate(0, 0, -(n - 1 - i)).Format("20060102")
		rows = append(rows, map[string]any{"ts_code": key, "trade_date": d,
			"open": c, "high": c, "low": c, "close": c, "vol": 1.0, "amount": c})
	}
	if _, err := db.InsertRows("daily", cols, rows); err != nil {
		t.Fatalf("灌日K %s: %v", key, err)
	}
}

// dspGolden / dspDeath：与 xasset 测试同族的两副形状（事件恰好落在最后一根）。
func dspGolden() []float64 {
	base := make([]float64, 0, 24)
	for i := 0; i < 20; i++ {
		base = append(base, 100-float64(i))
	}
	return append(base, 82, 88, 96, 106)
}

func dspDeath() []float64 {
	base := make([]float64, 0, 24)
	for i := 0; i < 20; i++ {
		base = append(base, 100+float64(i))
	}
	return append(base, 118, 112, 104, 94)
}

func dspFlat() []float64 {
	out := make([]float64, 30)
	for i := range out {
		out[i] = 50 // 走平：MA 无穿越、RSI=50 无回升事件
	}
	return out
}

// dspStack 测试局：配置管理器 + 双库 + 纸面柜台控制器 + 派发核 + 可注入的价/事件源。
type dspStack struct {
	d      *binanceDispatcher
	real   *store.DB
	d1     *store.DB
	cfgMgr *config.Manager
	prices map[string]float64
	events map[string][]data.XEvent
}

// dspNewStack 装配与 registry 同构的最小派发栈（纸面柜台 + 常可借证据 + 关键词打分器）。
func dspNewStack(t *testing.T, cfg config.BinanceConfig) *dspStack {
	t.Helper()
	mgr := config.NewManager("")
	mgr.SetBinanceConfigFor("u1", &cfg)
	realDB, err := store.Open(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	d1DB, err := store.Open(filepath.Join(t.TempDir(), "trading.db"))
	if err != nil {
		t.Fatal(err)
	}
	st := &dspStack{real: realDB, d1: d1DB, cfgMgr: mgr,
		prices: map[string]float64{}, events: map[string][]data.XEvent{}}
	router := trading.NewBrokerRouter(nil)
	for _, mkt := range []string{"US", "CRYPTO"} {
		view := config.BinanceBrokerView{Cfg: cfg, Market: mkt}
		if !view.PaperActive() {
			continue
		}
		prof := cfg.Spot
		if mkt == "US" {
			prof = cfg.Stock
		}
		pfee := 0.0
		if view.FuturesActive() {
			pfee = 0.0005
		}
		pe, perr := trading.NewBinancePaperExecutor(trading.BinancePaperOptions{
			DB: realDB, UserID: "u1", Market: mkt, InitialCash: prof.PaperCash,
			FeeRate: pfee, ShortMarginRate: prof.ShortMarginRate,
			GetPrice: func(code string) float64 { return st.prices[code] },
		})
		if perr != nil {
			t.Fatal(perr)
		}
		ctrl := trading.NewController(pe, realDB, "u1", view, nil, "binance")
		ctrl.SetShortBorrowEvidenceSource(func(market, code string) (bool, string) {
			return true, "纸面模拟借券"
		})
		router.Register(mkt, ctrl)
	}
	st.d = newBinanceDispatcher(mgr, "u1", d1DB, realDB, router,
		func(market, code string) float64 { return st.prices[code] },
		func(market string) []data.XEvent { return st.events[market] },
		data.NewXEventScorer(nil))
	return st
}

// dspPaperCfg 纸面盘四腿的公共配置：数据面开、交易面关、双市场 paper、派发自动档。
func dspPaperCfg() config.BinanceConfig {
	c := config.DefaultBinanceConfig()
	c.Enabled, c.DataPlane, c.Mode = false, true, "auto"
	c.Dispatch = config.BinanceDispatchConfig{Enabled: true, BearEnabled: true, MinConfidence: 0.25, EverySec: 60}
	for _, p := range []*config.BinanceMarketProfile{&c.Stock, &c.Spot} {
		p.Enabled, p.Paper, p.PaperCash = true, true, 1_000_000
		p.FixedAmount, p.DailyMaxBuys, p.DailyBudgetAmount, p.MaxPositions = 1000, 50, 1e9, 50
		p.ShortMarginRate = 0.5
	}
	c.Stock.QuoteSymbols = []string{"AAPL", "NVDA"}
	c.Spot.QuoteSymbols = []string{"BTCUSDT", "ETHUSDT"}
	c.RiskGate.MaxOrderAmount = 1e8
	c.RiskGate.DayLossLimitPct = 90
	c.RiskGate.SingleStockValuePct = 90
	return c
}

// countOrders 按 signal_id 前缀统计委托行数。
func countOrders(t *testing.T, db *store.DB, prefix string) int {
	t.Helper()
	orders, err := db.RealOrdersForUser("u1")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, o := range orders {
		if strings.HasPrefix(o.SignalID, prefix) {
			n++
		}
	}
	return n
}

// findPos 按 (code, side) 找持仓行。
func findPos(t *testing.T, db *store.DB, code, side string) *store.RealPosition {
	t.Helper()
	ps, err := db.RealPositionsForUser("u1")
	if err != nil {
		t.Fatal(err)
	}
	for i := range ps {
		if ps[i].TsCode == code && ps[i].Side == side {
			return &ps[i]
		}
	}
	return nil
}

// TestDispatchFourChains 四链主证：现货形态下 US/CRYPTO 各出做多+做空各一，
// 订单落库、方向正确、持仓/空头腿真实入账；同日重投幂等不多单。
func TestDispatchFourChains(t *testing.T) {
	now := time.Now()
	st := dspNewStack(t, dspPaperCfg())
	dspSeedBars(t, st.d1, "AAPL.US", dspGolden(), now)
	dspSeedBars(t, st.d1, "NVDA.US", dspDeath(), now)
	dspSeedBars(t, st.d1, "BTCUSDT", dspGolden(), now)
	dspSeedBars(t, st.d1, "ETHUSDT", dspFlat(), now)
	goldenLast := dspGolden()[len(dspGolden())-1] // 106
	deathLast := dspDeath()[len(dspDeath())-1]    // 94
	st.prices["AAPL"], st.prices["NVDA"], st.prices["BTCUSDT"], st.prices["ETHUSDT"] =
		goldenLast, deathLast, goldenLast, 50
	st.events["CRYPTO"] = []data.XEvent{{
		Market: "CRYPTO", Symbol: "ETHUSDT", Title: "Exchange hacked: funds stolen in exploit",
		URL: "https://x/1", PublishedAt: now,
	}}

	st.d.tickAll(now)

	us, cr := st.d.reports()["US"], st.d.reports()["CRYPTO"]
	if us.Desk != "paper" || cr.Desk != "paper" {
		t.Fatalf("纸面台未识别: US=%+v CRYPTO=%+v", us, cr)
	}
	if us.Placed != 2 || cr.Placed != 2 {
		t.Fatalf("四链应各投 2 单: US=%+v CRYPTO=%+v", us, cr)
	}
	if us.Rejected > 0 || cr.Rejected > 0 {
		t.Fatalf("不应有拒单: %+v %+v", us, cr)
	}
	// 委托方向落账锁
	orders, err := st.real.RealOrdersForUser("u1")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"xbuy:AAPL:": trading.SideBuy, "xshort:NVDA:": trading.SideShortOpen,
		"xbuy:BTCUSDT:": trading.SideBuy, "xshort:ETHUSDT:": trading.SideShortOpen,
	}
	for prefix, side := range want {
		hit := 0
		for _, o := range orders {
			if strings.HasPrefix(o.SignalID, prefix) {
				hit++
				if o.Side != side {
					t.Fatalf("%s 方向错: %s", prefix, o.Side)
				}
			}
		}
		if hit != 1 {
			t.Fatalf("%s 应恰落 1 单，实际 %d（notes=%v）", prefix, hit, append(us.Notes, cr.Notes...))
		}
	}
	// 账簿腿：多头有仓、空头有 short 向持仓
	if p := findPos(t, st.real, "AAPL", "long"); p == nil || p.Qty <= 0 {
		t.Fatal("AAPL 多头未入账")
	}
	if p := findPos(t, st.real, "NVDA", "short"); p == nil || p.Qty <= 0 {
		t.Fatal("NVDA 空头未入账")
	}
	if p := findPos(t, st.real, "BTCUSDT", "long"); p == nil {
		t.Fatal("BTCUSDT 多头未入账")
	}
	if p := findPos(t, st.real, "ETHUSDT", "short"); p == nil {
		t.Fatal("ETHUSDT 空头未入账")
	}

	// 同日重投：信号形状不变 → 幂等键全部命中，账簿不增单。
	before := len(orders)
	st.d.tickAll(now.Add(61 * time.Second))
	orders2, _ := st.real.RealOrdersForUser("u1")
	if len(orders2) != before {
		t.Fatalf("同日重投不得新增委托行: %d→%d", before, len(orders2))
	}
}

// TestDispatchFuturesProfileChains 合约腿：CRYPTO 档案换 umfutures 后四向同样跑通
// （纸面柜台不区分产品线，验证的是派发/风控链在合约档案下零卡壳）。
func TestDispatchFuturesProfileChains(t *testing.T) {
	now := time.Now()
	cfg := dspPaperCfg()
	cfg.Spot.ProductType = "umfutures"
	cfg.Spot.Leverage = 3
	cfg.Spot.QuoteSymbols = []string{"SOLUSDT"}
	st := dspNewStack(t, cfg)
	dspSeedBars(t, st.d1, "SOLUSDT", dspGolden(), now)
	st.prices["SOLUSDT"], st.prices["XRPUSDT"] = 106, 1.2
	st.events["CRYPTO"] = []data.XEvent{{
		Market: "CRYPTO", Symbol: "XRPUSDT", Title: "Ledger crash after breach", URL: "https://x/2", PublishedAt: now,
	}}
	// 多腿=技术金叉（SOL）、空腿=利空事件（XRP）：单向簿一票一行，两腿必须落在不同代号上。
	st.d.tickAll(now)
	if countOrders(t, st.real, "xbuy:SOLUSDT:") != 1 || countOrders(t, st.real, "xshort:XRPUSDT:") != 1 {
		orders, _ := st.real.RealOrdersForUser("u1")
		t.Fatalf("合约档案四向未跑通: %+v", orders)
	}
}

// TestDispatchExitsTPSL 退出腿：浮盈触止盈平多、浮亏触止损平空，退出单不占入场帽。
func TestDispatchExitsTPSL(t *testing.T) {
	now := time.Now()
	cfg := dspPaperCfg()
	cfg.Dispatch.TakeProfitPct = 3
	cfg.Dispatch.StopLossPct = 3
	st := dspNewStack(t, cfg)
	dspSeedBars(t, st.d1, "BTCUSDT", dspGolden(), now)
	dspSeedBars(t, st.d1, "ETHUSDT", dspFlat(), now)
	st.prices["BTCUSDT"], st.prices["ETHUSDT"] = 106, 50
	st.events["CRYPTO"] = []data.XEvent{{
		Market: "CRYPTO", Symbol: "ETHUSDT", Title: "Token collapse after fraud charges", URL: "https://x/3", PublishedAt: now,
	}}
	st.d.tickAll(now) // 先开多 BTC + 开空 ETH

	// 价格跳动：BTC +5%（止盈）、ETH +5%（空头浮亏触止损）
	st.prices["BTCUSDT"], st.prices["ETHUSDT"] = 111.3, 52.5
	st.d.tickAll(now.Add(61 * time.Second))
	if countOrders(t, st.real, "xexit:BTCUSDT:long:tp:") != 1 {
		t.Fatal("BTCUSDT 止盈平多单未落")
	}
	if countOrders(t, st.real, "xexit:ETHUSDT:short:sl:") != 1 {
		t.Fatal("ETHUSDT 止损平空单未落")
	}
	if p := findPos(t, st.real, "BTCUSDT", "long"); p != nil && p.Qty > 0 {
		t.Fatalf("止盈后多头仍挂账: %+v", p)
	}
	if p := findPos(t, st.real, "ETHUSDT", "short"); p != nil && p.Qty > 0 {
		t.Fatalf("止损后空头仍挂账: %+v", p)
	}
}

// TestDispatchOffMatrix 关闭态零行为矩阵：派发关/manual 档/无台/置信闸/轮内帽。
func TestDispatchOffMatrix(t *testing.T) {
	now := time.Now()

	// ① dispatch.enabled=false（出厂态）：零报告零委托。
	cfg := dspPaperCfg()
	cfg.Dispatch.Enabled = false
	st := dspNewStack(t, cfg)
	dspSeedBars(t, st.d1, "AAPL.US", dspGolden(), now)
	st.prices["AAPL"] = 106
	st.d.tickAll(now)
	if len(st.d.reports()) != 0 || countOrders(t, st.real, "xbuy:") != 0 {
		t.Fatal("派发关必须零行为")
	}

	// ② mode=manual：信号照出、一单不发。
	cfg2 := dspPaperCfg()
	cfg2.Mode = "manual"
	st2 := dspNewStack(t, cfg2)
	dspSeedBars(t, st2.d1, "AAPL.US", dspGolden(), now)
	st2.prices["AAPL"] = 106
	st2.d.tickAll(now)
	rep := st2.d.reports()["US"]
	if rep.Signals < 1 || rep.Placed != 0 || countOrders(t, st2.real, "xbuy:") != 0 {
		t.Fatalf("manual 档只观测: %+v", rep)
	}

	// ③ 无台（paper=false 且无凭证=Noop 数据面）：不投单。
	cfg3 := dspPaperCfg()
	cfg3.Stock.Paper, cfg3.Spot.Paper = false, false
	st3 := &dspStack{prices: map[string]float64{}, events: map[string][]data.XEvent{}}
	mgr3 := config.NewManager("")
	mgr3.SetBinanceConfigFor("u1", &cfg3)
	realDB, _ := store.Open(filepath.Join(t.TempDir(), "live.db"))
	d1DB, _ := store.Open(filepath.Join(t.TempDir(), "trading.db"))
	router3 := trading.NewBrokerRouter(nil)
	for _, mkt := range []string{"US", "CRYPTO"} {
		view := config.BinanceBrokerView{Cfg: cfg3, Market: mkt}
		router3.Register(mkt, trading.NewController(trading.NoopExecutor{}, realDB, "u1", view, nil, "binance"))
	}
	st3.d = newBinanceDispatcher(mgr3, "u1", d1DB, realDB, router3, nil, nil, data.NewXEventScorer(nil))
	st3.real, st3.d1 = realDB, d1DB
	dspSeedBars(t, d1DB, "AAPL.US", dspGolden(), now)
	st3.prices["AAPL"] = 106
	st3.d.tickAll(now)
	if r := st3.d.reports()["US"]; r.Desk != "" || r.Placed != 0 {
		t.Fatalf("无台（Noop 数据面）不得投单: %+v", r)
	}

	// ④ 置信闸：门槛 0.95 挡住金叉腿（conf≈0.6~0.9）。
	cfg4 := dspPaperCfg()
	cfg4.Dispatch.MinConfidence = 0.95
	st4 := dspNewStack(t, cfg4)
	dspSeedBars(t, st4.d1, "AAPL.US", dspGolden(), now)
	st4.prices["AAPL"] = 106
	st4.d.tickAll(now)
	if r := st4.d.reports()["US"]; r.Signals != 0 || countOrders(t, st4.real, "xbuy:") != 0 {
		t.Fatalf("置信闸未拦: %+v", r)
	}

	// ⑤ 轮内帽：MaxLiveOrders=1 时 US 两信号只投一。
	cfg5 := dspPaperCfg()
	cfg5.Dispatch.MaxLiveOrders = 1
	st5 := dspNewStack(t, cfg5)
	dspSeedBars(t, st5.d1, "AAPL.US", dspGolden(), now)
	dspSeedBars(t, st5.d1, "NVDA.US", dspDeath(), now)
	st5.prices["AAPL"], st5.prices["NVDA"] = 106, 94
	st5.d.tickAll(now)
	if r := st5.d.reports()["US"]; r.Signals != 2 || r.Placed != 1 {
		t.Fatalf("轮内帽未生效: %+v", r)
	}
}

// TestDispatchNoNewsNoBars 数据缺位姿势：研究库无档=不评估；事件源缺位=只跑技术腿；
// 派发器对 nil 价源/事件源闭包同样安全（fail-close 无价不发）。
func TestDispatchNoNewsNoBars(t *testing.T) {
	now := time.Now()
	st := dspNewStack(t, dspPaperCfg())
	st.d.tickAll(now) // 全空：零 panic；有台无数据=零信号零投单（报告只留观测）
	for mkt, r := range st.d.reports() {
		if r.Signals != 0 || r.Placed != 0 {
			t.Fatalf("无数据必须零信号零投单 %s: %+v", mkt, r)
		}
	}
	// 有 K 线无价源：市价单参考价退到末收盘，仍能成链（prices=nil 已在③测过，这里测有档无事件）
	dspSeedBars(t, st.d1, "AAPL.US", dspGolden(), now)
	st.d.tickAll(now.Add(61 * time.Second))
	if countOrders(t, st.real, "xbuy:AAPL:") != 1 {
		t.Fatal("末收盘回退参考价下单未跑通")
	}
}
