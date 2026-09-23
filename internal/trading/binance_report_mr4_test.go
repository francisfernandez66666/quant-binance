// binance_report_mr4_test.go — §MR-4A（2026-09-23）做空方向的下单映射与回报方向还原单测。
//
// 两段契约：
//  1. binanceSide：交易所线上只有 BUY/SELL——卖出开空→SELL、买入平仓→BUY，未知方向照旧拒；
//  2. applyReport 方向还原：回报帧的线方向（SELL/BUY）按本地占位委托行的真实方向折叠回
//     卖出开空/买入平仓，成交腿才能走对 ApplyRealFill 的空头分支（开空建空仓、平空减仓清行）。
//
// English: §MR-4A — the wire maps both short sides onto SELL/BUY, and the reporter folds the
// wire side back to the true short direction via the local placeholder order row, so the fill
// leg hits the short branches of ApplyRealFill (open creates a short row, cover drains it).
package trading

import (
	"testing"

	"quant-trading-v2/internal/store"
)

// TestBinanceSideMR4Mapping 四方向映射 + 未知方向拒绝（大小写/英文别名照旧兼容）。
func TestBinanceSideMR4Mapping(t *testing.T) {
	cases := map[string]string{
		SideBuy: "BUY", SideShortCover: "BUY", // 平空在交易所侧是一笔买入
		SideSell: "SELL", SideShortOpen: "SELL", // 开空在交易所侧是一笔卖出
		"buy": "BUY", "BUY": "BUY", "sell": "SELL", "SELL": "SELL",
	}
	for in, want := range cases {
		got, err := binanceSide(in)
		if err != nil || got != want {
			t.Fatalf("binanceSide(%q)=%q,%v 期望 %q", in, got, err, want)
		}
	}
	if _, err := binanceSide("卖出开仓"); err == nil {
		t.Fatal("未知方向必须拒绝（不得默认折叠成任一真实方向）")
	}
}

// spotTradeFrame 造一帧现货成交回报（executionReport x=TRADE X=FILLED）。
func spotTradeFrame(symbol, wireSide, orderID string, qty, px float64, tradeID string) []byte {
	return []byte(`{"e":"executionReport","s":"` + symbol + `","S":"` + wireSide +
		`","x":"TRADE","X":"FILLED","Q":"` + store.QtyString(qty) + `","Z":"` + store.QtyString(qty) +
		`","l":"` + store.QtyString(qty) + `","L":"` + store.QtyString(px) +
		`","n":"0","N":"USDT","i":` + orderID + `,"t":` + tradeID + `,"T":1759123456789,"C":"qt-x"}`)
}

// TestReporterMR4ShortDirectionRestored 开空→平空一轮：委托行存真实空头方向，
// 回报帧只带 SELL/BUY，成交腿入账方向必须是 卖出开空/买入平仓，空头持仓行随开平建立/清零。
func TestReporterMR4ShortDirectionRestored(t *testing.T) {
	r, db, _ := reportFixture(t, "CRYPTO")
	// ① 开空：本地占位行 Side=卖出开空（placeOrder 落库形态），交易所回 SELL。
	if _, err := db.UpsertRealOrder(store.RealOrder{
		OrderID: "9001", SignalID: "short:BTCUSDT:1", Code: "BTCUSDT", Side: SideShortOpen,
		Status: "已报", Price: 65000, Qty: 0.5, CreatedAt: "2026-09-23 01:00:00", UserID: "u1", Market: "CRYPTO",
	}); err != nil {
		t.Fatalf("seed short-open order: %v", err)
	}
	r.handleFrame(spotTradeFrame("BTCUSDT", "SELL", "9001", 0.5, 65000, "7001"))
	fs := fillsOf(t, db)
	if len(fs) != 1 || fs[0].Side != SideShortOpen || fs[0].SignalID != "short:BTCUSDT:1" {
		t.Fatalf("成交腿方向应还原为 %s, got %+v", SideShortOpen, fs)
	}
	p, err := db.RealPositionByCodeForUser("u1", "BTCUSDT")
	if err != nil || p.Side != "short" || p.Qty != 0.5 {
		t.Fatalf("开空应建空头持仓行, got %+v err=%v", p, err)
	}
	// ② 平空：本地占位行 Side=买入平仓，交易所回 BUY → 方向还原 + 空头行清空。
	if _, err := db.UpsertRealOrder(store.RealOrder{
		OrderID: "9002", SignalID: "cover:BTCUSDT:1", Code: "BTCUSDT", Side: SideShortCover,
		Status: "已报", Price: 64000, Qty: 0.5, CreatedAt: "2026-09-23 02:00:00", UserID: "u1", Market: "CRYPTO",
	}); err != nil {
		t.Fatalf("seed cover order: %v", err)
	}
	r.handleFrame(spotTradeFrame("BTCUSDT", "BUY", "9002", 0.5, 64000, "7002"))
	fs = fillsOf(t, db)
	if len(fs) != 2 {
		t.Fatalf("平空成交腿应入账, fills=%d", len(fs))
	}
	if _, err := db.RealPositionByCodeForUser("u1", "BTCUSDT"); err == nil {
		t.Fatal("全平后空头持仓行应清掉（qty 归零删行）")
	}
}

// TestReporterMR4WireSideKeptWithoutLocalRow 本地无委托行（回报先于回填到达的竞态）时，
// 成交腿保持线方向（买入/卖出）——与 CN 链同款降级，不做任何方向猜测。
func TestReporterMR4WireSideKeptWithoutLocalRow(t *testing.T) {
	r, db, _ := reportFixture(t, "CRYPTO")
	// 无本地行的 SELL 成交：ext 占位插委托行，成交腿方向=卖出（ApplyRealFill 对无持仓卖出
	// 按账本规则拒入账，这里只断言方向未被伪造）。
	r.handleFrame(spotTradeFrame("ETHUSDT", "SELL", "9101", 1, 3000, "7101"))
	o := orderRow(t, db, "9101")
	if o.SignalID != "ext:9101" || o.Side != SideSell {
		t.Fatalf("无主回报应落 ext 占位+线方向, got %+v", o)
	}
}
