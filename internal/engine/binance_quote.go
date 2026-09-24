// 文件职责：§市场分家-1 US/CRYPTO 的**单票现价查询面**——详情抽屉头部现价此前只有
// /api/stock/lookup（CN 四级链）一条腿，美股/加密货币标的点开必然打空或被 A股数据冒充。
// 本文件把已在位的行情 feed 快照（BinanceQuoteFeed.Quote，只认订阅池内标的）与
// 币安公共 REST 24h 快照（data-api.binance.vision 镜像 / equity 正式根，无需签名）
// 收成一个两档现价源：feed 命中优先（实时、带新鲜度），池外标的回落 REST 并做
// 10s 进程内 TTL 缓存（抽屉 5s 轮询不许放大成外呼风暴）。
//
// 诚实口径：两档都拿不到就返回 ok=false——绝不回 0 价、绝不借 CN 链兜底
// （"无证据≠中性"在展示腿的落点；0 价伪装停牌是 2026-09 事故族的老病灶）。
//
// English: per-symbol quote source for the US/CRYPTO detail drawer — feed snapshot first
// (fresh, cache-only), REST 24hr ticker fallback for symbols outside the subscription pool
// with a 10s in-process TTL; never fabricates a price and never touches the CN chain.
package engine

import (
	"context"
	"strings"
	"sync"
	"time"

	"quant-trading-v2/internal/data"
)

// binanceQuoteRestTTL REST 回落结果的进程内缓存时长。
// 抽屉头部 5s 一轮询，池外标的若每次都打 REST 会把观测面变成外呼放大器；
// 10s 对展示口径足够新（派发链不吃这条腿，它走 feed/REST 自己的价源）。
const binanceQuoteRestTTL = 10 * time.Second

// binanceQuoteRestTimeout 单次 REST 快照的外呼超时（展示腿宁可慢失败也不吊死请求）。
const binanceQuoteRestTimeout = 8 * time.Second

// BinanceQuoteView /api/binance/quote 的对外契约（JSON 直出，前端按 source 区分新鲜度话术）。
type BinanceQuoteView struct {
	Code      string  `json:"code"`       // 大写 symbol（CRYPTO=BTCUSDT；US=AAPL）
	Price     float64 `json:"price"`      // 最新价（恒 >0：0 价一律 ok=false，不伪装停牌）
	PrevClose float64 `json:"prev_close"` // 24h 参考价（源没给则 0，前端缺省不渲染涨跌）
	ChangePct float64 `json:"change_pct"` // 涨跌幅 %（源值优先，REST 24hr 口径）
	High      float64 `json:"high"`       // 24h 高
	Low       float64 `json:"low"`        // 24h 低
	Volume    float64 `json:"volume"`     // 24h 成交量（base 资产）
	Amount    float64 `json:"amount"`     // 24h 成交额（quote 资产）
	Source    string  `json:"source"`     // "feed"（WS 快照）| "rest"（24h 快照回落）
	AgeMs     int64   `json:"age_ms"`     // feed 腿=距最近一帧的毫秒龄；REST 腿=0（取数即最新）
}

// binanceQuoteSource 两档现价源（引擎 build 期装配，读多写零，一把互斥锁足够）。
type binanceQuoteSource struct {
	feeds map[string]*data.BinanceQuoteFeed // market("US"/"CRYPTO") → 在位行情 feed（可缺）

	mu      sync.Mutex
	clients map[string]*data.BinanceRestClient // market → 惰性 REST 客户端（httptest 可注入）
	bases   map[string]string                  // market → 客户端 base 覆盖（""=data 包缺省基址）
	cache   map[string]binanceQuoteCacheEntry  // market:CODE → 最近一次 REST 结果
	now     func() time.Time                   // 测试注入时钟
}

type binanceQuoteCacheEntry struct {
	view BinanceQuoteView
	at   time.Time
}

// newBinanceQuoteSource 构造现价源。feeds 按"装配时点的表内容"拷贝留存——
// registry build 期该表不再增删（feed 只在建市循环里落一次），拷贝防的是未来演化时
// 装配层复用同一 map 造成隐蔽共享。
func newBinanceQuoteSource(feeds map[string]*data.BinanceQuoteFeed) *binanceQuoteSource {
	cp := make(map[string]*data.BinanceQuoteFeed, len(feeds))
	for k, v := range feeds {
		cp[k] = v
	}
	return &binanceQuoteSource{
		feeds:   cp,
		clients: map[string]*data.BinanceRestClient{},
		bases:   map[string]string{},
		cache:   map[string]binanceQuoteCacheEntry{},
		now:     time.Now,
	}
}

