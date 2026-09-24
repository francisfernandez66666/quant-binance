// alert_drive_test.go — §ALERTDRIVE（2026-09-24 抄榜批项5）评估节拍并发锁。
//
// 锁的对象：本批把 RunAlertEvaluation 从「只有 A股 scoreCycle 一个调用点」改成
// 「scoreCycle + 7×24 币安维护拍（cmd/quant/main.go bnMaint）两个调用点」。
// 原因不是锦上添花：§CN-MASTER 出厂把 A股总开关关成 false，那条唯一的评估链在缺省配置下根本不跑，
// 于是 §高-3 辛苦接好的告警出口变成「线接上了、没人合闸」。而 Alerter.states 是无锁 map，
// 两个调用点一旦并发进入就是 Go runtime 级 fatal（concurrent map writes），比不推送严重得多。
//
// 本文件用 -race 下的并发调用把这个防线钉住：8 个 goroutine 各自反复跑评估、同时有 goroutine
// 抖动量规；若哪天有人把包内 evalMu 摘掉，-race 会直接报出竞争（非 -race 时 Go runtime
// 对并发写 map 亦会 fatal 退出），而不是留下一个偶发的静默故障。
//
// English: concurrency lock for §ALERTDRIVE — alert evaluation is now driven by two call sites
// (the CN score cycle and the 24/7 binance maintenance tick), so the package must serialize it
// itself because Alerter.states is an unsynchronized map.
package metrics

import (
	"sync"
	"testing"
)

// TestRunAlertEvaluationConcurrentSafe 并发调用 RunAlertEvaluation 不得触碰无锁状态。
// 只在 -race 下有判定力（普通 go test 也跑，作为压测不报错即可）。
//
// 相位设计（刻意不靠"抖动量规碰运气"）：第一版让一个 goroutine 反复翻 breaker_active、
// 八个 goroutine 跑评估，结果单包连跑时扰动方在评估方被调度之前就跑完了 60 次翻转，
// 全场量规恒 0 → 零事件 → 断言"至少一条投递"假红（自伤形态第 N 次）。
// 现在改成两相：① 量规钉在破线值 1 → 全新评估器必然至少 fire 一次（谁先跑都一样）；
// ② 量规钉回 0 → 必发配对的 recover。并发压力由 8 个 goroutine 在每相内同时进入提供。
func TestRunAlertEvaluationConcurrentSafe(t *testing.T) {
	oldAlerter, oldRouter := globalAlerter, globalAlertRouter
	t.Cleanup(func() {
		SetAlertSink(nil)
		SetGauge("breaker_active", 0)
		globalAlerter, globalAlertRouter = oldAlerter, oldRouter
	})
	globalAlerter, globalAlertRouter = NewAlerter(), newAlertRouter(nil)

	var mu sync.Mutex
	var got []AlertDelivery
	SetAlertSink(func(d AlertDelivery) {
		mu.Lock()
		got = append(got, d)
		mu.Unlock()
	})

	// drive 起 n 个 goroutine 同时跑 rounds 轮评估（并发压力源）。
	drive := func(n, rounds int) {
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < rounds; j++ {
					RunAlertEvaluation()
				}
			}()
		}
		wg.Wait()
	}

	// 相①：持续破线 → 恰好一条 alert（评估器去重 + 路由冷却双保险，并发下也不许刷屏）。
	SetGauge("breaker_active", 1)
	drive(8, 30)
	mu.Lock()
	fires := countKind(got, "breaker_open", KindAlert)
	mu.Unlock()
	if fires != 1 {
		t.Fatalf("并发评估必须恰好出站 1 条 alert（0=链路没跑通，>1=去重/限频被并发打穿），got %d", fires)
	}

	// 相②：回落 → 必发配对销案（resolved），且不得再有孤立 alert。
	SetGauge("breaker_active", 0)
	drive(8, 30)
	mu.Lock()
	defer mu.Unlock()
	if n := countKind(got, "breaker_open", KindResolved); n != 1 {
		t.Fatalf("并发回落应恰好 1 条 resolved，got %d（全部投递 %+v）", n, got)
	}
	if n := countKind(got, "breaker_open", KindAlert); n != 1 {
		t.Fatalf("回落相不得再刷 alert，got %d", n)
	}
}
