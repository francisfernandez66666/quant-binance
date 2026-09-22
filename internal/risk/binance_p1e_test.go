// 文件职责：§P1-e 风控闸市场矩阵测试（PLAN §9：14 闸 × CN/CRYPTO/US 逐格断言）。
// 判定姿势：每格构造「该闸必然命中」的订单与夹具（其余闸全关），断言——
//   矩阵 ✅ → v.Gate 命中该闸；矩阵 ➖ → v.Pass 放行（短路生效）。
// 时间夹具：固定北京 2026-09-23 23:00（=UTC 15:00=纽约 11:00，三地同日期），
// 排除按日账查询的跨日噪声；market_today 用例另用 00:30 时刻制造三地日期分歧。
// English: §P1-e PLAN §9 matrix — 14 gates × 3 markets asserted cell by cell: armed cells must
// hit their gate id, inactive cells must pass (market short-circuit).
package risk

import (
	"errors"
	"testing"
	"time"

	"quant-trading-v2/internal/cntime"
	"quant-trading-v2/internal/config"
	"quant-trading-v2/internal/store"
)

// p1eDay 三地日期一致的交易日历日（北京周三 23:00 = UTC 15:00 = 纽约 11:00）。
const p1eDay = "2026-09-23"

// p1eGate 固定时钟的闸实例 + 临时账本（now=北京 2026-09-23 23:00）。
func p1eGate(t *testing.T) (*Gate, *store.DB) {
	t.Helper()
	db := gateDB(t)
	g := NewGate(db, "u_p1e", nil)
	g.now = func() time.Time { return time.Date(2026, 9, 23, 23, 0, 0, 0, cntime.Loc) }
	return g, db
}

// p1eCode 各市场的代表性代码（CN 带后缀、CRYPTO 币对、US 简写）。
func p1eCode(market string) string {
	switch market {
	case "CRYPTO":
		return "BTCUSDT"
	case "US":
		return "AAPL"
	default:
		return "600000.SH"
	}
}

// p1eOrder 基线订单：除测试目标外全部中性（无战法、陈旧度未提供）。
func p1eOrder(market, side string) LiveOrder {
	return LiveOrder{
		SignalID: "SIG-P1E", Code: p1eCode(market), Name: "测试", Strategy: "龙头",
		Side: side, Price: 10, Qty: 100, Amount: 1000, StalenessMs: -1, Market: market,
	}
}

// p1eFill 按市场落一笔当日成交（market 必填——P1c 校验拒非 CN 代码走 CN 通道）。
func p1eFill(t *testing.T, db *store.DB, market, orderID, side string, price, qty, amount float64) {
	t.Helper()
	m := market
	if m == "" {
		m = "CN"
	}
	f := store.RealFill{OrderID: orderID, Code: p1eCode(market), Side: side,
		Price: price, Qty: qty, Amount: amount, TradedAt: p1eDay + " 10:00:00",
		SignalID: "F-" + orderID, UserID: "u_p1e", Market: m}
	if err := db.ApplyRealFill(f); err != nil {
		t.Fatalf("seed fill %s %s: %v", orderID, market, err)
	}
}

// p1eSeedPosition 按市场落持仓行。
func p1eSeedPosition(t *testing.T, db *store.DB, market string, qty float64) {
	t.Helper()
	m := market
	if m == "" {
		m = "CN"
	}
	if _, err := db.UpsertRealPositions([]store.RealPosition{{TsCode: p1eCode(market), Name: "测试",
		Qty: qty, CostPrice: 10, UserID: "u_p1e", Market: m}}); err != nil {
		t.Fatalf("seed position %s: %v", market, err)
	}
}

// p1eScenario 单道闸的矩阵场景：setup 装弹（配置+夹具+规则源），order 构造命中单。
type p1eScenario struct {
	gate   string
	active map[string]bool // market → PLAN §9 该格是否启用
	setup  func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string)
}

