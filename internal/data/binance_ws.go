// 文件职责：币安 WebSocket **通用通道层**（PLAN_BINANCE_MULTI_ASSET §2.4/§2.8/§6.9，Phase 3 行情面）。
//
// 三件事，且只做这三件事：
//  1. 传输注入接缝（WsTransport/WsDialFunc）——本仓库 go.mod **零 websocket 依赖**
//     （GAP_BINANCE_READINESS §1 实锤），行情链一贯 net/http 直连不引三方库
//     （types.go 包注释）。因此这里只实现与 socket 无关的通道状态机：真实拨号由装配层
//     注入（官方 connector-go 的 streams client、或自写的 RFC6455 升级皆可），单测用假传输
//     驱动 解析/重连/静默 三条路径，不需要真 socket；
//  2. 断线重连 + 重订阅（指数退避，MinBackoff→MaxBackoff）——PLAN §2.4 明文
//     "断线重连后必须重订阅"；
//  3. **WS 静默检测**（PLAN §6.9 家族规则：静默 >90s 即判不健康并触发轮询兜底）——
//     内部 lastMsg 时间戳 + SilenceMs()/Healthy() 两个出口；静默超限时看门狗主动
//     关闭当前连接逼出重连，绝不让"看起来连着"变成"数据是新的"（§M2 伪造新鲜度同族）。
//
// PING/PONG：币安服务端发 ping 帧必须由客户端回 pong（§2.4），这一步发生在具体
// WsTransport 实现内部（connector 已内置），本层只消费数据帧，不重复实现协议。
//
// English: transport-agnostic Binance WebSocket channel. The repo vendors no websocket
// library, so the socket itself is injected (WsDialFunc) while this file owns the
// reconnect/backoff state machine and the §6.9 90s-silence health rule (SilenceMs/Healthy,
// watchdog closes a silent socket to force a resubscribe).
package data

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ── 端点常量表（GAP_BINANCE_READINESS §5「镜像端点锁」：镜像项必须在表内，防回退主域直连）──
const (
	// BinanceSpotWSProd 现货行情/回报组合流域（PLAN §2.8/§2.6）。
	BinanceSpotWSProd = "wss://stream.binance.com:9443"
	// BinanceSpotWSTestnet 现货 testnet 行情域（仅现货有 testnet，PLAN §2.1）。
	BinanceSpotWSTestnet = "wss://testnet.binance.vision"
	// BinanceEquityWSProd 美股行情/回报域（PLAN §2.4，nbstream）。
	BinanceEquityWSProd = "wss://nbstream.binance.com/equity"

	// BinanceSpotRestProd 现货 REST 主域（签名交易面）。
	BinanceSpotRestProd = "https://api.binance.com"
	// BinanceSpotRestTestnet 现货 REST testnet（下单/行情沙箱）。
	BinanceSpotRestTestnet = "https://testnet.binance.vision"
	// BinanceSpotRestMirror 现货**公共行情镜像**（GAP §G-4/W-4：本机/广州唯一可达的行情面，
	// 覆盖 klines/ticker/depth/exchangeInfo，不含下单/账户/WS）。缺省行情基址。
	BinanceSpotRestMirror = "https://data-api.binance.vision"
	// BinanceEquityRestProd 美股 REST 主域（/sapi/v1/equity/*，无 testnet 变体）。
	BinanceEquityRestProd = "https://api.binance.com"
	// BinanceVisionHistoryBase 历史全量下载域（PLAN §2.8，zip+CHECKSUM，MIT）。
	BinanceVisionHistoryBase = "https://data.binance.vision"
)

// BinanceWSDefaultSilenceLimit WS 静默阈值（PLAN §6.9：>90s 判不健康 → 轮询兜底/重连）。
// 与 §WS-C 陈旧行情闸是两个不同的量：本阈值管"通道还活着吗"，后者管"价格还能用来下单吗"。
const BinanceWSDefaultSilenceLimit = 90 * time.Second

// WsTransport 一条已建立的 WebSocket 连接的最小读写面。
// 实现方负责消化控制帧（ping→pong），ReadMessage 只吐数据帧；返回 error 即视为断线。
// English: minimal read/write surface of one established socket; the implementation owns
// control frames (ping/pong) and any error from ReadMessage is treated as a disconnect.
type WsTransport interface {
	ReadMessage() ([]byte, error)
	WriteMessage(payload []byte) error
	Close() error
}

// WsDialFunc 拨号接缝：装配层注入真实实现（connector / 自研 RFC6455）。
// ctx 取消或 Stop 后必须尽快返回错误，不得泄漏连接。
type WsDialFunc func(ctx context.Context, url string) (WsTransport, error)

