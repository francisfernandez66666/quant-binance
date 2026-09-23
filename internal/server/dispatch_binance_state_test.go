// dispatch_binance_state_test.go §战法批-5 观测面行为锁：/api/binance/state 的 "dispatch" 节
// ① 未 SetDispatchSource → 响应不含 "dispatch" 键（零配置零行为，同 fng/events 惯例）；
// ② 注入后按（账号,市场）取数：有摘要的市场呈 {ok:true, report:{…}}，
//
//	从未派发过的市场只呈 {"ok":false}——无记录≠在派发，不代造零值报告；
//
// ③ 闭包收到的 userID 与请求账号一致（多账号各看各的派发台）。
// English: locks for the §strategy-batch-5 "dispatch" node — absent without the closure,
// per-(account, market) report shape, and no fabricated zero-report for unticked markets.
package server

import (
	"testing"

	"quant-trading-v2/internal/trading"
)

// TestBinanceStateDispatchAbsentWithoutSource 未注入：整节省略。
func TestBinanceStateDispatchAbsentWithoutSource(t *testing.T) {
	s := newFNGStateServer()
	s.SetEngineController(fngFakeCtrl{router: trading.NewBrokerRouter(nil)})
	out := callBinanceState(t, s)
	if _, exists := out["dispatch"]; exists {
		t.Fatalf("未注入 DispatchSource 不应出现 dispatch 节: %v", out["dispatch"])
	}
}

// TestBinanceStateDispatchSection 注入后两市场两态：US 有最近一轮摘要、CRYPTO 从未跑过。
func TestBinanceStateDispatchSection(t *testing.T) {
	s := newFNGStateServer()
	s.SetEngineController(fngFakeCtrl{router: trading.NewBrokerRouter(nil)})
	var seenUID, lastUID string
	var calls int
	s.SetDispatchSource(func(uid, market string) (any, bool) {
		lastUID = seenUID
		seenUID = uid
		if calls > 0 && lastUID != seenUID {
			t.Errorf("两市场闭包收到的账号不一致: %q vs %q", lastUID, seenUID)
		}
		calls++
		if market != "US" {
			return nil, false // 与 engine.DispatchReport 同 json 形状的 map（server 不依赖 engine 类型）
		}
		return map[string]any{
			"at": "2026-09-23T22:00:00Z", "universe": 4, "signals": 2,
			"placed": 1, "rejected": 0, "exit_placed": 1, "desk": "paper",
		}, true
	})
	out := callBinanceState(t, s)
	node, ok := out["dispatch"].(map[string]any)
	if !ok || len(node) != 2 {
		t.Fatalf("dispatch 节应为 US/CRYPTO 两市场对象: %v", out["dispatch"])
	}
	us, ok := node["US"].(map[string]any)
	if !ok || us["ok"] != true {
		t.Fatalf("US 节应 ok=true: %v", node["US"])
	}
	rep, ok := us["report"].(map[string]any)
	if !ok || rep["desk"] != "paper" || rep["placed"] != float64(1) || rep["exit_placed"] != float64(1) {
		t.Fatalf("US report 字段透传错: %v", us["report"])
	}
	cp, ok := node["CRYPTO"].(map[string]any)
	if !ok || cp["ok"] != false || len(cp) != 1 {
		t.Fatalf("CRYPTO 未派发节应只含 ok=false（不代造零值报告）: %v", cp)
	}
	if calls != 2 {
		t.Fatalf("dispatch 节应对 US/CRYPTO 各调一次闭包，实际 %d 次", calls)
	}
}