// p1eScenarios 14 道闸的场景表（与 PLAN §9 矩阵逐行对齐）。
func p1eScenarios() []p1eScenario {
	all := map[string]bool{"CN": true, "CRYPTO": true, "US": true}
	cnOnly := map[string]bool{"CN": true}
	cryptoOnly := map[string]bool{"CRYPTO": true}
	cryptoUS := map[string]bool{"CRYPTO": true, "US": true}
	return []p1eScenario{
		{"st", cnOnly, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			// 名称带 *ST 特征（命中判定只看 Name，无需配置）。
		}},
		{"blacklist", all, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			cfg.Blacklist = []string{p1eCode(market)}
		}},
		{"max_order_amount", all, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			cfg.RiskGate.MaxOrderAmount = 500 // 基线单 Amount 1000 > 帽
		}},
		{"t1_sellable", cnOnly, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			// 当日买入 100@10 → 卖 100 可卖量 0（CN 必拦；CRYPTO/US 应被市场短路放行）
			p1eFill(t, db, market, "P1E-T1B", "买入", 10, 100, 1000)
		}},
		{"limit_up_down", cnOnly, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			cfg.RiskGate.LimitUpBlockBuy = true // 昨收 10 买价 11 ≥ 涨停 11
		}},
		{"stale_quote", all, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			cfg.RiskGate.StaleQuoteMs = 30000 // 基线单需覆写 StalenessMs=60000
		}},
		{"price_cross_check", cnOnly, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			cfg.RiskGate.CrossCheckPct = 1
			cfg.RiskGate.CrossCheckShadow = boolPtr(false) // 影子→正式（命中即拒）
			g.SetCrossPriceSource(func(string) (float64, error) { return 8, nil })
		}},
		{"day_loss", all, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			cfg.RiskGate.DayLossLimitPct = 0.1
			cfg.InitialCapital = 100000
			p1eFill(t, db, market, "P1E-DLB", "买入", 10, 100, 1000)
			p1eFill(t, db, market, "P1E-DLS", "卖出", 8, 100, 800) // 已实现亏损 200 ≥ 0.1%
		}},
		{"concentration", all, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			cfg.RiskGate.SingleStockValuePct = 20 // 25000/100000=25% > 20%
			if err := db.UpsertRealAccount(store.RealAccount{UserID: "u_p1e", AvailableCash: 100000,
				UpdatedAt: p1eDay + " 09:00:00"}); err != nil {
				t.Fatalf("account: %v", err)
			}
		}},
		{"buy_discipline", all, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			cfg.DailyMaxBuys = 1
			p1eFill(t, db, market, "P1E-BD1", "买入", 10, 100, 1000) // 今日已成交 1 笔 → 第 2 笔拦
		}},
		{"whitelist", all, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			cfg.Strategies = []string{"龙头"} // 命中单在 order 侧覆写为非白名单战法
		}},
		{"max_positions", all, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			cfg.MaxPositions = 1
			p1eSeedPosition(t, db, market, 100)
		}},
		{"min_notional", cryptoOnly, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			g.SetSymbolRulesSource(func(string) (SymbolRules, error) { return SymbolRules{MinNotional: 10}, nil })
		}},
		{"lot_precision", cryptoUS, func(t *testing.T, cfg *config.QMTConfig, g *Gate, db *store.DB, market string) {
			g.SetSymbolRulesSource(func(string) (SymbolRules, error) { return SymbolRules{StepSize: 0.001}, nil })
		}},
	}
}

// TestGateMarketMatrixP1E §P1-e 主锁：PLAN §9 矩阵 14 闸 × 3 市场逐格断言启停。
func TestGateMarketMatrixP1E(t *testing.T) {
	markets := []string{"CN", "CRYPTO", "US"}
	for _, sc := range p1eScenarios() {
		for _, m := range markets {
			g, db := p1eGate(t)
			cfg := qmtCfg()
			sc.setup(t, &cfg, g, db, m)
			o := p1eOrder(m, SideBuy)
			switch sc.gate {
			case "st":
				o.Name = "*ST测试"
			case "t1_sellable":
				o = p1eOrder(m, SideSell)
			case "limit_up_down":
				o.PrevClose, o.Price = 10, 11
			case "stale_quote":
				o.StalenessMs = 60000
			case "concentration":
				o.Amount = 25000
			case "whitelist":
				o.Strategy, o.StrategyType = "跟风杂法", "跟风杂法"
			case "min_notional":
				o.Amount = 5
			case "lot_precision":
				if m == "CRYPTO" {
					o.Qty = 0.0005 // 非 stepSize 0.001 整数倍
				} else {
					o.Price = 180.555 // 小数位 >2
					o.Qty = 5
					o.Amount = 902.775
				}
			}
			v := g.CheckLiveOrder(cfg, o)
			if sc.active[m] {
				if v.Pass || v.Gate != sc.gate {
					t.Fatalf("矩阵格 %s×%s 应命中: got %+v", sc.gate, m, v)
				}
			} else if !v.Pass {
				t.Fatalf("矩阵格 %s×%s 应放行（市场短路）: got %+v", sc.gate, m, v)
			}
		}
	}
}

