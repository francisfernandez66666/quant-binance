// 文件职责：币安行情 feed 的**快照装配层**（Phase 3 第 1 项，PLAN §2.4/§7）。
// 帧入口（binance_quotes_io.go）只写 pending 缓冲，本文件按 FlushInterval 节拍把整批 tick
// 一次性装配成仓库既有的 MarketSnapshot 形状：
//   - Stocks 以 **symbol 大写** 为 key（与 A 股 code 同构，策略/风控读侧无需分支）；
//   - Time=本轮刷新时刻、Source=BINANCE-SPOT / BINANCE-STK（源标签即 §WS-C 白名单项）；
//   - per-symbol 事件时刻另记 stamp（币安是真流，逐票新鲜度比 A 股"整轮一起刷"更精确），
//     出口 SymbolStalenessMs 沿用 §M2 口径：-1=从未见过该票（未知），消费端禁止当 0 用。
//
// 两条反假绿纪律：① StockInfo 与已发布快照共享指针，改价前必须先复制 struct（-race 可见）；
// ② PrevClose 只在源真给参考价时才填（§P1-5），Close 一律按"最新价"口径写，两者不混塞。
//
// English: snapshot assembly for the Binance feed. Ticks buffered by the frame entry are turned
// into the repo's MarketSnapshot shape once per flush tick; keys are upper-case symbols, source
// is BINANCE-SPOT/BINANCE-STK, and StockInfo entries are copied before mutation because published
// snapshots share pointers.
package data

import (
	"time"
)

// flushLoop 快照刷新循环：节拍 FlushInterval（缺省 1s），Stop 时收尾一轮后退出。
func (f *BinanceQuoteFeed) flushLoop() {
	ticker := time.NewTicker(f.opt.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-f.stopCh:
			f.Flush() // 停机收尾：把最后一批 tick 落进快照，别丢数据
			return
		case <-ticker.C:
			f.Flush()
		}
	}
}

// Flush 把 pending 批量并入快照并整体替换，返回本轮落地的票数（0=无新帧，快照不动）。
// 刻意"整批一次替换 + 回调在锁外"：读侧要么看到旧快照要么看到新快照，不会看到半成品。
func (f *BinanceQuoteFeed) Flush() int {
	f.mu.Lock()
	if len(f.pending) == 0 {
		f.mu.Unlock()
		return 0
	}
	now := f.opt.Now()
	stocks := make(map[string]*StockInfo, len(f.pending)+8)
	if f.snap != nil { // 先复制旧 map：本轮没更新的票保持原值（绝不删票）
		for k, v := range f.snap.Stocks {
			stocks[k] = v
		}
	}
	hits := 0
	for sym, t := range f.pending {
		f.lastTick[sym] = t
		ev := binanceTickTime(t, now)
		f.stamp[sym] = ev
		old := stocks[sym]
		cp := StockInfo{Code: sym}
		if old != nil {
			cp = *old // 复制后再改：与已发布快照共享的指针不能原地写
		}
		applyBinanceTick(&cp, t)
		stocks[sym] = &cp
		hits++
	}
	delete0 := len(f.pending)
	f.pending = map[string]binanceTick{}
	snap := &MarketSnapshot{Stocks: stocks, Time: now}
	// §M1 契约：快照源名必须走 **snap.Source = QuoteSource*/BinanceQuoteSource* 常量** 的
	// 赋值写法——AST 双向锁只认"快照变量 .Source 赋值 + 常量右值"这一形态（复合字面量里的
	// 动态表达式 f.SourceTag() 会被判绕开契约）（Agent B 缺陷⑥收口，与 qmt_feed.go 同姿势）。
	if NormalizeMarketKey(f.opt.Market) == "CRYPTO" {
		snap.Source = BinanceQuoteSourceSpot
	} else {
		snap.Source = BinanceQuoteSourceUS
	}
	f.snap = snap
	cb := f.opt.OnSnapshot
	f.flushes.Add(1)
	f.mu.Unlock()
	if hits > 0 && delete0 > 0 {
		f.logThrottled("快照刷新: 本轮 %d 票, 累计刷新 %d 轮, Source=%s", hits, f.flushes.Load(), snap.Source)
	}
	if cb != nil {
		cb(snap)
	}
	return hits
}

// applyBinanceTick 把 tick 写进 StockInfo（tick 不携带的字段一律保留原值，不清零）。
// ChangePct 优先用源值；源没给且有可信参考价则本地折算，绝不凭空造数。
func applyBinanceTick(si *StockInfo, t binanceTick) {
	si.Price = t.Price
	si.Close = t.Price // 本仓库 Close 常用作"最新价"口径（§P1-5 歧义字段，昨收看 PrevClose）
	if t.Open > 0 {
		si.Open = t.Open
	}
	if t.High > 0 {
		si.High = t.High
	}
	if t.Low > 0 {
		si.Low = t.Low
	}
	// 其余量价字段：tick 携带（>0）才覆盖，0=该帧未给 → 保留原值，绝不抹零。
	if t.PrevClose > 0 {
		si.PrevClose = t.PrevClose
	}
	if t.Volume > 0 {
		si.Volume = t.Volume
	}
	if t.Amount > 0 {
		si.Amount = t.Amount
	}
	// 涨跌幅两档：源值优先；源没给但有可信昨收才本地折算，两者全无则不动既有值（不造数）。
	switch {
	case t.ChangePct != 0:
		si.ChangePct = t.ChangePct
	case si.PrevClose > 0:
		si.ChangePct = (t.Price/si.PrevClose - 1) * 100
	}
}

// Snapshot 返回当前快照浅拷贝（结构体 + map 逐键复制，*StockInfo 按"存入后不可变"共享）。
// 未刷出任何一轮时返回 nil——与 Fetcher.Snapshot() 行为一致，调用方的判空分支不用改。
func (f *BinanceQuoteFeed) Snapshot() *MarketSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snap == nil {
		return nil
	}
	cp := *f.snap
	cp.Stocks = make(map[string]*StockInfo, len(f.snap.Stocks))
	for k, v := range f.snap.Stocks {
		cp.Stocks[k] = v
	}
	return &cp
}

// Quote 取单票最新行情（返回副本指针，策略侧读一个标的用）。nil=从未见过。
func (f *BinanceQuoteFeed) Quote(symbol string) *StockInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snap == nil {
		return nil
	}
	si, ok := f.snap.Stocks[normalizeBinanceSymbol(symbol)]
	if !ok || si == nil {
		return nil
	}
	cp := *si
	return &cp
}

// WatchSymbols 已见过行情的标的列表（大写，无序）；供巡检打印"池子有没有真的活"。
func (f *BinanceQuoteFeed) WatchSymbols() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.watchSymbolsOf(f.snap)
}
