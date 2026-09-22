// data storage tests for §BINANCE-P1c（PLAN §5）：market/currency 迁移（历史库补列/PK 重建/幂等重开）、
// 三市场代码校验分治、逐市场删除隔离（CN↔CRYPTO 互不误伤）、成交回报市场打标与计价币缺省。
// English: P1c multi-asset store tests — schema migration (ALTER backfill + PK rebuild + reopen
// idempotency on a legacy DB), per-market ts_code validation, market-scoped delete isolation,
// and market/currency stamping on fill reports. CN behavior is covered by the pre-existing suite.
package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrateMarketSchemaP1C 历史库（无 market/currency、real_positions 旧 PK (ts_code,user_id)、
// orders 单 signal_id 唯一）Open 后应：补列并回填 CN/CNY、重建为 (market,ts_code,user_id) 且
// qty REAL、建 idx_orders_market/idx_fills_market；二次 Open 幂等（结构不再变、数据不丢）。
// 特别锁定 orders 一次性重建携列回归：重建若不携带 market/currency，ALTER 先补的列会被
// 改名销毁 → 建市场索引时 no such column（TestSchemaMigrationP01P02 同族陷阱的常驻哨兵）。
func TestMigrateMarketSchemaP1C(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`
		CREATE TABLE real_positions (
			ts_code TEXT NOT NULL, name TEXT DEFAULT '', qty INTEGER NOT NULL DEFAULT 0,
			cost_price REAL NOT NULL DEFAULT 0, amount REAL NOT NULL DEFAULT 0,
			highest_price REAL NOT NULL DEFAULT 0, strategy TEXT DEFAULT '',
			signal_id TEXT DEFAULT '', updated_at TEXT NOT NULL, user_id TEXT DEFAULT '',
			buy_date TEXT DEFAULT '', PRIMARY KEY (ts_code, user_id)
		);
		CREATE TABLE orders (
			order_id TEXT PRIMARY KEY, signal_id TEXT UNIQUE, code TEXT NOT NULL,
			side TEXT NOT NULL, status TEXT NOT NULL, price REAL, qty INTEGER NOT NULL,
			created_at TEXT NOT NULL, user_id TEXT DEFAULT ''
		);
		INSERT INTO real_positions (ts_code, name, qty, updated_at) VALUES ('600000.SH','浦发',100,'2026-09-01 10:00:00');
		INSERT INTO orders (order_id, signal_id, code, side, status, qty, created_at)
			VALUES ('O1','SIG1','600000.SH','买入','已报',100,'2026-09-01 10:00:00');
	`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy: %v", err)
	}
	ddl := func(name string) string {
		var s string
		if err := db.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&s); err != nil {
			t.Fatalf("ddl %s: %v", name, err)
		}
		return s
	}
	if got := ddl("real_positions"); !strings.Contains(got, "PRIMARY KEY (market, ts_code, user_id)") || !strings.Contains(got, "qty REAL") {
		t.Fatalf("real_positions 应重建为市场复合主键且 qty REAL:\n%s", got)
	}
	var m, cur string
	if err := db.db.QueryRow(`SELECT market, currency FROM real_positions WHERE ts_code='600000.SH'`).Scan(&m, &cur); err != nil || m != "CN" || cur != "CNY" {
		t.Fatalf("遗留持仓行应回填 CN/CNY，got market=%q currency=%q err=%v", m, cur, err)
	}
	if err := db.db.QueryRow(`SELECT market, currency FROM orders WHERE order_id='O1'`).Scan(&m, &cur); err != nil || m != "CN" || cur != "CNY" {
		t.Fatalf("遗留委托行应携带 CN/CNY（重建携列回归锁），got market=%q currency=%q err=%v", m, cur, err)
	}
	var nIdx int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN ('idx_orders_market','idx_fills_market')`).Scan(&nIdx); err != nil || nIdx != 2 {
		t.Fatalf("市场索引应齐备 2 个，got %d err=%v", nIdx, err)
	}
	db.Close()

	// 二次 Open：迁移全部走「已是目标结构→跳过」分支，结构与数据恒定（幂等锁）。
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	defer db2.Close()
	if got := ddl2(db2, "real_positions"); got != "" && !strings.Contains(got, "PRIMARY KEY (market, ts_code, user_id)") {
		t.Fatalf("二次 Open 后主键结构漂移: %s", got)
	}
	var n int
	if err := db2.db.QueryRow(`SELECT COUNT(*) FROM real_positions WHERE ts_code='600000.SH' AND market='CN'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("二次 Open 后旧行应原样保留，got %d err=%v", n, err)
	}
}

func ddl2(db *DB, name string) string {
	var s string
	_ = db.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&s)
	return s
}

