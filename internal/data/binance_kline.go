// 文件职责：币安 **REST 历史/兜底行情面**（Phase 3 第 1 项，PLAN §2.8 K线口径 +
// GAP §G-1"端点未实测→基址可注入"）。三件事：
//
//	KLines     现货 /api/v3/klines（array-of-arrays，0=openTime…6=closeTime…8=quoteVol）
//	           → 仓库既有 []data.KLine；美股 /sapi/v1/equity/klines（**PLAN §2.8 明确美股
//	           当前无历史 K线**，本方法按同族形状尽力解析，失败即返回 error 让上层显式降级）；
//	Ticker     /api/v3/ticker/24hr 单票快照（WS 断流时的 §6.9 轮询兜底）；
//	ExchangeInfo 交易规则（见 binance_symbol_rules.go）。
//
// 三条硬约束：
//  1. 基址必须可注入（Base 字段）：镜像端点 data-api.binance.vision（公网可达，只代理
//     klines/ticker/depth/exchangeInfo，即 PLAN §2.9 的"镜像端点锁"）、生产 api.binance.com、
//     testnet 三选一，单测注入 httptest；
//  2. 时间戳统一按 **epoch 毫秒** 处理。注意 ⚠ data.binance.vision 归档与现货 testnet 部分
//     接口用"自 2025-01-01 的微秒"口径（PLAN §2.8），由 normalizeBinanceEpoch 收口，
//     绝不在调用点各猜各的；
//  3. 任何字段解析失败即整行丢弃并计数，不填 0 冒充有效 K线。
//
// English: REST history/fallback surface for Binance (klines, 24h ticker, exchangeInfo). Base URL
// is injectable (public mirror / prod / testnet / httptest); timestamps normalize to epoch ms in
// one place; malformed rows are dropped and counted, never zero-filled.
package data

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// K线区间（两市场共用；⚠ 美股流无 1m，见 PLAN §2.4 表）。
const (
	BinanceInterval1m  = "1m"
	BinanceInterval5m  = "5m"
	BinanceInterval1h  = "1h"
	BinanceInterval1d  = "1d"
	BinanceInterval1w  = "1w"
	BinanceInterval1mo = "1mo" // 现货归档月线口径（PLAN §2.8：文件名/参数用 1mo 而非 1M）
)

// binanceVisionEpochBase data.binance.vision 归档起点（PLAN §2.8：该源的微秒时间戳以此为原点）。
var binanceVisionEpochBase = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// BinanceRestClient 币安公共 REST 客户端（**只读公共端点，不签名**，故不涉及任何密钥）。
type BinanceRestClient struct {
	Base   string // 基址（镜像/生产/testnet/httptest）
	Client *http.Client
	// Equity 走 true 时路径前缀为 /sapi/v1/equity/*（美股），false 为 /api/v3/*（现货）。
	Equity bool
	// Timeout 零值=10s。
	Timeout time.Duration
}

// NewBinanceRestClient 构造只读 REST 客户端。base 为空时按市场选缺省基址。
func NewBinanceRestClient(base string, equity bool) *BinanceRestClient {
	if strings.TrimSpace(base) == "" {
		base = BinanceSpotRestMirror // 缺省镜像：公网可达且 PLAN §2.9  mandates 只读公共面走镜像
		if equity {
			base = BinanceEquityRestProd
		}
	}
	timeout := 10 * time.Second
	return &BinanceRestClient{
		Base:   strings.TrimSuffix(base, "/"),
		Equity: equity,
		Client: &http.Client{Timeout: timeout},
	}
}

// klinesPath K线端点路径（两市场不同前缀）。
func (c *BinanceRestClient) klinesPath() string {
	if c.Equity {
		return "/sapi/v1/equity/klines"
	}
	return "/api/v3/klines"
}

// getJSON 发一次 GET 并解码（状态码非 200 直接带截断 body 返回错误，便于排障且不含凭证）。
func (c *BinanceRestClient) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := c.Base + path
	if len(q) > 0 {
		full += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, full, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	cl := c.Client
	if cl == nil {
		cl = &http.Client{Timeout: timeout}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return fmt.Errorf("binance rest %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8MB 上限，防异常大响应吃内存
	if err != nil {
		return fmt.Errorf("binance rest %s 读体失败: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("binance rest %s: HTTP %d %s", path, resp.StatusCode, truncateForLog(raw))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("binance rest %s JSON 解码失败: %w", path, err)
	}
	return nil
}

// truncateForLog 错误消息里的 body 截断（排障够用，且不放大日志）。
func truncateForLog(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// KLines 拉 K线（limit<=0 时取 500 上限内），返回按时间升序的 []KLine。
// interval 传 PLAN §2.8 口径（1m/5m/1h/1d/1w/1mo）。
func (c *BinanceRestClient) KLines(ctx context.Context, symbol, interval string, limit int) ([]KLine, error) {
	sym := normalizeBinanceSymbol(symbol)
	if sym == "" || strings.TrimSpace(interval) == "" {
		return nil, fmt.Errorf("binance klines: symbol/interval 不能为空（got %q/%q）", sym, interval)
	}
	if limit <= 0 || limit > 1500 {
		limit = 500 // 币安上限 1500，缺省取一屏够用又不超单次负载的值
	}
	q := url.Values{"symbol": {sym}, "interval": {strings.TrimSpace(interval)}, "limit": {strconv.Itoa(limit)}}
	var rows []([]any)
	if err := c.getJSON(ctx, c.klinesPath(), q, &rows); err != nil {
		return nil, err
	}
	out := make([]KLine, 0, len(rows))
	for _, r := range rows {
		k, ok := parseBinanceKlineRow(r)
		if !ok {
			continue // 脏行丢弃：不拿 0 价冒充有效 K线
		}
		out = append(out, k)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("binance klines %s/%s: 返回 %d 行但无一条可解析（端点口径待实测？）", sym, interval, len(rows))
	}
	return out, nil
}
