// 文件职责：sentiment.go 相位判级的边界表锁（§ENH-A1）。阈值几何按 FNG 官方口径：
// 冰点 ≤25 / 中性 26-75 / 高潮 ≥76，边界值逐一钉死（24/25、75/76 各跨一侧）；
// 核心锁的是"无证据≠中性"——hasEvidence=false 或 value 越界必须落 ("unknown", false)，
// 任何一条边界把 unknown 洗成 neutral 都让情绪腿在缺数据时静默错判方向。
//
// English: boundary table for SentimentPhase — freezing <=25 / neutral 26-75 /
// euphoria >=76 pinned at every edge, plus the hard rule that missing or out-of-range
// evidence yields ("unknown", false) and can never collapse into neutral.
package xasset

import "testing"

// TestSentimentPhaseBoundaryTable 边界表：0/24/25 冰点、26/50/75 中性、76/100 高潮。
func TestSentimentPhaseBoundaryTable(t *testing.T) {
	cases := []struct {
		value int
		phase string
	}{
		{0, "freezing"},   // 刻度下界：极度恐慌
		{24, "freezing"},  // 冰点内沿
		{25, "freezing"},  // 冰点上沿（含）
		{26, "neutral"},   // 中性下沿（含）
		{50, "neutral"},   // 中轴
		{75, "neutral"},   // 中性上沿（含）
		{76, "euphoria"},  // 高潮下沿（含）
		{100, "euphoria"}, // 刻度上界：极度贪婪
	}
	for _, tc := range cases {
		phase, ok := SentimentPhase(tc.value, true)
		if !ok || phase != tc.phase {
			t.Errorf("SentimentPhase(%d, true) = (%q, %v)，期望 (%q, true)", tc.value, phase, ok, tc.phase)
		}
	}
}

// TestSentimentPhaseNoEvidenceIsUnknown 无证据/坏证据必须 unknown 且 ok=false——
// 这是整条情绪腿的 fail-safe：宁可不判级，绝不把"没数据"读成"中性可操作"。
func TestSentimentPhaseNoEvidenceIsUnknown(t *testing.T) {
	bad := []struct {
		value       int
		hasEvidence bool
		why         string
	}{
		{0, false, "无证据即使值合法"},
		{50, false, "无证据即使值中性"},
		{78, false, "无证据即使值在高潮区"},
		{-1, true, "下越界视为无证据"},
		{101, true, "上越界视为无证据"},
		{-1000, true, "离谱负数"},
		{9999, true, "离谱正数"},
	}
	for _, tc := range bad {
		phase, ok := SentimentPhase(tc.value, tc.hasEvidence)
		if ok {
			t.Errorf("SentimentPhase(%d, %v)（%s）不应有 ok=true", tc.value, tc.hasEvidence, tc.why)
		}
		if phase != "unknown" {
			t.Errorf("SentimentPhase(%d, %v)（%s）应返回 unknown，实际 %q", tc.value, tc.hasEvidence, tc.why, phase)
		}
	}
}
