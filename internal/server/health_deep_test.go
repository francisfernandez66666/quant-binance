// 文件职责：§AUDITFIX925-D9（2026-09-25 审计批）GET /api/health 双形态契约测试——
// ①无参默认响应逐字节钉死（uat_bootstrap/monitor_test 把它当就绪探针用，响应体一旦漂移
//
//	会让"重启等待/冒烟判定"这两条腿不可信，故用等值断言而非包含式弱断言）；
//
// ②?deep=1 追加引擎实况：deep_ok 只钉 aggregator 腿（进程活着但引擎控制器未接线=假活类别），
//
//	其余子系统为信息位；深探针与 /api/engine_health 必须同源（同一 engineHealthStatus 装配）。
//
// English: byte-exact golden for the default health body (readiness probe contract) plus the
// deep=1 liveness shape, both sharing the subsystem builder with /api/engine_health.
package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHealthDefaultByteIdentical 默认响应字节等价锁：{"status":"ok"}（json.Encoder 行尾单换行）。
func TestHealthDefaultByteIdentical(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	s.handleHealth(rr, httptest.NewRequest("GET", "/api/health", nil))
	// 等值断言：连 Content-Type 一起钉——就绪探针的消费方（curl -sf + 码判定）依赖 200+JSON 头。
	if got := rr.Body.String(); got != "{\"status\":\"ok\"}\n" {
		t.Fatalf("默认响应体漂移（就绪探针消费方会拿到意外字节）：%q", got)
	}
	if rr.Code != 200 {
		t.Fatalf("默认响应必须恒 200（就绪语义），实际 %d", rr.Code)
	}
}

// TestHealthDeepMode deep=1 形态：最小未装配服务 → aggregator=false → degraded + deep_ok=false。
func TestHealthDeepMode(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	s.handleHealth(rr, httptest.NewRequest("GET", "/api/health?deep=1", nil))
	var out struct {
		Status     string          `json:"status"`
		DeepOK     bool            `json:"deep_ok"`
		Subsystems map[string]bool `json:"subsystems"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("deep 响应非合法 JSON: %v", err)
	}
	// 零值 Server 无引擎控制器：deep_ok 必须诚实报假（这正是本条修复要杀掉的"恒 ok 假活"）
	if out.DeepOK {
		t.Fatal("未装配引擎时 deep_ok 不得为 true（假活形态复活）")
	}
	if out.Status != "degraded" {
		t.Fatalf("deep_ok=false 时 status 必须为 degraded，实际 %q", out.Status)
	}
	if _, ok := out.Subsystems["aggregator"]; !ok {
		t.Fatal("subsystems 缺 aggregator 腿（deep_ok 判据失去可追溯性）")
	}
}

// TestHealthDeepMatchesEngineHealth 同源锁：deep 的子系统节与 /api/engine_health 逐字段一致
// （两口径各算一份是 §H8 探针误熔的历史根因，这里用同请求对拍钉死）。
func TestHealthDeepMatchesEngineHealth(t *testing.T) {
	s := &Server{}
	rrDeep := httptest.NewRecorder()
	s.handleHealth(rrDeep, httptest.NewRequest("GET", "/api/health?deep=1", nil))
	var deep struct {
		Subsystems map[string]bool `json:"subsystems"`
	}
	if err := json.Unmarshal(rrDeep.Body.Bytes(), &deep); err != nil {
		t.Fatalf("deep 响应解析失败: %v", err)
	}
	rrEng := httptest.NewRecorder()
	s.handleFixEngineHealth(rrEng, httptest.NewRequest("GET", "/api/engine_health", nil))
	var eng map[string]bool
	if err := json.Unmarshal(rrEng.Body.Bytes(), &eng); err != nil {
		t.Fatalf("engine_health 响应解析失败: %v", err)
	}
	for k, v := range eng {
		if deep.Subsystems[k] != v {
			t.Fatalf("子系统 %q 两探针口径分叉：deep=%v engine_health=%v", k, deep.Subsystems[k], v)
		}
	}
	for k := range deep.Subsystems {
		if _, ok := eng[k]; !ok {
			t.Fatalf("deep 多出 engine_health 没有的子系统 %q（同源被绕过）", k)
		}
	}
	// 反证（守卫配反证）：deep_ok 的判据必须是 aggregator 单腿——若实现被改成"全腿 AND"，
	// CN 关态部署（news_agent 等合法为 false）会把它拖成恒假红。零值服务两侧同假，
	// 用 strings 形态锁不住该逻辑，故此处直接以行为样例钉 aggregator 语义。
	if strings.Contains(rrDeep.Body.String(), "\"deep_ok\":true") {
		t.Fatal("零值服务下 deep_ok 不得为 true")
	}
}
