// 文件职责：§战法批 币安纸面成交柜台（binance_paper.go）行为单测——四方向现金/持仓
// 结算、fail-close 拒单族、金额换量、重启回放资金连续性、State/Health 读面。
// English: behavior tests for the paper desk — four-side cash & position settlement,
// fail-close refusals, notional→qty folding, restart replay continuity, read surfaces.
package trading

import (
	"math"
	"path/filepath"
	"strings"
	"testing"

	"quant-trading-v2/internal/store"
)

// paperTestDB 开临时库供纸面柜台落账（Cleanup 自动关连接，测试零残留）。
func paperTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "paper.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newPaper 按市场/初始资金/融券保证金率构造纸面执行器，构造失败直接 Fatal。
func newPaper(t *testing.T, db *store.DB, market string, cash, marginRate float64) *BinancePaperExecutor {
	t.Helper()
	e, err := NewBinancePaperExecutor(BinancePaperOptions{
		DB: db, UserID: "u1", Market: market, InitialCash: cash, ShortMarginRate: marginRate,
	})
	if err != nil {
		t.Fatalf("new paper: %v", err)
	}
	return e
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func reqOf(id, code, side string, qty, price float64) OrderRequest {
	return OrderRequest{SignalID: id, Code: code, Name: code, Side: side, Price: price, Qty: qty}
}

// mustPlacePlace 断言受理成功的快捷门（失败直接 t.Fatalf 带原因）。
func mustPlace(t *testing.T, fn func(OrderRequest) (*OrderResult, error), req OrderRequest, market string) string {
	t.Helper()
	req.Market = market
	res, err := fn(req)
	if err != nil {
		t.Fatalf("place err: %v", err)
	}
	if !res.OK {
		t.Fatalf("place rejected: %s", res.Err)
	}
	return res.OrderID
}

// TestPaperLongRoundTrip 做多往返：开仓含费扣现金、平仓回笼含费，全平后持仓行清除。
func TestPaperLongRoundTrip(t *testing.T) {
	db := paperTestDB(t)
	e := newPaper(t, db, "CRYPTO", 100000, 1)
	req := reqOf("buy:BTCUSDT:s1", "BTCUSDT", SideBuy, 1, 100)
	req.Market = "CRYPTO"
	mustPlace(t, e.PlaceBuy, req, "CRYPTO")
	// 100 + 0.1 费
	if !approx(e.Cash(), 100000-100.1) {
		t.Fatalf("开多后现金错位: %v", e.Cash())
	}
	positions, _ := db.RealPositionsForUser("u1")
	if len(positions) != 1 || positions[0].Side != "long" || !approx(positions[0].CostPrice, 100.1) {
		t.Fatalf("多头持仓行错位: %+v", positions)
	}
	orders, _ := db.RealOrdersForUser("u1")
	if len(orders) != 1 || orders[0].Status != "已成" {
		t.Fatalf("委托行未推进到已成: %+v", orders)
	}
	sell := reqOf("sell:BTCUSDT:s1", "BTCUSDT", SideSell, 1, 110)
	mustPlace(t, e.PlaceSell, sell, "CRYPTO")
	// 现金 = 99899.9 + (110 − 0.11) = 100009.79
	if !approx(e.Cash(), 100009.79) {
		t.Fatalf("平多后现金错位: %v", e.Cash())
	}
	positions, _ = db.RealPositionsForUser("u1")
	for _, p := range positions {
		if p.TsCode == "BTCUSDT" {
			t.Fatalf("全平后持仓行应清除: %+v", p)
		}
	}
}

// TestPaperShortRoundTrip 做空往返：开空按保证金率冻结、平空返还本金+盈亏（未含费均价基准，
// 双边各扣一次费）、空头行方向章正确。
func TestPaperShortRoundTrip(t *testing.T) {
	db := paperTestDB(t)
	e := newPaper(t, db, "CRYPTO", 100000, 1)
	open := reqOf("short_open:BTCUSDT:s1", "BTCUSDT", SideShortOpen, 0.5, 60000)
	mustPlace(t, e.PlaceSell, open, "CRYPTO")
	// 冻结 30000 + 开空费 0.5×60000×0.001=30 → 69970
	if !approx(e.Cash(), 69970) {
		t.Fatalf("开空后现金错位: %v", e.Cash())
	}
	positions, _ := db.RealPositionsForUser("u1")
	if len(positions) != 1 || positions[0].Side != "short" || !approx(positions[0].Qty, 0.5) {
		t.Fatalf("空头持仓行错位: %+v", positions)
	}
	cover := reqOf("short_cover:BTCUSDT:s1", "BTCUSDT", SideShortCover, 0.5, 59000)
	mustPlace(t, e.PlaceBuy, cover, "CRYPTO")
	// 返还 30000 + 盈亏 (60000−59000)×0.5=500 − 平仓费 0.5×59000×0.001=29.5 → 100440.5
	if !approx(e.Cash(), 100440.5) {
		t.Fatalf("平空盈利后现金错位: %v", e.Cash())
	}
	positions, _ = db.RealPositionsForUser("u1")
	for _, p := range positions {
		if p.TsCode == "BTCUSDT" && p.Qty > 0 {
			t.Fatalf("平空后应无空头残留: %+v", p)
		}
	}
}

// TestPaperFailCloseRefusals fail-close 拒单族：现金不足/无仓可平/超量平仓/市场章不符/
// 方向错方法/价不可得——全部 OK:false 且账簿零落行。
func TestPaperFailCloseRefusals(t *testing.T) {
	db := paperTestDB(t)
	e := newPaper(t, db, "CRYPTO", 1000, 1)
	cases := []struct {
		name    string
		fn      func(OrderRequest) (*OrderResult, error)
		req     OrderRequest
		market  string
		wantErr string
	}{
		{"现金不足", e.PlaceBuy, reqOf("x1", "BTCUSDT", SideBuy, 100, 60000), "CRYPTO", "现金不足"},
		{"无空头可平", e.PlaceBuy, reqOf("x2", "ETHUSDT", SideShortCover, 1, 3000), "CRYPTO", "无对应可平仓位"},
		{"平多以多易空", e.PlaceSell, reqOf("x3", "ETHUSDT", SideSell, 1, 3000), "CRYPTO", "无对应可平仓位"},
		{"市场章不符", e.PlaceBuy, reqOf("x4", "BTCUSDT", SideBuy, 1, 10), "US", "市场章不符"},
		{"开空用买向方法", e.PlaceBuy, reqOf("x5", "BTCUSDT", SideShortOpen, 1, 10), "CRYPTO", "PlaceBuy 只接受"},
		{"平空用卖向方法", e.PlaceSell, reqOf("x6", "BTCUSDT", SideShortCover, 1, 10), "CRYPTO", "PlaceSell 只接受"},
		{"无参考价", e.PlaceBuy, reqOf("x7", "BTCUSDT", SideBuy, 1, 0), "CRYPTO", "无可用参考价"},
		{"无量无金额", e.PlaceBuy, reqOf("x8", "BTCUSDT", SideBuy, 0, 10), "CRYPTO", "数量与金额均缺失"},
	}
	for i, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			r := c.req
			r.Market = c.market
			res, err := c.fn(r)
			if err != nil {
				t.Fatalf("期望业务拒单不应报链路错误: %v", err)
			}
			if res.OK || !strings.Contains(res.Err, c.wantErr) {
				t.Fatalf("拒单口径错位: ok=%v err=%q want 含 %q", res.OK, res.Err, c.wantErr)
			}
			_ = i
		})
	}
	if fills, _ := db.RealFills(); len(fills) != 0 {
		t.Fatalf("拒单族不得留下任何成交行: %d", len(fills))
	}
	if orders, _ := db.RealOrdersForUser("u1"); len(orders) != 0 {
		t.Fatalf("拒单族不得留下任何委托行: %d", len(orders))
	}
}

