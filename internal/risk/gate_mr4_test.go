// gate_mr4_test.go — §MR-4A（2026-09-23）做空风控闸单测。
//
// 覆盖四件事：
//  1. 空头方向（卖出开空/买入平仓）市场闸——CN 一律入口拒（fail-close + 高优告警），US/CRYPTO 放行到闸清单；
//  2. 第 16 道闸 short_borrow 的三段判定——档案保证金率 0=禁用、证据源未装配=拒（fail-close，
//     与其余数据闸"未装配=跳过"姿势刻意相反）、证据源判不可借=拒并带回原因；
//  3. 方向性闸对开空的扩展（黑名单/白名单/集中度/持仓数/日内熔断）与对平空的豁免（退出通道必须保留）；
//  4. 买入纪律只管买入：空头两向都不吃买入预算，同场景买入单必须仍被拦（防"扩展"改成"放行一切"）。
//
// English: §MR-4A short-side risk gate tests — CN shorts rejected at the entry (fail-close, alert),
// the 16th short_borrow gate (margin switch / unwired-source reject / unavailability reject),
// opening-side gates extended to short-open while cover stays exempt, and buy-discipline
// deliberately untouched by shorts (a buy in the same scenario must still be blocked).
package risk

import (
	"strings"
	"testing"

	"quant-trading-v2/internal/config"
	"quant-trading-v2/internal/store"
)

// mr4View 构造零风险闸的币安市场视图（RiskGate 留零值=全关，逐用例只开需要的那道，
// 免得出厂默认的 DayLossLimitPct=3/集中度=20 之类干扰单闸断言）。
func mr4View(market string, margin float64) config.BinanceBrokerView {
	c := config.BinanceConfig{}
	c.Mode = "auto"
	p := config.BinanceMarketProfile{Enabled: true, ShortMarginRate: margin}
	if market == "US" {
		c.Stock = p
	} else {
		c.Spot = p
	}
	return config.BinanceBrokerView{Cfg: c, Market: market}
}

// mr4Order US/CRYPTO 空头订单视图（Code 用互斥形态防串场）。
func mr4Order(market, side, code string) LiveOrder {
	return LiveOrder{
		SignalID: "SIG-MR4-" + side + code, Code: code, Name: code,
		Side: side, Price: 100, Qty: 1, Amount: 100, Market: market, StalenessMs: -1,
	}
}

// TestGateMR4ShortSidesRejectedOnCN 空头方向在 CN（含空市场键）入口即拒：
// 命中闸=short_side_market_unsupported、高优告警、且不进任何后续闸的留痕。
func TestGateMR4ShortSidesRejectedOnCN(t *testing.T) {
	var alerts int
	g := NewGate(gateDB(t), "u_mr4cn", func(_, _ string, _ string) { alerts++ })
	for _, market := range []string{"", "CN"} {
		for _, side := range []string{SideShortOpen, SideShortCover} {
			v := g.CheckLiveOrder(qmtCfg(), mr4Order(market, side, "600000.SH"))
			if v.Pass || v.Gate != "short_side_market_unsupported" {
				t.Fatalf("CN 空头 %s/%s 应入口拒, got %+v", market, side, v)
			}
		}
	}
	if alerts != 4 {
		t.Fatalf("空头市场闸应为机构级高优告警（4 笔 4 次）, got %d", alerts)
	}
}

// TestGateMR4ShortOpenAllowedWithEvidence US/CRYPTO 开空：档案开做空 + 证据源判可借 → 放行。
func TestGateMR4ShortOpenAllowedWithEvidence(t *testing.T) {
	for _, market := range []string{"US", "CRYPTO"} {
		g := NewGate(gateDB(t), "u_mr4ok", nil)
		g.SetShortBorrowEvidenceSource(func(mk, code string) (bool, string) { return true, "" })
		if v := g.CheckLiveOrder(mr4View(market, 0.5), mr4Order(market, SideShortOpen, "TSLA")); !v.Pass {
			t.Fatalf("%s 开空（档案启用+可借证据）应放行, got %+v", market, v)
		}
	}
}

