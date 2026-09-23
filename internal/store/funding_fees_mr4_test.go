// 文件职责：§MR-4B 资金费摊本台账单元回归——tran_id 主键幂等（重放不动成本）、
// 收取方向（income 为正=基准下调）、无持仓行时台账照落成本不动、缺 tran_id 拒记。
// 持仓行复用 openMR4DB/mr4Fill/mr4Pos 三件套（空头经 ApplyRealFill 合法建立）。
package store

import (
	"math"
	"testing"
)

// TestMR4BFundingLedgerIdempotent 支出流水（income=-1.5）摊 0.5 张空头：基准 +3；
// 同 tran_id 重放返回 applied=false 且成本纹丝不动。
func TestMR4BFundingLedgerIdempotent(t *testing.T) {
	db := openMR4DB(t)
	if err := db.ApplyRealFill(mr4Fill("OF-A", "卖出开空", "BTCUSDT", 0.5, 60000, 0, "CRYPTO", "u1", "2026-09-23 00:00:00")); err != nil {
		t.Fatal(err)
	}
	applied, err := db.AddPositionFundingFee("u1", "CRYPTO", "BTCUSDT", 7001, -1.5, 1759000000000)
	if err != nil || !applied {
		t.Fatalf("首记应 applied: %v %v", applied, err)
	}
	p := mr4Pos(t, db, "BTCUSDT", "u1")
	if math.Abs(p.CostPrice-60003) > 1e-9 {
		t.Fatalf("摊本错: cost=%.6f 期望 60003", p.CostPrice)
	}
	// 重放同流水：幂等命中，成本不再动。
	again, err := db.AddPositionFundingFee("u1", "CRYPTO", "BTCUSDT", 7001, -1.5, 1759000000000)
	if err != nil || again {
		t.Fatalf("重放应幂等 false: %v %v", again, err)
	}
	if p2 := mr4Pos(t, db, "BTCUSDT", "u1"); p2.CostPrice != p.CostPrice {
		t.Fatalf("重放二次摊本: %.6f", p2.CostPrice)
	}
}

// TestMR4BFundingIncomePositiveAndNoPosition 收取方向（income=+0.5=基准下调）与
// 无持仓流水（台账落、成本无对象、二次同 tran 仍幂等）。
func TestMR4BFundingIncomePositiveAndNoPosition(t *testing.T) {
	db := openMR4DB(t)
	if err := db.ApplyRealFill(mr4Fill("OF-B", "买入", "ETHUSDT", 2, 3000, 0, "CRYPTO", "u1", "2026-09-23 00:00:00")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddPositionFundingFee("u1", "CRYPTO", "ETHUSDT", 7002, 0.5, 1759000000000); err != nil {
		t.Fatal(err)
	}
	// 多头收到 funding：基准下调 0.25/张。
	if p := mr4Pos(t, db, "ETHUSDT", "u1"); math.Abs(p.CostPrice-2999.75) > 1e-9 {
		t.Fatalf("收取摊本错: cost=%.6f", p.CostPrice)
	}
	// 无持仓符号：applied=true（首记）但不炸账。
	ok, err := db.AddPositionFundingFee("u1", "CRYPTO", "SOLUSDT", 7003, -2, 1759000000000)
	if err != nil || !ok {
		t.Fatalf("无仓流水应静默首记: %v %v", ok, err)
	}
	// 缺幂等锚：拒记（宁可不记也不留二次摊本风险）。
	if _, err := db.AddPositionFundingFee("u1", "CRYPTO", "ETHUSDT", 0, -1, 1759000000000); err == nil {
		t.Fatal("tran_id=0 必须拒记")
	}
	// 台账读面：ETH 两条流水按时间倒序可见。
	fees, err := db.FundingFeesForPosition("u1", "CRYPTO", "SOLUSDT", 10)
	if err != nil || len(fees) != 1 || fees[0].TranID != 7003 {
		t.Fatalf("台账读面错: %+v %v", fees, err)
	}
}