// TestGateMarketNormalizeCNIdentityP1E 空 Market 与 "CN" 行为恒等 + marketToday CN 恒等于 today。
// （CN 链零回归的入口级证明：存量调用方全部不带 Market。）
func TestGateMarketNormalizeCNIdentityP1E(t *testing.T) {
	g, _ := p1eGate(t)
	cfg := qmtCfg()
	cfg.RiskGate.MaxOrderAmount = 500
	blank := p1eOrder("", SideBuy)
	cn := p1eOrder("CN", SideBuy)
	v1, v2 := g.CheckLiveOrder(cfg, blank), g.CheckLiveOrder(cfg, cn)
	if v1.Pass || v2.Pass || v1.Gate != "max_order_amount" || v1.Gate != v2.Gate {
		t.Fatalf("空 Market 应与 CN 同闸命中: %+v vs %+v", v1, v2)
	}
	if g.marketToday("") != g.today() || g.marketToday("CN") != g.today() {
		t.Fatalf("CN 日键必须与旧口径逐字节一致")
	}
	// 未知市场值兜底 CN（不报错、不放行非 CN 短路红利）
	unknown := p1eOrder("JP", SideBuy)
	if v := g.CheckLiveOrder(cfg, unknown); v.Pass || v.Gate != "max_order_amount" {
		t.Fatalf("未知市场应兜底 CN 口径: %+v", v)
	}
}

// TestGateMarketTodayTimezoneP1E 日键按市场时区分叉：北京 00:30 时 UTC/纽约仍是「昨日」——
// day_loss 的按日账查询必须用市场日键才命中（CN 日键查昨日种子则放行）。
func TestGateMarketTodayTimezoneP1E(t *testing.T) {
	db := gateDB(t)
	g := NewGate(db, "u_p1e", nil)
	g.now = func() time.Time { return time.Date(2026, 9, 23, 0, 30, 0, 0, cntime.Loc) } // =UTC 09-22 16:30
	if d := g.marketToday("CRYPTO"); d != "2026-09-22" {
		t.Fatalf("CRYPTO 日键应为 UTC 昨日: %s", d)
	}
	if d := g.marketToday("CN"); d != "2026-09-23" {
		t.Fatalf("CN 日键应为北京今日: %s", d)
	}
	// 种子落在 UTC 日（22 日）：CRYPTO 单熔断命中、CN 单日键（23 日）查无损失放行。
	f := store.RealFill{OrderID: "P1E-TZ1", Code: "BTCUSDT", Side: "买入", Market: "CRYPTO",
		Price: 10, Qty: 100, Amount: 1000, TradedAt: "2026-09-22 15:00:00", SignalID: "TZ1", UserID: "u_p1e"}
	f2 := f
	f2.OrderID, f2.SignalID, f2.Side, f2.Price, f2.Amount = "P1E-TZ2", "TZ2", "卖出", 8, 800
	if err := db.ApplyRealFill(f); err != nil {
		t.Fatalf("seed buy: %v", err)
	}
	if err := db.ApplyRealFill(f2); err != nil {
		t.Fatalf("seed sell: %v", err)
	}
	cfg := qmtCfg()
	cfg.RiskGate.DayLossLimitPct = 0.1
	cfg.InitialCapital = 100000
	if v := g.CheckLiveOrder(cfg, p1eOrder("CRYPTO", SideBuy)); v.Pass || v.Gate != "day_loss" {
		t.Fatalf("CRYPTO 应按 UTC 日键命中 day_loss: %+v", v)
	}
	cnOrder := p1eOrder("CN", SideBuy)
	cnOrder.Code = "600000.SH" // CN 日键 23 日无种子 → 放行（同时证明日键未跨市场串账）
	if v := g.CheckLiveOrder(cfg, cnOrder); !v.Pass {
		t.Fatalf("CN 日键（北京 23 日）查无损失应放行: %+v", v)
	}
}

