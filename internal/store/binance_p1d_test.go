// §P1-d 回归测（PLAN §5.2-3/§6.3）：Qty int→float64 的落库配套——
//   - 历史库（qty INTEGER）开库后三账本 qty 列重建为 REAL 且随行索引/自增序列/全列搬运无损；
//   - 二次开库幂等（已是 REAL 即跳过）；
//   - 判重保护跨重建存活（idx_fills_idem_notid 复合键仍拒重复回报）；
//   - 碎股/加密小数全链路往返（ApplyRealFill → fills/real_positions → SumFilledQty）；
//   - roundQty 三市场口径 + QTY 序列化兼容（整数值 JSON 串与旧 int 逐字节同「无小数点」）。
// English: P1-d regression — legacy INTEGER qty rebuilt to REAL losslessly (indexes, autoincrement,
// all carried columns), idempotent re-open, replay-dedup survives the rebuild, fractional fills
// round-trip, and roundQty/JSON-serialization per-market contracts.
package store

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrateQtyRealP1D 用「pre-P1c 形态」历史库（orders 单 signal_id 唯一 + 三表 qty INTEGER、
// 无 market/currency/trade_id）开库：ALTER 补列 → 唯一键/列型重建接力 → 断言 REAL、
// 数据与索引齐、二次开库结构恒定，并验证重建后小数可落库、重复回报仍被幂等拒。
func TestMigrateQtyRealP1D(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy_qty.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`
		CREATE TABLE orders (
			order_id TEXT PRIMARY KEY, signal_id TEXT UNIQUE, code TEXT NOT NULL,
			side TEXT NOT NULL, status TEXT NOT NULL, price REAL, qty INTEGER NOT NULL,
			created_at TEXT NOT NULL, user_id TEXT DEFAULT ''
		);
		CREATE TABLE fills (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id TEXT NOT NULL, code TEXT NOT NULL, side TEXT NOT NULL,
			price REAL NOT NULL, qty INTEGER NOT NULL, amount REAL NOT NULL,
			traded_at TEXT NOT NULL, signal_id TEXT DEFAULT '', user_id TEXT DEFAULT '',
			fee REAL DEFAULT 0, stamp_tax REAL DEFAULT 0, serial TEXT DEFAULT ''
		);
		CREATE TABLE shadow_orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT DEFAULT '', signal_id TEXT NOT NULL, code TEXT DEFAULT '',
			name TEXT DEFAULT '', strategy TEXT DEFAULT '', strategy_id TEXT DEFAULT '',
			side TEXT DEFAULT '', price REAL DEFAULT 0, qty INTEGER DEFAULT 0,
			amount REAL DEFAULT 0, created_at TEXT DEFAULT ''
		);
		INSERT INTO orders (order_id, signal_id, code, side, status, price, qty, created_at)
			VALUES ('O1','S1','600000.SH','买入','已成',10,100,'2026-09-20 09:30:00');
		INSERT INTO fills (id, order_id, code, side, price, qty, amount, traded_at, signal_id, serial)
			VALUES (7,'O1','600000.SH','买入',10,100,1000,'2026-09-20 09:30:05','S1','SER-7');
		INSERT INTO shadow_orders (id, signal_id, code, side, price, qty, amount, created_at)
			VALUES (3,'SH1','600000.SH','买入',10,100,1000,'2026-09-20 09:31:00');
	`); err != nil {
		t.Fatalf("create legacy: %v", err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open legacy: %v", err)
	}
	for _, tbl := range []string{"orders", "fills", "shadow_orders"} {
		ct, err := db.columnType(tbl, "qty")
		if err != nil || ct != "REAL" {
			t.Fatalf("%s.qty 应重建为 REAL，got %q err=%v", tbl, ct, err)
		}
	}
	// 全列搬运无损：fills 显式 id=7 保留、serial 与 ALTER 后补列齐
	var id int64
	var serial, mkt, cur string
	if err := db.db.QueryRow(`SELECT id, serial, market, currency FROM fills WHERE order_id='O1'`).
		Scan(&id, &serial, &mkt, &cur); err != nil || id != 7 || serial != "SER-7" || mkt != "CN" || cur != "CNY" {
		t.Fatalf("fills 搬运失真: id=%d serial=%q market=%q currency=%q err=%v", id, serial, mkt, cur, err)
	}
	// 自增序列续位：重建后新行 id 必须 >7（不撞旧主键）
	if err := db.ApplyRealFill(RealFill{OrderID: "O9", Code: "600519.SH", Side: "买入", Price: 1500,
		Qty: 100, Amount: 150000, TradedAt: "2026-09-21 09:30:00", SignalID: "S9"}); err != nil {
		t.Fatalf("post-migrate fill: %v", err)
	}
	if err := db.db.QueryRow(`SELECT id FROM fills WHERE order_id='O9'`).Scan(&id); err != nil || id <= 7 {
		t.Fatalf("自增序列应续位 >7，got id=%d err=%v", id, err)
	}
	// 判重索引跨重建存活：重复 O9 回报幂等命中，不再落第二行
	if err := db.ApplyRealFill(RealFill{OrderID: "O9", Code: "600519.SH", Side: "买入", Price: 1500,
		Qty: 100, Amount: 150000, TradedAt: "2026-09-21 09:30:00", SignalID: "S9"}); err != nil {
		t.Fatalf("duplicate must be idempotent: %v", err)
	}
	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM fills WHERE order_id='O9'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("重复回报应被幂等拒，fills=%d err=%v", n, err)
	}
	// schema 块索引补建锁：随行索引不得随 DROP 消失
	for _, idx := range []string{"idx_fills_traded_at", "idx_shadow_day", "idx_orders_market", "idx_fills_market", "idx_fills_idem_notid", "idx_fills_trade"} {
		if err := db.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&n); err != nil || n != 1 {
			t.Fatalf("索引 %s 缺失（重建后未补建）: n=%d err=%v", idx, n, err)
		}
	}
	db.Close()

	// 二次开库幂等：qty 已 REAL → migrateQtyReal 全跳过，结构与数据恒定
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	defer db2.Close()
	for _, tbl := range []string{"orders", "fills", "shadow_orders"} {
		ct, err := db2.columnType(tbl, "qty")
		if err != nil || ct != "REAL" {
			t.Fatalf("二次开库 %s.qty 漂移: %q err=%v", tbl, ct, err)
		}
	}
	if err := db2.db.QueryRow(`SELECT COUNT(*) FROM fills`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("二次开库 fills 应恒为 2，got %d err=%v", n, err)
	}
}

