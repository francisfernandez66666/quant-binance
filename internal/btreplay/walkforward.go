// walkforward.go §ENH-B6 样本外验证（walk-forward）：把「全样本最优」拆成
// IS（In-Sample，训练窗）与 OOS（Out-of-Sample，验证窗）两段——网格在 IS 段重选出
// Top-K 候选，再逐点在 OOS 段重放，输出各点 OOS 触发数/胜率/盈亏比/日频 Sharpe；
// Pareto 推荐解评分键从全样本 Sharpe 切换为 OOS 日频 Sharpe（键名 oos_ir，§回测自动
// 增强 A1 既定约定：IR 取验证窗日频口径 = (mean−rf/252)/std，与 perfMetricsRF 同源）。
//
// 回归铁律：本文件只在 SweepConfig.WalkForward 非 nil 时被调用——开关关闭时
// SWEEP_JSON 载荷与基线逐字节等价（walk_forward 键 omitempty 缺位、推荐键不切换）。
// 防的正是「样本内曲线拟合出的冠军在全新行情段原形毕露」：低样本 OOS 点标注
// low_sample_oos 且不参与推荐；全部低样本/无 IS 解时推荐为 nil（前端降级为纯前沿）。
// English: §ENH-B6 walk-forward — IS Top-K selection replayed on the OOS window; the Pareto
// recommendation key switches to OOS daily Sharpe (oos_ir). Zero payload change when disabled.
package btreplay

import (
	"math"
	"sort"

	"quant-trading-v2/internal/data"
)

// WalkForwardOptions §ENH-B6 样本外验证参数（SweepConfig 指针字段，nil=关闭走旧行为）。
type WalkForwardOptions struct {
	ISRatio    float64 // 训练窗占日期轴比例，缺省 0.7（须落在 (0,1) 开区间，否则取缺省）
	MinOOSTrig int     // OOS 段最低触发样本，缺省 10（低于此=low_sample_oos，不参与推荐）
	TopK       int     // 从 IS 段带入 OOS 重放的候选数，缺省 20
}

// wfEffective 归一化配置：非法/零值回退缺省档（与 minTriggersForObj 同风格的宽容入参）。
func (w *WalkForwardOptions) wfEffective() (ratio float64, minOOS, topK int) {
	ratio, minOOS, topK = 0.7, 10, 20
	if w == nil {
		return
	}
	if w.ISRatio > 0 && w.ISRatio < 1 {
		ratio = w.ISRatio
	}
	if w.MinOOSTrig > 0 {
		minOOS = w.MinOOSTrig
	}
	if w.TopK > 0 {
		topK = w.TopK
	}
	return
}

// comboKey 组合五维参数键（止盈/止损/持仓/门槛/ATR 倍数）——IS 选点与 OOS 重放、
// 以及 Pareto 前沿点的推荐键切换全部按此键对齐（浮点键值来自同一 stepRange 生成器，
// 无累加路径差异，可安全作 map 键）。
type comboKey [5]float64

// keyOf 从模拟结果反推组合键（字段与 simulateUniform 入参一一对应）。
func keyOf(r *sweepResult) comboKey {
	return comboKey{r.Trail, r.StopLossPct, float64(r.Hold), r.MinScore, r.AtrStopMult}
}

// walkForwardPoint 单点两窗档案：IS 选段结果 + OOS 重放结果 + 低样本标记。
type walkForwardPoint struct {
	IS        sweepResult
	OOS       sweepResult
	LowSample bool // OOS 触发数 < MinOOSTrig：数字可查但不参推荐
}

// walkForwardResult 单战法样本外验证汇总。
type walkForwardResult struct {
	SplitDate  string  // 切分日（YYYYMMDD，OOS 首交易日）
	ISRatio    float64 // 生效的训练窗比例
	MinOOS     int     // 生效的 OOS 最低触发样本
	TopK       int     // 生效的 IS Top-K
	ISTriggers int     // IS 段入场事件数
	OOStiggers int     // OOS 段入场事件数
	Qualified  int     // 非低样本、可参推荐的点数
	Points     []walkForwardPoint
	OOSIR      map[comboKey]float64 // 合格点的 OOS 日频 Sharpe（推荐键切换的数据源）
}