// TestPaperOversellRejectedButRoundTripAllowed 超量平仓拒单 + 分批开平现金连续性。
func TestPaperOversellRejectedButRoundTripAllowed(t *testing.T) {
	db := paperTestDB(t)
	e := newPaper(t, db, "CRYPTO", 100000, 1)
	mustPlace(t, e.PlaceSell, reqOf("o1", "BTCUSDT", SideShortOpen, 1, 60000), "CRYPTO")
	over := reqOf("o2", "BTCUSDT", SideShortCover, 2, 60000)
	over.Market = "CRYPTO"
	res, err := e.PlaceBuy(over)
	if err != nil || res.OK || !strings.Contains(res.Err, "超过持仓") {
		t.Fatalf("超量平空必须拒: %+v %v", res, err)
	}
	mustPlace(t, e.PlaceBuy, reqOf("o3", "BTCUSDT", SideShortCover, 0.4, 58000), "CRYPTO")
	mustPlace(t, e.PlaceBuy, reqOf("o4", "BTCUSDT", SideShortCover, 0.6, 61000), "CRYPTO")
	// 冻结 60000+费60 → 39940；返 24000+800−23.2 → 64716.8；返 36000−600−36.6 → 100080.2
	if !approx(e.Cash(), 100080.2) {
		t.Fatalf("分批平仓现金错位: %v", e.Cash())
	}
}