// BinanceWSOptions 通道配置。URL/Dial 必填，其余零值走缺省。
type BinanceWSOptions struct {
	Name      string                    // 日志标签（如 "binance-spot-quotes"），不落任何凭证
	URL       string                    // 完整订阅 URL（组合流或裸流；由 StreamURL* 助手拼）
	Dial      WsDialFunc                // 传输注入接缝
	OnMessage func(payload []byte)      // 数据帧回调（业务解析层；在通道协程内串行执行）
	Subscribe func(t WsTransport) error // 连接建立后的 RPC 式订阅（SUBSCRIBE 帧）；组合流留空

	SilenceLimit time.Duration    // 静默阈值，缺省 90s（§6.9）
	MinBackoff   time.Duration    // 重连退避起点，缺省 1s
	MaxBackoff   time.Duration    // 重连退避上限，缺省 30s
	Now          func() time.Time // 时钟注入（单测用；nil=time.Now）
}

// BinanceWS 单条币安 WS 通道：连接-读取-重连-静默检测的状态机。
// 生命周期：Start() 非阻塞起协程，Stop() 幂等收摊；Healthy()/SilenceMs() 随时可读。
type BinanceWS struct {
	opt BinanceWSOptions

	mu     sync.Mutex
	cancel context.CancelFunc // 当前协程树的取消函数（nil=未运行）
	closed bool               // Stop 已调用

	connected  atomic.Bool
	lastMsg    atomic.Int64 // 最近一次"通道有动静"的时刻（unix nano）；0=从未（含刚连上未收帧）
	reconnects atomic.Int64 // 重连次数（运维指标 /api/binance/state 的 WS 订阅数佐证）
	silenceHit atomic.Int64 // 最近一次静默/断线日志时刻（unix 秒，日志节流用）
	msgCount   atomic.Int64 // 累计数据帧数（判"这轮到底有没有数据"）
}

// NewBinanceWS 构造通道；URL/Dial 缺失直接返回 error（宁可启动即失败，也不起一个静默假活协程）。
func NewBinanceWS(opt BinanceWSOptions) (*BinanceWS, error) {
	if strings.TrimSpace(opt.URL) == "" {
		return nil, errors.New("binance ws: URL 为空")
	}
	if opt.Dial == nil {
		return nil, errors.New("binance ws: Dial 传输未注入（本仓库无 websocket 依赖，须由装配层提供）")
	}
	// 缺省项统一在构造期收口（调用方零值即得安全默认，避免运行期再判 nil/0）：
	if opt.SilenceLimit <= 0 {
		opt.SilenceLimit = BinanceWSDefaultSilenceLimit
	}
	if opt.MinBackoff <= 0 {
		opt.MinBackoff = time.Second
	}
	if opt.MaxBackoff < opt.MinBackoff {
		opt.MaxBackoff = 30 * time.Second
	}
	// 通道名/时钟同样兜底（测试注入可控时钟走 opt.Now）。
	if opt.Name == "" {
		opt.Name = "binance-ws"
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &BinanceWS{opt: opt}, nil
}

// Start 启动读循环（非阻塞）。重复调用无副作用（已在跑的通道不再起第二个协程）。
func (c *BinanceWS) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.run(ctx)
}

// Stop 幂等停止：取消协程树并记 closed，之后的 Start 不再复活（生命周期与 QMTFeed 同姿势）。
func (c *BinanceWS) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
}

// now 统一时钟出口（测试注入）。
func (c *BinanceWS) now() time.Time { return c.opt.Now() }

// SilenceMs 距最近一次通道活动（连接建立或收到数据帧）的毫秒数；-1=从未活动（未知，
// 消费端禁止当 0 用——与 Fetcher.StalenessMs 的 §M2 语义同族）。
func (c *BinanceWS) SilenceMs() int64 {
	v := c.lastMsg.Load()
	if v == 0 {
		return -1
	}
	return c.now().Sub(time.Unix(0, v)).Milliseconds()
}

// Healthy 通道是否健康：已连接 **且** 静默未超阈值。
// 静默但未断线是最危险的形态（socket 还在、数据已停），故必须同时看两件事（PLAN §6.9）。
func (c *BinanceWS) Healthy() bool {
	if !c.connected.Load() {
		return false
	}
	s := c.SilenceMs()
	return s >= 0 && s <= c.opt.SilenceLimit.Milliseconds()
}

// Connected 当前是否持有已建立的连接（不含新鲜度判断）。
func (c *BinanceWS) Connected() bool { return c.connected.Load() }

// Reconnects 累计重连次数。English: cumulative reconnect count.
func (c *BinanceWS) Reconnects() int64 { return c.reconnects.Load() }

// MessageCount 累计收到的数据帧数。
func (c *BinanceWS) MessageCount() int64 { return c.msgCount.Load() }

// markActivity 记一次"通道有动静"。连接建立也算动静（否则刚连上就被判静默）。
func (c *BinanceWS) markActivity() { c.lastMsg.Store(c.now().UnixNano()) }

