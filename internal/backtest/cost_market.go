// cost_market.go — 文件职责：§BINANCE-P4（PLAN Phase 4 第 3 条）CostModel 三市场参数化。
// 本文件为纯增量新文件：不改动 cost.go 任何既有函数/字段/默认值（CN 链行为字节不变），
// 仅提供按市场装配入口 CostModelForMarket 与新市场专属费率表/纯函数：
//
//	· CN     —— 复用 DefaultCostModel（万 2.5 佣金 + 最低 5 元 + 卖出印花税 0.05%），零变化；
//	· CRYPTO —— maker/taker 费率（现货基础档 taker 万 10 = 0.1%，BNB 抵扣 25% 折扣 → 万 7.5），
//	            无印花税/最低佣金；滑点上抬（薄饼深度差于 A 股盘口，缺省单边 10bp）；
//	            资金费率仅存在于永续合约，现货回测不计（PLAN §2.8 备注，接合约回测时再扩字段）。
//	· US     —— 零佣金（Binance Stocks 平台费最低 $0.35/单，借 MinCommission 槽位表达，
//	            币种为 USD——AssumeNotional 同步换成美元名义额）；卖出监管费两笔：
//	            SEC §31 费（按卖出成交额比例，现行 ~$27.80/百万美元，每年复核）复用
//	            StampTaxRate 的"卖出单边"语义槽位；FINRA TAF 按股计费（$0.000166/股、
//	            单笔 $8.30 封顶）CostModel 形状装不下（无股数维度），单独提供
//	            USCostFraction(notional, shares) 精确计入。
//
// English: append-only three-market cost model; CN keeps DefaultCostModel byte-identical,
// crypto gets maker/taker tables, US adds SEC-31 (sell-side, reuses the stamp slot) and a
// share-count-aware TAF helper that the notional-only CostModel shape cannot express.
package backtest

import "strings"

// CryptoFeeParams 加密现货费率表（单边费率，买卖各按各自腿型计）。
type CryptoFeeParams struct {
	MakerRate      float64 // maker 挂单费率（现货基础 VIP0 档 0.001）
	TakerRate      float64 // taker 吃单费率（同上；两币对档位一致时 maker==taker）
	BnbDiscount    float64 // BNB 抵扣折扣（0.25 = 减免 25%）
	SlippageBps    float64 // 单边滑点建议值（bp），深度差的币对可上调
	AssumeNotional float64 // 回测假设每笔名义额（USDT）
}

// DefaultCryptoFees 现货 VIP0 基础档费率表（PLAN §2.8：taker 0.1% 级）。
// 回测撮合按"吃单"保守计价（无对手方保证），故 CostModelForMarket 取 TakerRate。
var DefaultCryptoFees = CryptoFeeParams{
	MakerRate:      0.001,
	TakerRate:      0.001,
	BnbDiscount:    0.25,
	SlippageBps:    10,
	AssumeNotional: 10000,
}

// USFeeParams 美股（Binance Stocks）监管费率表——零佣金结构，成本全在监管费与平台费。
type USFeeParams struct {
	CommissionRate   float64 // 名义佣金率（Binance Stocks=0；切券商/换 SIP 付费源后可非零）
	PlatformFeeMin   float64 // 平台费最低 $0.35/单（买卖双边各自适用）
	Sec31PerNotional float64 // SEC §31 卖出费率：$27.80/百万美元 ≈ 0.0000278（每年调整需复核）
	TafPerShare      float64 // FINRA TAF：$0.000166/股（卖出）
	TafCap           float64 // TAF 单笔封顶 $8.30
	SlippageBps      float64 // 单边滑点建议（IEX 碎股盘口宽，缺省 5bp）
	AssumeNotional   float64 // 回测假设每笔名义额（USD）
}

