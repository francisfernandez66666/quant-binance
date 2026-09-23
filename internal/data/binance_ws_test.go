// 文件职责：币安 WS 通道层单测（Phase 3 第 1 项，PLAN §2.4 连接方式 + §6.9 静默检测）。
// 本仓库没有 websocket 依赖（go.mod 零 ws 库），故用 fake WsTransport 过接线：
// 覆盖"帧回调→计数→健康位"、"静默超阈值→主动断开→重连"、"退避复位"、URL 拼装四组语义。
package data

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeWsTransport 内存假连接：in 通道喂帧，Close 幂等关闭并逼 ReadMessage 返回 EOF。
type fakeWsTransport struct {
	in      chan []byte
	closeCh chan struct{}
	once    sync.Once
	mu      sync.Mutex
	writes  [][]byte
}

func newFakeWsTransport(buf int) *fakeWsTransport {
	return &fakeWsTransport{in: make(chan []byte, buf), closeCh: make(chan struct{})}
}

// push 投一帧（测试侧模拟服务端推送）。
func (f *fakeWsTransport) push(payload []byte) {
	select {
	case f.in <- payload:
	case <-f.closeCh:
	}
}

// ReadMessage 优先吐已到达的帧，其次等帧/等关闭（避免 select 随机偏向导致的假 flake）。
func (f *fakeWsTransport) ReadMessage() ([]byte, error) {
	select {
	case b := <-f.in:
		return b, nil
	default:
	}
	select {
	case b := <-f.in:
		return b, nil
	case <-f.closeCh:
		return nil, io.EOF
	}
}

// WriteMessage 记录出站帧（RPC SUBSCRIBE 断言用）。
func (f *fakeWsTransport) WriteMessage(payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, append([]byte(nil), payload...))
	return nil
}

// Close 幂等关闭。
func (f *fakeWsTransport) Close() error {
	f.once.Do(func() { close(f.closeCh) })
	return nil
}

// fakeBinanceDialer 记录每次拨号 URL 并产出独立假连接；err 非空则模拟握手失败。
type fakeBinanceDialer struct {
	mu     sync.Mutex
	urls   []string
	trs    []*fakeWsTransport
	err    error
	dialAt time.Time // 每次拨号的真实时刻（退避节拍打点用）
	gaps   []time.Duration
}

// dial 假拨号器：记录 URL/时刻后返回假传输；d.err 非空时模拟拨号失败族（不产生传输）。
func (d *fakeBinanceDialer) dial(ctx context.Context, url string) (WsTransport, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		d.urls = append(d.urls, url)
		return nil, d.err
	}
	t := newFakeWsTransport(16)
	d.trs = append(d.trs, t)
	d.urls = append(d.urls, url)
	if !d.dialAt.IsZero() {
		d.gaps = append(d.gaps, time.Since(d.dialAt))
	}
	d.dialAt = time.Now()
	return t, nil
}

// snapshot 返回 (URL 列表, 假连接列表) 副本。
func (d *fakeBinanceDialer) snapshot() ([]string, []*fakeWsTransport) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.urls...), append([]*fakeWsTransport(nil), d.trs...)
}

// calls 拨号次数。
func (d *fakeBinanceDialer) calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.urls)
}

// pushLatest 向最近一条假连接喂帧。
func (d *fakeBinanceDialer) pushLatest(payload []byte) bool {
	_, trs := d.snapshot()
	if len(trs) == 0 {
		return false
	}
	trs[len(trs)-1].push(payload)
	return true
}

// wait 轮询直到条件成立（超时=测试失败）。
func wait(t *testing.T, d time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待超时（%s）: %s", d, desc)
}

// TestBinanceWSNewRejectsBadConfig 构造期拒绝空 URL / 未注入传输（不起静默假活协程）。
func TestBinanceWSNewRejectsBadConfig(t *testing.T) {
	if _, err := NewBinanceWS(BinanceWSOptions{Dial: (&fakeBinanceDialer{}).dial}); err == nil {
		t.Fatal("URL 为空应报错")
	}
	if _, err := NewBinanceWS(BinanceWSOptions{URL: BinanceSpotWSProd}); err == nil {
		t.Fatal("Dial 未注入应报错（本仓库无 ws 依赖，必须显式注入）")
	}
}