// runWalkForward 单战法样本外验证主流程：
//  1. 全局日期轴 = 所有参与股票 K 线日期的并集（升序），切分点 = 轴上 ISRatio 分位；
//  2. 触发事件按信号日日期分入 IS/OOS 两段（同一预计算 trigger 集，只切归属，不重跑信号）；
//  3. 同一组合网格在 IS 段重放（simulateUniform 复用，门槛沿用 §W7 minTriggers），按目标
//     函数取 Top-K；
//  4. Top-K 逐点在 OOS 段重放，OOS 触发 < MinOOSTrig 的点标低样本、其 IR 不进推荐源。
//
// 返回 nil = 数据不足以切分（日期轴 <2 日 / 任一窗零触发），调用方按「无 WF 段」降级，
// 载荷 walk_forward 键保持缺位（omitempty），旧输出形状不受扰动。
// English: IS Top-K selection + OOS replay; nil when the calendar can't be split (payload stays
// byte-identical to baseline in that case).
func runWalkForward(name, kind string, wf *WalkForwardOptions, trigs []sweepTrigger,
	klines map[string][]data.KLine, atrs map[string][]float64, sc *slipCtx,
	combos []combo5, obj string, minTriggers int, rf float64) *walkForwardResult {
	if len(trigs) == 0 || len(combos) == 0 {
		return nil
	}
	ratio, minOOS, topK := wf.wfEffective()

	// 1) 全局日期轴并集（触发分区与切分日共用同一轴，保证 OOS 是"未来段"而非交错抽样）。
	axis := make(map[string]struct{})
	for _, kls := range klines {
		for i := range kls {
			axis[kls[i].Date.Format("20060102")] = struct{}{}
		}
	}
	dates := make([]string, 0, len(axis))
	for d := range axis {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	if len(dates) < 2 {
		return nil // 单一交易日无法两窗切分——宁可无 WF，不做假切分
	}
	split := int(math.Round(ratio * float64(len(dates))))
	if split < 1 {
		split = 1
	}
	if split > len(dates)-1 {
		split = len(dates) - 1
	}
	splitDate := dates[split] // OOS 首日；IS = 严格早于该日

	// 2) 触发两窗归属：按信号日（入场决策日）切，入场在次日开盘，跨界污染最小。
	var trigsIS, trigsOOS []sweepTrigger
	for _, t := range trigs {
		kls, ok := klines[t.code]
		if !ok || t.sigIdx < 0 || t.sigIdx >= len(kls) {
			continue // 越界防御：异常触发不进任何一窗
		}
		if kls[t.sigIdx].Date.Format("20060102") < splitDate {
			trigsIS = append(trigsIS, t)
		} else {
			trigsOOS = append(trigsOOS, t)
		}
	}
	if len(trigsIS) == 0 || len(trigsOOS) == 0 {
		return nil
	}

	// 3) IS 段网格重放 → 目标函数 Top-K（沿用 §W7 最小样本门槛：训练窗噪声点不带入 OOS）。
	type isCand struct {
		res   sweepResult
		score float64
	}
	var cands []isCand
	for _, cb := range combos {
		r := simulateUniform(name, kind, trigsIS, klines, cb.tp, cb.sl, cb.hold, cb.score, cb.atr, atrs, rf, sc)
		if r.Count < minTriggers {
			continue
		}
		r.ObjectiveScore = objectiveValue(obj, &r)
		cands = append(cands, isCand{res: r, score: r.ObjectiveScore})
	}
	if len(cands) == 0 {
		return nil // IS 段无过门槛解——无候选可带入 OOS，空 WF 好于假 WF
	}
	// 排序确定性：目标分降序，并列按触发数、再按组合键字典序（浮点分平局时结果不抖）。
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		if cands[i].res.Count != cands[j].res.Count {
			return cands[i].res.Count > cands[j].res.Count
		}
		ki, kj := keyOf(&cands[i].res), keyOf(&cands[j].res)
		for x := range ki {
			if ki[x] != kj[x] {
				return ki[x] < kj[x]
			}
		}
		return false
	})
	if len(cands) > topK {
		cands = cands[:topK]
	}

	// 4) OOS 重放 + 推荐键数据源。
	out := &walkForwardResult{
		SplitDate: splitDate, ISRatio: ratio, MinOOS: minOOS, TopK: topK,
		ISTriggers: len(trigsIS), OOStiggers: len(trigsOOS),
		OOSIR: map[comboKey]float64{},
	}
	for _, c := range cands {
		oos := simulateUniform(name, kind, trigsOOS, klines,
			c.res.Trail, c.res.StopLossPct, c.res.Hold, c.res.MinScore, c.res.AtrStopMult, atrs, rf, sc)
		p := walkForwardPoint{IS: c.res, OOS: oos, LowSample: oos.Count < minOOS}
		if !p.LowSample {
			out.OOSIR[keyOf(&c.res)] = oos.Sharpe // oos_ir = OOS 日频 Sharpe（perfMetricsRF 同口径）
			out.Qualified++
		}
		out.Points = append(out.Points, p)
	}
	return out
}

// walkForwardJSON WF 结果 → SWEEP_JSON walk_forward 节。逐点两窗数字并列展示；
// low_sample_oos=true 的点保留数字但推荐端跳过（与 §W7 兜底档同一诚实原则：可查不可荐）。
func walkForwardJSON(wf *walkForwardResult) map[string]any {
	if wf == nil {
		return nil
	}
	briefSide := func(r *sweepResult) map[string]any {
		return map[string]any{
			"trigger_count": r.Count, "win_rate": r.WinRate, "profit_factor": r.ProfitFactor,
			"sharpe": r.Sharpe, "expectancy": r.Expectancy,
		}
	}
	points := make([]map[string]any, 0, len(wf.Points))
	for i := range wf.Points {
		p := &wf.Points[i]
		points = append(points, map[string]any{
			"params": map[string]any{"take_profit_pct": p.IS.Trail, "stop_loss_pct": p.IS.StopLossPct,
				"hold_days": p.IS.Hold, "min_score": p.IS.MinScore, "atr_stop_mult": p.IS.AtrStopMult},
			"is":             briefSide(&p.IS),
			"oos":            briefSide(&p.OOS),
			"oos_ir":         p.OOS.Sharpe, // 与 OOSIR 源同值（日频口径），低样本案仅作展示
			"low_sample_oos": p.LowSample,
		})
	}
	return map[string]any{
		"split_date": wf.SplitDate, "is_ratio": wf.ISRatio,
		"min_oos_triggers": wf.MinOOS, "top_k": wf.TopK,
		"is_triggers": wf.ISTriggers, "oos_triggers": wf.OOStiggers,
		"qualified": wf.Qualified, "points": points,
	}
}
