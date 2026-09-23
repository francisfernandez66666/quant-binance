// slippage_passthrough.go §ENH-B7 滑点校准回灌实单挂价（engine 侧消费端）。
//
// 思路：回测侧已有同源能力——btreplay.buildSlipCtx 用 store.PaperSlippageCalib
// （paper_trades 实测「信号价→成交价」分方向中位数，bp）给模拟回放定价。本文件把
// 同一份校准喂给实单挂价：开关 = risk_gate.slippage_passthrough（比例帽，0=关）。
//   - 买入挂价 = 参考价 × (1 + buyFrac)，卖出镜像 × (1 − sellFrac)；
//   - buyFrac/sellFrac = 中位数 bp/1e4，钳制进 [0, 帽]——中位数为负（实测买得更便宜）
//     不回灌成"挂更优价"，保持现状等价，防止把校准噪声变成主动让价；
//   - 分组口径与 btreplay 完全同源：paper.PoolKeyForStrategy(显示名, 规则ID) 战法级
//     优先，样本不足（单方向 < 校准最少样本）回退全局再试一次；仍不足 → 双侧 frac=0
//     并 opslog 按日留痕（宁不用小样本噪声价）；
//   - 热载：配置每次下单时经 ctrl.QMT() 现取，与既有 qmt 配置 5s 热同步（待生效队列）
//     同链路，改配置无需重启。
//
// 缺省 0 = 请求载荷逐字节等价现状（连乘法都不做，浮点位不动）。
// 生效前提说明：网关侧 req.Price 仅在 price_type=limit 时被消费（market 单按对手方
// 最优价撮合），limit 形态下本调价为"预置滑点预算"，market 形态下仅影响金额展示与
// 金额帽闸口径。
// English: §ENH-B7 — feeds the paper-fill slippage calibration (same source/strategy-level
// fallback as btreplay) into live order limit pricing, capped by risk_gate.slippage_passthrough;
// 0 (default) keeps the request byte-identical; negative medians clamp to 0 (never improve).
package engine

import (
	"quant-trading-v2/internal/config"
	"quant-trading-v2/internal/opslog"
	"quant-trading-v2/internal/paper"
	"quant-trading-v2/internal/store"
	"quant-trading-v2/internal/trading"
)

// slipFracs 计算买卖两侧的挂价滑点比例（各自 ∈[0,cap]，0=不调价）。
// ctrl 由调用方传入（autoPlace/sellRealPosition 本就持有），避免二次取锁找控制器。
// strategy/strategyID 与 §C6 幂等键无关——仅派生校准分组池键（btreplay 同源）。
func (e *Engine) slipFracs(ctrl *trading.Controller, strategy, strategyID string) (buyFrac, sellFrac float64) {
	slipCap := ctrl.QMT().RiskGate.SlippagePassthrough
	if slipCap <= 0 {
		return 0, 0
	}
	e.mu.RLock()
	db := e.d1Store
	e.mu.RUnlock()
	if db == nil {
		// 研究库未装配（测试/最小装配）：静默等价现状——本开关是增强而非硬依赖。
		return 0, 0
	}
	// 校准窗口/最少样本与回测侧同一套内置默认（backtest.go 常量），不另开配置面。
	window := config.BacktestDefaultCalibWindowDays
	minSample := config.BacktestDefaultCalibMinSample
	poolKey := paper.PoolKeyForStrategy(strategy, strategyID)
	var calib *store.SlippageCalib
	if poolKey != "" {
		if c, err := db.PaperSlippageCalib(poolKey, window); err == nil && c != nil &&
			c.BuyN >= minSample && c.SellN >= minSample {
			calib = c
		}
	}
	// 战法级缺失/样本不足 → 全局样本回退（A.3 分组口径，与 btreplay 一致）
	if calib == nil {
		if g, err := db.PaperSlippageCalib("", window); err == nil && g != nil &&
			g.BuyN >= minSample && g.SellN >= minSample {
			calib = g
		}
	}
	if calib == nil {
		opslog.DayOnce("slipcalib:insufficient", func() {
			opslog.Logf("quant", "滑点回灌已开(帽=%.4f)但近%d天实测样本不足（双向≥%d，池=%s），挂价维持参考价不调价",
				slipCap, window, minSample, poolKey)
		})
		return 0, 0
	}
	// 中位数 bp → 比例，钳制 [0, 帽]：负中位数（买得更便宜）取 0，不让校准反向改善挂价。
	clamp := func(medBps float64) float64 {
		frac := medBps / 10000
		if frac <= 0 {
			return 0
		}
		if frac > slipCap {
			return slipCap
		}
		return frac
	}
	return clamp(calib.BuyMedBps), clamp(calib.SellMedBps)
}

// slipBuyPrice 买入侧应用：frac=0 时原样返回（零浮点运算，载荷逐字节等价）。
func slipBuyPrice(price, frac float64) float64 {
	if frac <= 0 {
		return price
	}
	return price * (1 + frac)
}

// slipSellPrice 卖出侧镜像：frac=0 时原样返回。
func slipSellPrice(price, frac float64) float64 {
	if frac <= 0 {
		return price
	}
	return price * (1 - frac)
}
