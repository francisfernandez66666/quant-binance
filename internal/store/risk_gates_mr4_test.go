// risk_gates_mr4_test.go — §MR-4A（2026-09-23）风控账本口径的做空侧单测。
//
// 覆盖两条闸口数据腿：
//  1. TodayRealizedPnlForMarket 的空头平仓腿——买入平仓实现盈亏 =（开空均价 − 平仓价）× 数量
//     （成本取空头持仓行 CostPrice，全平后回落当日 卖出开空 成交均价；方向算反即红）；
//  2. TotalAssetsForMarket 的空头行计负市值——开空回款已在账户现金，持仓再计正市值即双计
//     （CN 恒多头行，逐字节不变由既有测试兜底）。
//
// English: §MR-4A ledger-side unit tests — short-cover realized P&L signs (with fallback to the
// day's short-open average once the row is gone) and short rows counted negative in total assets.
package store

import "testing"

// TestMR4RealizedPnlShortCover 一轮开空→平空的当日实现盈亏：@100 开空 10、@120 平空 → −200；
// 对照组多头 @10 买 5、@12 卖 → +10。全平后持仓行已消失，成本走当日开空均价回落腿。
func TestMR4RealizedPnlShortCover(t *testing.T) {
	d := openMR4DB(t)
	const day = "2026-09-22"
	if err := d.ApplyRealFill(mr4Fill("O-P1", "卖出开空", "PLTR", 10, 100, 0, "US", "u1", day+" 10:00:00")); err != nil {
		t.Fatalf("seed short open: %v", err)
	}
	if err := d.ApplyRealFill(mr4Fill("O-P2", "买入平仓", "PLTR", 10, 120, 0, "US", "u1", day+" 11:00:00")); err != nil {
		t.Fatalf("seed short cover: %v", err)
	}
	if err := d.ApplyRealFill(mr4Fill("O-P3", "买入", "COIN", 5, 10, 0, "US", "u1", day+" 12:00:00")); err != nil {
		t.Fatalf("seed long buy: %v", err)
	}
	if err := d.ApplyRealFill(mr4Fill("O-P4", "卖出", "COIN", 5, 12, 0, "US", "u1", day+" 13:00:00")); err != nil {
		t.Fatalf("seed long sell: %v", err)
	}
	pnl, err := d.TodayRealizedPnlForMarket("u1", day, "US")
	if err != nil {
		t.Fatalf("pnl: %v", err)
	}
	// 空头腿 (100−120)×10=−200；多头腿 (12−10)×5=+10 → 合计 −190。
	if pnl != -190 {
		t.Fatalf("空头平仓实现盈亏口径错误: 期望 −190（−200+10）, got %.2f", pnl)
	}
	// 反向剧本（价格下跌=空头盈利）单测符号：@100 开空、@90 平空 → +100
	d2 := openMR4DB(t)
	if err := d2.ApplyRealFill(mr4Fill("O-W1", "卖出开空", "HOOD", 10, 100, 0, "US", "u2", day+" 10:00:00")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d2.ApplyRealFill(mr4Fill("O-W2", "买入平仓", "HOOD", 10, 90, 0, "US", "u2", day+" 11:00:00")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pnl2, err := d2.TodayRealizedPnlForMarket("u2", day, "US")
	if err != nil || pnl2 != 100 {
		t.Fatalf("空头浮盈方向应计正: 期望 +100, got %.2f err=%v", pnl2, err)
	}
}

// TestMR4TotalAssetsShortNegative 总资产口径：现金 1000 + 空头行（10×现价/成本 100）计 −1000
// → 权益 0；对照多头行 → 2000。
func TestMR4TotalAssetsShortNegative(t *testing.T) {
	d := openMR4DB(t)
	if err := d.UpsertRealAccount(RealAccount{UserID: "u1", Market: "US", AvailableCash: 1000}); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	if err := d.ApplyRealFill(mr4Fill("O-A1", "卖出开空", "TSLA", 10, 100, 0, "US", "u1", "2026-09-22 10:00:00")); err != nil {
		t.Fatalf("seed short: %v", err)
	}
	if total, err := d.TotalAssetsForMarket("u1", "US"); err != nil || total != 0 {
		t.Fatalf("空头行必须计负市值（回款已在现金）: 期望 0, got %.2f err=%v", total, err)
	}
	if err := d.ApplyRealFill(mr4Fill("O-A2", "买入", "NVDA", 10, 100, 0, "US", "u1", "2026-09-22 10:30:00")); err != nil {
		t.Fatalf("seed long: %v", err)
	}
	if total, err := d.TotalAssetsForMarket("u1", "US"); err != nil || total != 1000 {
		// 现金 1000 − 空头 1000 + 多头 1000 = 1000
		t.Fatalf("多空混合权益错误: 期望 1000, got %.2f err=%v", total, err)
	}
}

// TestMR4CostBasisDirectionGuard 成本基准的方向护栏：多头卖出腿不得拿空头行成本计价
// （单向持仓下该组合本会被 ApplyRealFill 拒，但脏数据形态下宁可 fail-open 不计入，
// 也绝不能把盈亏算反方向）。构造：先建空头行，再手工塞一条同码 卖出 fills（绕过 ApplyRealFill
// 直写），断言多头腿因成本不可知而 fail-open（返回 0），而不是拿空头均价给出反向数字。
func TestMR4CostBasisDirectionGuard(t *testing.T) {
	d := openMR4DB(t)
	const day = "2026-09-23"
	if err := d.ApplyRealFill(mr4Fill("O-G1", "卖出开空", "SMCI", 10, 100, 0, "US", "u1", day+" 10:00:00")); err != nil {
		t.Fatalf("seed short: %v", err)
	}
	// 直写脏数据：同码当日一笔 卖出（正常链会被方向冲突拒，这里模拟历史脏行）
	if _, err := d.db.Exec(`INSERT INTO fills(order_id,serial,code,side,price,qty,amount,fee,traded_at,signal_id,user_id,market,currency)
		VALUES('O-G2','','SMCI','卖出',50,10,500,0,?, 'S-G2','u1','US','USD')`, day+" 11:00:00"); err != nil {
		t.Fatalf("dirty fill: %v", err)
	}
	pnl, err := d.TodayRealizedPnlForMarket("u1", day, "US")
	if err != nil {
		t.Fatalf("pnl: %v", err)
	}
	// 多头腿：持仓行是空头（方向护栏拒绝作价）→ 回落当日"买入"均价 → 无 → fail-open 计 0。
	// 若无护栏，将得到 (50−100)×10=−500 的假亏损。
	if pnl != 0 {
		t.Fatalf("多头腿不得用空头行成本计价: 期望 fail-open 0, got %.2f", pnl)
	}
}
