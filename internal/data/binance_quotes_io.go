// 文件职责：币安行情 feed 的**通道出口与帧入口**（Phase 3，PLAN §6.9 姿势）。
// 本文件只做两件事：
//  1. 生命周期/健康出口：Start/Stop、Healthy()/SilenceMs()（§6.9 静默 >90s 判不健康，判定在
//     底层 BinanceWS）、Reconnects/Stats（巡检判"到底有没有数据"，防"订阅成功但零命中"假绿）；
//  2. 帧入口 HandlePayload：解析 → 过滤（不可用/超龄）→ 落 pending 缓冲，**不改快照**。
//     快照装配在下一个 Flush 节拍一次性完成（binance_quotes_snap.go），避免每帧改快照导致
//     读侧撕裂与锁风暴（与 5s Fetcher"整轮一起刷"同构）。
//
// 两条反伪造新鲜度纪律（照抄 §ENH-5 QMTFeed / §M2 姿势）：超龄 tick（缺省 >30s）丢弃——
// 断流重连后币安会回放旧帧，旧价盖新价比没价更危险；0 价/无 symbol 只计数不注入。
//
// English: lifecycle/health exits and the frame entry point of the Binance quote feed.
// Frames only buffer into `pending`; snapshot assembly happens on the Flush tick. Stale or
// invalid ticks are dropped and counted, never written.
package data

import (
	"log"
	"strings"
	"time"
)

// Start 启动 WS 通道 + 快照刷新循环（非阻塞）。
func (f *BinanceQuoteFeed) Start() {
	f.ws.Start()
	go f.flushLoop()
}

// Stop 幂等停止（关刷新循环并收摊底层通道）。
func (f *BinanceQuoteFeed) Stop() {
	f.stopOnce.Do(func() {
		close(f.stopCh)
		f.ws.Stop()
	})
}

// Healthy 通道健康位（§6.9）：已连接且静默未超阈值；false 时消费方应退回 REST 兜底/轮询。
func (f *BinanceQuoteFeed) Healthy() bool { return f.ws.Healthy() }

// SilenceMs WS 静默毫秒（-1=从未有活动，未知）；阈值缺省 BinanceWSDefaultSilenceLimit=90s。
func (f *BinanceQuoteFeed) SilenceMs() int64 { return f.ws.SilenceMs() }

// Reconnects 累计重连次数（/api/binance/state 的 WS 健康卡可直接展示）。
func (f *BinanceQuoteFeed) Reconnects() int64 { return f.ws.Reconnects() }

// URL 实际订阅 URL（排障用，不含任何凭证）。
func (f *BinanceQuoteFeed) URL() string { return f.ws.opt.URL }

// SourceTag 本源市场标签（BINANCE-SPOT / BINANCE-STK）——MarketSnapshot.Source 的取值。
func (f *BinanceQuoteFeed) SourceTag() string {
	if NormalizeMarketKey(f.opt.Market) == "CRYPTO" {
		return BinanceQuoteSourceSpot
	}
	return BinanceQuoteSourceUS
}

// Stats 返回 (收帧数, 刷新轮数, 丢弃超龄 tick 数, 丢弃非法帧数)。
func (f *BinanceQuoteFeed) Stats() (recv, flushes, dropStale, dropInvalid int64) {
	return f.recv.Load(), f.flushes.Load(), f.dropStale.Load(), f.dropInvalid.Load()
}

// HandlePayload 解析一帧并写入待刷缓冲，返回命中的 tick 数（测试可直调，无需 socket）。
// 解析失败只计数 + 节流日志：一个坏帧不该让整条通道掉线（订阅回执/心跳即此形态）。
func (f *BinanceQuoteFeed) HandlePayload(payload []byte) int {
	f.recv.Add(1)
	ticks, err := f.parse(payload)
	if err != nil {
		f.dropInvalid.Add(1)
		f.logThrottled("行情帧解析失败（已丢弃，累计 %d）: %v", f.dropInvalid.Load(), err)
		return 0
	}
	now := f.opt.Now()
	n := 0
	f.mu.Lock()
	for _, t := range ticks {
		if !t.valid() {
			f.dropInvalid.Add(1)
			continue
		}
		if now.Sub(binanceTickTime(t, now)) > f.opt.MaxTickAge {
			f.dropStale.Add(1) // 超龄 tick：退回 REST，绝不用旧价盖新价
			continue
		}
		// ⚠ 写入键必须走 normalizeBinanceSymbol：读侧（Quote/SymbolStalenessMs/快照消费方）
		// 一律大写归一查询，写侧若按解析层原样入表，自定义 Parse 吐出小写 symbol 就会造成
		// "写小写键、读大写键"的不对称——tick 永远查不到（Agent B 缺陷②修复）。
		f.pending[normalizeBinanceSymbol(t.Symbol)] = t
		n++
	}
	f.mu.Unlock()
	return n
}

// binanceTickTime tick 的事件时间；源未给（0/非法）时用落地时间兜底，绝不当"更旧"处理。
func binanceTickTime(t binanceTick, now time.Time) time.Time {
	if t.TimeMs > 0 {
		return time.UnixMilli(t.TimeMs)
	}
	return now
}

// logThrottled feed 专属错误日志节流（60s 一条），与 qmt_feed 同惯例；不落任何凭证。
func (f *BinanceQuoteFeed) logThrottled(format string, args ...any) {
	nowSec := f.opt.Now().Unix()
	prev := f.lastLog.Swap(nowSec)
	if prev == 0 || nowSec-prev >= 60 {
		log.Printf("[binance-quotes:"+NormalizeMarketKey(f.opt.Market)+"] "+format, args...)
	}
}

// watchSymbolsOf 快照 key 列表（内部工具；大小写已在解析层统一为大写）。
func (f *BinanceQuoteFeed) watchSymbolsOf(snap *MarketSnapshot) []string {
	if snap == nil {
		return nil
	}
	out := make([]string, 0, len(snap.Stocks))
	for k := range snap.Stocks {
		out = append(out, strings.ToUpper(k))
	}
	return out
}