// TestPaperNotionalFoldsToQty 金额/Notional 单折叠成数量（合约市价无 quoteOrderQty 的派发前置
// 在执行器里的镜像形态）。
func TestPaperNotionalFoldsToQty(t *testing.T) {
	db := paperTestDB(t)
	e := newPaper(t, db, "US", 100000, 1)
	r := OrderRequest{SignalID: "buy:AAPL.US:n1", Code: "AAPL.US", Name: "Apple", Side: SideBuy, Price: 200, Notional: 1000, Market: "US"}
	mustPlace(t, e.PlaceBuy, r, "US")
	fills, _ := db.RealFills()
	if len(fills) != 1 || !approx(fills[0].Qty, 5) {
		t.Fatalf("Notional 换量错位: %+v", fills)
	}
	if fills[0].Currency != "USD" { // 本仓口径：US 腿计价币恒 USD（currencyForSymbol）
		t.Fatalf("US 腿计价币章错位: %+v", fills[0])
	}
}

// TestPaperCashSurvivesRestart 重启回放：新柜台实例读同一账簿，现金与空头影子台账连续
// （不回到初始值、不把开空冻结凭空蒸发）；重启后的新成交必须真正落成交行
// （实例盐保证 orderID 跨重启唯一，判重锚不吃新单——撞车回归锁）。
func TestPaperCashSurvivesRestart(t *testing.T) {
	db := paperTestDB(t)
	e := newPaper(t, db, "CRYPTO", 100000, 1)
	mustPlace(t, e.PlaceBuy, reqOf("r1", "ETHUSDT", SideBuy, 2, 3000), "CRYPTO")
	mustPlace(t, e.PlaceSell, reqOf("r2", "BTCUSDT", SideShortOpen, 0.5, 60000), "CRYPTO")
	before := e.Cash() // 100000 − (6000+6) − (30000+30) = 63964
	if !approx(before, 63964) {
		t.Fatalf("种账序列现金错位: %v", before)
	}
	e2 := newPaper(t, db, "CRYPTO", 100000, 1)
	if !approx(e2.Cash(), before) {
		t.Fatalf("重启后现金断裂: %v vs %v", e2.Cash(), before)
	}
	// 影子台账同样连续：平掉回放重建的空头，返还冻结+盈亏一致
	mustPlace(t, e2.PlaceBuy, reqOf("r3", "BTCUSDT", SideShortCover, 0.5, 59000), "CRYPTO")
	// 63964 + 返还 30000 + 盈亏 500 − 平仓费 29.5 = 94434.5
	if !approx(e2.Cash(), 94434.5) {
		t.Fatalf("回放后平空现金错位: %v", e2.Cash())
	}
	fills, _ := db.RealFills()
	if len(fills) != 3 {
		t.Fatalf("重启后新成交被判重锚吞掉（fills 应 3 行实为 %d）: %+v", len(fills), fills)
	}
}

// TestPaperStateHealthCancel 读面：State 按市场过滤持仓/委托、Account 章为 paper:<市场>、
// Health 恒真、Cancel 幂等 nil。
func TestPaperStateHealthCancel(t *testing.T) {
	db := paperTestDB(t)
	// 同库再开一个 US 柜台种一行 US 持仓，验证跨市场过滤
	us := newPaper(t, db, "US", 100000, 1)
	mustPlace(t, us.PlaceBuy, reqOf("s-us", "AAPL.US", SideBuy, 1, 200), "US")
	cp := newPaper(t, db, "CRYPTO", 100000, 1)
	mustPlace(t, cp.PlaceBuy, reqOf("s-cr", "BTCUSDT", SideBuy, 1, 60000), "CRYPTO")
	st, err := cp.State()
	if err != nil || !st.Connected || st.Account != "paper:CRYPTO" {
		t.Fatalf("State 错位: %+v %v", st, err)
	}
	if len(st.Positions) != 1 || st.Positions[0].TsCode != "BTCUSDT" {
		t.Fatalf("State 持仓未按市场过滤: %+v", st.Positions)
	}
	if len(st.Orders) != 1 || st.Orders[0].SignalID != "s-cr" {
		t.Fatalf("State 委托未按市场过滤: %+v", st.Orders)
	}
	if ok, err := cp.Health(); !ok || err != nil {
		t.Fatalf("纸面健康恒真被破坏: %v %v", ok, err)
	}
	if err := cp.Cancel("paper-nope"); err != nil {
		t.Fatalf("Cancel 幂等失败: %v", err)
	}
}

// TestPaperConstructorGuards 构造闸：DB/UserID 必填、CN 市场拒（CN 用自己的 paper 引擎）。
func TestPaperConstructorGuards(t *testing.T) {
	db := paperTestDB(t)
	if _, err := NewBinancePaperExecutor(BinancePaperOptions{UserID: "u1", Market: "CRYPTO"}); err == nil {
		t.Fatal("缺 DB 必须拒")
	}
	if _, err := NewBinancePaperExecutor(BinancePaperOptions{DB: db, UserID: "u1", Market: "CN"}); err == nil {
		t.Fatal("CN 市场必须拒（本柜台不服务 A 股链）")
	}
}
