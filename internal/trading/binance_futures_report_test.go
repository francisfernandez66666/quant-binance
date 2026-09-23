// 文件职责：§MR-4B 合约回报链/资金费腿回归（binance_report.go / binance_funding.go 的
// FuturesActive 分叉面）。全部走 httptest 假 fapi（newMockBinance 的 fapi 桩），不外发。
// 覆盖：ORDER_TRADE_UPDATE 帧端到端落账（含空头方向还原与对冲模式拒收）、合约 listenKey
// 走 fapi 端点、REST 差分查合约明细补投、资金费流水摊本与 tran_id 重放幂等。
package trading

import (
	"context"
	"strings"
	"testing"

	"quant-trading-v2/internal/store"
)

// futuresReportFixture 合约视图接收器（不外呼）：futures 执行器 + 独立临时库。
func futuresReportFixture(t *testing.T) (*BinanceReporter, *store.DB, *mockBinance) {
	t.Helper()
	m := newMockBinance(t)
	db := reportDB(t)
	r, err := NewBinanceReporter(BinanceReporterOptions{
		Exec: futuresExecutorFor(t, m, nil), DB: db, UserID: "u1", Market: "CRYPTO",
	})
	if err != nil {
		t.Fatalf("reporter: %v", err)
	}
	return r, db, m
}

// TestReporterFuturesFrameToLedger 合约帧端到端：开空成交帧（SELL/TRADE/FILLED）经本地
// 占位委托行还原真实方向 卖出开空 → 空头持仓行建立；重放幂等；对冲模式帧（po=SHORT）
// 整帧拒收（错方向成交绝不允许入账）。
func TestReporterFuturesFrameToLedger(t *testing.T) {
	r, db, _ := futuresReportFixture(t)
	const bigID = "9007199254740993"
	seedReportOrder(t, db, "CRYPTO", bigID, "shortopen:BTCUSDT:g1", "卖出开空", "已报")
	frame := func(x, X string, body string) []byte {
		return []byte(`{"e":"ORDER_TRADE_UPDATE","T":1759123456789,"o":{"s":"BTCUSDT","S":"SELL","x":"` + x +
			`","X":"` + X + `","q":"0.5","l":"0.5","ap":"60000","L":"60000","n":"0.03","i":` + bigID +
			`,"t":5566778899,"po":"BOTH"` + body + `}}`)
	}
	r.handleFrame(frame("NEW", "ACCEPTED", ""))
	if got := orderRow(t, db, bigID); got.Status != "已报" {
		t.Fatalf("ACCEPTED 帧后状态=%s", got.Status)
	}
	r.handleFrame(frame("TRADE", "FILLED", ""))
	fills := fillsOf(t, db)
	if len(fills) != 1 {
		t.Fatalf("成交腿应入账 1 条，实际 %d", len(fills))
	}
	// 方向还原锁：线方向 SELL 必须折回 卖出开空（否则 ApplyRealFill 会打成多头卖出 no-op）。
	if fills[0].Side != "卖出开空" || fills[0].TradeID != "BTCUSDT:5566778899" {
		t.Fatalf("方向/判重锚错: %+v", fills[0])
	}
	pos, err := db.RealPositionByCodeForUser("u1", "BTCUSDT")
	if err != nil || pos.Side != "short" || pos.Qty != 0.5 {
		t.Fatalf("空头持仓行未建立: %+v %v", pos, err)
	}
	// 重放同帧：fills 幂等不涨行。
	r.handleFrame(frame("TRADE", "FILLED", ""))
	if n := len(fillsOf(t, db)); n != 1 {
		t.Fatalf("重放后 fills 仍应 1 条，实际 %d", n)
	}
	// 对冲模式帧（po=SHORT）：整帧拒收，ignored+1、账本零扰动。
	before := len(fillsOf(t, db))
	ig := r.ignored.Load()
	r.handleFrame([]byte(`{"e":"ORDER_TRADE_UPDATE","T":1,"o":{"s":"BTCUSDT","S":"BUY","x":"TRADE","X":"FILLED","q":"0.5","l":"0.5","ap":"59000","L":"59000","i":99,"t":100,"po":"SHORT"}}`))
	if r.ignored.Load() != ig+1 || len(fillsOf(t, db)) != before {
		t.Fatal("对冲模式帧必须拒收且不入账")
	}
}

