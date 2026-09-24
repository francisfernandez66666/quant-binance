// 文件职责：§市场分家-1 GET /api/binance/quote 的行为锁——
// ① 分轨闸：market 缺省/CN/非法一律 400，CN 现价唯一通道仍是 /api/stock/lookup；
// ② 零配置零行为：未注入 quoteSource 闭包时端点不炸，如实回 {"ok":false}+reason；
// ③ 有价视图：闭包给回 BinanceQuoteView 形状的值时字段展平到顶层且 ok=true、market 回显；
// ④ 闭包按 (账号,市场,代码) 三参传递——server 不猜代码归属，参数原样穿透。
//
// English: behavior lock for the non-CN drawer quote endpoint: CN/garbage market rejected
// with 400 (track separation), nil-closure answers ok=false honestly, and a present view is
// flattened to the response root with ok=true; the closure receives (user, market, code) verbatim.
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// callQuote 直调 handleBinanceQuote 并解 JSON。
func callQuote(t *testing.T, s *Server, query string) (int, map[string]any) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleBinanceQuote(rr, httptest.NewRequest(http.MethodGet, "/api/binance/quote"+query, nil))
	var out map[string]any
	if rr.Body.Len() > 0 {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode body=%s: %v", rr.Body.String(), err)
		}
	}
	return rr.Code, out
}

// TestQuoteEndpointMarketGate CN/空/非法市场全部 400——A股现价绝不从本端点流出。
func TestQuoteEndpointMarketGate(t *testing.T) {
	s := &Server{}
	for _, q := range []string{"?market=CN&code=600519", "?code=BTCUSDT", "?market=FOREX&code=X", "?market=US&code=%20"} {
		code, _ := callQuote(t, s, q)
		if code != http.StatusBadRequest {
			t.Fatalf("查询 %q 必须 400，实为 %d", q, code)
		}
	}
}

// TestQuoteEndpointNilClosure 未装配币安链：200 + ok=false + reason（前端按"无快照"渲染）。
func TestQuoteEndpointNilClosure(t *testing.T) {
	s := &Server{}
	code, out := callQuote(t, s, "?market=CRYPTO&code=btcusdt")
	if code != 200 {
		t.Fatalf("期望 200，实为 %d", code)
	}
	if v, _ := out["ok"].(bool); v {
		t.Fatal("未注入闭包必须 ok=false")
	}
	if r, _ := out["reason"].(string); r == "" {
		t.Fatal("ok=false 必须带 reason（不许静默）")
	}
	if out["market"] != "CRYPTO" || out["code"] != "BTCUSDT" {
		t.Fatalf("回显不符: %+v", out) // code 大写归一后回显
	}
}

// TestQuoteEndpointViewFlatten 闭包给回带 json tag 的视图值 → 展平顶层 + ok=true + market 回显；
// 并锁三参穿透（直调无 auth 中间件时 uid 为空串——market 归一、code 大写原样到闭包）。
func TestQuoteEndpointViewFlatten(t *testing.T) {
	var gotUID, gotMkt, gotCode string
	s := &Server{quoteSource: func(uid, mkt, code string) (any, bool) {
		gotUID, gotMkt, gotCode = uid, mkt, code
		return map[string]any{
			"code": code, "price": 50000.5, "prev_close": 49000.0, "change_pct": 2.05,
			"high": 50100.0, "low": 48900.0, "volume": 12.0, "amount": 600000.0,
			"source": "feed", "age_ms": float64(1200),
		}, true
	}}
	code2, out2 := callQuote(t, s, "?market=crypto&code=btcusdt") // 小写入参：端点归一后穿透
	if code2 != 200 {
		t.Fatalf("期望 200，实为 %d", code2)
	}
	if v, _ := out2["ok"].(bool); !v {
		t.Fatalf("闭包给价时必须 ok=true: %+v", out2)
	}
	if out2["price"].(float64) != 50000.5 || out2["source"].(string) != "feed" {
		t.Fatalf("视图未展平: %+v", out2)
	}
	if out2["market"].(string) != "CRYPTO" {
		t.Fatalf("market 回显错: %+v", out2)
	}
	if gotMkt != "CRYPTO" || gotCode != "BTCUSDT" {
		t.Fatalf("闭包参数穿透错: uid=%q mkt=%q code=%q", gotUID, gotMkt, gotCode)
	}
}
