// 文件职责：CryptoPanic 客户端（cryptopanic.go）的行为锁单测，零网络（httptest 夹具）。
// 四条锁：① 夹具 JSON → Symbol 拼接（BTC→BTCUSDT、小写入参也归一、空币=Symbol ""）
// 与字段直投；② 空 token 惰性——Enabled false、Fetch 明确报错且**服务器绝不被调**
// （哨断言）；③ 401 错误串**不含 token**（密钥红线）；④ 拉取结果过 DedupeEvents
// 的 URL+日键去重（滚动窗口重叠投喂幂等）。
package data

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const cpTestToken = "SECRET-TOKEN-abc123"

// cpTestFixture 夹具：BTC/eth（小写混合）/空币 + 一条重复 URL 同日（供 ④ 去重）。
const cpTestFixture = `{"count":4,"results":[
 {"title":"Bitcoin ETF inflows surge","url":"https://news.local/a1","created_at":"2026-09-01T08:30:00.000Z",
  "currency":"BTC","source":{"title":"CoinDesk"},"votes":{"up":120,"down":3}},
 {"title":"Ethereum upgrade live","url":"https://news.local/a2","created_at":"2026-09-01T09:00:00Z",
  "currency":"eth","source":{"title":"The Block"},"votes":{"up":80,"down":1}},
 {"title":"Macro hot take, no currency","url":"https://news.local/a3","created_at":"bad-time",
  "currency":"","source":{"title":"Blog"},"votes":{"up":5,"down":5}},
 {"title":"Bitcoin ETF inflows surge (dup)","url":"https://news.local/a1","created_at":"2026-09-01T08:30:00Z",
  "currency":"BTC","source":{"title":"CoinDesk"},"votes":{"up":121,"down":3}}
]}`

// TestCryptoPanicFetchSymbolAndFields 锁 ①④：Symbol 拼接、字段直投、坏时间=零值、URL 去重。
func TestCryptoPanicFetchSymbolAndFields(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.URL.Path != "/api/v1/posts/" {
			t.Errorf("path got=%q", r.URL.Path)
		}
		_, _ = w.Write([]byte(cpTestFixture))
	}))
	defer srv.Close()

	c := NewCryptoPanicClient("  "+cpTestToken+"  ", srv.URL) // 构造期 TrimSpace
	if !c.Enabled() {
		t.Fatal("带 token 应 Enabled")
	}
	events, err := c.Fetch([]string{"btc", " ETH ", "ETH", ""}, 20)
	if err != nil {
		t.Fatalf("合法响应应成功: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("拉取原始条数 want=4 got=%d", len(events))
	}
	// 请求形状：token、去重后小写归一的 currencies、limit 都带上。
	for _, want := range []string{"auth_token=" + cpTestToken, "currencies=BTC,ETH", "limit=20"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query 缺 %q got=%q", want, gotQuery)
		}
	}
	if events[0].Market != "CRYPTO" || events[0].Symbol != "BTCUSDT" || events[0].Title == "" {
		t.Fatalf("entry1 got: %+v", events[0])
	}
	if events[0].PublishedAt.String() != "2026-09-01 08:30:00 +0000 UTC" {
		t.Fatalf("created_at 毫秒 RFC3339 got=%v", events[0].PublishedAt)
	}
	if events[1].Symbol != "ETHUSDT" { // 小写入参归一
		t.Fatalf("entry2 Symbol got=%q", events[1].Symbol)
	}
	if events[2].Symbol != "" || !events[2].PublishedAt.IsZero() {
		t.Fatalf("空币=Symbol 空串、坏时间=零值（不编数据）, got: %+v", events[2])
	}
	// ④ URL+日键去重：a1 同日重复 → 4 条变 3 条，首次保留。
	deduped := DedupeEvents(events)
	if len(deduped) != 3 {
		t.Fatalf("去重后 want=3 got=%d", len(deduped))
	}
	if deduped[0].Title != "Bitcoin ETF inflows surge" {
		t.Fatalf("去重应保留首条, got=%q", deduped[0].Title)
	}
}

// TestCryptoPanicEmptyTokenInert 锁 ②：空 token=整客户端惰性——Enabled false、
// Fetch 明确报错、服务器哨绝不被调（惰性零行为，与「空配置=不装配」惯例一致）。
func TestCryptoPanicEmptyTokenInert(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	for _, tok := range []string{"", "   ", "\t"} {
		c := NewCryptoPanicClient(tok, srv.URL)
		if c.Enabled() {
			t.Fatal("空/全空白 token 应 Enabled=false")
		}
		events, err := c.Fetch([]string{"BTC"}, 10)
		if err == nil || !strings.Contains(err.Error(), "拒发") {
			t.Fatalf("空 token Fetch 应明确报拒发, got err=%v", err)
		}
		if len(events) != 0 {
			t.Fatalf("惰性客户端必须空集, got=%d", len(events))
		}
	}
	if called {
		t.Fatal("空 token 时服务器绝不该被调（一个请求都不发）")
	}
}

// TestCryptoPanic401ErrorRedactsToken 锁 ③：401 错误串含「key 无效/配额」且**绝不含 token**。
func TestCryptoPanic401ErrorRedactsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := NewCryptoPanicClient(cpTestToken, srv.URL)
	_, err := c.Fetch(nil, 0)
	if err == nil {
		t.Fatal("401 必须报错")
	}
	if strings.Contains(err.Error(), cpTestToken) {
		t.Fatalf("错误信息绝不得含 token（密钥红线）, got=%q", err.Error())
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "key 无效") {
		t.Fatalf("401 应报「HTTP 401（key 无效/配额）」样, got=%q", err.Error())
	}
}

// TestCryptoPanicBaseAndDefaults 缺省基址= cryptopanic.com、去尾斜杠、构造期 token 收口。
func TestCryptoPanicBaseAndDefaults(t *testing.T) {
	c := NewCryptoPanicClient(" t ", "")
	if c.Base != CryptoPanicDefaultBase {
		t.Fatalf("空 base 应缺省, got=%q", c.Base)
	}
	if c.AuthToken != "t" {
		t.Fatalf("token 应构造期 TrimSpace, got=%q", c.AuthToken)
	}
	if got := NewCryptoPanicClient("t", "https://x.local/").Base; got != "https://x.local" {
		t.Fatalf("尾斜杠必须去掉, got=%q", got)
	}
}