// TestReporterFuturesListenKeyAndDiff 合约 listenKey 走 fapi 端点（免签头面，键形 LK-FUT）；
// REST 差分：在途集合缺席的单 → /fapi/v1/order 明细补投成 REST 事件（状态推进+成交兜底）。
func TestReporterFuturesListenKeyAndDiff(t *testing.T) {
	r, db, m := futuresReportFixture(t)
	key, err := r.listenKey(context.Background(), false)
	if err != nil || key != "LK-FUT" {
		t.Fatalf("合约 listenKey=%q err=%v", key, err)
	}
	if n := len(m.queries("/fapi/v1/listenKey")); n != 1 {
		t.Fatalf("listenKey 必须打 fapi 端点，实际 fapi %d 次", n)
	}
	if n := len(m.queries("/api/v3/userDataStream")); n != 0 {
		t.Fatal("合约视图不得触达现货 userDataStream")
	}
	// 差分：本地"已报"单不在 fapi 在途（缺省 []），明细给出 FILLED+累计成交。
	// 先建 0.5 空头（平空成交要有仓可减——单向簿会拒"无空头可平"，这正是账本方向纪律）。
	if err := db.ApplyRealFill(store.RealFill{
		OrderID: "OF0", Code: "BTCUSDT", Name: "BTCUSDT", Side: "卖出开空",
		Price: 59000, Qty: 0.5, Amount: 29500, TradedAt: "2026-09-23 00:00:00",
		SignalID: "open:BTCUSDT:g0", TradeID: "BTCUSDT:ft0", UserID: "u1", Market: "CRYPTO", Currency: "USDT",
	}); err != nil {
		t.Fatal(err)
	}
	seedReportOrder(t, db, "CRYPTO", "1234", "shortcover:BTCUSDT:g2", "买入平仓", "已报")
	m.fapiBodies["/fapi/v1/order"] = `{"orderId":1234,"symbol":"BTCUSDT","status":"FILLED","side":"BUY",
		"price":"0","origQty":"0.5","executedQty":"0.5","cumQuote":"30000","avgPrice":"60000","updateTime":1759123456789}`
	r.pollOnce(context.Background())
	if got := orderRow(t, db, "1234"); got.Status != "已成" {
		t.Fatalf("差分后状态=%s", got.Status)
	}
	fills := fillsOf(t, db)
	// 只数平空腿（前置开空行带 SignalID open:BTCUSDT:g0，按信号键过滤）。
	var cover *store.RealFill
	for i := range fills {
		if fills[i].SignalID == "shortcover:BTCUSDT:g2" {
			cover = &fills[i]
		}
	}
	if cover == nil || cover.Side != "买入平仓" || cover.Price != 60000 {
		t.Fatalf("差分成交补投错: %+v", fills)
	}
}

// TestReporterFundingPollAndIdempotent 资金费腿：income 流水（-1.5 USDT 支出）摊入空头
// 含费成本基准（与开空佣金同族基准加费），tran_id 台账让同窗重放零副作用；
// 零额流水跳过；游标推进后不再回拉旧窗。
func TestReporterFundingPollAndIdempotent(t *testing.T) {
	r, db, m := futuresReportFixture(t)
	if err := db.ApplyRealFill(store.RealFill{
		OrderID: "OF1", Code: "BTCUSDT", Name: "BTCUSDT", Side: "卖出开空",
		Price: 60000, Qty: 0.5, Amount: 30000, TradedAt: "2026-09-23 00:00:00",
		SignalID: "f-sig1", TradeID: "BTCUSDT:ft1", UserID: "u1", Market: "CRYPTO", Currency: "USDT",
	}); err != nil {
		t.Fatal(err)
	}
	m.fapiBodies["/fapi/v1/income"] = `[
		{"symbol":"BTCUSDT","incomeType":"FUNDING_FEE","income":"-1.5","time":1759000000000,"tranId":7001},
		{"symbol":"BTCUSDT","incomeType":"FUNDING_FEE","income":"0","time":1759000001000,"tranId":7002}]`
	r.pollFundingOnce()
	pos, err := db.RealPositionByCodeForUser("u1", "BTCUSDT")
	if err != nil {
		t.Fatal(err)
	}
	// 支出 1.5 USDT / 0.5 张 → 基准抬 3（60003）。
	if pos.CostPrice-60003 > 1e-9 {
		t.Fatalf("资金费未摊本: cost=%.6f", pos.CostPrice)
	}
	if r.fundingApplied.Load() != 1 {
		t.Fatalf("入账计数=%d，期望 1", r.fundingApplied.Load())
	}
	// 同窗重放（游标不推进的失败语义演练）：台账幂等，成本不再动。
	r.pollFundingOnce()
	pos2, _ := db.RealPositionByCodeForUser("u1", "BTCUSDT")
	if pos2.CostPrice != pos.CostPrice {
		t.Fatalf("重放二次摊本: %.6f → %.6f", pos.CostPrice, pos2.CostPrice)
	}
	if n := len(m.queries("/fapi/v1/income")); n < 2 {
		t.Fatalf("income 应被拉取，实际 %d 次", n)
	}
	// 游标推进锁：首轮回看后游标已落非零（后续轮从游标续拉而非重扫 24h）。
	if r.fundingCursorMs.Load() == 0 {
		t.Fatal("整窗消化后游标必须推进")
	}
}

// TestFuturesForkNegativeLocks 分叉负锁：现货视图的接收器三处都不许碰 fapi
// （listenKey 打现货端点、帧解析走 executionReport、差分走 /api/v3 面）。
func TestFuturesForkNegativeLocks(t *testing.T) {
	m := newMockBinance(t)
	db := reportDB(t)
	r, err := NewBinanceReporter(BinanceReporterOptions{
		Exec: executorFor(t, m, "CRYPTO"), DB: db, UserID: "u1", Market: "CRYPTO",
	})
	if err != nil {
		t.Fatal(err)
	}
	if key, kerr := r.listenKey(context.Background(), false); kerr != nil || key != "LK-TEST" {
		t.Fatalf("现货 listenKey=%q err=%v", key, kerr)
	}
	if n := len(m.queries("/fapi/v1/listenKey")); n != 0 {
		t.Fatal("现货视图不得请求 fapi listenKey")
	}
	// 现货视图收到合约帧形（外层信封+内层 o）：解析必失败、走 ignored 不入库。
	before := r.ignored.Load()
	r.handleFrame([]byte(`{"e":"ORDER_TRADE_UPDATE","T":1,"o":{"s":"BTCUSDT","S":"SELL","X":"FILLED","x":"NEW","i":42,"po":"BOTH"}}`))
	if r.ignored.Load() == before {
		t.Fatal("现货解析器遇合约帧形必须计入 ignored")
	}
	// 现货视图 WS URL 绝不落 fstream 域（testnet/主网两形态都由现货函数裁决）。
	if strings.Contains(r.userStreamURL("K1"), "fstream") {
		t.Fatalf("现货 WS URL 混入合约域: %s", r.userStreamURL("K1"))
	}
}
