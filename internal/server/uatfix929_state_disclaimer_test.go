// uatfix929_state_disclaimer_test.go §UATFIX929-C 行为锁：/api/binance/state 顶层披露签署键。
// 背景（09-29 全量 UAT 审计缺陷 C）：前端状态卡 BinanceStatusCard.jsx:126 从 state 载荷读
// disclaimer_signed_at，而该键此前只由 config 面（binanceConfigView）发出——state 越正常、
// 卡片越恒显「未签署」（坏得越对）。修复＝state 与 config 同源补发顶层键。
// 本锁两腿：① state 键存在且等于 cfg 字段真值（含空串态——空串也要发键，前端据此渲「未签署」）；
// ② 两面逐字节一致（state 顶层 == binanceConfigView 同键），堵住未来任何一侧单独改口径的漂移。
// English: locks the §UATFIX929-C fix — state must carry disclaimer_signed_at at top level,
// byte-identical to the config view, including the empty-string (unsigned) case.
package server

import (
	"testing"

	"quant-trading-v2/internal/config"
	"quant-trading-v2/internal/trading"
)

// newDisclaimerStateServer 组装一个带披露签署时间的 state 测试服务（复用 fng 状态测试骨架）。
func newDisclaimerStateServer(signedAt string) *Server {
	rules := &config.Rules{}
	rules.Binance.DisclaimerSignedAt = signedAt
	s := &Server{cfg: &config.Manager{Rules: rules}}
	s.SetEngineController(fngFakeCtrl{router: trading.NewBrokerRouter(nil)})
	return s
}

// TestUATFIX929StateDisclaimerSignedPresent 签署态：state 顶层键存在且等于配置真值。
func TestUATFIX929StateDisclaimerSignedPresent(t *testing.T) {
	const stamp = "2026-09-20T12:00:00Z"
	s := newDisclaimerStateServer(stamp)
	out := callBinanceState(t, s)
	got, exists := out["disclaimer_signed_at"]
	if !exists {
		t.Fatalf("state 必须顶层发 disclaimer_signed_at 键（前端卡 :126 消费）: %v", out)
	}
	if got != stamp {
		t.Fatalf("state 披露签署值与配置真值不一致: 期望 %q 实得 %v", stamp, got)
	}
	// 第二腿：config 面同键逐字节一致（两面同源，任何一侧改口径必红）。
	view := binanceConfigView(s.cfg.GetBinanceConfigFor(""))
	if view["disclaimer_signed_at"] != got {
		t.Fatalf("state 与 config 两面披露签署键漂移: state=%v config=%v", got, view["disclaimer_signed_at"])
	}
}

// TestUATFIX929StateDisclaimerUnsignedEmptyStillEmitted 未签署态：空串也要发键——
// 前端渲染以「键值真伪」判「未签署」，缺键与空串在前端同形，但缺键会把「state 发没发这个字段」
// 的契约问题伪装成「未签署」的业务事实（正是缺陷 C 的成因形态），故锁「必发键」。
func TestUATFIX929StateDisclaimerUnsignedEmptyStillEmitted(t *testing.T) {
	s := newDisclaimerStateServer("")
	out := callBinanceState(t, s)
	got, exists := out["disclaimer_signed_at"]
	if !exists {
		t.Fatalf("未签署态也必须发键（空串），不得缺键: %v", out)
	}
	if got != "" {
		t.Fatalf("未签署态键值必须为空串，实得 %v", got)
	}
	view := binanceConfigView(s.cfg.GetBinanceConfigFor(""))
	if view["disclaimer_signed_at"] != got {
		t.Fatalf("空串态两面仍须一致: state=%v config=%v", got, view["disclaimer_signed_at"])
	}
}
