// 文件职责：币安行情 WS feed 的**配置与构造**（Phase 3 第 1 项，PLAN §2.4/§2.8/§7、
// GAP_BINANCE_READINESS §G-8）。构造期做三件事：市场键校验（CN 直接拒——A 股链不动）、
// 订阅流名拼装、解析器选择；快照装配见 binance_quotes_snap.go。
//
// 加市场不换市场：feed 拥有**独立** MarketSnapshot（Source=BINANCE-SPOT / BINANCE-STK），
// 默认不写 5s Fetcher（GAP §G-8：币安 universe 与 A 股监控池分轨，混池会让新浪/腾讯链对
// BTCUSDT/AAPL 打空并污染 §WS-C 陈旧闸）；确需并池由装配层显式调 MergeInto。
//
// English: configuration + construction of the Binance market-data WS feed. It owns an
// independent MarketSnapshot (source tags BINANCE-SPOT/BINANCE-STK) and never touches the CN
// 5s Fetcher unless MergeInto is called explicitly (GAP §G-8 pool separation).
package data

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 行情源名（MarketSnapshot.Source 取值；PLAN §7 要求 BINANCE-SPOT/BINANCE-STK 标签）。
// ⚠ §M1 契约（internal/data/source.go）：源名常量 BinanceQuoteSourceSpot/BinanceQuoteSourceUS
// 定义在 source.go 的 §M1 常量块（AST 扫描器只认该形态），并登记进 AllQuoteSources() +
// golden qmt_gateway/contract/quote_sources.json；改任何一侧必须
// `go test ./internal/data -run TestQuoteSourcesGolden -update` 再生产 golden，
// 否则 quote_sources_contract_test.go 与 /api/status 白名单巡检会红（Agent B 缺陷⑥收口）。

// binanceFeedDefaultMaxTickAge tick 超龄丢弃缺省值（断流重连后旧帧不覆盖新价）。
const binanceFeedDefaultMaxTickAge = 30 * time.Second

// BinanceQuoteFeedOptions 行情 feed 配置。Symbols（或 Streams）+ Dial 必填，其余零值走缺省。
type BinanceQuoteFeedOptions struct {
	Market     string // "CRYPTO" | "US"（决定流名与源标签；CN 直接拒——那条链不动）
	WsBase     string // 空=按市场取 BinanceSpotWSProd / BinanceEquityWSProd（可注入 mock）
	Symbols    []string
	StreamKind string   // 见 SpotStream*/EquityStream* 常量；空=miniTicker / price
	Streams    []string // 显式流名（非空则跳过 Symbols+StreamKind 拼装，便于接 @kline 等）
	Dial       WsDialFunc

	SilenceLimit  time.Duration // §6.9 静默阈值，缺省 90s
	FlushInterval time.Duration // 快照刷新节拍，缺省 1s
	MaxTickAge    time.Duration // tick 超龄丢弃，缺省 30s
	Now           func() time.Time
	OnSnapshot    func(*MarketSnapshot) // 每轮刷新后回调（可空；接 SSE/落盘/监控）

	// Parse 自定义解析（单测/新流型注入；nil=按市场+StreamKind 选内置解析器）。
	Parse func(payload []byte) ([]binanceTick, error)
}

