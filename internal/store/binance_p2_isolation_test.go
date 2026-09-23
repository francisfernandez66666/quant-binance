// 文件职责：binance_p2_isolation_test.go——§BINANCE-P2（PLAN §15.2）结算币种隔离回归：
// 聚合口径（成交金额/笔数、在途冻结、卖出回款、已实现盈亏、总资产、账户行）必须按
// market 分账——USDT/USD 的成交与资金不得占用 CNY 预算，反之亦然；同时锁 CN 无后缀
// 老方法 = CN 作用域（存量 QMT 链行为不变）与 real_account 旧表单主键的重建迁移。
// English: market-scoped aggregate isolation locks (§15.2) plus the real_account v2 rebuild migration.
package store

import (
	"path/filepath"
	"testing"
)

// newP2TestDB 在临时目录开全新库（含 market 列迁移），Cleanup 自动关连接、目录随用例结束回收。
func newP2TestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "p2.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// p2Fill 构造一笔带市场的买入/卖出成交。
func p2Fill(code, side, market, day string, amount float64) RealFill {
	return RealFill{
		OrderID: "P2-" + code + day, Code: code, Side: side, Market: market,
		Price: 10, Qty: amount / 10, Amount: amount, UserID: "u_p2", TradedAt: day + " 10:00:00",
	}
}

// TestP2FillsMarketIsolation 成交金额/笔数/回款三本账按市场分账，无后缀口径恒等 CN。
func TestP2FillsMarketIsolation(t *testing.T) {
	db := newP2TestDB(t)
	for _, f := range []RealFill{
		p2Fill("600001.SH", "买入", "CN", "2026-09-22", 1000),
		p2Fill("AAPL", "买入", "US", "2026-09-22", 500),
		p2Fill("AAPL", "买入", "US", "2026-09-22", 300),
		p2Fill("BTCUSDT", "买入", "CRYPTO", "2026-09-22", 200),
		p2Fill("600002.SH", "卖出", "CN", "2026-09-22", 900),
		p2Fill("MSFT", "卖出", "US", "2026-09-22", 80),
	} {
		if err := db.ApplyRealFill(f); err != nil {
			t.Fatalf("ApplyRealFill: %v", err)
		}
	}
	day := "2026-09-22"
	if s, _ := db.SumBuyFilledAmountByDay("u_p2", day); s != 1000 {
		t.Fatalf("CN 无后缀买入成交额=%v，期望 1000（US/CRYPTO 不得混入）", s)
	}
	if s, _ := db.SumBuyFilledAmountByDayForMarket("u_p2", day, "US"); s != 800 {
		t.Fatalf("US 买入成交额=%v，期望 800", s)
	}
	if s, _ := db.SumBuyFilledAmountByDayForMarket("u_p2", day, ""); s != 1000 {
		t.Fatalf("空 market 归一后=%v，期望与 CN 恒等", s)
	}
	if s, _ := db.SumBuyFilledAmountByDayForMarket("u_p2", day, "CRYPTO"); s != 200 {
		t.Fatalf("CRYPTO 买入成交额=%v，期望 200", s)
	}
	if n, _ := db.CountBuyFilledOrdersByDayForMarket("u_p2", day, "US"); n != 1 {
		t.Fatalf("US 成交笔数=%d，期望 1（同委托两笔部成合一，且 CN/CRYPTO 不占 US 额度）", n)
	}
	if s, _ := db.SumSellFilledAmountByDayForMarket("u_p2", day, "US"); s != 80 {
		t.Fatalf("US 卖出回款=%v，期望 80", s)
	}
	if s, _ := db.SumSellFilledAmountByDay("u_p2", day); s != 900 {
		t.Fatalf("CN 无后缀卖出回款=%v，期望 900", s)
	}
}