// TestGateMR4ShortBorrowFailClose 第 16 道闸三段判定的前两段负例：
// ①保证金率 0=该市场禁做空；②证据源未装配=拒一切开空（fail-close，新能力默认不可用）。
func TestGateMR4ShortBorrowFailClose(t *testing.T) {
	// ① 档案禁用
	g1 := NewGate(gateDB(t), "u_mr4m", nil)
	g1.SetShortBorrowEvidenceSource(func(string, string) (bool, string) { return true, "" })
	v := g1.CheckLiveOrder(mr4View("US", 0), mr4Order("US", SideShortOpen, "TSLA"))
	if v.Pass || v.Gate != "short_borrow" || !strings.Contains(v.Reason, "short_margin_rate") {
		t.Fatalf("保证金率 0 应被 short_borrow 禁用, got %+v", v)
	}
	// ② 证据源未装配（即使档案已开做空）
	g2 := NewGate(gateDB(t), "u_mr4w", nil)
	v = g2.CheckLiveOrder(mr4View("US", 0.5), mr4Order("US", SideShortOpen, "TSLA"))
	if v.Pass || v.Gate != "short_borrow" || !strings.Contains(v.Reason, "未装配") {
		t.Fatalf("证据源未装配必须 fail-close 拒开空, got %+v", v)
	}
	// ③ 证据源判不可借：原因原样带回
	g3 := NewGate(gateDB(t), "u_mr4u", nil)
	g3.SetShortBorrowEvidenceSource(func(string, string) (bool, string) { return false, "无券源（hard-to-borrow）" })
	v = g3.CheckLiveOrder(mr4View("US", 0.5), mr4Order("US", SideShortOpen, "TSLA"))
	if v.Pass || v.Gate != "short_borrow" || !strings.Contains(v.Reason, "无券源") {
		t.Fatalf("不可借应拒并带回原因, got %+v", v)
	}
}

// TestGateMR4CoverNeverBlocked 平空是退出通道：档案禁做空、证据源未装配、黑名单命中，
// 一律不得拦 买入平仓（与卖出豁免全部开仓闸同族语义）。
func TestGateMR4CoverNeverBlocked(t *testing.T) {
	g := NewGate(gateDB(t), "u_mr4c", nil)
	cfg := mr4View("US", 0) // 做空禁用 + 未装配证据源
	if v := g.CheckLiveOrder(cfg, mr4Order("US", SideShortCover, "TSLA")); !v.Pass {
		t.Fatalf("平空（退出动作）不得被做空档案/借券闸拦截, got %+v", v)
	}
	cfg2 := mr4View("US", 0.5)
	cfg2.Cfg.Stock.Blacklist = []string{"TSLA"} // 黑名单只拦开仓方向
	if v := g.CheckLiveOrder(cfg2, mr4Order("US", SideShortCover, "TSLA")); !v.Pass {
		t.Fatalf("平空不得被黑名单强迫扛单, got %+v", v)
	}
}

