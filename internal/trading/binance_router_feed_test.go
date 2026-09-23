// 文件职责：§P3 BrokerRouter feed 观测位单测（RegisterFeed/FeedStats 契约）——
// 锁四件事：① name 升序稳定输出（UI 行序不随 map 迭代抖动）；② 空 name / nil 闭包拒注册；
// ③ stats 闭包 panic 降级为「失活行」（Healthy=false、SilenceMs/Frames=-1 未知哨兵），
// 观测面绝不带崩 /api/binance/state；④ 同名重复注册覆盖（热替换不双计）。
// English: §P3 router feed-observability tests — sorted stable output, invalid registrations
// rejected, panicking stat closures degrade to an inert row, re-register overwrites.
package trading

import "testing"

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
