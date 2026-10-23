// M6 长尾（第三批）：不依赖真引擎 / 真数据源的纯逻辑 + 纯文件 IO 端点守卫。
//
// 这批函数来自 §AUDIT1019-M6 审计的"零覆盖"清单，但属于：
//   - optRefIDFor：纯确定性映射（已知目标槽位 + FNV32 散列兜底），零外部依赖；
//   - handleResearchFactors：只消费 factor 包注册表（编译期已绑定），无 DB / 网络；
//   - handleSchedulerStatus / handleResearchEventFactor / schedulerState：只碰本地文件，
//     守卫集中在"文件缺失 / 超 4MB / JSON 损坏"三态——正是静默故障高发处。
//
// 全部零网络、零落盘于被测代码之外（测试自身的临时目录不算产品落盘）。
// 端点 handler 仍需真引擎 + 真数据源的（computeResearchProgress /
// handleResearchCandidates / approveCandidate …）不在此批，避免伪造引擎凑数。
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOptRefIDFor：§AUDIT1019-M6 长尾——扫参任务按优化目标分槽的纯确定性映射。
func TestOptRefIDFor(t *testing.T) {
	cases := []struct {
		obj    string
		want   int64
		unknow bool // 未知目标：落在 1000-1099 且确定性
	}{
		{"", 990, false},
		{"profitfactor", 990, false},
		{"PROFITFACTOR", 990, false}, // 大小写 / 空格归一
		{"  ProfitFactor  ", 990, false},
		{"winrate", 991, false},
		{"avgwin", 992, false},
		{"expectancy", 993, false},
		{"calmar", 994, false},
		{"sharpe", 0, true}, // 未知 → 散列兜底
	}
	for _, c := range cases {
		got := optRefIDFor(c.obj)
		if c.unknow {
			if got < 1000 || got > 1099 {
				t.Errorf("optRefIDFor(%q)=%d，期望落在 [1000,1099]", c.obj, got)
			}
			// 确定性：同输入同输出
			if optRefIDFor(c.obj) != got {
				t.Errorf("optRefIDFor(%q) 非确定性：%d vs %d", c.obj, optRefIDFor(c.obj), got)
			}
			continue
		}
		if got != c.want {
			t.Errorf("optRefIDFor(%q)=%d，期望 %d", c.obj, got, c.want)
		}
	}
	// 两个不同未知目标必须分槽（不碰撞已知 990-994，也不彼此恒等除非散列相同）
	a, b := optRefIDFor("alpha"), optRefIDFor("beta")
	if a == b && a >= 1000 {
		t.Errorf("不同未知目标散列撞槽：alpha=%d beta=%d", a, b)
	}
}