// TestGateSymbolRulesFailOpenP1E 两道新闸的数据缺口 fail-open：规则源未注入/查询失败/
// 规则零值一律放行（数据类闸与 stale_quote 同姿势，绝不因缺数据误拦）。
func TestGateSymbolRulesFailOpenP1E(t *testing.T) {
	// 未注入（默认）：小名义额 + 碎量都放行。
	g, _ := p1eGate(t)
	cfg := qmtCfg()
	o := p1eOrder("CRYPTO", SideBuy)
	o.Amount, o.Qty = 1, 0.0007
	if v := g.CheckLiveOrder(cfg, o); !v.Pass {
		t.Fatalf("无规则源应全放行: %+v", v)
	}
	// 注入但查询报错/零值 → 仍放行。
	g2, _ := p1eGate(t)
	g2.SetSymbolRulesSource(func(string) (SymbolRules, error) { return SymbolRules{}, errRulesBoom })
	if v := g2.CheckLiveOrder(cfg, o); !v.Pass {
		t.Fatalf("规则查询失败/零值应 fail-open: %+v", v)
	}
	// 对齐正例：合法 stepSize 与名义额放行（反例由矩阵格覆盖）。
	g3, _ := p1eGate(t)
	g3.SetSymbolRulesSource(func(string) (SymbolRules, error) {
		return SymbolRules{MinNotional: 5, StepSize: 0.001}, nil
	})
	o3 := p1eOrder("CRYPTO", SideBuy)
	o3.Amount, o3.Qty = 50, 0.5
	if v := g3.CheckLiveOrder(cfg, o3); !v.Pass {
		t.Fatalf("合规 CRYPTO 单应放行: %+v", v)
	}
}

// errRulesBoom 规则源故障哨兵。
var errRulesBoom = errors.New("rules source unavailable")

// TestGateLotPrecisionDecimalGeometryP1E 判定姿势边界锁：
// US 恰好 2 位小数放行（含浮点不可精确表示但最短往返 ≤2 位的形态）；
// CRYPTO stepSize 整数倍在浮点噪声下不误杀（0.3/0.1 类经典例）。
func TestGateLotPrecisionDecimalGeometryP1E(t *testing.T) {
	g, _ := p1eGate(t)
	g.SetSymbolRulesSource(func(string) (SymbolRules, error) { return SymbolRules{StepSize: 0.1}, nil })
	cfg := qmtCfg()
	// US：180.50→"180.5"(1位) 放行、180.55 放行、180.555 拒
	for _, tc := range []struct {
		price float64
		want  bool // true=放行
	}{{180.50, true}, {180.55, true}, {180.555, false}, {180.123456, false}, {180, true}} {
		o := p1eOrder("US", SideBuy)
		o.Price = tc.price
		if v := g.CheckLiveOrder(cfg, o); v.Pass != tc.want {
			t.Fatalf("US 限价 %v 放行期望 %v: %+v", tc.price, tc.want, v)
		}
	}
	// CRYPTO：0.3 是 0.1 的整数倍（浮点 0.3/0.1=2.9999999999999996 族不容差误杀）
	for _, qty := range []float64{0.3, 0.1, 3.7, 100.0} {
		o := p1eOrder("CRYPTO", SideBuy)
		o.Qty = qty
		o.Amount = 10000
		if v := g.CheckLiveOrder(cfg, o); !v.Pass {
			t.Fatalf("CRYPTO qty %s 应为 stepSize 0.1 合法量: %+v", store.QtyString(qty), v)
		}
	}
}
