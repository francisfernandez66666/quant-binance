// 文件职责：§MR-4A 做空账簿行为锁——开空/加空/平空闭环、方向冲突拒收、
// CN 做空方向 fail-close、平空委托纳入跨日清扫。写法镜像 real_positions_test 最小栈。
package store

import (
	"path/filepath"
	"testing"
)

func openMR4DB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "mr4.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mr4Fill(order, side, code string, qty, price, fee float64, market, userID, at string) RealFill {
	return RealFill{OrderID: order, Code: code, Name: "MR4 Co", Side: side, Price: price, Qty: qty,
		Amount: qty * price, Fee: fee, TradedAt: at, SignalID: order, UserID: userID, Market: market}
}

func mr4Pos(t *testing.T, d *DB, code, userID string) RealPosition {
	t.Helper()
	p, err := d.RealPositionByCodeForUser(userID, code)
	if err != nil {
		t.Fatalf("读持仓 %s: %v", code, err)
	}
	return p
}

// TestMR4ShortOpenAddCoverCycle 开空→加空（含费加权）→部分平空→全平清行。
func TestMR4ShortOpenAddCoverCycle(t *testing.T) {
	d := openMR4DB(t)
	if err := d.ApplyRealFill(mr4Fill("O1", "卖出开空", "AAPL", 100, 10, 1, "US", "u1", "2026-09-21 10:00:00")); err != nil {
		t.Fatal(err)
	}
	p := mr4Pos(t, d, "AAPL", "u1")
	if p.Side != "short" || p.Qty != 100 || p.CostPrice != 10.01 {
		t.Fatalf("开空簿记错: %+v", p)
	}
	if err := d.ApplyRealFill(mr4Fill("O2", "卖出开空", "AAPL", 100, 12, 1, "US", "u1", "2026-09-21 10:01:00")); err != nil {
		t.Fatal(err)
	}
	p = mr4Pos(t, d, "AAPL", "u1")
	if p.Qty != 200 || p.CostPrice != 11.01 { // (1001+1201)/200
		t.Fatalf("加空加权错: %+v", p)
	}
	if err := d.ApplyRealFill(mr4Fill("O3", "买入平仓", "AAPL", 50, 9, 0, "US", "u1", "2026-09-21 10:02:00")); err != nil {
		t.Fatal(err)
	}
	p = mr4Pos(t, d, "AAPL", "u1")
	if p.Side != "short" || p.Qty != 150 {
		t.Fatalf("平空减仓错: %+v", p)
	}
	if err := d.ApplyRealFill(mr4Fill("O4", "买入平仓", "AAPL", 150, 9, 0, "US", "u1", "2026-09-21 10:03:00")); err != nil {
		t.Fatal(err)
	}
	poses, err := d.RealPositionsForUser("u1")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range poses {
		if q.TsCode == "AAPL" {
			t.Fatalf("全平后 AAPL 行应清除: %+v", q)
		}
	}
}