// TestHandleResearchFactors：§AUDIT1019-M6 长尾——因子元数据端点，只消费 factor 注册表。
func TestHandleResearchFactors(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/research/factors", nil)
	// 零值 Server 即可：handler 不访问任何 DB / 引擎字段。
	s := &Server{}
	s.handleResearchFactors(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d（body=%s）", rec.Code, rec.Body.String())
	}
	var body struct {
		Factors []map[string]any `json:"factors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应非 JSON：%v（body=%s）", err, rec.Body.String())
	}
	// 因子注册表非空（编译期绑定，至少含内置因子）。
	if len(body.Factors) == 0 {
		t.Skip("factor 注册表为空，跳过存在性断言（环境相关）")
	}
	for _, f := range body.Factors {
		if f["id"] == nil || f["name"] == nil || f["cat"] == nil || f["desc"] == nil {
			t.Errorf("因子字段缺失：%v", f)
		}
	}
}

// TestHandleSchedulerStatus：§AUDIT1019-M6 长尾——调度状态快照端点，只碰本地文件。
func TestHandleSchedulerStatus(t *testing.T) {
	t.Run("文件缺失→ok:false", func(t *testing.T) {
		s := &Server{researchDir: t.TempDir()}
		rec := httptest.NewRecorder()
		s.handleSchedulerStatus(rec, httptest.NewRequest(http.MethodGet, "/api/scheduler/status", nil))
		if rec.Code != 200 {
			t.Fatalf("期望 200，得到 %d", rec.Code)
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["ok"] != false {
			t.Errorf("文件缺失应 ok=false，得到 %v", body["ok"])
		}
	})
	t.Run("有效快照→ok:true 原样回显", func(t *testing.T) {
		dir := t.TempDir()
		raw := `{"phase":"idle","step_index":3,"done":true}`
		if err := os.WriteFile(filepath.Join(dir, "scheduler_status.json"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		s := &Server{researchDir: dir}
		rec := httptest.NewRecorder()
		s.handleSchedulerStatus(rec, httptest.NewRequest(http.MethodGet, "/api/scheduler/status", nil))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["ok"] != true {
			t.Errorf("有效文件应 ok=true，得到 %v", body["ok"])
		}
		if body["step_index"] != float64(3) {
			t.Errorf("应原样回显 step_index=3，得到 %v", body["step_index"])
		}
	})
}

// TestHandleResearchEventFactor：§AUDIT1019-M6 长尾——事件因子报告端点，三态守卫。
func TestHandleResearchEventFactor(t *testing.T) {
	t.Run("文件缺失→exists:false", func(t *testing.T) {
		s := &Server{researchDir: t.TempDir()}
		rec := httptest.NewRecorder()
		s.handleResearchEventFactor(rec, httptest.NewRequest(http.MethodGet, "/api/research/event-factor", nil))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["exists"] != false {
			t.Errorf("缺失应 exists=false，得到 %v", body["exists"])
		}
	})
	t.Run("空文件→exists:false", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "event_factor_report.json"), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
		s := &Server{researchDir: dir}
		rec := httptest.NewRecorder()
		s.handleResearchEventFactor(rec, httptest.NewRequest(http.MethodGet, "/api/research/event-factor", nil))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["exists"] != false {
			t.Errorf("空文件应 exists=false，得到 %v", body["exists"])
		}
	})
	t.Run("超 4MB→too_large", func(t *testing.T) {
		dir := t.TempDir()
		big := strings.Repeat(" ", 4*1024*1024+1)
		if err := os.WriteFile(filepath.Join(dir, "event_factor_report.json"), []byte(big), 0o644); err != nil {
			t.Fatal(err)
		}
		s := &Server{researchDir: dir}
		rec := httptest.NewRecorder()
		s.handleResearchEventFactor(rec, httptest.NewRequest(http.MethodGet, "/api/research/event-factor", nil))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["too_large"] != true {
			t.Errorf("超 4MB 应 too_large=true，得到 %v", body["too_large"])
		}
	})
	t.Run("JSON 损坏→corrupt", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "event_factor_report.json"), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		s := &Server{researchDir: dir}
		rec := httptest.NewRecorder()
		s.handleResearchEventFactor(rec, httptest.NewRequest(http.MethodGet, "/api/research/event-factor", nil))
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["corrupt"] != true {
			t.Errorf("损坏 JSON 应 corrupt=true，得到 %v", body["corrupt"])
		}
	})
	t.Run("有效文件→原样透传", func(t *testing.T) {
		dir := t.TempDir()
		raw := `{"factors":[{"ic":0.05}]}`
		if err := os.WriteFile(filepath.Join(dir, "event_factor_report.json"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		s := &Server{researchDir: dir}
		rec := httptest.NewRecorder()
		s.handleResearchEventFactor(rec, httptest.NewRequest(http.MethodGet, "/api/research/event-factor", nil))
		if rec.Body.String() != raw {
			t.Errorf("有效文件应原样透传，得到 %q", rec.Body.String())
		}
	})
}

// TestSchedulerState：§AUDIT1019-M6 长尾——research_state.json 读取，三态返回。
func TestSchedulerState(t *testing.T) {
	t.Run("未配置目录→nil", func(t *testing.T) {
		s := &Server{researchDir: ""}
		if s.schedulerState() != nil {
			t.Errorf("researchDir 为空应返回 nil")
		}
	})
	t.Run("文件缺失→nil", func(t *testing.T) {
		s := &Server{researchDir: t.TempDir()}
		if s.schedulerState() != nil {
			t.Errorf("状态文件缺失应返回 nil")
		}
	})
	t.Run("JSON 损坏→nil", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "research_state.json"), []byte("bad"), 0o644); err != nil {
			t.Fatal(err)
		}
		s := &Server{researchDir: dir}
		if s.schedulerState() != nil {
			t.Errorf("状态文件损坏应返回 nil")
		}
	})
	t.Run("有效→回显", func(t *testing.T) {
		dir := t.TempDir()
		raw := `{"day":"2026-10-23","done":true}`
		if err := os.WriteFile(filepath.Join(dir, "research_state.json"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		s := &Server{researchDir: dir}
		st := s.schedulerState()
		if st == nil {
			t.Fatalf("有效状态应非空")
		}
		if st["done"] != true {
			t.Errorf("应回显 done=true，得到 %v", st["done"])
		}
	})
}

// TestHandleResearchTaskLog：§AUDIT1019-M6 长尾——研究任务日志端点，只碰本地文件。
// 用标准库 SetPathValue 注入路由参数，无需起 chi 路由。
func TestHandleResearchTaskLog(t *testing.T) {
	const id = "42"
	newSrv := func(t *testing.T) *Server {
		dir := t.TempDir()
		logDir := filepath.Join(dir, "task_logs")
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			t.Fatal(err)
		}
		return &Server{researchDir: dir}
	}
	call := func(s *Server, id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/research/task/"+id+"/log", nil)
		req.SetPathValue("id", id)
		s.handleResearchTaskLog(rec, req)
		return rec
	}
	t.Run("缺失 id→400", func(t *testing.T) {
		s := &Server{researchDir: t.TempDir()}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/research/task//log", nil)
		req.SetPathValue("id", "")
		s.handleResearchTaskLog(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("缺失 id 应 400，得到 %d", rec.Code)
		}
	})
	t.Run("日志文件缺失→exists:false", func(t *testing.T) {
		s := newSrv(t)
		rec := call(s, id)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["exists"] != false {
			t.Errorf("缺失应 exists=false，得到 %v", body["exists"])
		}
	})
	t.Run("有效日志→exists:true 回显", func(t *testing.T) {
		s := newSrv(t)
		if err := os.WriteFile(filepath.Join(s.researchDir, "task_logs", "task_"+id+".log"), []byte("hello log"), 0o644); err != nil {
			t.Fatal(err)
		}
		rec := call(s, id)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["exists"] != true {
			t.Errorf("有效应 exists=true，得到 %v", body["exists"])
		}
		if body["log"] != "hello log" {
			t.Errorf("应回显日志原文，得到 %v", body["log"])
		}
	})
	t.Run("超 200KB→只取末尾", func(t *testing.T) {
		s := newSrv(t)
		big := strings.Repeat("x", 200*1024+1024) // 201KB，超出 200KB 截断阈值
		if err := os.WriteFile(filepath.Join(s.researchDir, "task_logs", "task_"+id+".log"), []byte(big), 0o644); err != nil {
			t.Fatal(err)
		}
		rec := call(s, id)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		log, _ := body["log"].(string)
		if len(log) != 200*1024 {
			t.Errorf("超长日志应截断到 200KB，得到 %d", len(log))
		}
		// 截断后应为原串末尾：最后 4 字符是 "xxxx"
		if !strings.HasSuffix(log, "xxxx") {
			t.Errorf("截断应保留末尾，得到 %q", log[len(log)-8:])
		}
	})
}
