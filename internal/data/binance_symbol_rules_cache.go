// 文件职责：交易规则**缓存本体**（binance_symbol_rules_source.go 的数据面：TTL/索引/single-flight）。
// 单独成文件是因为这里的锁纪律最容易写错：loadedAt 与 rules 必须在同一把写锁里一起改，
// 否则读侧会看到"新时间戳 + 旧表"或反之（后者更糟：以为规则是新的）。
package data

import (
	"context"
	"time"
)

// fresh 命中且未过期才返回 true（过期项也返回值得知，但调用方要触发刷新）。
func (c *binanceRuleCache) fresh(symbol string, now time.Time) (BinanceSymbolRule, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.loadedAt.IsZero() || now.Sub(c.loadedAt) > c.ttl {
		return BinanceSymbolRule{}, false
	}
	r, ok := c.rules[symbol]
	return r, ok
}

// get 不看 TTL 地取值（刷新失败时沿用旧表的路径用）。
func (c *binanceRuleCache) get(symbol string) (BinanceSymbolRule, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.rules[symbol]
	return r, ok
}

// store 整表替换 + 一起写 loadedAt（同一把锁，见文件头锁纪律）。
func (c *binanceRuleCache) store(rules map[string]BinanceSymbolRule, now time.Time) {
	if len(rules) == 0 {
		return // 空表不覆盖旧表：一次拉回空 JSON 不该让证据消失
	}
	c.mu.Lock()
	c.rules = rules
	c.loadedAt = now
	c.mu.Unlock()
}

// snapshot 全表副本（外部只读排查用）。
func (c *binanceRuleCache) snapshot() map[string]BinanceSymbolRule {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]BinanceSymbolRule, len(c.rules))
	for k, v := range c.rules {
		out[k] = v
	}
	return out
}

// loadedTime 最近成功刷新时刻（零值=从未）。
func (c *binanceRuleCache) loadedTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.loadedAt
}

// tryBeginLoad 抢单次刷新令牌；false 表示已有协程在刷（调用方等结果，不重复发请求）。
func (c *binanceRuleCache) tryBeginLoad(_ time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loading {
		return false
	}
	c.loading = true
	return true
}

// finishLoad 释放令牌并唤醒等待者。
func (c *binanceRuleCache) finishLoad(_ time.Time) {
	c.mu.Lock()
	c.loading = false
	done := c.loadDone
	c.loadDone = make(chan struct{})
	c.mu.Unlock()
	if done != nil {
		close(done)
	}
}

// waitLoad 等正在进行的刷新结束（ctx 取消即返回 error，调用方退回旧表路径）。
// ⚠ loading 与 loadDone 必须**同一把读锁**里一起读：曾在 tryBeginLoad=false 与读
// loadDone 之间插入一次 finishLoad 的话，等待者拿到的是下一条永不关闭的信道，
// 只能空等到 ctx 超时（Agent B 缺陷③修复——同锁读取后：无人刷即立即返回，
// 有人刷则该信道必被其 finishLoad 关闭）。
func (c *binanceRuleCache) waitLoad(ctx context.Context) error {
	c.mu.RLock()
	loading := c.loading
	done := c.loadDone
	c.mu.RUnlock()
	if !loading {
		return nil // 空闲：上一轮已收尾，直接走调用方的重查/旧表路径
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// newBinanceRuleCache 构造缓存（ttl<=0 取 6h；now 注入给单测走可控 TTL）。
func newBinanceRuleCache(ttl time.Duration, now func() time.Time) *binanceRuleCache {
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	if now == nil {
		now = time.Now
	}
	return &binanceRuleCache{
		ttl:      ttl,
		now:      now,
		rules:    map[string]BinanceSymbolRule{},
		loadDone: make(chan struct{}),
	}
}
