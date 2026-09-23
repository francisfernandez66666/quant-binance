// 文件职责：币安行情 feed 的**新鲜度出口**（Phase 3 第 1 项，PLAN §2.4/§7，§M2/§WS-C 口径）。
//
//  1. SymbolStalenessMs：逐票陈旧度毫秒。-1=该票从未出现在本源（"未知"），消费端禁止把 -1
//     当 0=新鲜用（§M2 反伪造新鲜度纪律）。币安是真事件流，逐票比 A 股"整轮一起刷"更准，
//     故这里不复用 Fetcher.StalenessMs 的全局口径；
//  2. Fresh：§WS-C 硬闸的便捷判定，未知一律 false（不新鲜）；
//  3. SpreadBps：买一卖一价差（bp）。StockInfo 没有 bid/ask 字段，盘口只留在 tick 层，
//     故价差从 lastTick 取；无盘口的流（miniTicker/美股 price）返回 -1=未知。
//
// English: per-symbol quote staleness in ms (-1 = never seen, never treated as fresh), a
// convenience freshness gate, and bid-ask spread read from the tick layer (StockInfo has no
// bid/ask fields).
package data

import (
	"strings"
	"time"
)

// SymbolStalenessMs 单票行情陈旧度（毫秒；-1=该票从未出现在本源，未知）。
// 事件时刻缺失时退回快照整体时间戳，保持"至少不是 0"的保守口径。
func (f *BinanceQuoteFeed) SymbolStalenessMs(symbol string) int64 {
	key := normalizeBinanceSymbol(symbol)
	f.mu.Lock()
	stamp, hasStamp := f.stamp[key]
	snap := f.snap
	now := f.opt.Now()
	f.mu.Unlock()
	if !hasStamp {
		if snap == nil || snap.Time.IsZero() {
			return -1
		}
		if _, has := snap.Stocks[key]; !has {
			return -1
		}
		stamp = snap.Time
	}
	d := now.Sub(stamp).Milliseconds()
	if d < 0 {
		return 0 // 源时间戳超前（交易所/本机时钟漂移）：按 0 处理，不返回负数
	}
	return d
}

// Fresh 单票是否仍在 maxAge 内（§WS-C 硬闸用）；未知（-1）判不新鲜。
func (f *BinanceQuoteFeed) Fresh(symbol string, maxAge time.Duration) bool {
	ms := f.SymbolStalenessMs(symbol)
	return ms >= 0 && ms <= maxAge.Milliseconds()
}

// SpreadBps 买一卖一价差（基点；-1=本源该票无盘口数据，未知）。
func (f *BinanceQuoteFeed) SpreadBps(symbol string) float64 {
	key := normalizeBinanceSymbol(symbol)
	f.mu.Lock()
	t, ok := f.lastTick[key]
	f.mu.Unlock()
	if !ok || t.Bid <= 0 || t.Ask <= 0 || t.Ask < t.Bid {
		return -1
	}
	mid := (t.Bid + t.Ask) / 2
	if mid <= 0 {
		return -1
	}
	return (t.Ask - t.Bid) / mid * 10000
}

// normalizeBinanceSymbol 标的键归一：去空格 + 大写，与解析层输出保持同一 key 口径。
func normalizeBinanceSymbol(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// pickPositive 优先取新值，新值为 0（源未给）则保留旧值——绝不把已有数抹成 0。
func pickPositive(newVal, oldVal float64) float64 {
	if newVal > 0 {
		return newVal
	}
	return oldVal
}
