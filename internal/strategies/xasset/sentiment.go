// 文件职责：XASSET 加密情绪腿的**相位判级纯函数**（§ENH-A1）——把外部恐慌贪婪指数
// （alternative.me FNG，0-100 整数刻度）折算成战法包可用的三档情绪相位：
// 冰点 freezing（≤25，极度恐慌）/ 中性 neutral（26-75）/ 高潮 euphoria（≥76，极度贪婪）。
//
// 设计纪律（本文件的全部要点都在这两条上）：
//  1. **无证据≠中性**：hasEvidence=false（数据源未接入/缓存为空/证据超龄）一律返回
//     ("unknown", false)，绝不舍入到中性档——与仓内 §9 market_halt 闸"无证据≠可交易"
//     同姿势：缺数据时把情绪读成"温和"会让逆势腿在真冰点里接飞刀，也让追涨腿在
//     真高潮里裸奔，两个方向都是静默错判；
//  2. 纯函数零状态：不持缓存、不发 HTTP、不碰时钟——采样与缓存龄归 data.FNGClient
//     （internal/data/fng.go），本函数只做阈值几何；value 越界（上游/调用方给坏数）
//     视同无证据，宁 unknown 不猜档。
//
// English: pure threshold function mapping a Fear & Greed value (0-100) to the three
// sentiment phases used by the XASSET crypto leg — freezing (<=25) / neutral (26-75) /
// euphoria (>=76). Missing or invalid evidence returns ("unknown", false) instead of
// silently collapsing to neutral, mirroring the §9 market-halt "no evidence is not a
// license to trade" posture. Sampling and cache freshness live in data.FNGClient.
package xasset

// 情绪相位常量（下游战法/风控按字符串比对，golden 输出用同名值）。
const (
	SentimentPhaseFreezing = "freezing" // 冰点：value ≤ 25（恐慌盘出清区）
	SentimentPhaseNeutral  = "neutral"  // 中性：26 ≤ value ≤ 75（无情绪边可乘）
	SentimentPhaseEuphoria = "euphoria" // 高潮：value ≥ 76（贪婪过热区）
	SentimentPhaseUnknown  = "unknown"  // 无证据/坏数：拒绝判级（ok=false）
)

// 相位切分阈值（FNG 官方口径的 Extreme Fear / Extreme Greed 边界取整）。
const (
	sentimentFreezingMax = 25  // 冰点上沿（含）
	sentimentEuphoriaMin = 76  // 高潮下沿（含）
	sentimentValueMin    = 0   // 刻度下界，越界视为无证据
	sentimentValueMax    = 100 // 刻度上界，越界视为无证据
)

// SentimentPhase 恐慌贪婪值 → 情绪相位。hasEvidence=false 或 value 越界 0-100 时
// 返回 ("unknown", false)——调用方必须消费 ok：false 分支不允许回退成 neutral 使用。
func SentimentPhase(value int, hasEvidence bool) (phase string, ok bool) {
	// 证据闸在前：没有证据（或证据本身是坏数）就没有相位可言。
	if !hasEvidence || value < sentimentValueMin || value > sentimentValueMax {
		return SentimentPhaseUnknown, false
	}
	switch {
	case value <= sentimentFreezingMax: // 0-25：冰点
		return SentimentPhaseFreezing, true
	case value >= sentimentEuphoriaMin: // 76-100：高潮
		return SentimentPhaseEuphoria, true
	default: // 26-75：中性（唯一"有证据的中性"路径）
		return SentimentPhaseNeutral, true
	}
}
