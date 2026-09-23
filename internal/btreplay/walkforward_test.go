// 文件职责：§ENH-B6 walkforward.go 单测——合成「样本内冠军在样本外翻车」序列：
// IS 段每 3 天一轮回（入场次日 +10% 摸到 TP8 止盈线），OOS 段每 5 天一轮回（+6% 摸不到
// TP8、第 4 天 -10% 打穿 SL5）；组合 A(TP8,SL5,hold30) IS 全胜、OOS 全败，组合
// B(TP15,SL5,hold2) IS 微亏（+10% 内超期）、OOS 满胜（+6% 内超期）。
// 锁五件事：① 日期轴切分与触发两窗归属计数；② IS Top-K 点全部带入 OOS 重放且两窗数字
// 正确（胜率镜像翻转）；③ 推荐键切换——oosIR=nil 走全样本 Sharpe 旧键、非 nil 只认
// OOS 合格点（A 被 B 反超）、空 map（全低样本）→ nil 降级；④ 数据不足/全低样本的
// nil 与标记语义（low_sample_oos 可查不可荐）；⑤ walkForwardJSON 节形状。
// English: §ENH-B6 walk-forward tests — synthetic IS-favorable/OOS-reversal series; split
// counting, Top-K replay correctness, recommendation-key switch, and honest degradation paths.
package btreplay

import (
	"testing"

	"quant-trading-v2/internal/config"
	data "quant-trading-v2/internal/data"
)

// wfKlines 单股票包装：合成 K 线序列 → simulateUniform 所需的 code→bars 映射。
func wfKlines(kls []data.KLine) map[string][]data.KLine {
	return map[string][]data.KLine{"AAA": kls}
}

// comboB key 辅助：按五维参数直接组键（与 keyOf 同构，测试端不依赖结果字段反推）。
func wfKey(tp, sl float64, hold int, score, atr float64) comboKey {
	return comboKey{tp, sl, float64(hold), score, atr}
}

func TestWalkForwardSplitAndReplayX(t *testing.T) {
	const isBars, oosBars = 112, 48 // 160 根：IS 37 个触发（sigIdx 0..108 步长3），OOS 9 个（112..152 步长5）
	closes := make([]float64, 0, isBars+oosBars)
	for i := 0; i < isBars; i++ {
		closes = append(closes, []float64{100, 100, 110}[i%3])
	}
	// OOS 轮内刻意不等幅（涨档 103~107 恒 <TP8、砸盘档 86~92 恒破 SL5）：等幅轮会让两组合
	// 的日收益序列各自退化为常数（std=0 → 日频 Sharpe 恒 0），oos_ir 比较失去判别力。
	for i := 0; i < oosBars; i++ {
		if i < 45 { // 9 个完整翻车轮，尾部 3 根静置不产生交易歧义
			c := i / 5
			switch i % 5 {
			case 2:
				closes = append(closes, 103+float64(c%4))
			case 3:
				closes = append(closes, 104+float64((c*3)%4))
			case 4:
				closes = append(closes, 86+float64((c*5)%7))
			default:
				closes = append(closes, 100)
			}
		} else {
			closes = append(closes, 100)
		}
	}
	kls := mkKLine(closes)
	klines := wfKlines(kls)
	var trigs []sweepTrigger
	// IS 信号止于 108：最后一轮（108 入场 110 出场）整笔落在样本窗内，杜绝跨界污染计数。
	for s := 0; s <= isBars-4; s += 3 {
		trigs = append(trigs, sweepTrigger{ad: 0, code: "AAA", sigIdx: s, entry: kls[s+1].Open, score: -1, highest: kls[s].High, buySlip: 5, sellSlip: 5, fillR: 1})
	}
	for s := isBars; s < isBars+45; s += 5 {
		trigs = append(trigs, sweepTrigger{ad: 0, code: "AAA", sigIdx: s, entry: kls[s+1].Open, score: -1, highest: kls[s].High, buySlip: 5, sellSlip: 5, fillR: 1})
	}
	combos := []combo5{{tp: 8, sl: 5, hold: 30}, {tp: 15, sl: 5, hold: 2}}
	// minTriggers=15：B 组 hold=2 出场致 nextFree 与 3 日信号距互斥、每轮隔一触发才进场，
	// IS 实际成单 19 笔——门槛取 20 会把 B 挤到线外（夹具敏感性，非引擎缺陷）。
	wf := runWalkForward("测试战法", "", &WalkForwardOptions{MinOOSTrig: 5, TopK: 2},
		trigs, klines, nil, nil, combos, "winrate", 15, 0)
	if wf == nil {
		t.Fatal("数据充足时 WF 不得降级为 nil")
	}
	if wf.SplitDate != kls[isBars].Date.Format("20060102") {
		t.Fatalf("切分日须=日期轴 70%% 分位（OOS 首日）: %s", wf.SplitDate)
	}
	if wf.ISTriggers != 37 || wf.OOStiggers != 9 {
		t.Fatalf("两窗触发归属错: IS=%d OOS=%d", wf.ISTriggers, wf.OOStiggers)
	}
	if len(wf.Points) != 2 || wf.Qualified != 2 {
		t.Fatalf("Top-K=2 全部应过 OOS 样本线: %+v", wf.Points)
	}
	// 胜率镜像翻转：A 组 IS 全胜 / OOS 全败；B 组反过来——推荐键不切换就会选中 A（翻车冠军）。
	var pa, pb *walkForwardPoint
	for i := range wf.Points {
		switch wf.Points[i].IS.Trail {
		case 8:
			pa = &wf.Points[i]
		case 15:
			pb = &wf.Points[i]
		}
	}
	if pa == nil || pb == nil {
		t.Fatal("两点组合未全部回放到")
	}
	if pa.IS.WinRate != 100 || pa.OOS.WinRate != 0 || pa.OOS.Count != 9 {
		t.Fatalf("A 组应 IS 全胜/OOS 全败9笔: is=%v oos=%v", pa.IS, pa.OOS)
	}
	if pb.IS.WinRate != 0 || pb.OOS.WinRate != 100 {
		t.Fatalf("B 组应 IS 全败/OOS 全胜: is=%v oos=%v", pb.IS, pb.OOS)
	}
	if !(wf.OOSIR[wfKey(15, 5, 2, 0, 0)] > wf.OOSIR[wfKey(8, 5, 30, 0, 0)]) {
		t.Fatalf("oos_ir 须反映真实验证窗强弱: %+v", wf.OOSIR)
	}
	// 推荐键切换：旧键（全样本 Sharpe）会选拟合冠军，新键必须选 OOS 稳健解。
	front := []sweepResult{
		{Name: "A", Trail: 8, StopLossPct: 5, Hold: 30, Count: 46, WinRate: 70, ProfitFactor: 3, Sharpe: 4, Calmar: 2},
		{Name: "B", Trail: 15, StopLossPct: 5, Hold: 2, Count: 46, WinRate: 40, ProfitFactor: 1, Sharpe: 1, Calmar: 0.5},
	}
	cfg := config.ParetoConfig{MinWinRate: 30, MinProfitFactor: 0.5, MinSharpe: 0.5, MinCalmar: 0.1}
	if r := recommendedSolution(front, cfg, nil); r == nil || r.Trail != 8 {
		t.Fatalf("oosIR=nil 须保持旧全样本 Sharpe 键: %v", r)
	}
	if r := recommendedSolution(front, cfg, wf.OOSIR); r == nil || r.Trail != 15 {
		t.Fatalf("oosIR 在位须切 OOS 键（A 无合格 OOS 记录不得参荐）: %v", r)
	}
	if r := recommendedSolution(front, cfg, map[comboKey]float64{}); r != nil {
		t.Fatalf("OOS 全低样本（空合格集）推荐须降级 nil，不得矮子里拔将军: %v", r)
	}
	// JSON 节形状：键齐备 + low_sample_oos 显式在位（worker/前端按此判可荐性）。
	j := walkForwardJSON(wf)
	if j["split_date"] == nil || j["points"] == nil || j["qualified"] == nil {
		t.Fatalf("walk_forward 节缺键: %v", j)
	}
	if walkForwardJSON(nil) != nil {
		t.Fatal("nil 结果须序列化为 nil（载荷键缺位=基线等价）")
	}
}

