// 文件职责：币安行情 feed 的**显式并池注入** MergeInto（Phase 3 第 1 项，GAP_BINANCE_READINESS
// §G-8 + §ENH-5 QMTFeed.applyTicks 同纪律）。
//
// 默认**不并池**：GAP §G-8 要求币安 universe 与 A 股 5s 监控池分轨——混池会让新浪/腾讯链对
// BTCUSDT/AAPL 打空、拉低源健康度，还会让 §WS-C 陈旧闸读到混合口径。只有装配层显式调用
// MergeInto 才把币安票写进 Fetcher（例如"用同一套策略循环跑美股"的实验场景）。
//
// 三条反伪造新鲜度纪律（照抄 qmt_feed.go:158-217 的姿势）：
//  1. Fetcher.Snapshot() 是浅拷贝、*StockInfo 与内部快照共享指针 → 改价前必须复制 struct；
//  2. 只有真命中 >=1 票才调 IngestSnapshot（该调用会刷 lastOK，无条件注入=给断流造假新鲜，
//     会误解除 §WS-C 陈旧行情闸）；CN 基线快照为 nil 时直接返回 0，不注入空壳；
//  3. Name/Sector/NetInflow/HasFlow 等币安不携带的字段保留原值，0 值字段用 pickPositive 兜底，
//     绝不把已有的新浪开盘价/成交量抹成 0。
//
// English: opt-in merge of Binance-sourced symbols into the CN 5s fetcher. Copies StockInfo before
// mutation (Snapshot shares pointers), injects only on real hits (IngestSnapshot refreshes lastOK),
// and preserves fields the exchange feed does not carry.
package data

import (
	"time"
)

// MergeInto 把本源已见且未超龄的标的合并进 CN Fetcher，返回注入票数（0=未注入，未触碰 lastOK）。
func (f *BinanceQuoteFeed) MergeInto(target *Fetcher) int {
	if target == nil {
		return 0
	}
	base := target.Snapshot()
	if base == nil {
		return 0 // CN 首轮采集未跑：等新浪链建立基线，不注入空壳快照
	}
	f.mu.Lock()
	maxAge := f.opt.MaxTickAge
	now := f.opt.Now()
	src := f.SourceTag()
	copies := make([]*StockInfo, 0, len(f.stamp))
	for sym, stamp := range f.stamp {
		if now.Sub(stamp) > maxAge {
			continue // 超龄票不覆盖已有价（币安断流重连会回放旧帧）
		}
		if si := f.stockCopyLocked(sym); si != nil {
			copies = append(copies, si)
		}
	}
	f.mu.Unlock()
	// 本轮没有在龄票：直接 0，绝不空手调 IngestSnapshot（那会给断流造假新鲜、误解除 §WS-C 闸）。
	if len(copies) == 0 {
		return 0
	}
	// 基线快照缺 map 时按票数控容建表（首次并池的空池场景）。
	if base.Stocks == nil {
		base.Stocks = make(map[string]*StockInfo, len(copies))
	}
	// 逐票字段级合并：币安流不携带的字段（Name/Sector/资金流）保留 CN 原值，0 值用 pickPositive 兜底。
	hits := 0
	for _, si := range copies {
		old := base.Stocks[si.Code]
		cp := *si
		if old != nil {
			merged := *old // 保留 Name/Sector/资金流等币安不携带的字段
			merged.Price = cp.Price
			merged.Close = cp.Close
			merged.ChangePct = cp.ChangePct
			merged.Open = pickPositive(cp.Open, merged.Open)
			merged.High = pickPositive(cp.High, merged.High)
			merged.Low = pickPositive(cp.Low, merged.Low)
			merged.PrevClose = pickPositive(cp.PrevClose, merged.PrevClose)
			merged.Volume = pickPositive(cp.Volume, merged.Volume)
			merged.Amount = pickPositive(cp.Amount, merged.Amount)
			cp = merged
		}
		// 写回的是独立副本指针：base 里其它票仍与内部快照共享，不许原地改。
		base.Stocks[si.Code] = &cp
		hits++
	}
	if hits == 0 {
		return 0
	}
	// 时钟未注入（构造绕过）时兜底真实时间，快照 Time 不许留零值。
	if now.IsZero() {
		now = time.Now()
	}
	// 整表一次性注入（与 5s Fetcher"整轮一起刷"同构，避免逐票 push 撕裂读侧）。
	target.IngestSnapshot(&MarketSnapshot{
		Stocks: base.Stocks,
		Sector: base.Sector,
		Time:   now,
		Source: src,
	})
	// ⚠ §M1 契约：Source 写成 BINANCE-SPOT/BINANCE-STK 前，请先在 internal/data/source.go 登记
	// QuoteSource* 常量并入 AllQuoteSources()，再 -update 重新生成 golden，
	// 否则 quote_sources_contract_test.go 与 /api/status 白名单巡检会红。
	f.logThrottled("已并池 %d 票进 CN 监控池（Source=%s，GAP §G-8 显式模式）", hits, src)
	return hits
}

// stockCopyLocked 持锁状态下取某票的独立副本（避免调用方改到已发布快照的共享指针）。
func (f *BinanceQuoteFeed) stockCopyLocked(symbol string) *StockInfo {
	if f.snap == nil {
		return nil
	}
	si := f.snap.Stocks[symbol]
	if si == nil {
		return nil
	}
	cp := *si
	return &cp
}
