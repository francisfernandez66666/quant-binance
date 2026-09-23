// registry_events.go §ENH-A4/B8 事件血源腿的进程级装配面（EDGAR=US、CryptoPanic=CRYPTO）。
//
// 姿势与 registry_fng.go 同族（懒踢刷新 + 节流 + 失败保旧缓存）：
//   - 装配点=引擎构建的 US/CRYPTO 分支，仅当配置给了凭证（EDGAR UA / CryptoPanic key）
//     才 attachXEventSource——空键=整腿不装配=快照恒 ok=false（零配置零行为）；
//   - fetch 闭包即「构造注入源闭包」面（PLAN §2 汇入行）：战法链后续（Phase 5 派发批）
//     从这里拿候选，不改装配代码；本批消费面=观测（/api/binance/state "events" 节）；
//   - 缓存：每市场一份去重后的最近成功批次（data.DedupeEvents 的 URL+日键幂等），
//     失败轮保留旧证据——观测面读到的永远是"最近一次真候选"而不是崩溃链；
//   - 密钥红线：token 只存在于客户端闭包内，快照/日志/错误串零密钥面（客户端已自证）。
//
// English: §ENH-A4/B8 process-wide assembly for the event legs (EDGAR=US, CryptoPanic=
// CRYPTO): lazy throttled refresh, dedup'd last-good cache per market, empty credential ⇒
// leg never built; this batch's consumer is the state page, strategy dispatch stays Phase 5.
package engine

import (
	"log"
	"sync"
	"time"

	"quant-trading-v2/internal/data"
)

// xeventRefreshTTL 事件证据新鲜期：热帖/8-K 都是小时级节奏，15 分钟重取足够观测面使用。
const xeventRefreshTTL = 15 * time.Minute

// xeventKickMinInterval 异步刷新踢发最小间隔（快照轮询再密也不放大外呼）。
const xeventKickMinInterval = 60 * time.Second

// xeventCap 每市场快照保留条数上限（观测面只展示最近一批，防响应体无界膨胀）。
const xeventCap = 30

// xeventLeg 单市场事件腿：注入源闭包 + 最近成功批次缓存。
type xeventLeg struct {
	mu        sync.Mutex
	fetch     func() ([]data.XEvent, error) // 构造注入源闭包（EDGAR/CryptoPanic 客户端）
	cached    []data.XEvent                 // 最近一次成功批次（已去重、已截断）
	arrivedAt time.Time                     // 成功时刻（Age 分母）
	kickAt    time.Time                     // 最近踢发时刻（节流）
	now       func() time.Time              // 测试注入时钟；nil=time.Now
}

// attachXEventSource 引擎构建装配点：幂等挂腿（已有则不动、保留在跑缓存）。
// market 只接受 "US"/"CRYPTO"（其余忽略——调用分支已收口，防御性兜底）。
func (r *Registry) attachXEventSource(market string, fetch func() ([]data.XEvent, error)) {
	if market != "US" && market != "CRYPTO" || fetch == nil {
		return
	}
	r.xevMu.Lock()
	defer r.xevMu.Unlock()
	if r.xevLegs == nil {
		r.xevLegs = make(map[string]*xeventLeg)
	}
	if _, ok := r.xevLegs[market]; !ok {
		r.xevLegs[market] = &xeventLeg{fetch: fetch}
	}
}

// xeventsSnapshot 市场事件快照（内部面）：新鲜缓存零外呼；过期踢异步刷新，
// 本轮如实返回旧批次或 ok=false（HTTP 读路径绝不等外源）。
func (r *Registry) xeventsSnapshot(market string) ([]data.XEvent, int64, bool) {
	r.xevMu.Lock()
	leg := r.xevLegs[market]
	r.xevMu.Unlock()
	if leg == nil {
		return nil, -1, false // 未装配（凭证空/市场链关）：无证据不造数
	}
	// 惰性节流决策（锁内一次完成）：距上批超 TTL 且距上次踢发超最小间隔才踢刷新一轮；
	// 本轮仍回旧缓存，绝不为 HTTP 请求同步触网。
	var kick bool
	leg.mu.Lock()
	now := time.Now
	if leg.now != nil {
		now = leg.now
	}
	if leg.arrivedAt.IsZero() || now().Sub(leg.arrivedAt) > xeventRefreshTTL {
		if now().Sub(leg.kickAt) >= xeventKickMinInterval {
			leg.kickAt = now()
			kick = true
		}
	}
	cached, arrived := leg.cached, leg.arrivedAt
	leg.mu.Unlock()
	if kick {
		go func() {
			evs, err := leg.fetch()
			if err != nil {
				log.Printf("[engine] 事件腿 %s 刷新失败（保留旧批次）: %v", market, err)
				return
			}
			deduped := data.DedupeEvents(evs)
			if len(deduped) > xeventCap {
				deduped = deduped[len(deduped)-xeventCap:] // 尾部=较新（两客户端都按时间正序产出）
			}
			leg.mu.Lock()
			leg.cached, leg.arrivedAt = deduped, time.Now()
			if leg.now != nil {
				leg.arrivedAt = leg.now()
			}
			leg.mu.Unlock()
		}()
	}
	if arrived.IsZero() {
		return nil, -1, false // 首刷在途/从未成功
	}
	ageSec := int64(time.Since(arrived).Round(time.Second) / time.Second)
	if ageSec < 0 {
		ageSec = 0
	}
	out := make([]data.XEvent, len(cached))
	copy(out, cached) // 值拷贝快照：调用方排序/截断不得扰动腿内缓存
	return out, ageSec, true
}

// XEventsJSON 对外快照面（server.SetXEventsSource 闭包的实现，main.go 装配）：
// 返回可直接进 JSON 的事件列表（字段口径见 data.XEvent 文件头）。
func (r *Registry) XEventsJSON(market string) (events []map[string]any, ageSec int64, ok bool) {
	evs, age, ok := r.xeventsSnapshot(market)
	if !ok {
		return nil, age, false
	}
	events = make([]map[string]any, 0, len(evs))
	for _, ev := range evs {
		events = append(events, map[string]any{
			"market":       ev.Market,
			"symbol":       ev.Symbol,
			"ticker":       ev.Ticker,
			"title":        ev.Title,
			"url":          ev.URL,
			"published_at": ev.PublishedAt.UTC().Format(time.RFC3339),
		})
	}
	return events, age, true
}
