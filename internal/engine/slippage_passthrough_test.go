// slippage_passthrough_test.go §ENH-B7 滑点校准回灌挂价行为锁：
// ①帽=0 → 双侧 frac=0 且挂价函数逐字节原样返回（现状等价锁）；
// ②战法级池命中（中位数 bp→比例）；③战法级缺失 → 全局样本回退；
// ④样本不足（双向<30）→ 0 不调价；⑤帽钳制 + 负中位数（买得更便宜）不回灌成更优价。
// English: §ENH-B7 locks — zero cap byte-identity, strategy-pool median lookup, global
// fallback, insufficient-sample no-op, cap clamp + non-negative clamp.
package engine

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"quant-trading-v2/internal/config"
	"quant-trading-v2/internal/store"
	"quant-trading-v2/internal/trading"
)

// slipTestCtrl 构造仅用于读 QMT() 配置的控制器（永不下单，NoopExecutor 足够）。
func slipTestCtrl(t *testing.T, capF float64) *trading.Controller {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.QMTConfig{Enabled: true, RiskGate: config.RiskGateConfig{SlippagePassthrough: capF}}
	return trading.NewController(trading.NoopExecutor{}, db, "u_1", cfg, nil)
}

// seedSlipFills 写入 n 条同方向、固定 bps 的模拟盘成交（strategyType=池键；空=全局样本行）。
// filled_at 按分钟错开避开 UNIQUE(user_id,code,side,filled_at)。
func seedSlipFills(t *testing.T, db *store.DB, strategyType, side string, bps float64, n int) {
	t.Helper()
	day := time.Now().Format("2006-01-02")
	sig := 10.0
	price := sig * (1 + bps/10000) // 买贵为正；卖侧信号价换算同为正口径（下方显式区分）
	if side == "sell" {
		price = sig * (1 - bps/10000) // 卖便宜为正
	}
	recs := make([]store.PaperTradeRecord, 0, n)
	for i := 0; i < n; i++ {
		recs = append(recs, store.PaperTradeRecord{
			UserID: "u_1", Code: fmt.Sprintf("60%04d.SH", i%7), StrategyType: strategyType,
			Side: side, Price: price, SignalPrice: sig, Qty: 100, Amount: price * 100,
			FilledAt: fmt.Sprintf("%s 09:%02d:00", day, i%60),
		})
	}
	if err := db.SavePaperTrades(recs); err != nil {
		t.Fatal(err)
	}
}

func almostEq(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

// TestSlipFracsZeroCap 帽=0：即使库里有足量样本也恒 (0,0)，且价格函数原样返回（等价锁）。
func TestSlipFracsZeroCap(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trading.db"))
	if err != nil {
		t.Fatal(err)
	}
	seedSlipFills(t, db, "dragon", "buy", 20, 35)
	seedSlipFills(t, db, "dragon", "sell", 40, 35)
	e := &Engine{}
	e.SetD1Store(db)
	buy, sell := e.slipFracs(slipTestCtrl(t, 0), "龙头", "")
	if buy != 0 || sell != 0 {
		t.Fatalf("帽=0 必须 (0,0)，got %v/%v", buy, sell)
	}
	if p := slipBuyPrice(12.34, 0); !almostEq(p, 12.34) {
		t.Fatalf("帽=0 买入挂价须逐字节原样: %v", p)
	}
	if p := slipSellPrice(12.34, 0); !almostEq(p, 12.34) {
		t.Fatalf("帽=0 卖出挂价须逐字节原样: %v", p)
	}
}

// TestSlipFracsStrategyPool 战法级命中：龙头→dragon 池中位数 20bp/40bp → 0.002/0.004（帽内）。
func TestSlipFracsStrategyPool(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trading.db"))
	if err != nil {
		t.Fatal(err)
	}
	seedSlipFills(t, db, "dragon", "buy", 20, 35)
	seedSlipFills(t, db, "dragon", "sell", 40, 35)
	seedSlipFills(t, db, "", "buy", 500, 35) // 全局样本更差——战法级命中时不得越权回退
	e := &Engine{}
	e.SetD1Store(db)
	buy, sell := e.slipFracs(slipTestCtrl(t, 0.01), "龙头", "")
	if !almostEq(buy, 0.002) || !almostEq(sell, 0.004) {
		t.Fatalf("战法级中位数期望 0.002/0.004, got %v/%v", buy, sell)
	}
	if p := slipBuyPrice(10.0, buy); !almostEq(p, 10.02) {
		t.Fatalf("买入挂价 10×(1+0.002) 期望 10.02, got %v", p)
	}
	if p := slipSellPrice(10.0, sell); !almostEq(p, 9.96) {
		t.Fatalf("卖出挂价 10×(1−0.004) 期望 9.96, got %v", p)
	}
}

// TestSlipFracsGlobalFallback 战法池零样本 → 回退全局（btreplay A.3 同源口径）。
func TestSlipFracsGlobalFallback(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trading.db"))
	if err != nil {
		t.Fatal(err)
	}
	seedSlipFills(t, db, "n_shape", "buy", 30, 35)
	seedSlipFills(t, db, "n_shape", "sell", 60, 35)
	e := &Engine{}
	e.SetD1Store(db)
	buy, sell := e.slipFracs(slipTestCtrl(t, 0.01), "龙头", "") // dragon 池无样本 → 全局命中
	if !almostEq(buy, 0.003) || !almostEq(sell, 0.006) {
		t.Fatalf("全局回退期望 0.003/0.006, got %v/%v", buy, sell)
	}
}

// TestSlipFracsInsufficientSample 双向样本不足 → (0,0)（宁不用小样本噪声价）。
func TestSlipFracsInsufficientSample(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trading.db"))
	if err != nil {
		t.Fatal(err)
	}
	seedSlipFills(t, db, "", "buy", 20, 10) // 仅 10 条 <30
	seedSlipFills(t, db, "", "sell", 20, 40)
	e := &Engine{}
	e.SetD1Store(db)
	if buy, sell := e.slipFracs(slipTestCtrl(t, 0.01), "龙头", ""); buy != 0 || sell != 0 {
		t.Fatalf("样本不足必须 (0,0)，got %v/%v", buy, sell)
	}
}

// TestSlipFracsCapClampAndNegative 帽钳制（300bp>帽1%→帽）+ 买侧负中位数（买得更便宜）→0。
func TestSlipFracsCapClampAndNegative(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "trading.db"))
	if err != nil {
		t.Fatal(err)
	}
	seedSlipFills(t, db, "", "buy", 300, 35)  // 3bp... 实为 300bp=3% → 钳到帽 1%
	seedSlipFills(t, db, "", "sell", -50, 35) // 负滑点（卖得更贵）→ 不回灌成更优价 → 0
	e := &Engine{}
	e.SetD1Store(db)
	buy, sell := e.slipFracs(slipTestCtrl(t, 0.01), "龙头", "")
	if !almostEq(buy, 0.01) {
		t.Fatalf("期望帽钳制 0.01, got %v", buy)
	}
	if sell != 0 {
		t.Fatalf("负中位数须钳 0（不主动让出更优价），got %v", sell)
	}
}

// TestSlipFracsNilDB 研究库未装配（测试/最小装配）→ 静默 (0,0)，不panic。
func TestSlipFracsNilDB(t *testing.T) {
	e := &Engine{}
	if buy, sell := e.slipFracs(slipTestCtrl(t, 0.01), "龙头", ""); buy != 0 || sell != 0 {
		t.Fatalf("nil 库期望 (0,0)，got %v/%v", buy, sell)
	}
}
