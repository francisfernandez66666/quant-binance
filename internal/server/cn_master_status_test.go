// 文件职责：§CN-MASTER A股总开关的 /api/status 展示锁（两条）：
//  1. 缺省（未注入 SetCNMaster）→ cn_master=false —— 与配置出厂"默认关闭"口径一致；
//  2. SetCNMaster(true) → cn_master=true —— 开回旧行为时前端导航如实全显。
//
// 该字段只承载展示（前端导航隐藏），不参与后端判定；装配门的正锁在 main.go 启动路径，
// 属进程级布线，无法在包内单测覆盖，故此处锁住"快照→下发"的契约段。
// English: §CN-MASTER locks — /api/status carries the boot-frozen CN flag both ways.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"quant-trading-v2/internal/display"
)

func callStatusAPI(t *testing.T, s *Server) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	s.handleFixStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态端点应 200，实际 %d, body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	return out
}

func TestStatusCNMasterDefaultsOff(t *testing.T) {
	s := &Server{agg: display.New()}
	got := callStatusAPI(t, s)["cn_master"]
	if got != false {
		t.Fatalf("未注入时 cn_master 应为 false（出厂默认关闭），实际 %#v", got)
	}
}

func TestStatusCNMasterReflectsSet(t *testing.T) {
	s := &Server{agg: display.New()}
	s.SetCNMaster(true)
	if got := callStatusAPI(t, s)["cn_master"]; got != true {
		t.Fatalf("SetCNMaster(true) 后 cn_master 应为 true，实际 %#v", got)
	}
}