// TestP2FreezeAndOpenSellIsolation 在途冻结与在途卖量按市场分账。
func TestP2FreezeAndOpenSellIsolation(t *testing.T) {
	db := newP2TestDB(t)
	day := "2026-09-22"
	for _, o := range []RealOrder{
		{OrderID: "pend:b1", SignalID: "b1", Code: "600001.SH", Side: "买入", Status: "已报", Price: 10, Qty: 100, CreatedAt: day + " 09:31:00", UserID: "u_p2", Market: "CN"},
		{OrderID: "pend:b2", SignalID: "b2", Code: "AAPL", Side: "买入", Status: "已报", Price: 200, Qty: 5, CreatedAt: day + " 09:31:00", UserID: "u_p2", Market: "US"},
		{OrderID: "pend:s1", SignalID: "s1", Code: "AAPL", Side: "卖出", Status: "部成", Price: 200, Qty: 3, CreatedAt: day + " 10:00:00", UserID: "u_p2", Market: "US"},
	} {
		if _, err := db.UpsertRealOrder(o); err != nil {
			t.Fatalf("UpsertRealOrder: %v", err)
		}
	}
	if f, _ := db.LocalBuyFrozen("u_p2", day); f != 1000 {
		t.Fatalf("CN 无后缀冻结=%v，期望 1000（US 在途不得混入）", f)
	}
	if f, _ := db.LocalBuyFrozenForMarket("u_p2", day, "US"); f != 1000 {
		t.Fatalf("US 冻结=%v，期望 1000", f)
	}
	if q := db.SumOpenSellQtyForMarket("u_p2", "AAPL", day, "US"); q != 3 {
		t.Fatalf("US 在途卖量=%v，期望 3", q)
	}
	if q := db.SumOpenSellQty("u_p2", "AAPL", day); q != 0 {
		t.Fatalf("CN 口径查 US 卖单=%v，期望 0（跨市场不可见）", q)
	}
}

// TestP2RealAccountMarketRows 账户行按 (user_id, market) 各存各的，CN 读取不串 USDT。
func TestP2RealAccountMarketRows(t *testing.T) {
	db := newP2TestDB(t)
	if err := db.UpsertRealAccount(RealAccount{UserID: "u_p2", AvailableCash: 111, UpdatedAt: "2026-09-22 09:00:00"}); err != nil {
		t.Fatalf("upsert CN: %v", err)
	}
	if err := db.UpsertRealAccount(RealAccount{UserID: "u_p2", Market: "CRYPTO", AvailableCash: 222, UpdatedAt: "2026-09-22 09:00:00"}); err != nil {
		t.Fatalf("upsert CRYPTO: %v", err)
	}
	cn, _ := db.GetRealAccount("u_p2")
	cr, _ := db.GetRealAccountForMarket("u_p2", "CRYPTO")
	if cn.AvailableCash != 111 || cr.AvailableCash != 222 {
		t.Fatalf("账户行串市: CN=%v CRYPTO=%v", cn.AvailableCash, cr.AvailableCash)
	}
	us, _ := db.GetRealAccountForMarket("u_p2", "US")
	if us.AvailableCash != 0 || us.Market != "US" {
		t.Fatalf("US 缺行应返回零值不报错: %+v", us)
	}
	// 全局遗留行兜底仅限 CN：CRYPTO 查询不得吃到 user_id='' 的行。
	if err := db.UpsertRealAccount(RealAccount{UserID: "", Market: "CN", AvailableCash: 999, UpdatedAt: "2026-09-22 09:00:00"}); err != nil {
		t.Fatalf("upsert global: %v", err)
	}
	if c2, _ := db.GetRealAccountForMarket("u_other", "CRYPTO"); c2.AvailableCash != 0 {
		t.Fatalf("CRYPTO 越币兜底了全局行: %v", c2.AvailableCash)
	}
	if c3, _ := db.GetRealAccount("u_other"); c3.AvailableCash != 999 {
		t.Fatalf("CN 全局行兜底失效: %v", c3.AvailableCash)
	}
}

// TestP2RealAccountLegacyRebuild 旧表单主键（无 market 列）库升级后自动重建，存量行归 CN。
func TestP2RealAccountLegacyRebuild(t *testing.T) {
	db := newP2TestDB(t)
	if _, err := db.db.Exec(`DROP TABLE IF EXISTS real_account`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`CREATE TABLE real_account (
		user_id TEXT PRIMARY KEY, available_cash REAL NOT NULL DEFAULT 0,
		frozen_cash REAL NOT NULL DEFAULT 0, total_asset REAL NOT NULL DEFAULT 0,
		market_value REAL NOT NULL DEFAULT 0, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO real_account VALUES('u_p2', 888, 0, 0, 0, '2026-09-22 09:00:00')`); err != nil {
		t.Fatal(err)
	}
	acc, err := db.GetRealAccount("u_p2")
	if err != nil || acc.AvailableCash != 888 {
		t.Fatalf("重建迁移后存量 CN 行丢失: %+v err=%v", acc, err)
	}
	if err := db.UpsertRealAccount(RealAccount{UserID: "u_p2", Market: "US", AvailableCash: 7, UpdatedAt: "2026-09-22 10:00:00"}); err != nil {
		t.Fatalf("v2 复合格式 upsert 失败: %v", err)
	}
	if cn, _ := db.GetRealAccount("u_p2"); cn.AvailableCash != 888 {
		t.Fatalf("US 行覆写了 CN 行: %v", cn.AvailableCash)
	}
}

