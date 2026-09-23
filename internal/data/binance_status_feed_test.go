// 文件职责：美股交易状态流装配层（binance_status_feed.go）的行为锁单测。
// 三条锁：① 构造期拒空（Symbols 空/全空白必须报错，绝不起空池假活通道）；
// ② 订阅 URL 形状（每票两态流、小写流名、组合流基址可注入）；
// ③ GateSource 三态语义（无证据=(false,false)、NORMAL=(true,false)、HALTED=(true,true)），
//
//	并锁"fail-close 判定在闸侧不在源侧"——本文件只吐事实，不替 §9 闸做放行决定。
//
// 零网络：Dial 注入必死假拨号且从不调 Start()；证据经 HandlePayload 直投。
package data

import (
	"testing"
	"time"
)

// TestBinanceStatusFeedLocksP3 §P3 状态流装配锁。
func TestBinanceStatusFeedLocksP3(t *testing.T) {
	// ① 空/全空白 Symbols：构造必须报错（闸保持惰性的唯一正门是"不装配"）。
	if _, err := NewBinanceStatusFeed(BinanceStatusFeedOptions{Dial: binanceTestDeadDial}); err == nil {
		t.Fatal("Symbols 为空必须构造报错")
	}
	if _, err := NewBinanceStatusFeed(BinanceStatusFeedOptions{
		Symbols: []string{" ", "\t"}, Dial: binanceTestDeadDial}); err == nil {
		t.Fatal("Symbols 全空白必须构造报错（禁止空池订阅）")
	}
	// ② 订阅 URL：AAPL + brk.b 两票 → 四条小写流，tradingStatus 在前 tradability 在后。
	clock := newBinanceTestClock(binanceTestEpoch)
	f, err := NewBinanceStatusFeed(BinanceStatusFeedOptions{
		Symbols: []string{"AAPL", "brk.b"}, WsBase: "wss://test.local/equity",
		Dial: binanceTestDeadDial, TTL: time.Minute, Now: clock.now})
	if err != nil {
		t.Fatalf("合法配置应构造成功: %v", err)
	}
	want := "wss://test.local/equity/stream?streams=aapl@tradingstatus/aapl@tradability/" +
		"brk.b@tradingstatus/brk.b@tradability"
	if got := f.ws.opt.URL; got != want {
		t.Fatalf("订阅 URL got=%q want=%q", got, want)
	}
	// ③ GateSource 三态：初始无证据 → (false,false)（闸据此 fail-close 拒单）。
	src := f.GateSource()
	if has, halted := src("aapl"); has || halted {
		t.Fatalf("无证据应回 (false,false), got=(%v,%v)", has, halted)
	}
	// 投一帧 HALTED：变 (true,true)。
	if n := f.Store().HandlePayload([]byte(`{"stream":"AAPL@tradingStatus","data":{"S":"AAPL","tradingStatus":"HALTED"}}`)); n != 1 {
		t.Fatalf("HALTED 帧应入库 1 条, got=%d", n)
	}
	if has, halted := src("AAPL"); !has || !halted {
		t.Fatalf("停牌证据应回 (true,true), got=(%v,%v)", has, halted)
	}
	// 投一帧 NORMAL：变 (true,false)——同票后帧覆盖前帧（在龄窗口内）。
	if n := f.Store().HandlePayload([]byte(`{"S":"AAPL","tradingStatus":"TRADING"}`)); n != 1 {
		t.Fatalf("TRADING 帧应入库 1 条, got=%d", n)
	}
	if has, halted := src("AAPL"); !has || halted {
		t.Fatalf("恢复交易证据应回 (true,false), got=(%v,%v)", has, halted)
	}
	// 证据 TTL 过期后回到 (false,false)：闸门语义是"未确认"，不是"沿用上次的 NORMAL"。
	clock.advance(2 * time.Minute) // TTL=1min，越界即失效
	if has, halted := src("AAPL"); has || halted {
		t.Fatalf("证据过期必须回 (false,false)（fail-close），got=(%v,%v)", has, halted)
	}
	// Start/Stop 幂等（假拨号下协程只会重连失败，不阻塞、不 panic）。
	f.Start()
	f.Start()
	f.Stop()
	f.Stop()
}
