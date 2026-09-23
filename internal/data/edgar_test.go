// 文件职责：EDGAR 8-K Atom 客户端（edgar.go）的行为锁单测，零网络（httptest 夹具）。
// 五条锁：① Atom 宽容解析（两种 ticker 形态各一 + 纯 CIK 无 ticker 一 + 无 ticker
// 文本一）→ 条数/Market/Ticker/Title/URL 字段；② published 缺失回退 updated；
// ③ 空 UA 拒发（服务器哨=绝不被调）；④ HTTP 500 降级空集+err；⑤ 请求形状
// （path/query 透传 + UA 头真的带上）。ticker 提取失败=空串如实呈现，绝不编造。
package data

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// edgarTestAtomFixture 构造夹具：四条 entry 覆盖两形态 ticker/纯 CIK/无 ticker +
// 一条 published 缺失（回退 updated）——共 5 条。
const edgarTestAtomFixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Current Form 8-K Filings</title>
  <entry>
    <title>ACME Industries Inc. (ACME, CIK 0001000000)</title>
    <updated>2026-09-01T09:00:00-04:00</updated>
    <published>2026-09-01T09:00:00-04:00</published>
    <link href="https://www.sec.gov/Archives/edgar/data/1000000/0001.rn.txt" rel="alternate"/>
    <summary>ACME Industries Inc. filing</summary>
  </entry>
  <entry>
    <title>BRK Holdings Ltd. (0000000000)</title>
    <updated>2026-09-02T10:00:00-04:00</updated>
    <link href="https://www.sec.gov/Archives/edgar/data/2/0002.rn.txt" type="text/html"/>
    <summary>BRK Holdings Ltd. (CIK 0000000002, BRK.B) filed 8-K</summary>
  </entry>
  <entry>
    <title>Only Cik Co (CIK 0000000003)</title>
    <updated>2026-09-03T11:00:00-04:00</updated>
    <published>2026-09-03T11:00:00-04:00</published>
    <link href="https://www.sec.gov/Archives/edgar/data/3/0003.rn.txt"/>
    <summary>no ticker in summary either</summary>
  </entry>
  <entry>
    <title>Plain Company Name</title>
    <updated>2026-09-04T12:00:00-04:00</updated>
    <published>2026-09-04T12:00:00-04:00</published>
    <link href="https://www.sec.gov/Archives/edgar/data/4/0004.rn.txt"/>
    <summary></summary>
  </entry>
  <entry>
    <title>Timeless Inc. (TMLSS, CIK 0000000005)</title>
    <updated>2026-09-05T13:30:00Z</updated>
    <link href="https://www.sec.gov/Archives/edgar/data/5/0005.rn.txt"/>
    <summary>published 字段整个缺失 → 必须回退 updated</summary>
  </entry>
</feed>`

// TestEdgarFetchRecent8KAtomParse 锁 ①②：夹具 Atom → 5 条、字段逐项、published 回退 updated。
func TestEdgarFetchRecent8KAtomParse(t *testing.T) {
	var gotPath, gotQuery, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotUA = r.URL.Path, r.URL.RawQuery, r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(edgarTestAtomFixture))
	}))
	defer srv.Close()

	c := NewEDGARClient(srv.URL, "QuantResearch admin@example.com")
	events, err := c.FetchRecent8K(40)
	if err != nil {
		t.Fatalf("合法 Atom 应解析成功: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("条数 want=5 got=%d", len(events))
	}
	// ⑤ 请求形状：path 透传、count 附带、UA 真的带上。
	if gotPath != "/cgi-bin/browse-edgar" {
		t.Fatalf("path got=%q", gotPath)
	}
	if !strings.Contains(gotQuery, "action=getcurrent") || !strings.Contains(gotQuery, "type=8-K") ||
		!strings.Contains(gotQuery, "output=atom") || !strings.Contains(gotQuery, "count=40") {
		t.Fatalf("query 形状不符 got=%q", gotQuery)
	}
	if gotUA == "" {
		t.Fatal("UA 头必须带上")
	}
	// entry1：标题 ticker 形态 "(ACME, CIK …)"。
	e0 := events[0]
	if e0.Market != "US" || e0.Ticker != "ACME" {
		t.Fatalf("entry1 got: %+v", e0)
	}
	if e0.URL != "https://www.sec.gov/Archives/edgar/data/1000000/0001.rn.txt" {
		t.Fatalf("entry1 URL got=%q", e0.URL)
	}
	if !e0.PublishedAt.Equal(time.Date(2026, 9, 1, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("entry1 PublishedAt got=%v（RFC3339 带时区应归一到 13:00Z）", e0.PublishedAt)
	}
	// entry2：published 缺失→回退 updated；ticker 从摘要 "(CIK …, BRK.B)" 后式提取。
	e1 := events[1]
	if e1.Ticker != "BRK.B" {
		t.Fatalf("entry2 摘要后式 ticker got=%q want=BRK.B", e1.Ticker)
	}
	if e1.PublishedAt.IsZero() {
		t.Fatal("entry2 published 缺失应回退 updated，不得为零值")
	}
	if !e1.PublishedAt.Equal(time.Date(2026, 9, 2, 14, 0, 0, 0, time.UTC)) {
		t.Fatalf("entry2 回退 updated got=%v", e1.PublishedAt)
	}
	// entry3：纯 CIK 无 ticker → 空串如实呈现；entry4：无任何 CIK 括号 → 空串。
	if events[2].Ticker != "" || events[3].Ticker != "" {
		t.Fatalf("无 ticker 形态必须空串如实呈现, got=%q/%q", events[2].Ticker, events[3].Ticker)
	}
	// entry5：ticker 前式 + 无 published 也回退 updated（updated 为 Z 结尾）。
	if events[4].Ticker != "TMLSS" || events[4].PublishedAt.IsZero() {
		t.Fatalf("entry5 got: %+v", events[4])
	}
}

// TestEdgarEmptyUARefusesSend 锁 ③：空 UA 直接拒发——服务器哨断言绝不被调。
func TestEdgarEmptyUARefusesSend(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	for _, ua := range []string{"", "   "} {
		c := NewEDGARClient(srv.URL, ua)
		events, err := c.FetchRecent8K(10)
		if err == nil {
			t.Fatal("空 UA 必须报错")
		}
		if len(events) != 0 {
			t.Fatalf("失败必须空集, got=%d", len(events))
		}
	}
	if called {
		t.Fatal("空 UA 时服务器绝不该被调（拒发在出网之前）")
	}
}

// TestEdgarHTTP500DegradesEmpty 锁 ④：非 200 → 空集 + err，不 panic 不编数据。
func TestEdgarHTTP500DegradesEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewEDGARClient(srv.URL, "QuantResearch admin@example.com")
	events, err := c.FetchRecent8K(0)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("500 应降级为空集+状态码错误, got=%v err=%v", events, err)
	}
	if len(events) != 0 {
		t.Fatalf("500 时绝不得返回编造数据, got=%d", len(events))
	}
}

// TestEdgarBaseDefaultAndTrim 缺省基址=sec.gov、构造去尾斜杠（httptest 注入形态锁）。
func TestEdgarBaseDefaultAndTrim(t *testing.T) {
	if got := NewEDGARClient("", "ua@x.com").Base; got != EDGARDefaultBase {
		t.Fatalf("空 base 应缺省 %q, got=%q", EDGARDefaultBase, got)
	}
	if got := NewEDGARClient("https://m.local/", "ua@x.com").Base; got != "https://m.local" {
		t.Fatalf("尾斜杠必须去掉, got=%q", got)
	}
}