// TestGateMR4OpeningGatesCoverShortOpen 五道"开仓向"闸对卖出开空的扩展断言，
// 每道闸独立小场景（零值档案只开被测闸），并逐一验证平空/卖出同场景仍放行。
func TestGateMR4OpeningGatesCoverShortOpen(t *testing.T) {
	t.Run("blacklist", func(t *testing.T) {
		g := NewGate(gateDB(t), "u_mr4bl", nil)
		g.SetShortBorrowEvidenceSource(func(string, string) (bool, string) { return true, "" })
		cfg := mr4View("US", 0.5)
		cfg.Cfg.Stock.Blacklist = []string{"TSLA"}
		if v := g.CheckLiveOrder(cfg, mr4Order("US", SideShortOpen, "TSLA")); v.Pass || v.Gate != "blacklist" {
			t.Fatalf("开空应被黑名单拦（新增敞口）, got %+v", v)
		}
		if v := g.CheckLiveOrder(cfg, mr4Order("US", SideShortCover, "TSLA")); !v.Pass {
			t.Fatalf("平空应放行（退出通道）, got %+v", v)
		}
	})
	t.Run("whitelist", func(t *testing.T) {
		g := NewGate(gateDB(t), "u_mr4wl", nil)
		g.SetShortBorrowEvidenceSource(func(string, string) (bool, string) { return true, "" })
		cfg := mr4View("US", 0.5)
		cfg.Cfg.Stock.Strategies = []string{"alpha"}
		o := mr4Order("US", SideShortOpen, "TSLA")
		o.Strategy = "beta"
		if v := g.CheckLiveOrder(cfg, o); v.Pass || v.Gate != "whitelist" {
			t.Fatalf("非白名单战法开空应被拦, got %+v", v)
		}
	})
	t.Run("concentration", func(t *testing.T) {
		db := gateDB(t)
		g := NewGate(db, "u_mr4cc", nil)
		g.SetShortBorrowEvidenceSource(func(string, string) (bool, string) { return true, "" })
		if err := db.UpsertRealAccount(store.RealAccount{UserID: "u_mr4cc", Market: "US", AvailableCash: 10000}); err != nil {
			t.Fatalf("seed account: %v", err)
		}
		cfg := mr4View("US", 0.5)
		cfg.Cfg.RiskGate.SingleStockValuePct = 10
		o := mr4Order("US", SideShortOpen, "TSLA")
		o.Amount = 5000 // 50% > 10% 帽
		if v := g.CheckLiveOrder(cfg, o); v.Pass || v.Gate != "concentration" {
			t.Fatalf("开空应受单票集中度帽约束（名义敞口不看方向）, got %+v", v)
		}
	})
	t.Run("max_positions", func(t *testing.T) {
		db := gateDB(t)
		g := NewGate(db, "u_mr4mp", nil)
		g.SetShortBorrowEvidenceSource(func(string, string) (bool, string) { return true, "" })
		// 已有 1 个 US 多头持仓，档案上限 1 → 开空不得再占新仓位数
		if err := db.ApplyRealFill(store.RealFill{OrderID: "O-MP", Code: "NVDA", Name: "NVDA", Side: "买入",
			Price: 100, Qty: 1, Amount: 100, TradedAt: "2026-09-22 10:00:00", SignalID: "S-MP",
			UserID: "u_mr4mp", Market: "US"}); err != nil {
			t.Fatalf("seed long: %v", err)
		}
		cfg := mr4View("US", 0.5)
		cfg.Cfg.Stock.MaxPositions = 1
		if v := g.CheckLiveOrder(cfg, mr4Order("US", SideShortOpen, "TSLA")); v.Pass || v.Gate != "max_positions" {
			t.Fatalf("空头同样占用持仓数上限, got %+v", v)
		}
		// 同一上限下平空放行（不新开仓位）
		if v := g.CheckLiveOrder(cfg, mr4Order("US", SideShortCover, "TSLA")); !v.Pass {
			t.Fatalf("平空不受持仓数上限约束, got %+v", v)
		}
	})
	t.Run("day_loss", func(t *testing.T) {
		db := gateDB(t)
		g := NewGate(db, "u_mr4dl", nil)
		g.SetShortBorrowEvidenceSource(func(string, string) (bool, string) { return true, "" })
		if err := db.UpsertRealAccount(store.RealAccount{UserID: "u_mr4dl", Market: "US", AvailableCash: 100000}); err != nil {
			t.Fatalf("seed account: %v", err)
		}
		day := g.marketToday("US")
		// 当日完成一轮空头：@100 开空 1000 → @120 平空，实现亏损 20000 = 权益 20% ≥ 阈值 1%
		if err := db.ApplyRealFill(store.RealFill{OrderID: "O-DL1", Code: "AMD", Name: "AMD", Side: SideShortOpen,
			Price: 100, Qty: 1000, Amount: 100000, TradedAt: day + " 10:00:00", SignalID: "S-DL1",
			UserID: "u_mr4dl", Market: "US"}); err != nil {
			t.Fatalf("seed short open: %v", err)
		}
		if err := db.ApplyRealFill(store.RealFill{OrderID: "O-DL2", Code: "AMD", Name: "AMD", Side: SideShortCover,
			Price: 120, Qty: 1000, Amount: 120000, TradedAt: day + " 11:00:00", SignalID: "S-DL2",
			UserID: "u_mr4dl", Market: "US"}); err != nil {
			t.Fatalf("seed short cover: %v", err)
		}
		cfg := mr4View("US", 0.5)
		cfg.Cfg.RiskGate.DayLossLimitPct = 1
		if v := g.CheckLiveOrder(cfg, mr4Order("US", SideShortOpen, "TSLA")); v.Pass || v.Gate != "day_loss" {
			t.Fatalf("空头实现亏损同样触发日内熔断（开空=新开敞口被断）, got %+v", v)
		}
		// 熔断不断退出：平空/卖出照常放行
		if v := g.CheckLiveOrder(cfg, mr4Order("US", SideShortCover, "TSLA")); !v.Pass {
			t.Fatalf("日内熔断不得拦平空退出, got %+v", v)
		}
	})
}

// TestGateMR4BuyDisciplineIgnoresShortSides 买入纪律口径只管"买成几笔"：
// 当日买入预算耗尽时，买入单被 buy_discipline 拦，而开空/平空都不吃该预算（必须放行）。
func TestGateMR4BuyDisciplineIgnoresShortSides(t *testing.T) {
	db := gateDB(t)
	g := NewGate(db, "u_mr4bd", nil)
	g.SetShortBorrowEvidenceSource(func(string, string) (bool, string) { return true, "" })
	day := g.marketToday("US")
	if err := db.ApplyRealFill(store.RealFill{OrderID: "O-BD", Code: "MSTR", Name: "MSTR", Side: "买入",
		Price: 50, Qty: 2, Amount: 100, TradedAt: day + " 09:40:00", SignalID: "S-BD",
		UserID: "u_mr4bd", Market: "US"}); err != nil {
		t.Fatalf("seed buy fill: %v", err)
	}
	cfg := mr4View("US", 0.5)
	cfg.Cfg.Stock.DailyMaxBuys = 1 // 今日已买成 1 笔 → 预算耗尽
	if v := g.CheckLiveOrder(cfg, mr4Order("US", SideBuy, "COIN")); v.Pass || v.Gate != "buy_discipline" {
		t.Fatalf("买入预算耗尽时买入单必须被拦（对照组）, got %+v", v)
	}
	if v := g.CheckLiveOrder(cfg, mr4Order("US", SideShortOpen, "COIN")); !v.Pass {
		t.Fatalf("开空不动用买入预算，不得被 buy_discipline 误拦, got %+v", v)
	}
	if v := g.CheckLiveOrder(cfg, mr4Order("US", SideShortCover, "COIN")); !v.Pass {
		t.Fatalf("平空是退出动作，不得被 buy_discipline 误拦, got %+v", v)
	}
}