// TestValidTsCodeP1C 三正则分治（PLAN §5.3）：CN 与旧 realTsCodeRe 逐字节同口径，
// CRYPTO=大写交易对，US=字母代号（含 BRK.B 型内点）；跨市场形态互拒，缺省市场走 CN。
func TestValidTsCodeP1C(t *testing.T) {
	cases := []struct {
		market, code string
		want         bool
	}{
		{"CN", "600519.SH", true}, {"CN", "000001.SZ", true}, {"CN", "920001.BJ", true},
		{"CN", "BTCUSDT", false}, {"CN", "AAPL", false}, {"CN", "60051", false},
		{"CRYPTO", "BTCUSDT", true}, {"CRYPTO", "ETHUSDC", true}, {"CRYPTO", "1000PEPEUSDT", true},
		{"CRYPTO", "600519.SH", false}, {"CRYPTO", "btcusdt", false}, {"CRYPTO", "B", false},
		{"US", "AAPL", true}, {"US", "BRK.B", true}, {"US", "SPY", true},
		{"US", "600519.SH", false}, {"US", "ABCDEFGHIJK", false}, // US 上限 10 字符
		{"", "600519.SH", true}, {"", "BTCUSDT", false}, // 缺省市场 = CN 口径
	}
	for i, c := range cases {
		if got := validTsCode(c.market, c.code); got != c.want {
			t.Fatalf("case %d: validTsCode(%q,%q)=%v want %v", i, c.market, c.code, got, c.want)
		}
	}
}

// TestNormalizeMarketP1C 市场串归一与计价币缺省：空/大小写/未知值的兜底口径。
func TestNormalizeMarketP1C(t *testing.T) {
	if normalizeMarket("") != "CN" || normalizeMarket(" crypto ") != "CRYPTO" || normalizeMarket("us") != "US" {
		t.Fatal("normalizeMarket 归一失败")
	}
	if defaultCurrency("CN") != "CNY" || defaultCurrency("US") != "USD" || defaultCurrency("CRYPTO") != "USDT" {
		t.Fatal("defaultCurrency 缺省失败")
	}
	if defaultCurrency("MARS") != "CNY" {
		t.Fatal("未知市场应兜底 CNY（与存量回填同口径）")
	}
}

// TestReconcilePositionsMarketIsolationP1C ReconcilePositionsForUser 的删除/计数按市场划界：
// CN 空快照只清 CN 行、CRYPTO 快照外行不受波及（反之亦然）——跨市场误删闸的正面锁。
func TestReconcilePositionsMarketIsolationP1C(t *testing.T) {
	db := testDB(t)
	if n, err := db.ReconcilePositionsForUser("u1", "CN", []RealPosition{
		{TsCode: "600000.SH", Qty: 100, CostPrice: 10, Amount: 1000}}); err != nil || n != 1 {
		t.Fatalf("CN 对账: n=%d err=%v", n, err)
	}
	if n, err := db.ReconcilePositionsForUser("u1", "CRYPTO", []RealPosition{
		{TsCode: "BTCUSDT", Qty: 2, CostPrice: 60000, Amount: 120000}}); err != nil || n != 1 {
		t.Fatalf("CRYPTO 对账: n=%d err=%v", n, err)
	}
	if n, err := db.ReconcilePositionsForUser("u1", "US", []RealPosition{
		{TsCode: "AAPL", Qty: 10, CostPrice: 200, Amount: 2000}}); err != nil || n != 1 {
		t.Fatalf("US 对账: n=%d err=%v", n, err)
	}
	// CN 全平：只该清走 600000.SH，BTCUSDT/AAPL 不动
	if n, err := db.ReconcilePositionsForUser("u1", "CN", nil); err != nil || n != 0 {
		t.Fatalf("CN 空快照: n=%d err=%v", n, err)
	}
	left, _ := db.RealPositionsForUser("u1")
	if len(left) != 2 {
		t.Fatalf("CN 全平后应剩 CRYPTO+US 两行, got %d", len(left))
	}
	for _, p := range left {
		if p.Market == "CN" {
			t.Fatalf("CN 行未被清除: %+v", p)
		}
	}
	// CRYPTO 全平：只剩 AAPL
	if _, err := db.ReconcilePositionsForUser("u1", "CRYPTO", nil); err != nil {
		t.Fatal(err)
	}
	left, _ = db.RealPositionsForUser("u1")
	if len(left) != 1 || left[0].Market != "US" || left[0].TsCode != "AAPL" {
		t.Fatalf("CRYPTO 全平后应只剩 US/AAPL, got %+v", left)
	}
}