// TestBinanceWSFramesDriveHealth 帧驱动健康位：建连即活、收到帧计数、Stop 后不再产帧。
func TestBinanceWSFramesDriveHealth(t *testing.T) {
	d := &fakeBinanceDialer{}
	var got sync.WaitGroup
	got.Add(2)
	c, err := NewBinanceWS(BinanceWSOptions{
		Name: "unit",
		URL:  StreamURLBare(BinanceSpotWSProd, "btcusdt@miniTicker"),
		Dial: d.dial,
		OnMessage: func([]byte) {
			got.Done()
		},
		SilenceLimit: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	defer c.Stop()
	wait(t, 2*time.Second, "通道未建立", c.Connected)
	if !c.Healthy() {
		t.Fatalf("建连且未静默应判健康，SilenceMs=%d", c.SilenceMs())
	}
	for i := 0; i < 2; i++ {
		if !d.pushLatest([]byte(`{"e":"test"}`)) {
			t.Fatal("假连接尚未产出")
		}
	}
	wait(t, 2*time.Second, "两帧未回调", func() bool { return c.MessageCount() >= 2 })
	got.Wait()
	if c.SilenceMs() < 0 {
		t.Fatalf("SilenceMs 不应为 -1（已活动）：%d", c.SilenceMs())
	}
	if urls, _ := d.snapshot(); urls[0] != "wss://stream.binance.com:9443/ws/btcusdt@miniTicker" {
		t.Fatalf("订阅 URL 拼装错误: %s", urls[0])
	}
}

// TestBinanceWSSilenceTriggersReconnect §6.9 核心：静默超阈值 → 判不健康 → 主动断开 → 重连
// （不依赖对端 FIN）。健康位用后台 1ms 轮询采集，避免"重连太快看不见不健康窗口"的假断言。
func TestBinanceWSSilenceTriggersReconnect(t *testing.T) {
	d := &fakeBinanceDialer{}
	c, err := NewBinanceWS(BinanceWSOptions{
		Name:         "silent",
		URL:          BinanceSpotWSProd + "/stream?streams=btcusdt@miniTicker",
		Dial:         d.dial,
		SilenceLimit: 90 * time.Millisecond, // 90s 按比例缩到 90ms，看门狗 30ms 采样
		MinBackoff:   10 * time.Millisecond,
		MaxBackoff:   20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	defer c.Stop()
	wait(t, 2*time.Second, "首次拨号", func() bool { return d.calls() >= 1 })
	var sawUnhealthy atomic.Bool
	done := make(chan struct{})
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		for {
			select {
			case <-done:
				return
			default:
			}
			if !c.Healthy() {
				sawUnhealthy.Store(true)
			}
			time.Sleep(time.Millisecond)
		}
	}()
	wait(t, 3*time.Second, "静默未触发重连", func() bool { return d.calls() >= 2 })
	close(done)
	<-pollDone
	if !sawUnhealthy.Load() {
		t.Fatal("静默窗口内健康位从未翻转=false → §6.9 判定形同虚设")
	}
	if c.Reconnects() < 1 {
		t.Fatalf("重连计数应 >=1，实际 %d", c.Reconnects())
	}
}

// TestBinanceWSDialErrorBackoffGrows 持续握手失败：退避成倍增长且不超上限、计数继续。
func TestBinanceWSDialErrorBackoffGrows(t *testing.T) {
	d := &fakeBinanceDialer{err: errors.New("no route")}
	c, err := NewBinanceWS(BinanceWSOptions{
		Name: "dialfail", URL: BinanceSpotWSProd, Dial: d.dial,
		SilenceLimit: time.Second, MinBackoff: 10 * time.Millisecond, MaxBackoff: 40 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	defer c.Stop()
	wait(t, 3*time.Second, "握手失败未持续重连", func() bool { return c.Reconnects() >= 3 })
	if c.Connected() || c.Healthy() {
		t.Fatal("从未建连却报健康=假绿")
	}
	if c.SilenceMs() != -1 {
		t.Fatalf("从未活动应返回 -1（未知），实际 %d", c.SilenceMs())
	}
	urls, _ := d.snapshot()
	for _, u := range urls {
		if u == "" {
			t.Fatal("失败拨号也须带 URL 上下文")
		}
	}
}

// TestBinanceWSStopIsIdempotent Stop 可重复调用且不再复活协程。
func TestBinanceWSStopIsIdempotent(t *testing.T) {
	d := &fakeBinanceDialer{}
	c, err := NewBinanceWS(BinanceWSOptions{Name: "stop", URL: BinanceSpotWSProd, Dial: d.dial,
		SilenceLimit: time.Second, MinBackoff: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	c.Start()
	wait(t, 2*time.Second, "未建连", c.Connected)
	c.Stop()
	c.Stop()
	before := d.calls()
	c.Start() // Stop 之后不得复活
	time.Sleep(80 * time.Millisecond)
	if after := d.calls(); after != before {
		t.Fatalf("Stop 后 Start 复活了通道: %d → %d", before, after)
	}
}

// TestBinanceWSStreamURLBuilders URL 拼装对拍 PLAN §2.4 三种连接方式。
func TestBinanceWSStreamURLBuilders(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"裸流", StreamURLBare(BinanceSpotWSProd, "btcusdt@miniTicker"),
			"wss://stream.binance.com:9443/ws/btcusdt@miniTicker"},
		{"裸流斜杠归一", StreamURLBare(BinanceSpotWSProd+"/", "/btcusdt@bookTicker"),
			"wss://stream.binance.com:9443/ws/btcusdt@bookTicker"},
		{"组合流", StreamURLCombined(BinanceSpotWSTestnet, []string{"btcusdt@miniTicker", " ethusdt@miniTicker "}),
			"wss://testnet.binance.vision/stream?streams=btcusdt@miniTicker/ethusdt@miniTicker"},
		{"testnet 现货", StreamURLBare(BinanceSpotWSTestnet, "btcusdt@trade"),
			"wss://testnet.binance.vision/ws/btcusdt@trade"},
		{"美股全市场价", StreamURLBare(BinanceEquityWSProd, "price"),
			"wss://nbstream.binance.com/equity/ws/price"},
		{"用户流不含凭证入串", UserStreamURLBare(BinanceEquityWSProd, " listenKey123 ", "@orderReport"),
			"wss://nbstream.binance.com/equity/ws/listenKey123@orderReport"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, tc.got, tc.want)
		}
	}
}
