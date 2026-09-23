// registry_events_test.go §ENH-A4/B8 事件腿装配面行为锁：
// ①未装配/非法市场 → ok=false 零外呼、不建腿；②首快照踢刷新、缓存=去重后批次、
// 新鲜期零重复外呼、重复 attach 幂等不清缓存；③TTL 过期踢发轮失败 → 旧批次原样保留；
// ④XEventsJSON 对外字段形状。测试全 mock（fetch 闭包注入），不触外网。
// English: locks for the event-leg assembly face — inert markets never call out, first
// snapshot kicks one refresh, dedup'd last-good cache survives failures, JSON shape fixed.
package engine

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"quant-trading-v2/internal/data"
)

// fakeXEvents 生成 n 条 URL 互不相同的候选（去重测试用 withDup 手动追加同链重投行）。
func fakeXEvents(n int) []data.XEvent {
	out := make([]data.XEvent, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, data.XEvent{
			Market: "US", Ticker: "MSFT", Title: fmt.Sprintf("8-K #%d", i),
			URL:         fmt.Sprintf("https://example.com/e%d", i),
			PublishedAt: time.Date(2026, 9, 23, 8, 0, i, 0, time.UTC),
		})
	}
	return out
}

// waitForSnapshot 轮询快照直到 ok 或超时（异步踢发落定需要等待，测试专用小循环）。
func waitForSnapshot(t *testing.T, r *Registry, market string) ([]data.XEvent, int64, bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if evs, age, ok := r.xeventsSnapshot(market); ok {
			return evs, age, true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, -1, false
}

// TestXEventsNotAttachedNoEgress 未装配市场（含被忽略的非法市场键）：ok=false、fetch 永不触达。
func TestXEventsNotAttachedNoEgress(t *testing.T) {
	r := &Registry{}
	var calls atomic.Int64
	r.attachXEventSource("CN", func() ([]data.XEvent, error) { calls.Add(1); return nil, nil })
	r.xevMu.Lock()
	_, hasCN := r.xevLegs["CN"]
	r.xevMu.Unlock()
	if hasCN {
		t.Fatal("非法市场 CN 不得建腿（市场收口在装配面）")
	}
	evs, age, ok := r.xeventsSnapshot("US")
	if ok || evs != nil || age != -1 {
		t.Fatalf("未装配期望 (nil,-1,false)，got %v/%v/%v", evs, age, ok)
	}
	if calls.Load() != 0 {
		t.Fatal("未装配不得外呼")
	}
}

// TestXEventsKickDedupeIdempotent 首快照踢刷新→去重批次入缓存；新鲜期重复快照零外呼；
// 重复 attach 不替换腿、不清缓存（引擎重建幂等）。
func TestXEventsKickDedupeIdempotent(t *testing.T) {
	var calls atomic.Int64
	r := &Registry{}
	rows := append(fakeXEvents(5), data.XEvent{}) // 第 6 条=第 3 条同日同链重投（去重键测样例）
	rows[5] = rows[2]
	r.attachXEventSource("US", func() ([]data.XEvent, error) {
		calls.Add(1)
		return rows, nil
	})
	if _, _, ok := r.xeventsSnapshot("US"); ok {
		t.Fatal("首刷在途轮必须 ok=false（无证据不造数）")
	}
	evs, age, ok := waitForSnapshot(t, r, "US")
	if !ok || age < 0 {
		t.Fatalf("首刷应成功: %v/%v", evs, ok)
	}
	if len(evs) != 5 {
		t.Fatalf("6 条含 1 组同日同链，去重后期望 5，got %d", len(evs))
	}
	before := calls.Load()
	r.xeventsSnapshot("US")
	r.xeventsSnapshot("US")
	if calls.Load() != before {
		t.Fatalf("新鲜期不得重复打源: %d→%d", before, calls.Load())
	}
	r.attachXEventSource("US", func() ([]data.XEvent, error) { return nil, errors.New("不得替换") })
	if got, _, isOK := r.xeventsSnapshot("US"); !isOK || len(got) != 5 {
		t.Fatalf("重复 attach 不得重置腿/缓存: %v/%v", got, isOK)
	}
}

// TestXEventsFailureKeepsCache TTL 过期踢发轮失败：本轮与后续都照常返回旧批次，
// arrivedAt 不被失败轮推进（不断供、不假新）。
func TestXEventsFailureKeepsCache(t *testing.T) {
	var fail atomic.Bool
	r := &Registry{}
	r.attachXEventSource("CRYPTO", func() ([]data.XEvent, error) {
		if fail.Load() {
			return nil, errors.New("源挂了")
		}
		return []data.XEvent{{Market: "CRYPTO", Symbol: "BTCUSDT", Title: "T", URL: "u1",
			PublishedAt: time.Now().UTC()}}, nil
	})
	evs, _, ok := waitForSnapshot(t, r, "CRYPTO")
	if !ok || len(evs) != 1 {
		t.Fatal("首轮应成功")
	}
	r.xevMu.Lock()
	leg := r.xevLegs["CRYPTO"]
	r.xevMu.Unlock()
	fail.Store(true)
	leg.mu.Lock()
	oldArrived := leg.arrivedAt
	leg.arrivedAt = time.Now().Add(-xeventRefreshTTL - time.Second) // 拨过 TTL 触发再踢
	leg.kickAt = time.Time{}                                        // 解开节流，保证本轮真踢
	leg.mu.Unlock()
	if got, _, isOK := r.xeventsSnapshot("CRYPTO"); !isOK || len(got) != 1 {
		t.Fatalf("踢发失败轮仍须返回旧批次: %v/%v", got, isOK)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		leg.mu.Lock()
		settled := !leg.arrivedAt.After(oldArrived) && len(leg.cached) == 1
		leg.mu.Unlock()
		if settled {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("失败轮不得刷新缓存 arrivedAt（旧证据龄不重置）")
}

// TestXEventsJSONShape 对外 JSON 面：未装配 (nil,-1,false)；成功批次字段齐 + RFC3339 UTC。
func TestXEventsJSONShape(t *testing.T) {
	r := &Registry{}
	if evs, age, ok := r.XEventsJSON("US"); ok || evs != nil || age != -1 {
		t.Fatalf("未装配市场期望 (nil,-1,false)")
	}
	r.attachXEventSource("US", func() ([]data.XEvent, error) { return fakeXEvents(2), nil })
	deadline := time.Now().Add(3 * time.Second)
	var evs []map[string]any
	for time.Now().Before(deadline) {
		if got, _, isOK := r.XEventsJSON("US"); isOK {
			evs = got
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(evs) != 2 {
		t.Fatalf("期望 2 条, got %d", len(evs))
	}
	for _, k := range []string{"market", "symbol", "ticker", "title", "url", "published_at"} {
		if _, has := evs[0][k]; !has {
			t.Fatalf("JSON 事件缺字段 %s: %+v", k, evs[0])
		}
	}
	if pa, isStr := evs[0]["published_at"].(string); !isStr || len(pa) < 20 {
		t.Fatalf("published_at 应为 RFC3339 串, got %v", evs[0]["published_at"])
	}
}
