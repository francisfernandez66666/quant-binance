// 文件职责：美股交易状态**证据流**装配层（Phase 3 §P3，PLAN §2.4 tradingStatus/tradability）。
// 把三块散件接成一条可启停的通道：
//
//	BinanceTradingStatus   证据库（TTL/分类/巡检计数，binance_trading_status.go）
//	HandlePayload          帧入口（组合流后缀分流，binance_trading_status_io.go）
//	BinanceWS              连接状态机（重连/静默检测，binance_ws.go）
//
// 下游只有一条腿：GateSource() 交给 risk.Gate.SetHaltEvidenceSource（§9 market_halt 闸）。
// 空 Symbols 直接拒构造——没配 status_symbols 就是"不启用该闸"，装配层据此保持闸惰性，
// 绝不起一条订阅空池的静默假活通道（与行情 feed NewBinanceQuoteFeed 同纪律）。
//
// English: wiring layer for the US trading-status evidence stream — store + frame entry +
// websocket state machine, exposed to the risk gate via GateSource. Empty symbol lists are
// rejected at construction so the gate stays inert instead of running a dead channel.
package data

import (
	"fmt"
	"strings"
	"time"
)

// BinanceStatusFeedOptions 状态流配置。Symbols + Dial 必填，其余零值走缺省。
type BinanceStatusFeedOptions struct {
	Symbols      []string // 订阅的币安美股代码（AAPL 形态；空=不启用，构造报错）
	WsBase       string   // 空=BinanceEquityWSProd（testnet/代理可注入）
	Dial         WsDialFunc
	TTL          time.Duration    // 证据在龄窗口，缺省 5min（与 NewBinanceTradingStatus 同口径）
	SilenceLimit time.Duration    // §6.9 静默阈值，缺省 90s
	Now          func() time.Time // 时钟注入（单测虚拟时间）
}

// BinanceStatusFeed 一条美股 tradingStatus/tradability 组合流 + 证据库。
type BinanceStatusFeed struct {
	ws    *BinanceWS
	store *BinanceTradingStatus
}

// NewBinanceStatusFeed 构造：校验 → 拼两态订阅流（<sym>@tradingStatus + @tradability，
// 每票双流同库分表入库）→ 建证据库并挂巡检 → 建 WS 通道（帧入口直连 HandlePayload）。
func NewBinanceStatusFeed(opt BinanceStatusFeedOptions) (*BinanceStatusFeed, error) {
	var streams, syms []string
	for _, sym := range opt.Symbols {
		s := normalizeBinanceSymbol(sym)
		if s == "" {
			continue // 空白项静默剔除；全空白由下面的总闸兜住
		}
		syms = append(syms, s)
		low := strings.ToLower(s)
		streams = append(streams,
			low+"@"+strings.ToLower(EquityStreamTradingStatus),
			low+"@"+strings.ToLower(EquityStreamTradability))
	}
	if len(streams) == 0 {
		return nil, fmt.Errorf("binance status: Symbols 为空/全空白——闸保持惰性不装配，禁止空池订阅")
	}
	base := strings.TrimSpace(opt.WsBase)
	if base == "" {
		base = BinanceEquityWSProd
	}
	silence := opt.SilenceLimit
	if silence <= 0 {
		silence = BinanceWSDefaultSilenceLimit
	}
	st := NewBinanceTradingStatus(opt.TTL, opt.Now)
	st.Watch(syms...) // 订阅池即巡检池：miss 计数/Unconfirmed 口径从构造起就对全部票生效
	ws, err := NewBinanceWS(BinanceWSOptions{
		Name:         "binance-status-us",
		URL:          StreamURLCombined(base, streams),
		Dial:         opt.Dial,
		SilenceLimit: silence,
		Now:          opt.Now,
		// 帧入口返回 0 条=与本库无关的帧（心跳/回执/行情串流），只有真入库才计数。
		OnMessage: func(payload []byte) { st.HandlePayload(payload) },
	})
	if err != nil {
		return nil, fmt.Errorf("binance status: 通道构造失败: %w", err)
	}
	return &BinanceStatusFeed{ws: ws, store: st}, nil
}

// Start 启动读循环（非阻塞；巡检池已在构造期挂好，见 NewBinanceStatusFeed）。
func (f *BinanceStatusFeed) Start() {
	f.ws.Start()
}

// Stop 幂等停止（通道协程树取消；证据库留读——停机收尾窗口里 §9 闸仍可查最后证据）。
func (f *BinanceStatusFeed) Stop() {
	f.ws.Stop()
}

// Watch 追加巡检订阅（配置热更后新票即进 miss 统计；证据仍按流上实帧建立）。
func (f *BinanceStatusFeed) Watch(symbols ...string) {
	f.store.Watch(symbols...)
}

// Store 证据库本体（只读出口：/api/binance/state 展示 Halted/Unconfirmed 计数用）。
func (f *BinanceStatusFeed) Store() *BinanceTradingStatus { return f.store }

// Healthy / SilenceMs / Frames 通道健康面（router.RegisterFeed 闭包注册的形状）。
func (f *BinanceStatusFeed) Healthy() bool    { return f.ws.Healthy() }
func (f *BinanceStatusFeed) SilenceMs() int64 { return f.ws.SilenceMs() }
func (f *BinanceStatusFeed) Frames() int64    { return f.ws.MessageCount() }

// GateSource §9 market_halt 闸的注入面：risk.Gate.SetHaltEvidenceSource(feed.GateSource())。
// 返回 (hasEvidence, halted)——**无证据≠可交易**：HasEvidence false 时第二返回值为
// false 也无意义，调用方（gate.checkMarketHalt）按 fail-close 拒单，本函数不越权判。
func (f *BinanceStatusFeed) GateSource() func(symbol string) (bool, bool) {
	return func(symbol string) (bool, bool) {
		sym := normalizeBinanceSymbol(symbol)
		if !f.store.HasEvidence(sym) {
			return false, false
		}
		return true, f.store.Halted(sym)
	}
}
