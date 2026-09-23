// 文件职责：交易规则查表的**缓存与刷新**（加市场第 3 项，配合 binance_symbol_rules.go 的类型层）。
// 设计约束：
//   - 一次 exchangeInfo 拉全市场（币安该端点本来就返回全量），按 symbol 建索引，避免逐票一问；
//   - TTL 缺省 6h；过期后首个查询触发同步刷新（single-flight：并发查询只有一个真的发请求，
//     其余等结果），不会把交易所打穿；
//   - 刷新失败时**保留旧值**并记节流日志：规则空表意味着 §9 证据闸无据可查，
//     "旧规则"比"没规则"安全，但消费方必须知道数据可能过期（LoadedAt 出口暴露）。
package data

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// BinanceRuleSource 规则查表器（对 Gate.SetSymbolRulesSource 的 data 侧等价物）。
type BinanceRuleSource struct {
	client *BinanceRestClient
	cache  *binanceRuleCache

	logMu sync.Mutex
	logAt time.Time
}

// NewBinanceRuleSource 构造查表器。client.Base 决定打哪个端点（镜像/生产/testnet/httptest），
// client.Equity 决定现货还是美股前缀；ttl<=0 取缺省 6h。
func NewBinanceRuleSource(client *BinanceRestClient, ttl time.Duration) *BinanceRuleSource {
	if client == nil {
		client = NewBinanceRestClient("", false)
	}
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	return &BinanceRuleSource{
		client: client,
		cache:  newBinanceRuleCache(ttl, time.Now),
	}
}

// SetClock 注入时钟（单测走可控 TTL；生产别调）。
func (s *BinanceRuleSource) SetClock(now func() time.Time) {
	if now != nil {
		s.cache.now = now
	}
}

// Rule 取单票规则：命中且未过期直接返回；否则触发刷新后再取。
// 找不到该 symbol 时返回 error（调用方据此拒单，而不是拿零值规则放行）。
func (s *BinanceRuleSource) Rule(ctx context.Context, symbol string) (BinanceSymbolRule, error) {
	key := normalizeBinanceSymbol(symbol)
	if key == "" {
		return BinanceSymbolRule{}, fmt.Errorf("binance rules: symbol 为空")
	}
	if r, ok := s.cache.fresh(key, s.cache.now()); ok {
		return r, nil
	}
	if err := s.Refresh(ctx); err != nil {
		if r, ok := s.cache.get(key); ok {
			return r, nil // 有旧值就用旧的（并下方记一条过期日志）
		}
		return BinanceSymbolRule{}, err
	}
	r, ok := s.cache.get(key)
	if !ok {
		return BinanceSymbolRule{}, symbolRuleError(key, "exchangeInfo 未包含该 symbol")
	}
	return r, nil
}

// Refresh 强制拉一次全量规则（single-flight；返回 error 表示本次未更新，旧值仍在表里）。
func (s *BinanceRuleSource) Refresh(ctx context.Context) error {
	now := s.cache.now()
	if !s.cache.tryBeginLoad(now) {
		// 别人在刷：等它完成（最多等一次请求超时），避免并发打交易所
		return s.cache.waitLoad(ctx)
	}
	raw, err := s.fetchExchangeInfo(ctx)
	s.cache.finishLoad(now)
	if err != nil {
		s.logThrottled("exchangeInfo 刷新失败（沿用旧规则表，LoadedAt=%s）: %v",
			s.cache.loadedAt.Format(time.RFC3339), err)
		return err
	}
	s.cache.store(parseBinanceExchangeInfo(raw), now)
	return nil
}

// LoadedAt 规则表最近一次成功刷新时刻（零值=从未成功；巡检/闸据此判证据新鲜度）。
func (s *BinanceRuleSource) LoadedAt() time.Time { return s.cache.loadedTime() }

// All 返回全表副本（排查用；不做 TTL 判断，调用方先看 LoadedAt）。
func (s *BinanceRuleSource) All() map[string]BinanceSymbolRule { return s.cache.snapshot() }

// fetchExchangeInfo 拉规则端点并解码为 map（美股/现货同一形状，差异在 filters 键）。
func (s *BinanceRuleSource) fetchExchangeInfo(ctx context.Context) (map[string]any, error) {
	var raw map[string]any
	if err := s.client.getJSON(ctx, s.client.exchangeInfoPath(), nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// logThrottled 同类故障日志 60s 一条（与 qmt_feed/binance_ws 同惯例，不含任何凭证）。
func (s *BinanceRuleSource) logThrottled(format string, args ...any) {
	now := s.cache.now()
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if !s.logAt.IsZero() && now.Sub(s.logAt) < time.Minute {
		return
	}
	s.logAt = now
	log.Printf("[binance-rules] "+format, args...)
}