// TestP2TotalAssetsIsolation 总资产分母按市场：现金行 + 同市场持仓。
func TestP2TotalAssetsIsolation(t *testing.T) {
	db := newP2TestDB(t)
	if err := db.UpsertRealAccount(RealAccount{UserID: "u_p2", AvailableCash: 1000, UpdatedAt: "2026-09-22 09:00:00"}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertRealAccount(RealAccount{UserID: "u_p2", Market: "CRYPTO", AvailableCash: 500, UpdatedAt: "2026-09-22 09:00:00"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpsertRealPositions([]RealPosition{
		{TsCode: "600001.SH", Name: "A", Qty: 100, CostPrice: 10, CurPrice: 12, UserID: "u_p2", Market: "CN"},
		{TsCode: "BTCUSDT", Name: "BTC", Qty: 1, CostPrice: 60000, CurPrice: 65000, UserID: "u_p2", Market: "CRYPTO"},
	}); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.TotalAssets("u_p2"); v != 1000+1000 {
		// 口径与遗留 TotalAssets 一致：RealPositionsForUser 不回读 cur_price，市值按成本价估。
		t.Fatalf("CN 总资产=%v，期望 2000（CRYPTO 持仓/USDT 现金不得混入）", v)
	}
	if v, _ := db.TotalAssetsForMarket("u_p2", "CRYPTO"); v != 500+60000 {
		t.Fatalf("CRYPTO 总资产=%v，期望 60500（成本价口径）", v)
	}
}

// TestP2RealizedPnlIsolation 已实现盈亏按市场：US 卖单盈亏不进 CN 熔断口径。
func TestP2RealizedPnlIsolation(t *testing.T) {
	db := newP2TestDB(t)
	day := "2026-09-22"
	for _, f := range []RealFill{
		{OrderID: "c1", Code: "600001.SH", Side: "买入", Market: "CN", Price: 10, Qty: 10, Amount: 100, UserID: "u_p2", TradedAt: day + " 09:31:00"},
		{OrderID: "c2", Code: "600001.SH", Side: "卖出", Market: "CN", Price: 8, Qty: 10, Amount: 80, UserID: "u_p2", TradedAt: day + " 14:31:00"},
		{OrderID: "u1", Code: "AAPL", Side: "买入", Market: "US", Price: 100, Qty: 10, Amount: 1000, UserID: "u_p2", TradedAt: day + " 09:31:00"},
		{OrderID: "u2", Code: "AAPL", Side: "卖出", Market: "US", Price: 50, Qty: 10, Amount: 500, UserID: "u_p2", TradedAt: day + " 14:31:00"},
	} {
		if err := db.ApplyRealFill(f); err != nil {
			t.Fatalf("ApplyRealFill: %v", err)
		}
	}
	// 卖出只减 qty 不清成本行（qty<=0 待对账清除），成本基准仍可查。
	cn, err := db.TodayRealizedPnl("u_p2", day)
	if err != nil {
		t.Fatal(err)
	}
	if cn != -20 {
		t.Fatalf("CN 无后缀已实现盈亏=%v，期望 -20（US 亏损不得混入）", cn)
	}
	if us, _ := db.TodayRealizedPnlForMarket("u_p2", day, "US"); us != -500 {
		t.Fatalf("US 已实现盈亏=%v，期望 -500", us)
	}
	if c, _ := db.TodayRealizedPnlForMarket("u_p2", day, "CRYPTO"); c != 0 {
		t.Fatalf("CRYPTO 无成交应=0，实际 %v", c)
	}
}