// TestMR4DirectionConflictsRejected 方向冲突矩阵：多头行上开空拒、空头行上买入拒、
// 空头行上卖出拒、无行平空拒——全部事务内拒收且不污染 fills。
func TestMR4DirectionConflictsRejected(t *testing.T) {
	d := openMR4DB(t)
	// 多头行
	if err := d.ApplyRealFill(mr4Fill("B1", "买入", "NVDA", 100, 20, 0, "US", "u1", "2026-09-21 10:00:00")); err != nil {
		t.Fatal(err)
	}
	if err := d.ApplyRealFill(mr4Fill("S1", "卖出开空", "NVDA", 100, 21, 0, "US", "u1", "2026-09-21 10:01:00")); err == nil {
		t.Fatal("多头行上开空必须拒收（单向簿）")
	}
	// 空头行
	if err := d.ApplyRealFill(mr4Fill("S2", "卖出开空", "TSLA", 50, 10, 0, "US", "u1", "2026-09-21 10:02:00")); err != nil {
		t.Fatal(err)
	}
	if err := d.ApplyRealFill(mr4Fill("B2", "买入", "TSLA", 10, 9, 0, "US", "u1", "2026-09-21 10:03:00")); err == nil {
		t.Fatal("空头行上普通买入必须拒收（平仓走专用向）")
	}
	if err := d.ApplyRealFill(mr4Fill("S3", "卖出", "TSLA", 10, 9, 0, "US", "u1", "2026-09-21 10:03:30")); err == nil {
		t.Fatal("空头行上普通卖出必须拒收（开仓走专用向）")
	}
	if err := d.ApplyRealFill(mr4Fill("C1", "买入平仓", "MSFT", 10, 9, 0, "US", "u1", "2026-09-21 10:04:00")); err == nil {
		t.Fatal("无空头行平空必须拒收（不伪造行）")
	}
	// 拒收事务不得留 fills：TSLA 只应有 S2 一笔。
	fills, err := d.RealFills()
	if err != nil {
		t.Fatal(err)
	}
	nT := 0
	for _, f := range fills {
		if f.Code == "TSLA" {
			nT++
		}
	}
	if nT != 1 {
		t.Fatalf("被拒事务不得落 fills, TSLA fills=%d", nT)
	}
	// 多头行仍在且未动账
	p := mr4Pos(t, d, "NVDA", "u1")
	if p.Side != "long" || p.Qty != 100 {
		t.Fatalf("拒收不得动多头行: %+v", p)
	}
}

// TestMR4ShortSidesRejectedOnCN CN 市场收到做空向成交=fail-close 拒收（负向锁行为面）。
func TestMR4ShortSidesRejectedOnCN(t *testing.T) {
	d := openMR4DB(t)
	if err := d.ApplyRealFill(mr4Fill("X1", "卖出开空", "600000.SH", 100, 10, 0, "CN", "u1", "2026-09-21 10:00:00")); err == nil {
		t.Fatal("CN 市场开空必须拒收")
	}
	if err := d.ApplyRealFill(mr4Fill("X2", "买入平仓", "600000.SH", 100, 10, 0, "CN", "u1", "2026-09-21 10:00:00")); err == nil {
		t.Fatal("CN 市场平空必须拒收")
	}
	if ps, _ := d.RealPositions(); len(ps) != 0 {
		t.Fatalf("拒收不得建行: %+v", ps)
	}
}

// TestMR4SweepCoversShortOrder 跨日清扫（买单形态）纳入 买入平仓 方向的僵尸委托。
func TestMR4SweepCoversShortOrder(t *testing.T) {
	d := openMR4DB(t)
	if _, err := d.UpsertRealOrder(RealOrder{OrderID: "SO1", SignalID: "SG-SO1", Code: "AAPL",
		Side: "买入平仓", Status: "已报", Qty: 10, Price: 9, CreatedAt: "2026-09-20 10:00:00", UserID: "u1", Market: "US"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertRealOrder(RealOrder{OrderID: "SO2", SignalID: "SG-SO2", Code: "AAPL",
		Side: "卖出开空", Status: "已报", Qty: 10, Price: 9, CreatedAt: "2026-09-20 10:00:00", UserID: "u1", Market: "US"}); err != nil {
		t.Fatal(err)
	}
	n, err := d.SweepStaleBuyOrders("u1", "2026-09-21", "US")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("只有买单形态（买入/买入平仓）被扫，got=%d", n)
	}
	orders, err := d.RealOrdersForUser("u1")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range orders {
		switch o.OrderID {
		case "SO1":
			if o.Status != "废单" {
				t.Fatalf("买入平仓陈旧单应被扫, got=%s", o.Status)
			}
		case "SO2":
			if o.Status != "已报" {
				t.Fatalf("卖出开空不得被买单清扫误伤, got=%s", o.Status)
			}
		}
	}
}
