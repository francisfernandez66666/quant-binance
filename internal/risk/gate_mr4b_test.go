// 文件职责：§MR-4B 第 17 道闸 liq_distance 行为回归——阈值档案门、证据源装配门两级短路
// （数据闸缺省姿势：不配=不生效，与 short_borrow 的 fail-close 刻意相反），
// 距离不足拦截只作用于 卖出开空，平空/买入永不受闸。借券证据源恒真以隔离本闸。
package risk

import (
	"strings"
	"testing"

	"quant-trading-v2/internal/config"
)

// mr4bView CRYPTO 档案：做空已启用（margin>0）+ 借券恒可得（隔离前置闸），
// liq 参数由用例注入（阈值+证据源）。
func mr4bView(margin, liq float64) config.BinanceBrokerView {
	c := config.BinanceConfig{}
	c.Mode = "auto"
	c.Spot = config.BinanceMarketProfile{Enabled: true, ShortMarginRate: margin, LiqDistMinPct: liq}
	return config.BinanceBrokerView{Cfg: c, Market: "CRYPTO"}
}

// TestGateMR4BLiqDistanceMatrix 四象限：阈值 0=闸关（证据源说不可也放行）；
// 阈值>0 未装配=闸关；装配且不可=拦（留痕闸名+告警）；装配且可达=放行。
func TestGateMR4BLiqDistanceMatrix(t *testing.T) {
	bad := func(string, string) (bool, string) { return false, "距强平 1.20% < 阈值 5.00%" }
	good := func(string, string) (bool, string) { return true, "距离充足" }
	cases := []struct {
		name    string
		liq     float64
		src     func(string, string) (bool, string)
		wantHit bool
	}{
		{"threshold_zero_skips", 0, bad, false},
		{"unwired_skips", 5, nil, false},
		{"too_close_rejects", 5, bad, true},
		{"far_enough_passes", 5, good, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var alerts int
			g := NewGate(gateDB(t), "u_mr4b", func(_, _ string, _ string) { alerts++ })
			g.SetShortBorrowEvidenceSource(func(string, string) (bool, string) { return true, "合约恒可开空" })
			if c.src != nil {
				g.SetLiqDistanceEvidenceSource(c.src)
			}
			v := g.CheckLiveOrder(mr4bView(0.5, c.liq), mr4Order("CRYPTO", SideShortOpen, "BTCUSDT"))
			if c.wantHit {
				if v.Pass || v.Gate != "liq_distance" || !strings.Contains(v.Reason, "距强平") || alerts != 1 {
					t.Fatalf("应命中 liq_distance 且告警一次: %+v", v)
				}
				return
			}
			if !v.Pass {
				t.Fatalf("应放行，实际命中 %s: %s", v.Gate, v.Reason)
			}
		})
	}
}

// TestGateMR4BLiqDistanceOnlyShortOpen 方向豁免：多头开仓（买入）、平空（买入平仓）、
// 平多（卖出）在证据源喊"不可"时也必须放行——本闸只加严"继续加空"这一族动作。
func TestGateMR4BLiqDistanceOnlyShortOpen(t *testing.T) {
	for _, side := range []string{SideBuy, SideSell, SideShortCover} {
		g := NewGate(gateDB(t), "u_mr4b"+side, nil)
		g.SetShortBorrowEvidenceSource(func(string, string) (bool, string) { return true, "可得" })
		g.SetLiqDistanceEvidenceSource(func(string, string) (bool, string) { return false, "距强平不足" })
		v := g.CheckLiveOrder(mr4bView(0.5, 5), mr4Order("CRYPTO", side, "BTCUSDT"))
		if !v.Pass {
			t.Fatalf("%s 不应被 liq_distance 拦: %+v", side, v)
		}
	}
}