// run 连接-读取-重连主循环。
// 退避只在"这一轮没拿到过任何数据帧"时翻倍；拿到过数据即复位到 MinBackoff——
// 避免长时间正常后偶发一次断线被惩罚成长达 30s 的空窗。
func (c *BinanceWS) run(ctx context.Context) {
	backoff := c.opt.MinBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		gotData, err := c.serveOnce(ctx)
		if ctx.Err() != nil {
			return // Stop 主动收摊，不算故障
		}
		c.connected.Store(false)
		c.reconnects.Add(1)
		c.logThrottled("binance ws[%s]: 连接中断，%s 后重连: %v", c.opt.Name, backoff, err)
		if gotData {
			backoff = c.opt.MinBackoff // 本轮有数据 = 通道本身没问题，退避清零
		} else {
			backoff *= 2
			if backoff > c.opt.MaxBackoff {
				backoff = c.opt.MaxBackoff
			}
		}
		if !sleepCtx(ctx, backoff) {
			return
		}
	}
}

// serveOnce 建立一条连接并读到断线为止，返回"本轮是否收到过数据帧"与错误。
func (c *BinanceWS) serveOnce(ctx context.Context) (bool, error) {
	t, err := c.opt.Dial(ctx, c.opt.URL)
	if err != nil {
		return false, fmt.Errorf("拨号失败: %w", err)
	}
	defer func() { _ = t.Close() }()

	// 建连后必须重订阅（§2.4）：RPC 式流走 Subscribe，组合流订阅已编进 URL 故为 nil。
	if c.opt.Subscribe != nil {
		if serr := c.opt.Subscribe(t); serr != nil {
			return false, fmt.Errorf("订阅失败: %w", serr)
		}
	}
	c.connected.Store(true)
	c.markActivity()

	// 静默看门狗：超阈值即关连接逼出重连（PLAN §6.9 兜底轮询的触发点由消费方读 Healthy() 判定）。
	watchDone := make(chan struct{})
	go c.watchSilence(ctx, t, watchDone)

	// 读帧主循环：gotData=本连接是否真吃过帧（连上却零帧的假活态由外层重连决策识别）。
	gotData := false
	var readErr error
	for {
		payload, rerr := t.ReadMessage()
		if rerr != nil {
			readErr = rerr
			break
		}
		// 任何入站帧都算活动：先喂静默看门狗再计数，业务回调失败也不丢活动判定。
		c.markActivity()
		c.msgCount.Add(1)
		gotData = true
		if c.opt.OnMessage != nil {
			c.opt.OnMessage(payload)
		}
	}
	close(watchDone)
	return gotData, readErr
}

// watchSilence 静默看门狗：周期检查静默度，超限则关闭连接（逼 serveOnce 重连）。
func (c *BinanceWS) watchSilence(ctx context.Context, t WsTransport, done <-chan struct{}) {
	tick := time.NewTicker(c.opt.SilenceLimit / 3) // 3 次采样内发现问题，粒度足够
	defer tick.Stop()
	limitMs := c.opt.SilenceLimit.Milliseconds()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-tick.C:
			if s := c.SilenceMs(); s > limitMs {
				log.Printf("[binance-ws] binance ws[%s]: §6.9 静默 %ds > %s，主动断开触发重连/轮询兜底",
					c.opt.Name, s/1000, c.opt.SilenceLimit)
				_ = t.Close() // ReadMessage 随即返回错误，走统一重连路径
				return
			}
		}
	}
}

// logThrottled 同类故障日志 60s 一条（与 qmt_feed 同惯例，避免断线风暴刷屏）。
func (c *BinanceWS) logThrottled(format string, args ...any) {
	nowSec := c.now().Unix()
	prev := c.silenceHit.Swap(nowSec)
	if prev == 0 || nowSec-prev >= 60 {
		log.Printf("[binance-ws] "+format, args...)
	}
}

// sleepCtx 可被 ctx 打断的休眠；返回 false 表示 ctx 已取消。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// ── 订阅 URL 拼装（纯函数，单测直接对拍 PLAN §2.4 的连接方式）──

// StreamURLBare 裸流形式 /ws/<stream>：payload 不带 {stream,data} 信封。
// 例：StreamURLBare(BinanceSpotWSProd, "btcusdt@miniTicker")
//
//	→ wss://stream.binance.com:9443/ws/btcusdt@miniTicker
func StreamURLBare(base, stream string) string {
	return strings.TrimSuffix(base, "/") + "/ws/" + strings.TrimPrefix(stream, "/")
}

// StreamURLCombined 组合流形式 /stream?streams=A/B/C：payload 带 {stream,data} 信封。
// 空/空白流名会被剔除；全空时仍返回带空 streams= 的串（由调用方判错，不静默兜底）。
func StreamURLCombined(base string, streams []string) string {
	clean := make([]string, 0, len(streams))
	for _, s := range streams {
		if t := strings.TrimSpace(s); t != "" {
			clean = append(clean, t)
		}
	}
	return strings.TrimSuffix(base, "/") + "/stream?streams=" + strings.Join(clean, "/")
}

// UserStreamURLBare user-data / orderReport 裸流（PLAN §6.9 回报通道形状）。
// listenKey 只拼串、绝不入日志；suffix 美股为 "@orderReport"（PLAN §2.4），现货留空即裸 key。
func UserStreamURLBare(base, listenKey, suffix string) string {
	return StreamURLBare(base, strings.TrimSpace(listenKey)+strings.TrimSpace(suffix))
}
