// 文件职责：§P3 BrokerRouter feed 观测位单测（RegisterFeed/FeedStats 契约）——
// 锁四件事：① name 升序稳定输出（UI 行序不随 map 迭代抖动）；② 空 name / nil 闭包拒注册；
// ③ stats 闭包 panic 降级为「失活行」（Healthy=false、SilenceMs/Frames=-1 未知哨兵），
// 观测面绝不带崩 /api/binance/state；④ 同名重复注册覆盖（热替换不双计）。
// §ENH-X2 追加第五锁：ShutdownLive 停机收摊（TestRouterFeedShutdownX2）。
// English: §P3 router feed-observability tests — sorted stable output, invalid registrations
// rejected, panicking stat closures degrade to an inert row, re-register overwrites; plus
// §ENH-X2 shutdown fan-out coverage.
package trading

import (
	"sync"
	"testing"
)

func TestRouterFeedStatsP3(t *testing.T) {
	r := NewBrokerRouter(nil)
	// 非法注册双形态：空名 / nil 闭包，都不应进表。
	r.RegisterFeed("", "US", func() FeedStat { return FeedStat{} })
	r.RegisterFeed("nil-fn", "US", nil)
	if got := r.FeedStats(); len(got) != 0 {
		t.Fatalf("非法注册不得入表: %+v", got)
	}
	// 正常位 + 炸弹位：同名二次注册覆盖为「健康」。
	r.RegisterFeed("quotes-crypto", "CRYPTO", func() FeedStat {
		return FeedStat{Market: "CRYPTO", Healthy: true, SilenceMs: 120, Frames: 9}
	})
	r.RegisterFeed("status-us", "US", func() FeedStat { panic("stats boom") })
	r.RegisterFeed("quotes-crypto", "CRYPTO", func() FeedStat {
		return FeedStat{Market: "CRYPTO", Healthy: true, SilenceMs: 0, Frames: 42} // 覆盖：帧数只增不双计
	})
	got := r.FeedStats()
	if len(got) != 2 {
		t.Fatalf("应恰有 2 条 feed（覆盖后不双计）: %+v", got)
	}
	if got[0].Name != "quotes-crypto" || got[1].Name != "status-us" {
		t.Fatalf("输出须按 name 升序: %s, %s", got[0].Name, got[1].Name)
	}
	if got[0].Frames != 42 || !got[0].Healthy {
		t.Fatalf("同名注册应覆盖为最新闭包: %+v", got[0])
	}
	// panic 降级行：名称回填 + 失活哨兵（-1=未知，消费端禁当 0 用）。
	if got[1].Healthy || got[1].SilenceMs != -1 || got[1].Frames != -1 {
		t.Fatalf("panic 闭包应降级为失活行: %+v", got[1])
	}
}

// TestRouterFeedShutdownX2 §ENH-X2 停机链单测：RegisterFeedWithStop 的 stop 收纳与
// ShutdownLive 收摊语义——①每条带 stop 的 feed 恰好被调用；②nil stop（旧 RegisterFeed
// 语义）不参与也不炸；③单腿 stop panic 被隔离、其余腿照常关停（一扇门炸不封整条停机链）；
// ④同名覆盖时旧 stop 不残留（只执行最后一次注册的闭包）；⑤二次 ShutdownLive 安全。
func TestRouterFeedShutdownX2(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]int{}
	count := func(name string) func() {
		return func() {
			mu.Lock()
			hits[name]++
			mu.Unlock()
		}
	}
	r := NewBrokerRouter(nil)
	r.RegisterFeed("legacy-nil", "US", func() FeedStat { return FeedStat{} }) // 旧入口=无停止义务
	r.RegisterFeedWithStop("quotes-us", "US", func() FeedStat { return FeedStat{} }, count("quotes-us"))
	// 同名二次注册覆盖：旧 stop（first）不得残留执行。
	r.RegisterFeedWithStop("quotes-us", "US", func() FeedStat { return FeedStat{} }, count("quotes-us-final"))
	r.RegisterFeedWithStop("status-us", "US", func() FeedStat { return FeedStat{} }, func() { panic("stop boom") })
	r.RegisterFeedWithStop("quotes-crypto", "CRYPTO", func() FeedStat { return FeedStat{} }, count("quotes-crypto"))

	r.ShutdownLive()
	mu.Lock()
	// 快照后立即释锁：ShutdownLive 同步执行 stop 闭包，持锁再调会与 count() 的
	// mu.Lock 自锁死（hits 断言只读快照值，临界区内不再触发第二轮回摊）。
	snap := map[string]int{}
	for k, v := range hits {
		snap[k] = v
	}
	mu.Unlock()
	if snap["quotes-us"] != 0 {
		t.Fatalf("旧 stop 不得残留（同名覆盖后仍被调=泄漏）: %d", snap["quotes-us"])
	}
	if snap["quotes-us-final"] != 1 || snap["quotes-crypto"] != 1 {
		t.Fatalf("带 stop 的 feed 须各关一次: %+v", snap)
	}
	// panic 腿之后的 quotes-crypto 仍被关到（上面断言已证）；status-us 的 panic 被吞掉不外抛。
	// 二次调用安全：feed 集合不变，stop 再各执行一次（幂等义务在 feed 本体，路由层不吞二次调用）。
	r.ShutdownLive()
	mu.Lock()
	defer mu.Unlock()
	if hits["quotes-us-final"] != 2 || hits["quotes-crypto"] != 2 {
		t.Fatalf("二次 ShutdownLive 应再收摊一轮（feed 本体自证幂等）: %+v", hits)
	}
}