// SetRestBase 覆盖某市场的 REST 基址（httptest 注入用；空=用 data 包缺省——
// CRYPTO 走公共镜像 data-api.binance.vision，US 走 equity 正式根，均为免签名只读面）。
func (q *binanceQuoteSource) SetRestBase(market, base string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.bases == nil {
		q.bases = map[string]string{}
	}
	q.bases[market] = base
	delete(q.clients, market) // 基址变了就把惰性客户端重建
}

// quote 取单票现价：feed 命中（价>0）即返；池外标的回落 REST（带 TTL 缓存）。
// market 非 US/CRYPTO、或两档都无有效价 → ok=false（调用方如实呈现"无快照"，绝不回 CN 数据）。
func (q *binanceQuoteSource) quote(ctx context.Context, market, code string) (BinanceQuoteView, bool) {
	market = data.NormalizeMarketKey(market)
	if market != "US" && market != "CRYPTO" {
		return BinanceQuoteView{}, false
	}
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return BinanceQuoteView{}, false
	}
	// 第一档：行情 feed 快照（只覆盖订阅池；池外返回 nil 属常态，不是错误）。
	if f := q.feeds[market]; f != nil {
		if si := f.Quote(code); si != nil && si.Price > 0 {
			return BinanceQuoteView{
				Code: code, Price: si.Price, PrevClose: si.PrevClose, ChangePct: si.ChangePct,
				High: si.High, Low: si.Low, Volume: si.Volume, Amount: si.Amount,
				Source: "feed", AgeMs: f.SymbolStalenessMs(code),
			}, true
		}
	}
	// 第二档：REST 24h 快照——先查 TTL 缓存（抽屉 5s 轮询的节奏不该放大成 5s 一次外呼）。
	key := market + ":" + code
	q.mu.Lock()
	if ent, ok := q.cache[key]; ok && q.now().Sub(ent.at) < binanceQuoteRestTTL {
		q.mu.Unlock()
		return ent.view, true
	}
	client := q.clientLocked(market)
	q.mu.Unlock()

	// REST 取价：8s 硬超时防镜像域名抖动拖住 HTTP 线程；失败不写缓存（诚实缺位，
	// 下一次请求重试），成功才回填 TTL 缓存供窗口期内复用。
	cctx, cancel := context.WithTimeout(ctx, binanceQuoteRestTimeout)
	defer cancel()
	tick, err := client.LatestQuote(cctx, code)
	if err != nil {
		return BinanceQuoteView{}, false
	}
	view := BinanceQuoteView{
		Code: code, Price: tick.Price, PrevClose: tick.PrevClose, ChangePct: tick.ChangePct,
		High: tick.High, Low: tick.Low, Volume: tick.Volume, Amount: tick.Amount,
		Source: "rest", AgeMs: 0,
	}
	// 快照灌入 TTL 缓存（键 market:code，与读侧同源；写锁粒度只覆盖 map 赋值）
	q.mu.Lock()
	q.cache[key] = binanceQuoteCacheEntry{view: view, at: q.now()}
	q.mu.Unlock()
	return view, true
}

// clientLocked 惰性取/建 REST 客户端（调用方持锁）。
func (q *binanceQuoteSource) clientLocked(market string) *data.BinanceRestClient {
	if c, ok := q.clients[market]; ok && c != nil {
		return c
	}
	c := data.NewBinanceRestClient(q.bases[market], market == "US")
	q.clients[market] = c
	return c
}

// SetBinanceQuoteFeeds build 期把在位行情 feed 表交给引擎（§市场分家-1 现价腿）。
// 空表也照样装配：feed 未订阅时现价腿自动退化为 REST-only，仍不回 CN 链。
func (e *Engine) SetBinanceQuoteFeeds(feeds map[string]*data.BinanceQuoteFeed) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.bnQuote = newBinanceQuoteSource(feeds)
}

// BinanceQuote 引擎级只读入口：US/CRYPTO 单票现价（多账号各查自己引擎的 feed）。
// 引擎未接币安链（bnQuote=nil）/ CN / 两档无价 → ok=false。
func (e *Engine) BinanceQuote(ctx context.Context, market, code string) (BinanceQuoteView, bool) {
	e.mu.RLock()
	q := e.bnQuote
	e.mu.RUnlock()
	if q == nil {
		return BinanceQuoteView{}, false
	}
	return q.quote(ctx, market, code)
}
