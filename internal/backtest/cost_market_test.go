// cost_market_test.go — 文件职责：§BINANCE-P4 三市场成本模型断言（PLAN §12 测试金字塔·Go 单测）。
// 锁三件事：1) CN 档与改造前 DefaultCostModel 字节等价（回归铁律：CN 链行为不变）；
// 2) CRYPTO/US 档位字段映射（无印花税、taker 0.1%、$0.35 平台费下限、SEC31 卖出单边）；
// 3) TAF 按股封顶与 USCostFraction 退化口径一致性。
// English: pins CN byte-equality plus the crypto/US field mapping and the share-based TAF cap.
package backtest

import (
	"math"
	"testing"
)

func almost(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

// CN 等值锁：未知/空/小写市场一律回落默认成本模型——CN 老链成本口径逐字段不得漂移。
func TestCostModelForMarketCNUnchanged(t *testing.T) {
	want := DefaultCostModel()
	for _, m := range []string{"CN", "cn", "", "UNKNOWN"} {
		if got := CostModelForMarket(m); got != want {
			t.Errorf("CostModelForMarket(%q)=%+v want CN default %+v", m, got, want)
		}
	}
}

func TestCostModelForMarketCrypto(t *testing.T) {
	c := CostModelForMarket("CRYPTO")
	if c.StampTaxRate != 0 || c.MinCommission != 0 {
		t.Errorf("CRYPTO 应无印花税/最低佣金: %+v", c)
	}
	if !almost(c.CommissionRate, 0.001) {
		t.Errorf("CRYPTO taker 应为万10 got %v", c.CommissionRate)
	}
	if c.SlippageBps <= 0 {
		t.Errorf("CRYPTO 滑点应为正 got %v", c.SlippageBps)
	}
	// BNB 抵扣 25%：0.001 → 0.00075
	if !almost(CryptoTakerRate(DefaultCryptoFees, true), 0.00075) {
		t.Errorf("BNB 抵扣费率错 got %v", CryptoTakerRate(DefaultCryptoFees, true))
	}
	// 往返成本 = 滑点双边(2×10bp=0.002) + 佣金双边(2×0.001=0.002) = 0.004（大名义额不触发下限）
	if !almost(c.CostFraction(100000), 0.002+0.002) {
		t.Errorf("CRYPTO 往返成本率 got %v", c.CostFraction(100000))
	}
}

func TestCostModelForMarketUS(t *testing.T) {
	c := CostModelForMarket("US")
	if !almost(c.CommissionRate, 0) {
		t.Errorf("US 应零佣金 got %v", c.CommissionRate)
	}
	if !almost(c.MinCommission, 0.35) || !almost(c.AssumeNotional, DefaultUSFees.AssumeNotional) {
		t.Errorf("US 平台费/名义额槽位错 %+v", c)
	}
	// StampTaxRate 槽位承载 SEC §31（卖出单边按比例）
	if !almost(c.StampTaxRate, DefaultUSFees.Sec31PerNotional) {
		t.Errorf("US SEC31 槽位 got %v", c.StampTaxRate)
	}
	// 小额单（$100）：平台费 $0.35 折算 0.35% 双边生效，验证最低费下限不被低估
	if f := c.CostFraction(100); f < 0.007 {
		t.Errorf("US 小额单平台费下限未生效 got %v", f)
	}
}

func TestUSCostFractionTaf(t *testing.T) {
	// 1000 股 × $50 = $50k 名义：TAF = min(0.000166*1000, 8.30) = $0.166 → 0.166/50000
	base := CostModelForMarket("US").CostFraction(50000)
	withTaf := DefaultUSFees.USCostFraction(50000, 1000)
	if !almost(withTaf-base, 0.166/50000) {
		t.Errorf("TAF 增量错: base=%v with=%v", base, withTaf)
	}
	// 50000 股：TAF 触顶 $8.30
	if math.Abs(DefaultUSFees.USCostFraction(50*50000, 50000)-CostModelForMarket("US").CostFraction(50*50000)-8.30/(50*50000)) > 1e-12 {
		t.Error("TAF 应封顶 8.30")
	}
	// shares<=0 退化为 CostFraction 口径
	if !almost(DefaultUSFees.USCostFraction(50000, 0), base) {
		t.Error("shares=0 应退化为名义额口径")
	}
}
