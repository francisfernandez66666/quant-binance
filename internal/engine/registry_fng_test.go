// registry_fng_test.go §ENH-A1 装配面行为锁：未挂客户端 → ok=false 零外呼；
// 新鲜缓存 → 只读取不再打外部源；龄超 TTL → 异步踢一次刷新（同一分钟窗口内不重复踢）。
// English: locks for the §ENH-A1 registry face — no client ⇒ ok=false with zero egress,
// fresh cache ⇒ read-only, stale cache ⇒ exactly one throttled async refresh kick.
package engine

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fngStubServer 假 alternative.me：固定返回 value=42（Fear），计数访问次数。
func fngStubServer(t *testing.T, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"value":"42","value_classification":"Fear","timestamp":"1755900000"}],"error":null}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestFNGSnapshotNoClient 未装配（CRYPTO 关）：闭包在位也只报 ok=false，且绝不外呼。
func TestFNGSnapshotNoClient(t *testing.T) {
	r := &Registry{}
	v, cls, age, ok := r.FNGSnapshot()
	if ok || v != 0 || cls != "" || age != -1 {
		t.Fatalf("未装配期望 (0,\"\",-1,false)，got %v/%q/%v/%v", v, cls, age, ok)
	}
}

// TestFNGSnapshotFreshCache 新鲜缓存只读：两次快照外部命中次数恒 1（零重复外呼）。
func TestFNGSnapshotFreshCache(t *testing.T) {
	var hits atomic.Int64
	srv := fngStubServer(t, &hits)
	r := &Registry{}
	r.attachFNG()
	r.attachFNG() // 幂等：二次装配不重建客户端
	r.fngMu.Lock()
	c := r.fng
	r.fngMu.Unlock()
	if c == nil {
		t.Fatal("attachFNG 后客户端应在位")
	}
	c.Base = srv.URL
	if err := c.Refresh(); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("Refresh 期望外呼 1 次, got %d", hits.Load())
	}
	for i := 0; i < 3; i++ {
		v, cls, age, ok := r.FNGSnapshot()
		if !ok || v != 42 || cls != "Fear" || age < 0 {
			t.Fatalf("新鲜缓存快照异常: %v/%q/%v/%v", v, cls, age, ok)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("新鲜期不得再打外部源, hits=%d", hits.Load())
	}
}

// TestFNGSnapshotStaleKickThrottled 龄超 TTL → 踢一次异步刷新；同窗口内后续快照不重复踢。
func TestFNGSnapshotStaleKickThrottled(t *testing.T) {
	var hits atomic.Int64
	srv := fngStubServer(t, &hits)
	var nowNow atomic.Int64 // 虚拟时钟基准（unix 秒），测试手动拨表
	base := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	nowNow.Store(base.Unix())

	// 裸 Registry（无 HTTP 注入）也必须可挂 FNG 腿：attach 只建闭包模板，触网留到取数时
	r := &Registry{}
	r.attachFNG()
	r.fngMu.Lock()
	c := r.fng
	r.fngMu.Unlock()
	c.Base = srv.URL
	c.Now = func() time.Time { return time.Unix(nowNow.Load(), 0).UTC() }
	if err := c.Refresh(); err != nil {
		t.Fatal(err)
	}

	// 拨表 +7h：证据过期 → 快照如实返回旧值（ok=true, age≈7h）且异步踢新
	nowNow.Store(base.Add(7 * time.Hour).Unix())
	v, _, age, ok := r.FNGSnapshot()
	if !ok || v != 42 {
		t.Fatalf("踢发轮仍须如实返回旧证据: %v/%v", v, ok)
	}
	if age < 6*3600 {
		t.Fatalf("龄应≈7h，got %ds", age)
	}
	deadline := time.Now().Add(3 * time.Second)
	for hits.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if hits.Load() != 2 {
		t.Fatalf("过期证据应踢且只踢一次异步刷新, hits=%d", hits.Load())
	}
	// 再连打两轮：fngKickMinInterval 节流 + 刷新后龄回鲜，hits 必须停在 2
	for i := 0; i < 2; i++ {
		r.FNGSnapshot()
	}
	time.Sleep(100 * time.Millisecond)
	if hits.Load() != 2 {
		t.Fatalf("节流失效：hits=%d", hits.Load())
	}
}