// DefaultUSFees 美股默认监管费率表（数值出处：docs/RESEARCH_US_DATA_STRATEGY_20260922.md §费用行）。
var DefaultUSFees = USFeeParams{
	CommissionRate:   0,
	PlatformFeeMin:   0.35,
	Sec31PerNotional: 0.00002780,
	TafPerShare:      0.000166,
	TafCap:           8.30,
	SlippageBps:      5,
	AssumeNotional:   10000,
}

// CostModelForMarket 按市场装配回测成本模型。market 取值 CN|US|CRYPTO（大小写不敏感），
// 未知/空串回落 CN——与全局"缺省 A 股"纪律一致。
// 说明：
//
//	· US 档把 SEC §31 装进 StampTaxRate 槽（同为"卖出单边按比例"语义），平台费装进
//	  MinCommission 槽（币种 USD）；TAF 按股计费形状装不下，往返成本请改用 USCostFraction；
//	· CRYPTO 档无印花税/最低佣金，maker/taker 取 DefaultCryptoFees.TakerRate（保守吃单口径），
//	  BNB 抵扣用 CryptoTakerRate 自行换算后覆写入参模型即可（字段全导出，无隐藏状态）。
func CostModelForMarket(market string) CostModel {
	switch strings.ToUpper(strings.TrimSpace(market)) {
	case "CRYPTO":
		f := DefaultCryptoFees
		return CostModel{
			CommissionRate: f.TakerRate,
			MinCommission:  0,             // 加密无单笔最低费
			SlippageBps:    f.SlippageBps, // 盘口深度差于 A 股：单边 10bp 起
			StampTaxRate:   0,             // 无印花税（现货亦无资金费）
			AssumeNotional: f.AssumeNotional,
		}
	case "US":
		u := DefaultUSFees
		return CostModel{
			CommissionRate: u.CommissionRate,
			MinCommission:  u.PlatformFeeMin, // $0.35/单最低平台费（双边各自适用，同 MinCommission 语义）
			SlippageBps:    u.SlippageBps,
			StampTaxRate:   u.Sec31PerNotional, // 卖出单边按比例 → SEC §31 槽位复用（含注释声明）
			AssumeNotional: u.AssumeNotional,
		}
	default: // CN 与未知市场：与改造前完全同源
		return DefaultCostModel()
	}
}

// CryptoTakerRate 给 BNB 抵扣后的加密 taker 费率（纯函数）：enabled=false 原样返回。
func CryptoTakerRate(f CryptoFeeParams, bnbDeduct bool) float64 {
	if !bnbDeduct {
		return f.TakerRate
	}
	return f.TakerRate * (1 - f.BnbDiscount)
}

// USCostFraction 美股一笔「买→卖」往返的总成本率（含 TAF 按股项），语义对齐 CostModel.CostFraction：
// 往返拖累 = 滑点双边 + 平台费双边（MinCommission 下限折算）+ SEC §31 卖出单边 + TAF 卖出单边（封顶）。
// 入参 notional 为该笔名义成交额（美元，<=0 回落 AssumeNotional）；shares 为成交股数（<=0 时 TAF 记 0，
// 退化为 CostModelForMarket("US").CostFraction(notional) 口径）。
func (u USFeeParams) USCostFraction(notional, shares float64) float64 {
	if notional <= 0 {
		notional = u.AssumeNotional
	}
	c := CostModel{
		CommissionRate: u.CommissionRate,
		MinCommission:  u.PlatformFeeMin,
		SlippageBps:    u.SlippageBps,
		StampTaxRate:   u.Sec31PerNotional,
		AssumeNotional: u.AssumeNotional,
	}
	total := c.CostFraction(notional)
	if shares > 0 {
		taf := u.TafPerShare * shares
		if u.TafCap > 0 && taf > u.TafCap {
			taf = u.TafCap
		}
		total += taf / notional
	}
	return total
}

// USCostFraction 包级便捷入口：用 DefaultUSFees 表算美股往返成本率。
func USCostFraction(notional, shares float64) float64 {
	return DefaultUSFees.USCostFraction(notional, shares)
}
