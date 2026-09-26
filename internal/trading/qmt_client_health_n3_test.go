// qmt_client_health_n3_test.go — §N3（2026-09-26 全量审计）/health 403 可读性收口。
//
// 缺陷原文：网关 /health 只对回环与 ALLOWED_IPS 白名单放行（qmt_gateway/gateway.py _ip_allowed），
// 远程决策机忘配白名单时每条探测都吃 403；旧实现把 403 与"连不上/网关真死"混成同一句
// "gateway GET /health: HTTP 403: health endpoint is localhost-only"，熔断状态机
// （controller.HealthCheck）照旧计失败，但告警文案指错了排查方向——运维往"链路断了"查，
// 实际是配置漏项。
//
// 为何这样修：只给错误文本加一层「网关 403：疑似 ALLOWED_IPS 未包含本机出口 IP」说明，
// 判定语义逐字节不变（err 仍非 nil、Health 仍重探一次、熔断仍按探测失败开窗计时）。
// 本文件三条用例正是把这两件事分开钉住：① 文案必须含指定短语且保留原文；
// ② 非 403（500/网络层失败）不得被安上 403 说法；③ 成功路径零变化（ok=true 无误报）。
//
// English: a gateway 403 on /health is annotated with the likely ALLOWED_IPS misconfiguration
// while the failure semantics (non-nil error, one re-probe, breaker accounting) stay unchanged.
package trading

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// healthStatusStub 最小网关桩：/health 固定回某个状态码 + 一段与真网关一致的 403 文本。
type healthStatusStub struct {
	status   int
	body     string
	attempts int
}

func (s *healthStatusStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/health" {
		http.NotFound(w, r)
		return
	}
	s.attempts++
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(s.status)
	w.Write([]byte(s.body))
}

// TestQMTHealthForbiddenNamesAllowedIPs 锁 §N3 文案：403 必须点名 ALLOWED_IPS，
// 且原始错误文本（含 "HTTP 403"）必须保留（错误包装不吞旧信息）。
func TestQMTHealthForbiddenNamesAllowedIPs(t *testing.T) {
	stub := &healthStatusStub{status: http.StatusForbidden, body: `{"error":"health endpoint is localhost-only"}`}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c := NewQMTClient(srv.URL, "tk", 2*time.Second, 0)
	ok, err := c.Health()
	if ok {
		t.Fatalf("403 必须判为不健康（判定语义不得因文案改动而变），got ok=true")
	}
	if err == nil {
		t.Fatalf("403 必须返回非 nil error（熔断按 err!=nil 计探测失败）")
	}
	msg := err.Error()
	if !strings.Contains(msg, "网关 403：疑似 ALLOWED_IPS 未包含本机出口 IP") {
		t.Fatalf("错误文本缺少 403 归因说明, got=%q", msg)
	}
	if !strings.Contains(msg, "HTTP 403") {
		t.Fatalf("错误文本丢失原始状态码信息, got=%q", msg)
	}
	if !strings.Contains(msg, "ALLOWED_IPS") {
		t.Fatalf("错误文本应指路 ALLOWED_IPS 配置项, got=%q", msg)
	}
	// 状态码仍以可程序化方式可得（errors.As → gatewayHTTPError），供未来按码分类而不靠字符串
	var he *gatewayHTTPError
	if !errors.As(err, &he) || he.Status() != http.StatusForbidden {
		t.Fatalf("errors.As 应能取到 403 状态码载体, got=%v", err)
	}
	// Health 的"失败自动重探一次"语义不变：两次都要打到网关
	if stub.attempts != 2 {
		t.Fatalf("Health 应在首次失败后重探一次（共 2 次），got attempts=%d", stub.attempts)
	}
}

// TestQMTHealthNonForbiddenKeepsPlainError 其它状态码不得被安上 403 说法（不误报根因）。
func TestQMTHealthNonForbiddenKeepsPlainError(t *testing.T) {
	stub := &healthStatusStub{status: http.StatusInternalServerError, body: `{"error":"broker down"}`}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c := NewQMTClient(srv.URL, "tk", 2*time.Second, 0)
	ok, err := c.Health()
	if ok || err == nil {
		t.Fatalf("500 应判失败并带回错误, ok=%v err=%v", ok, err)
	}
	if strings.Contains(err.Error(), "ALLOWED_IPS") {
		t.Fatalf("500 不得误报为白名单问题, got=%q", err.Error())
	}
	var he *gatewayHTTPError
	if !errors.As(err, &he) || he.Status() != http.StatusInternalServerError {
		t.Fatalf("500 的状态码载体丢失, got=%v", err)
	}
}

// TestQMTHealthSuccessUnchanged 成功路径逐字节不变：200 + ok/broker_connected 为真时
// 无错误、无重探、也没有任何新文案混进来。
func TestQMTHealthSuccessUnchanged(t *testing.T) {
	stub := &healthStatusStub{status: http.StatusOK, body: `{"ok":true,"ts":"t","broker_connected":true}`}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c := NewQMTClient(srv.URL, "tk", 2*time.Second, 0)
	ok, err := c.Health()
	if err != nil || !ok {
		t.Fatalf("健康网关应返回 (true, nil)，got ok=%v err=%v", ok, err)
	}
	if stub.attempts != 1 {
		t.Fatalf("成功不得重探, got attempts=%d", stub.attempts)
	}
}

// TestQMTHealthNetworkErrorNotAnnotatedAsForbidden 网络层失败（连接被拒）走的是
// url.Error 而非状态码载体：文案不得出现 403 归因，语义仍是 err != nil。
func TestQMTHealthNetworkErrorNotAnnotatedAsForbidden(t *testing.T) {
	// 取一个只监听、立刻关闭的端口 → 连接必然失败（不依赖外部网络）
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	c := NewQMTClient("http://"+addr, "tk", 500*time.Millisecond, 0)
	ok, err := c.Health()
	if ok || err == nil {
		t.Fatalf("连不上应返回 (false, err), got ok=%v err=%v", ok, err)
	}
	if strings.Contains(err.Error(), "ALLOWED_IPS") || strings.Contains(err.Error(), "403") {
		t.Fatalf("网络层失败不得被说成 403/白名单问题, got=%q", err.Error())
	}
	var he *gatewayHTTPError
	if errors.As(err, &he) {
		t.Fatalf("网络层失败不该有 HTTP 状态码载体, got=%v", err)
	}
}
