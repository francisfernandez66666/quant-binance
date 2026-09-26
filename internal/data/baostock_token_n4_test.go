// baostock_token_n4_test.go — §N4（2026-09-26 全量审计）pydata sidecar 可选口令的客户端侧锁。
//
// 缺陷原文：cmd/pydata/server.py 全文零鉴权（审计实核 token/Authorization 计数 0），
// 防线只有"绑 127.0.0.1"，同机任意进程都能匿名拉取全市场研究数据。
//
// 为何这样修（并这样测）：sidecar 加了**可选** --token（缺省空 = 匿名放行），Go 客户端
// 以同样的可选入参携带 X-Pydata-Token 头。默认零行为变化是本缺陷的验收底线，所以两条用例
// 各锁一条腿：
// ① 空 token → 请求里**绝不能出现**该头（否则现网未启口令的 sidecar 会收到无意义头，
// 且未来若 sidecar 启口令、空值还被误判成「带了个空口令」）；
// ② 配了 token → 每个请求都必须带、且值逐字相等（漏某个方法就是「部分取数 401」）。
//
// English: empty token sends no header at all (byte-identical to today); a configured token
// must ride on every request.
package data

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// bsTokenMock 假 sidecar：记录每个请求看到的 X-Pydata-Token 头，并按 sidecar 的错误协议回包。
// 返回的 CSV 只满足"能被 call 解析"即可（本测试关心的是头，不是数据）。
func bsTokenMock(t *testing.T, seen *[]string, want string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Header.Get(PyDataTokenHeader))
		// 口令未启用（want 为空）→ 直接回数据；启用但不匹配 → 401 + "error: ..."（协议形态见
		// cmd/pydata/server.py 的 token_error_body 注释：Go 侧只认 error: 前缀）
		if want != "" && r.Header.Get(PyDataTokenHeader) != want {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("error: unauthorized"))
			return
		}
		w.Write([]byte("calendar_date,is_open\n2020-01-02,1\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestBaostockEmptyTokenSendsNoHeader 兼容锁：token 传空 = 一个鉴权头都不发。
func TestBaostockEmptyTokenSendsNoHeader(t *testing.T) {
	var seen []string
	srv := bsTokenMock(t, &seen, "")
	c := NewBaostockClient(srv.URL, "")
	rows, err := c.TradeDays("20200101", "20200105")
	if err != nil {
		t.Fatalf("未启口令的 sidecar 应正常取数: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("取数结果异常: %d 行", len(rows))
	}
	if len(seen) != 1 {
		t.Fatalf("应只发一次请求, got %d", len(seen))
	}
	if seen[0] != "" {
		t.Fatalf("空 token 不得发 %s 头, got=%q", PyDataTokenHeader, seen[0])
	}
	// http.Request.Header.Get 对"未设置"与"设为空串"同形，故再用一个显式桩确认头不存在。
	var hasHeader bool
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasHeader = r.Header[http.CanonicalHeaderKey(PyDataTokenHeader)]
		w.Write([]byte("calendar_date,is_open\n2020-01-02,1\n"))
	}))
	defer srv2.Close()
	if _, err := NewBaostockClient(srv2.URL, "").TradeDays("20200101", "20200105"); err != nil {
		t.Fatalf("TradeDays: %v", err)
	}
	if hasHeader {
		t.Fatalf("空 token 时请求里不该携带 %s 头（哪怕空值）", PyDataTokenHeader)
	}
}

// TestBaostockConfiguredTokenSendsHeaderOnEveryCall 配置后必发头，且所有方法口径一致。
func TestBaostockConfiguredTokenSendsHeaderOnEveryCall(t *testing.T) {
	var seen []string
	srv := bsTokenMock(t, &seen, "s3cr3t")
	c := NewBaostockClient(srv.URL, "s3cr3t")
	if _, err := c.TradeDays("20200101", "20200105"); err != nil {
		t.Fatalf("TradeDays（口令匹配）应成功: %v", err)
	}
	if _, err := c.AllStock(); err != nil {
		t.Fatalf("AllStock（口令匹配）应成功: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("应发出两次请求, got %d", len(seen))
	}
	for i, got := range seen {
		if got != "s3cr3t" {
			t.Fatalf("第 %d 次请求缺/错 %s 头: %q", i+1, PyDataTokenHeader, got)
		}
	}
}

// TestBaostockWrongTokenSurfacesError 口令不匹配时 sidecar 回 401 + "error: unauthorized"，
// 客户端必须把它当错误抛出（而不是静默解析成空表——这正是选择 error: 前缀而非 JSON 的原因）。
func TestBaostockWrongTokenSurfacesError(t *testing.T) {
	var seen []string
	srv := bsTokenMock(t, &seen, "right")
	c := NewBaostockClient(srv.URL, "wrong")
	rows, err := c.TradeDays("20200101", "20200105")
	if err == nil {
		t.Fatalf("口令不匹配必须报错, got rows=%v", rows)
	}
	if len(rows) != 0 {
		t.Fatalf("口令不匹配不得返回任何行, got %d", len(rows))
	}
	if got := err.Error(); !strings.Contains(got, "unauthorized") {
		t.Fatalf("错误文本应带 sidecar 的 unauthorized 原因, got=%q", got)
	}
	if len(seen) != 1 || seen[0] != "wrong" {
		t.Fatalf("应把配置的口令原样发出, got=%v", seen)
	}
}

// TestBaostockTokenTrimmed 构造入参做首尾空白裁剪：口令从 env/config 读来常带尾随空格，
// 不裁剪就会"看着一样却 401"，属自找的可操作性缺陷。
func TestBaostockTokenTrimmed(t *testing.T) {
	var seen []string
	srv := bsTokenMock(t, &seen, "s3cr3t")
	c := NewBaostockClient(srv.URL, "  s3cr3t\n")
	if _, err := c.TradeDays("20200101", "20200105"); err != nil {
		t.Fatalf("带空白的口令应被裁剪后正常鉴权: %v", err)
	}
	if len(seen) != 1 || seen[0] != "s3cr3t" {
		t.Fatalf("发出的头值未裁剪空白: %q", seen)
	}
}