// TestFractionalQtyP1D 碎股/加密小数全链路：CRYPTO 0.5 买入落库、持仓与聚合求和均按小数走
// （SQLite REAL 列 + Go float64 两端同型），CN 整数仓量行为不变。
func TestFractionalQtyP1D(t *testing.T) {
	db := testDB(t)
	f := RealFill{OrderID: "CB1", Code: "BTCUSDT", Side: "买入", Market: "CRYPTO", Price: 60000,
		Qty: 0.5, Amount: 30000, TradedAt: "2026-09-21 09:30:00", SignalID: "CB-SIG"}
	if err := db.ApplyRealFill(f); err != nil {
		t.Fatalf("fractional buy: %v", err)
	}
	if err := db.ApplyRealFill(RealFill{OrderID: "CB2", Code: "BTCUSDT", Side: "买入", Market: "CRYPTO", Price: 61000,
		Qty: 0.25, Amount: 15250, TradedAt: "2026-09-21 10:30:00", SignalID: "CB-SIG"}); err != nil {
		t.Fatalf("second fractional buy: %v", err)
	}
	p, err := db.RealPositionByCodeForUser("", "BTCUSDT")
	if err != nil {
		t.Fatalf("position lookup: %v", err)
	}
	if p.Qty != 0.75 {
		t.Fatalf("小数加仓累计应 0.75，got %v", p.Qty)
	}
	if got := db.SumFilledQty("", "CB-SIG"); got != 0.75 {
		t.Fatalf("SumFilledQty 应 0.75，got %v", got)
	}
}

// TestRoundQtyP1D 三市场取整口径（engine 侧引用同实现，此处在 store 包锁序列化兼容）：
// 整数值 float64 的 JSON 编码不带小数点——网关 /前端 /日志旧消费方看到的串与 int 时代逐字节同。
func TestQtyJSONCompatP1D(t *testing.T) {
	b, err := json.Marshal(RealFill{OrderID: "X", Code: "600000.SH", Side: "买入", Price: 10, Qty: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"qty":100`) || strings.Contains(string(b), "100.") {
		t.Fatalf("整数 Qty 的 JSON 串必须与 int 时代同形，got %s", b)
	}
	b2, _ := json.Marshal(RealPosition{TsCode: "BTCUSDT", Qty: 0.5})
	if !strings.Contains(string(b2), `"qty":0.5`) {
		t.Fatalf("小数 Qty 应原样出串，got %s", b2)
	}
	// QtyString 渲染锁：整数无小数点/无指数，小数最短形
	for q, want := range map[float64]string{100: "100", 1000000: "1000000", 0.5: "0.5", 0.00001: "0.00001", 1234.567: "1234.567"} {
		if got := QtyString(q); got != want {
			t.Fatalf("QtyString(%v)=%q want %q", q, got, want)
		}
	}
}
