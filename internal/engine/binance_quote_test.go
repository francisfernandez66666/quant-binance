// 文件职责：§市场分家-1 现价源单测（binance_quote.go）——四档口径逐条锁死：
// ① 市场闸：CN/空/非法一律 ok=false（CN 现价只许走 /api/stock/lookup 四级链）；
// ② feed 优先：订阅池内标的从行情快照直读，Source=feed、带新鲜度龄；
// ③ REST 回落：池外标的走 httptest 假 24h 快照，Source=rest，且 10s TTL 内
//
//	**只外呼一次**（抽屉 5s 轮询不许放大成外呼风暴——计数器是这条的唯一证法）；
//
// ④ 诚实失败：REST 非 200/0 价 → ok=false 且不缓存失败（下一请求可重试）。
//
// English: unit tests for the US/CRYPTO quote source — market gate, feed-first, REST
// fallback with a 10s TTL (single outbound per window), and honest failure (no caching
// of failures, never CN data).
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"quant-trading-v2/internal/data"
)

// deadDial 只直调 HandlePayload/Flush 的 feed 永不拨号；注入它是为了满足
// NewBinanceQuoteFeed 的构造期"Dial 必填"校验（与 data 包同族测试手法）。
func deadDial(_ context.Context, url string) (data.WsTransport, error) {
	return nil, errors.New("单测不该拨号: " + url)
}

// newFeedWithBTC 造一条 CRYPTO feed 并注入一帧 BTCUSDT 行情（不 Start，纯直调）。
func newFeedWithBTC(t *testing.T) *data.BinanceQuoteFeed {
	t.Helper()
	f, err := data.NewBinanceQuoteFeed(data.BinanceQuoteFeedOptions{
		Market: "CRYPTO", Symbols: []string{"BTCUSDT"}, Dial: deadDial,
	})
	if err != nil {
		t.Fatalf("feed 构造失败: %v", err)
	}
	// miniTicker 口径：c=最新价（大值+小数保留，断言时防浮点抄错）
	payload := []byte(`{"e":"24hrMiniTicker","E":` + strconv.FormatInt(time.Now().UnixMilli(), 10) +
		`,"s":"BTCUSDT","o":"49000","c":"50000.5","h":"50100","l":"48900","v":"12","Q":"600000"}`)
	if n := f.HandlePayload(payload); n != 1 {
		t.Fatalf("HandlePayload 命中数=%d，期望 1", n)
	}
	f.Flush()
	return f
}

// TestQuoteSourceMarketGate 市场闸：CN/空白/非法市场一律拒（分轨铁律的负向锁）。
func TestQuoteSourceMarketGate(t *testing.T) {
	q := newBinanceQuoteSource(nil)
	for _, mkt := range []string{"CN", "", "cn", "FOREX"} {
		if _, ok := q.quote(context.Background(), mkt, "600519"); ok {
			t.Fatalf("市场 %q 必须 ok=false（CN 现价只走 stock/lookup）", mkt)
		}
	}
	if _, ok := q.quote(context.Background(), "US", "  "); ok {
		t.Fatal("空 code 必须 ok=false")
	}
}

// TestQuoteSourceFeedFirst feed 在位的标的必须走 feed 档（REST 服务器压根不建）。
func TestQuoteSourceFeedFirst(t *testing.T) {
	feed := newFeedWithBTC(t)
	q := newBinanceQuoteSource(map[string]*data.BinanceQuoteFeed{"CRYPTO": feed})
	view, ok := q.quote(context.Background(), "crypto", "btcusdt") // 大小写混写也要命中
	if !ok {
		t.Fatal("feed 在价时应 ok=true")
	}
	if view.Source != "feed" || view.Price != 50000.5 || view.Code != "BTCUSDT" {
		t.Fatalf("视图不符: %+v", view)
	}
	if view.AgeMs < 0 {
		t.Fatalf("新鲜度龄非法: %d", view.AgeMs)
	}
}

// TestQuoteSourceRestFallbackAndTTL 池外标的走 REST；TTL 窗内二次查询零外呼。
func TestQuoteSourceRestFallbackAndTTL(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/sapi/v1/equity/ticker/24hr" {
			t.Errorf("REST 路径异常: %s（US 应走 equity 前缀）", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"lastPrice": "101.5", "prevClosePrice": "100", "priceChangePercent": "1.5",
			"highPrice": "102", "lowPrice": "99", "volume": "10", "quoteVolume": "1000",
		})
	}))
	defer srv.Close()
	q := newBinanceQuoteSource(nil)
	q.SetRestBase("US", srv.URL)
	view, ok := q.quote(context.Background(), "US", "AAPL")
	if !ok || view.Source != "rest" || view.Price != 101.5 || view.ChangePct != 1.5 {
		t.Fatalf("REST 回落视图不符: ok=%v %+v", ok, view)
	}
	if hits.Load() != 1 {
		t.Fatalf("首查应外呼 1 次，实为 %d", hits.Load())
	}
	if _, ok := q.quote(context.Background(), "US", "AAPL"); !ok {
		t.Fatal("TTL 窗内二次查询应命中缓存")
	}
	if hits.Load() != 1 {
		t.Fatalf("TTL 窗内不得二次外呼，实为 %d 次", hits.Load())
	}
	// 时钟拨过 TTL → 允许再外呼（缓存续期语义）
	q.mu.Lock()
	q.cache["US:AAPL"] = binanceQuoteCacheEntry{view: view, at: time.Now().Add(-binanceQuoteRestTTL - time.Second)}
	q.mu.Unlock()
	if _, ok := q.quote(context.Background(), "US", "AAPL"); !ok {
		t.Fatal("TTL 过期后应回源成功")
	}
	if hits.Load() != 2 {
		t.Fatalf("过期后应再外呼一次，实为 %d 次", hits.Load())
	}
}

// TestQuoteSourceHonestFailure REST 失败/0 价不缓存、不造假（下一请求可重试）。
func TestQuoteSourceHonestFailure(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	q := newBinanceQuoteSource(nil)
	q.SetRestBase("CRYPTO", srv.URL)
	if _, ok := q.quote(context.Background(), "CRYPTO", "NOVAUSDT"); ok {
		t.Fatal("REST 502 必须 ok=false（不许回 0 价伪装）")
	}
	if _, ok := q.quote(context.Background(), "CRYPTO", "NOVAUSDT"); ok {
		t.Fatal("失败不得进缓存")
	}
	if hits.Load() != 2 {
		t.Fatalf("失败后每请求应可重试，实呼 %d", hits.Load())
	}
}

// TestEngineQuoteWiring 引擎零值（未接币安链）时 BinanceQuote 恒 ok=false——
// 出厂关闸形态下 /api/binance/quote 只能拿到"无现价源"，不会误调任何外呼。
func TestEngineQuoteWiring(t *testing.T) {
	var e Engine
	if _, ok := e.BinanceQuote(context.Background(), "US", "AAPL"); ok {
		t.Fatal("未装配币安链的引擎必须 ok=false")
	}
	e.SetBinanceQuoteFeeds(map[string]*data.BinanceQuoteFeed{})
	// 基址钉到必然拒绝连接的本地端口：单测绝不真打公网镜像（外呼纪律）。
	e.bnQuote.SetRestBase("CRYPTO", "http://127.0.0.1:1")
	if _, ok := e.BinanceQuote(context.Background(), "CRYPTO", "BTCUSDT"); ok {
		t.Fatal("空 feed 表+REST 不可达必须 ok=false（无证据≠有价格）")
	}
}