// 护栏负例：样本太短/无战法候选等病态载荷必须拒绝出推荐键，不得硬算 oos_ir。
func TestWalkForwardGuardsX(t *testing.T) {
	closes := make([]float64, 0, 120)
	for i := 0; i < 120; i++ {
		closes = append(closes, []float64{100, 100, 110}[i%3])
	}
	kls := mkKLine(closes)
	klines := wfKlines(kls)
	trigs := []sweepTrigger{{code: "AAA", sigIdx: 0, entry: 100, score: -1, highest: 101, buySlip: 5, sellSlip: 5, fillR: 1}}
	combos := []combo5{{tp: 8, sl: 5, hold: 30}}
	// ① 日期轴不可切分（全库仅 1 个交易日）。
	if runWalkForward("x", "", nil, trigs, map[string][]data.KLine{"BBB": {kls[0]}}, nil, nil, combos, "winrate", 1, 0) != nil {
		t.Fatal("单一交易日日期轴必须降级 nil")
	}
	// ② IS 段无一过样本门槛 → 无候选可带入 OOS（空 WF 好于假 WF）。
	if wf := runWalkForward("x", "", &WalkForwardOptions{}, trigs, klines, nil, nil, combos, "winrate", 20, 0); wf != nil {
		t.Fatalf("IS 触发不足 minTriggers 须降级 nil: %+v", wf)
	}
	// ③ 低样本 OOS：数字保留、可荐数为 0。
	for s := 3; s < 118; s += 3 {
		trigs = append(trigs, sweepTrigger{code: "AAA", sigIdx: s, entry: 100, score: -1, highest: 101, buySlip: 5, sellSlip: 5, fillR: 1})
	}
	wf := runWalkForward("x", "", &WalkForwardOptions{MinOOSTrig: 999}, trigs, klines, nil, nil, combos, "winrate", 20, 0)
	if wf == nil || wf.Qualified != 0 || len(wf.Points) != 1 || !wf.Points[0].LowSample {
		t.Fatalf("全低样本点应标 low_sample_oos 且零合格: %+v", wf)
	}
	if len(wf.OOSIR) != 0 {
		t.Fatal("低样本点不得进推荐键数据源")
	}
}