// BinanceQuoteFeed 币安行情 WS feed：独立 MarketSnapshot + per-symbol 新鲜度 + §6.9 健康位。
// 生命周期与 QMTFeed 平行：Start 非阻塞、Stop 幂等。
type BinanceQuoteFeed struct {
	opt   BinanceQuoteFeedOptions
	ws    *BinanceWS
	parse func(payload []byte) ([]binanceTick, error)

	mu       sync.Mutex
	pending  map[string]binanceTick // 待落快照的 tick（按 symbol，后到覆盖先到）
	lastTick map[string]binanceTick // 最近一帧原样（盘口 Bid/Ask 不进 StockInfo，只留这里供价差估算）
	stamp    map[string]time.Time   // per-symbol 最近事件时刻（新鲜度闸用）
	snap     *MarketSnapshot

	recv        atomic.Int64 // 收到的帧数
	flushes     atomic.Int64 // 快照刷新轮数
	dropStale   atomic.Int64 // 丢弃的超龄 tick 数
	dropInvalid atomic.Int64 // 丢弃的非法帧/tick 数
	lastLog     atomic.Int64 // 日志节流（unix 秒）

	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewBinanceQuoteFeed 构造 feed（校验 + 缺省值 + 解析器选择）。
// 配置错误一律返回 error：宁可启动失败，也不起一条"订阅了个空池"的静默假活通道。
func NewBinanceQuoteFeed(opt BinanceQuoteFeedOptions) (*BinanceQuoteFeed, error) {
	market := NormalizeMarketKey(opt.Market)
	if market == "CN" {
		return nil, fmt.Errorf("binance quotes: Market 必须是 CRYPTO/US（CN 走既有新浪/腾讯链，收到 %q）", opt.Market)
	}
	// 流名装配双通道：显式 Streams 原样直通（@kline_1m 等新流型），否则按 市场+StreamKind+Symbols 拼装。
	streams := opt.Streams
	if len(streams) == 0 {
		if len(opt.Symbols) == 0 {
			return nil, fmt.Errorf("binance quotes: Symbols 与 Streams 不能同时为空")
		}
		streams = binanceStreamNames(market, opt.StreamKind, opt.Symbols)
	}
	// ⚠ 显式 Streams 分支同样要过"结果非空"闸：StreamURLCombined 会静默滤掉空白条目，
	// 全空白的显式列表曾能构造成功并订阅出 `streams=` 空池——正是文件头禁止的静默假活通道
	// （Agent B 缺陷⑤修复：先归一再判空，两条通道共用同一条拒收口径）。
	trimmed := make([]string, 0, len(streams))
	for _, s := range streams {
		if t := strings.TrimSpace(s); t != "" {
			trimmed = append(trimmed, t)
		}
	}
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("binance quotes: 有效流名为空（Symbols=%q Streams=%q）",
			strings.Join(opt.Symbols, ","), strings.Join(opt.Streams, ","))
	}
	streams = trimmed
	// WS 基址按市场分档取缺省（现货 stream.binance.com / 美股 nbstream/equity），WsBase 注入可覆盖（testnet/代理）。
	base := strings.TrimSpace(opt.WsBase)
	if base == "" {
		base = BinanceSpotWSProd
		if market == "US" {
			base = BinanceEquityWSProd
		}
	}
	// 可选参数统一落缺省：静默阈值(§6.9 90s)/快照刷新节拍(1s)/tick 超龄上限/时钟（测试注入虚拟时间）。
	if opt.SilenceLimit <= 0 {
		opt.SilenceLimit = BinanceWSDefaultSilenceLimit
	}
	if opt.FlushInterval <= 0 {
		opt.FlushInterval = time.Second
	}
	if opt.MaxTickAge <= 0 {
		opt.MaxTickAge = binanceFeedDefaultMaxTickAge
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	// feed 骨架：pending/lastTick/stamp 三张表都按 symbol（大写归一后）键控。
	f := &BinanceQuoteFeed{
		opt:      opt,
		pending:  map[string]binanceTick{},
		lastTick: map[string]binanceTick{},
		stamp:    map[string]time.Time{},
		stopCh:   make(chan struct{}),
	}
	// 解析器注入优先（单测/新流型），缺省按 市场+StreamKind 选内置解析器。
	f.parse = opt.Parse
	if f.parse == nil {
		f.parse = binanceTickerParser(market, opt.StreamKind)
	}
	ws, err := NewBinanceWS(BinanceWSOptions{
		Name: "binance-quotes-" + strings.ToLower(market),
		URL:  StreamURLCombined(base, streams),
		Dial: opt.Dial,
		OnMessage: func(payload []byte) { // 命中数由 feed 内部计数，通道层不需要回执
			f.HandlePayload(payload)
		},
		SilenceLimit: opt.SilenceLimit,
		Now:          opt.Now,
	})
	if err != nil {
		return nil, err
	}
	f.ws = ws
	return f, nil
}
