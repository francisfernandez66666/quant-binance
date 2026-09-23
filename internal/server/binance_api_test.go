// binance_api_test.go — §BINANCE-P2 运维端点行为锁。
// 覆盖两件事：① 未接线降级——引擎/LiveRouter 缺失时全部端点 503 fail-closed
// （绝不 200+error：白板修复教训，前端把 200 体当成功数据渲染会白屏）；
// ② orders 端点的市场章过滤——币安端点绝不回 CN 委托行（对账面板串市场=账本误导）。
package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"quant-trading-v2/internal/auth"
	"quant-trading-v2/internal/store"
)

// asUser 手工注入鉴权上下文（等价 authMiddleware 放行后的请求形态）：
// 未注入时 userIDFor 返回空串，账本按 user_id 过滤后必空——测试想验的是市场章过滤，
// 不是鉴权，所以补一个 u1 身份让数据可见。
func asUser(r *http.Request, uid string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxUserKey{}, &auth.User{ID: uid}))
}

// TestBinanceAPIFailClosedWithoutRouter 未接入实盘通道时全端点 503。
func TestBinanceAPIFailClosedWithoutRouter(t *testing.T) {
	s := &Server{} // registry/ctrl 皆 nil（模拟未装配币安链的最小服务）
	cases := []struct {
		name, method, path, body string
		h                        func(http.ResponseWriter, *http.Request)
	}{
		{"state", http.MethodGet, "/api/binance/state", "", s.handleBinanceState},
		{"cancel", http.MethodPost, "/api/binance/cancel", `{"market":"US","order_id":"1"}`, s.handleBinanceCancel},
		{"halt", http.MethodPost, "/api/binance/halt", `{"halted":true}`, s.handleBinanceHalt},
		{"exchange_info", http.MethodGet, "/api/binance/exchange_info?symbol=BTCUSDT", "", s.handleBinanceExchangeInfo},
		{"disclaimer", http.MethodPost, "/api/binance/disclaimer", `{"confirmed":true}`, s.handleBinanceDisclaimerPost},
	}
	for _, tc := range cases {
		var body io.Reader // GET 用 nil（typed-nil *strings.Reader 会让 NewRequest 里 r.Len() 炸）
		if tc.body != "" {
			body = strings.NewReader(tc.body)
		}
		r := httptest.NewRequest(tc.method, tc.path, body)
		rr := httptest.NewRecorder()
		tc.h(rr, r)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: 未接线应 503，实际 %d body=%s", tc.name, rr.Code, rr.Body.String())
		}
		var out map[string]string
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil || out["error"] == "" {
			t.Fatalf("%s: 503 响应应为 {\"error\":…}: %s", tc.name, rr.Body.String())
		}
	}
}

// TestBinanceOrdersMarketFilter orders 端点只回币安市场行：CN 委托绝不混入（market 章过滤），
// ?market=US 时进一步收窄到单市场。
func TestBinanceOrdersMarketFilter(t *testing.T) {
	s, db, _ := newTestResearchServer(t)
	seed := func(orderID, market, status string) {
		if _, err := db.UpsertRealOrder(store.RealOrder{
			OrderID: orderID, SignalID: "buy:" + orderID, Code: "X", Side: "买入", Status: status,
			Price: 1, Qty: 1, CreatedAt: "2026-09-23 10:00:00", UserID: "u1", Market: market,
		}); err != nil {
			t.Fatalf("seed %s: %v", orderID, err)
		}
	}
	seed("cn-1", "CN", "已报")
	seed("us-1", "US", "已成")
	seed("cry-1", "CRYPTO", "部成")

	list := func(query string) []store.RealOrder {
		rr := httptest.NewRecorder()
		s.handleBinanceOrders(rr, asUser(httptest.NewRequest(http.MethodGet, "/api/binance/orders"+query, nil), "u1"))
		if rr.Code != 200 {
			t.Fatalf("orders%s HTTP %d: %s", query, rr.Code, rr.Body.String())
		}
		var out []store.RealOrder
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v %s", err, rr.Body.String())
		}
		return out
	}
	all := list("")
	if len(all) != 2 {
		t.Fatalf("合并视图应只含 US+CRYPTO 两行，实际 %d: %+v", len(all), all)
	}
	for _, o := range all {
		if o.Market == "CN" {
			t.Fatalf("CN 委托混入币安端点: %+v", o)
		}
	}
	only := list("?market=US")
	if len(only) != 1 || only[0].OrderID != "us-1" {
		t.Fatalf("?market=US 应精确回 us-1，实际 %+v", only)
	}
}
