// middleware_branch_test.go — §AUDIT1019-M6（2026-10-21）核心包分支覆盖补强：
// internal/server 的**中间件链**与**错误信封**分支。
//
// 背景（为什么要补这些用例）：既有 admin_test.go 的 adminReq helper 是**直接向请求上下文
// 注入 user**，绕过了真实中间件；因此 authMiddleware 的令牌五态（缺失 / 空 Bearer / 篡改 /
// 未知令牌 / 裸令牌无前缀）、租户级频控 429、permMiddleware 的越权矩阵、muxWithJSONErrors
// 的 404 / 405 / 3xx 三分支，在单测层面一直是**零覆盖**——这些属安全关键面，此前只由集成层
// 字节级 UAT 从外部覆盖，跑不到分支内部，改坏了也不会红。本文件把这一层补上。
//
// 纪律：每条断言都对应一个「改坏即红」的具体分支，不写恒真的形态断言。
// English: branch-level tests for the auth/perm/admin middleware chain, tenant rate limiting,
// and the JSON error envelope — branches previously untested because the shared helper bypassed
// the real middleware by injecting the user straight into the request context.
package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quant-trading-v2/internal/auth"
)

// runAuthChain 用**真实** authMiddleware 包一个探针 handler 并打一枪，返回状态码、响应体、
// 中间件实际注入上下文的 user（nil 表示未放行）。
// English: runAuthChain fires one request through the real authMiddleware and reports the
// status, body, and the user the middleware injected (nil = rejected).
func runAuthChain(t *testing.T, s *Server, authzHeader string) (int, string, *auth.User, http.Header) {
	t.Helper()
	var got *auth.User
	h := s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		got = userFromContext(r)
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	req := httptest.NewRequest(http.MethodGet, "/api/m6probe", nil)
	if authzHeader != "" {
		req.Header.Set("Authorization", authzHeader)
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr.Code, rr.Body.String(), got, rr.Header()
}

// TestAuthMiddlewareTokenStates 认证中间件的令牌五态：缺失/空 Bearer/篡改/未知令牌必须 401，
// 合法令牌（带 Bearer 与裸令牌两种形态）必须 200 且真正注入上下文。
// English: the five token states of authMiddleware — four reject paths plus two accept forms.
func TestAuthMiddlewareTokenStates(t *testing.T) {
	s, _ := newAdminTestServer(t)
	m, err := s.auth.CreateUser("m6member", "pw", auth.RoleUser, nil, 0)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	cases := []struct {
		name    string
		header  string
		want    int
		wantMsg string
	}{
		{"缺失 Authorization 头", "", 401, "missing authorization token"},
		{"空 Bearer（只有前缀）", "Bearer ", 401, "invalid or expired token"},
		{"篡改令牌（尾部加字符）", "Bearer " + m.Token + "x", 401, "invalid or expired token"},
		{"未知令牌", "Bearer not-a-real-token", 401, "invalid or expired token"},
		{"合法令牌带 Bearer 前缀", "Bearer " + m.Token, 200, ""},
		{"合法裸令牌（无前缀，兼容分支）", m.Token, 200, ""},
	}
	for _, c := range cases {
		code, body, got, _ := runAuthChain(t, s, c.header)
		if code != c.want {
			t.Fatalf("%s: 期望 %d, got %d body=%s", c.name, c.want, code, body)
		}
		if code == 200 {
			// 放行必须真正把用户注入上下文，否则下游 handler 会拿到 nil 用户静默降级
			if got == nil || got.ID != m.ID {
				t.Fatalf("%s: 已放行但未注入正确的 user（got=%v）", c.name, got)
			}
		} else if !strings.Contains(body, c.wantMsg) {
			t.Fatalf("%s: 401 信封文案应为 %q, got %s", c.name, c.wantMsg, body)
		}
	}
}

// TestAuthMiddlewareTenantRateLimit429 租户级业务 API 频控：窗口内打满配额 → 429 且带
// Retry-After（§FIX-3：与匿名限流同口径，缺头会让前端无法判定重试窗口）。
// 手法：直接把限流器的命中序列预填到配额上限（同包可访问私有字段），避免真打 600 次。
// English: tenant quota exhausted ⇒ 429 with Retry-After; pre-fills the limiter's hit slice.
func TestAuthMiddlewareTenantRateLimit429(t *testing.T) {
	s, _ := newAdminTestServer(t)
	m, err := s.auth.CreateUser("m6rate", "pw", auth.RoleUser, nil, 0)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	tid := s.auth.TenantOf(m.ID)
	if tid == "" {
		t.Skip("该账号未归属租户，租户频控分支不适用")
	}
	rate := s.auth.TenantAPIRate(tid)
	now := time.Now()
	hits := make([]time.Time, 0, rate)
	for i := 0; i < rate; i++ {
		hits = append(hits, now)
	}
	s.tenantLimiter.hits = map[string][]time.Time{tid: hits}

	code, body, got, hdr := runAuthChain(t, s, "Bearer "+m.Token)
	if code != 429 {
		t.Fatalf("租户配额打满应 429, got %d body=%s（频控被绕过=安全面失守）", code, body)
	}
	if got != nil {
		t.Fatalf("被频控拒绝时不得注入 user（下游不该拿到身份）")
	}
	if hdr.Get("Retry-After") == "" {
		t.Fatalf("429 缺少 Retry-After 头（§FIX-3：前端据此判定重试窗口）")
	}
}

// runPermChain 用真实 permMiddleware 打一枪（内部含 authMiddleware，会真校验令牌）。
func runPermChain(s *Server, perm, token string) (int, string) {
	h := s.permMiddleware(perm, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	req := httptest.NewRequest(http.MethodGet, "/api/m6probe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr.Code, rr.Body.String()
}

// TestPermMiddlewareMatrix 权限位中间件三态越权矩阵：无权限位 403 / 有权限位 200 /
// 管理员隐式拥有全部权限位 200。另钉「错误信封必须点名是哪个权限位」（便于前端定位）。
// English: permission-bit matrix — missing bit 403, holding bit 200, admin implicit 200.
func TestPermMiddlewareMatrix(t *testing.T) {
	s, admin := newAdminTestServer(t)
	perm := auth.PermResearchApprove
	noPerm, err := s.auth.CreateUser("m6noperm", "pw", auth.RoleUser, nil, 0)
	if err != nil {
		t.Fatalf("create no-perm member: %v", err)
	}
	withPerm, err := s.auth.CreateUser("m6perm", "pw", auth.RoleUser, []string{perm}, 0)
	if err != nil {
		t.Fatalf("create perm member: %v", err)
	}

	if code, body := runPermChain(s, perm, noPerm.Token); code != 403 {
		t.Fatalf("无权限位成员应 403, got %d body=%s（越权放行=权限面失守）", code, body)
	} else if !strings.Contains(body, perm) {
		t.Fatalf("403 信封应点名权限位 %q, got %s", perm, body)
	}
	if code, body := runPermChain(s, perm, withPerm.Token); code != 200 {
		t.Fatalf("持有权限位成员应 200, got %d body=%s", code, body)
	}
	// 管理员隐式拥有全部权限位（HasPerm 对 admin 恒真），不得因未显式授权而 403
	if code, body := runPermChain(s, perm, admin.Token); code != 200 {
		t.Fatalf("管理员应隐式拥有权限位（200）, got %d body=%s", code, body)
	}
}

// runAdminChain 用真实 adminMiddleware 打一枪。
func runAdminChain(s *Server, token string) (int, string) {
	h := s.adminMiddleware(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	req := httptest.NewRequest(http.MethodGet, "/api/m6probe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr.Code, rr.Body.String()
}

// TestAdminMiddlewareBranches 管理员中间件：成员 403（且信封点名"仅管理员"）、管理员 200。
// 注：集成面（/api/action 等真实端点）由 h3_perms_test.go 覆盖，这里补的是中间件本体的分支。
func TestAdminMiddlewareBranches(t *testing.T) {
	s, admin := newAdminTestServer(t)
	m, err := s.auth.CreateUser("m6notadmin", "pw", auth.RoleUser, nil, 0)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if code, body := runAdminChain(s, m.Token); code != 403 {
		t.Fatalf("非管理员应 403, got %d body=%s", code, body)
	} else if !strings.Contains(body, "仅管理员") {
		t.Fatalf("403 信封应点名「仅管理员」, got %s", body)
	}
	if code, body := runAdminChain(s, admin.Token); code != 200 {
		t.Fatalf("管理员应 200, got %d body=%s", code, body)
	}
	// 无效令牌：adminMiddleware 内含 authMiddleware，未认证不得走到角色判定
	if code, _ := runAdminChain(s, "garbage-token"); code != 401 {
		t.Fatalf("非法令牌应 401（不得进入角色判定）, got %d", code)
	}
}

// TestMuxJSONErrorEnvelope §F3 错误信封三分支：未注册路径 404 JSON、方法不匹配 405 JSON、
// 规范化重定向 3xx 原样放行（不得被改写成 JSON）。命中注册路由时原样直通（流式零扰动）。
// English: §F3 envelope branches — 404/405 become JSON; canonical redirects stay 3xx.
func TestMuxJSONErrorEnvelope(t *testing.T) {
	s, _ := newAdminTestServer(t)
	h := s.chain(s.muxWithJSONErrors())
	do := func(method, path string) (int, string, http.Header) {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(method, path, nil))
		return rr.Code, rr.Body.String(), rr.Header()
	}

	// ① 未注册路径 → 404 JSON 信封（旧形态是 text/plain "404 page not found"）
	code, body, hdr := do(http.MethodGet, "/api/definitely-not-registered")
	if code != 404 {
		t.Fatalf("未注册路径应 404, got %d body=%s", code, body)
	}
	if ct := hdr.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("404 应是 JSON 信封（前端按 JSON 解析）, got Content-Type=%q body=%s", ct, body)
	}
	if !strings.Contains(body, "接口不存在") {
		t.Fatalf("404 信封文案应含「接口不存在」, got %s", body)
	}

	// ② 方法不匹配 → 405 JSON（/api/auth/login 只注册 POST）
	code, body, hdr = do(http.MethodGet, "/api/auth/login")
	if code != 405 {
		t.Fatalf("GET 一个只注册 POST 的端点应 405, got %d body=%s", code, body)
	}
	if ct := hdr.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("405 应是 JSON 信封, got Content-Type=%q body=%s", ct, body)
	}
	if !strings.Contains(body, "方法不允许") {
		t.Fatalf("405 信封文案应含「方法不允许」, got %s", body)
	}

	// ③ 路径规范化重定向（//api/health → /api/health）必须仍是 3xx，不许被改写成 JSON 信封
	code, _, _ = do(http.MethodGet, "//api/health")
	if code < 300 || code >= 400 {
		t.Fatalf("规范化重定向应放行 3xx（被改写成 JSON 信封会让前端拿到不可解析响应）, got %d", code)
	}
}

// TestIPLimiterSlidingWindow 滑动窗口限流器分支：未超限放行、超限拒绝、窗口过期后恢复；
// 并钉住「配额 <=0 即全拒」这一反直觉语义（len(recent) >= max 恒真）。
// English: sliding-window limiter branches, incl. the counter-intuitive "max<=0 rejects all".
func TestIPLimiterSlidingWindow(t *testing.T) {
	var l ipLimiter
	if !l.allow("1.2.3.4", 2, time.Minute) {
		t.Fatalf("首次请求应放行")
	}
	if !l.allow("1.2.3.4", 2, time.Minute) {
		t.Fatalf("窗口内第二次（配额 2）应放行")
	}
	if l.allow("1.2.3.4", 2, time.Minute) {
		t.Fatalf("窗口内第三次（配额 2）应拒绝")
	}
	// 窗口过期后计数归零（用极短窗口验证，避免用例依赖真实时间流逝）
	var l2 ipLimiter
	if !l2.allow("5.6.7.8", 1, 20*time.Millisecond) {
		t.Fatalf("首次应放行")
	}
	if l2.allow("5.6.7.8", 1, 20*time.Millisecond) {
		t.Fatalf("窗口内第二次（配额 1）应拒绝")
	}
	time.Sleep(40 * time.Millisecond)
	if !l2.allow("5.6.7.8", 1, 20*time.Millisecond) {
		t.Fatalf("窗口过期后应恢复放行（否则限流变成永久封禁）")
	}
	// 配额 <=0：语义是「全拒」而非「不限」，钉住以免有人"顺手改造成不限"
	var l3 ipLimiter
	if l3.allow("9.9.9.9", 0, time.Minute) {
		t.Fatalf("配额 0 应全拒（当前语义），改动前必须确认所有调用方传入的是 APIRateOr()")
	}
}