// TestUpsertRealPositionsCrossMarketP1C 全量对账（单租户入口）的逐市场 NOT IN：
// 混合快照双落 → CN-only 快照后 CRYPTO 行幸存、CN 旧行清除；行级 market 非法（按自带市场
// 口径）整批拒收。
func TestUpsertRealPositionsCrossMarketP1C(t *testing.T) {
	db := testDB(t)
	if n, err := db.UpsertRealPositions([]RealPosition{
		{TsCode: "600000.SH", Qty: 100, CostPrice: 10, Amount: 1000},
		{TsCode: "BTCUSDT", Market: "CRYPTO", Qty: 2, CostPrice: 60000, Amount: 120000},
	}); err != nil || n != 2 {
		t.Fatalf("混合快照: n=%d err=%v", n, err)
	}
	if n, err := db.UpsertRealPositions([]RealPosition{
		{TsCode: "000001.SZ", Qty: 100, CostPrice: 50, Amount: 5000},
	}); err != nil || n != 2 { // 000001 入 + 600000 清 + BTCUSDT 不受波及 = 2
		t.Fatalf("CN-only 快照: n=%d err=%v", n, err)
	}
	all, _ := db.RealPositions()
	if len(all) != 2 || all[0].TsCode != "000001.SZ" || all[1].TsCode != "BTCUSDT" || all[1].Market != "CRYPTO" {
		t.Fatalf("跨市场误删: %+v", all)
	}
	// 拒收锁：不带 market 的 BTCUSDT 按 CN 口径非法 → 整批拒收不落库
	if _, err := db.UpsertRealPositions([]RealPosition{
		{TsCode: "ETHUSDT", Qty: 1}, {TsCode: "600002.SH", Qty: 100},
	}); err == nil || !errors.Is(err, ErrInvalidPositionReport) {
		t.Fatalf("非法 CN 码应整批拒收, err=%v", err)
	}
	var cnt int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM real_positions WHERE ts_code='600002.SH'`).Scan(&cnt); err != nil || cnt != 0 {
		t.Fatalf("拒收批次不得部分落库, cnt=%d err=%v", cnt, err)
	}
}

// TestApplyRealFillMarketP1C 成交回报市场打标：空 market=CN 零回归；CRYPTO 行落
// market=CRYPTO/currency=USDT（写入方显式 currency 优先）；CN 卖单不触及同名加密行。
func TestApplyRealFillMarketP1C(t *testing.T) {
	db := testDB(t)
	// CN 缺省链（零回归 + 打标）
	if err := db.ApplyRealFill(RealFill{OrderID: "O1", Code: "600000.SH", Side: "买入",
		Price: 10, Qty: 100, Amount: 1000, TradedAt: "2026-09-21 09:35:00", SignalID: "S1"}); err != nil {
		t.Fatalf("CN buy: %v", err)
	}
	var m, cur string
	if err := db.db.QueryRow(`SELECT market, currency FROM real_positions WHERE ts_code='600000.SH'`).Scan(&m, &cur); err != nil || m != "CN" || cur != "CNY" {
		t.Fatalf("空 market 应归一 CN/CNY，got %q/%q err=%v", m, cur, err)
	}
	// 大小写不敏感的 crypto 市场 + 显式计价币（ETHUSDC 场景）
	if err := db.ApplyRealFill(RealFill{OrderID: "O2", Code: "BTCUSDT", Side: "买入", Market: " crypto ",
		Price: 60000, Qty: 2, Amount: 120000, TradedAt: "2026-09-21 09:36:00", SignalID: "S2"}); err != nil {
		t.Fatalf("CRYPTO buy: %v", err)
	}
	if err := db.db.QueryRow(`SELECT market, currency FROM real_positions WHERE ts_code='BTCUSDT'`).Scan(&m, &cur); err != nil || m != "CRYPTO" || cur != "USDT" {
		t.Fatalf("CRYPTO 行应 market=CRYPTO currency=USDT，got %q/%q err=%v", m, cur, err)
	}
	if err := db.ApplyRealFill(RealFill{OrderID: "O3", Code: "ETHUSDC", Side: "买入", Market: "CRYPTO",
		Currency: "USDC", Price: 3000, Qty: 4, Amount: 12000, TradedAt: "2026-09-21 09:37:00", SignalID: "S3"}); err != nil {
		t.Fatalf("USDC buy: %v", err)
	}
	if err := db.db.QueryRow(`SELECT currency FROM real_positions WHERE ts_code='ETHUSDC'`).Scan(&cur); err != nil || cur != "USDC" {
		t.Fatalf("显式 currency 应优先于缺省，got %q err=%v", cur, err)
	}
	// fills 行携带 market/currency
	var fm, fc string
	if err := db.db.QueryRow(`SELECT market, currency FROM fills WHERE order_id='O2'`).Scan(&fm, &fc); err != nil || fm != "CRYPTO" || fc != "USDT" {
		t.Fatalf("fills 应带 market/currency，got %q/%q err=%v", fm, fc, err)
	}
	// 跨市场同码防御：CN 卖出 600000.SH 后 CRYPTO 行仍在（qty<=0 删除谓词带 market）
	if err := db.ApplyRealFill(RealFill{OrderID: "O4", Code: "600000.SH", Side: "卖出",
		Price: 11, Qty: 100, Amount: 1100, TradedAt: "2026-09-21 14:00:00", SignalID: "S4"}); err != nil {
		t.Fatalf("CN sell: %v", err)
	}
	var cnt int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM real_positions WHERE ts_code='BTCUSDT' AND market='CRYPTO'`).Scan(&cnt); err != nil || cnt != 1 {
		t.Fatalf("CN 清仓不得波及 CRYPTO 行, cnt=%d err=%v", cnt, err)
	}
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM real_positions WHERE ts_code='600000.SH'`).Scan(&cnt); err != nil || cnt != 0 {
		t.Fatalf("CN 卖光后行应删除, cnt=%d err=%v", cnt, err)
	}
}
