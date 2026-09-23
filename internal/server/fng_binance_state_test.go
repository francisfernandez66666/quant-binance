// 文件职责：/api/binance/state 的 §ENH-A1 "fng" 节行为锁。钉两件事：
// ① 零配置零行为——未 SetFNGSource 时响应**不含** "fng" 键（前端旧版本零特判不受扰）；
// ② 注入后呈现缓存闭包读数：ok=true 带 value/classification/age_sec 三件，
//
//	ok=false 只带 ok=false——闭包报"无证据"时端点绝不代造中性值（无证据≠中性）。
//
// 控制器面用嵌入 nil 接口的假控制面（只覆盖 LiveRouter），路由为空白 BrokerRouter：
// 本用例锁的是 fng 节的有无与形状，不重复合并视图/健康度既有覆盖。
//
// English: behavior lock for the §ENH-A1 "fng" node of /api/binance/state — absent when
// SetFNGSource was never called (zero-config, zero-behavior), value/classification/age_sec
// when the injected closure reports evidence, and a bare {"ok":false} without any
// fabricated neutral otherwise.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"quant-trading-v2/internal/config"
	"quant-trading-v2/internal/trading"
)

// fngFakeCtrl 最小引擎控制面假件：嵌入大接口（未覆盖方法穿透即 panic，用例不触碰），
// 只实装 LiveRouter 让 handleBinanceState 走通 200 路径。
type fngFakeCtrl struct {
	EngineController
	router *trading.BrokerRouter
}

func (f fngFakeCtrl) LiveRouter() *trading.BrokerRouter { return f.router }

// newFNGStateServer 拼一个能过 handleBinanceState 最小 200 路径的服务：
// config.Manager 的 Rules 是指针字段（零值 nil 会让 GetBinanceConfigFor 解引用崩），
// 故手工给一个出厂空 Rules；store 留 nil → 账号级 KV 路径整体跳过，端点纯只读。
func newFNGStateServer() *Server {
	return &Server{cfg: &config.Manager{Rules: &config.Rules{}}}
}

// callBinanceState 直调 handler 并解出 JSON map。
func callBinanceState(t *testing.T, s *Server) map[string]any {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleBinanceState(rr, httptest.NewRequest(http.MethodGet, "/api/binance/state", nil))
	if rr.Code != 200 {
		t.Fatalf("期望 200，实际 %d body=%s", rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v %s", err, rr.Body.String())
	}
	return out
}

// TestBinanceStateFNGAbsentWithoutSource 未注入闭包：响应必须没有 fng 键（零配置零行为）。
func TestBinanceStateFNGAbsentWithoutSource(t *testing.T) {
	s := newFNGStateServer()
	s.SetEngineController(fngFakeCtrl{router: trading.NewBrokerRouter(nil)})
	out := callBinanceState(t, s)
	if _, exists := out["fng"]; exists {
		t.Fatalf("未注入 FNGSource 不应出现 fng 节: %v", out["fng"])
	}
}

// TestBinanceStateFNGSection 注入闭包：有证据带三件套、无证据只报 ok=false。
func TestBinanceStateFNGSection(t *testing.T) {
	s := newFNGStateServer()
	s.SetEngineController(fngFakeCtrl{router: trading.NewBrokerRouter(nil)})

	s.SetFNGSource(func() (int, string, int64, bool) { return 78, "Extreme Greed", 42, true })
	out := callBinanceState(t, s)
	node, ok := out["fng"].(map[string]any)
	if !ok {
		t.Fatalf("fng 节应为对象: %v", out["fng"])
	}
	if node["ok"] != true || node["value"] != float64(78) ||
		node["classification"] != "Extreme Greed" || node["age_sec"] != float64(42) {
		t.Fatalf("fng 节形状错: %v", node)
	}

	// 切到无证据分支：只许报 ok=false，不许出现 value（端点层同样"无证据≠中性"）。
	s.SetFNGSource(func() (int, string, int64, bool) { return 0, "", 0, false })
	out = callBinanceState(t, s)
	node = out["fng"].(map[string]any)
	if node["ok"] != false || len(node) != 1 {
		t.Fatalf("无证据 fng 节应只含 ok=false: %v", node)
	}
}
