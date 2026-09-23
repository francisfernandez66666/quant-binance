// 文件职责：美股 **交易状态证据库**（Phase 3 第 4 项，PLAN §2.4 tradingStatus/tradability +
// §9 market_halt 证据闸）。
//
// 为什么单独一层：§9 的硬闸要求"熔断/停牌"必须**有据可查**才放行或拦截。币安美股把这件事
// 挂在两条流上：
//
//	{SYM}@tradingStatus —— 状态机（NORMAL / HALTED / LULD 熔断 / CLOSE / BREAK …，具体取值待实测）
//	{SYM}@tradability   —— 可交易位（含 SSR 短卖限制等约束，取值待实测）
//
// 证据闸三条纪律（这是本文件的全部意义）：
//  1. **无证据 ≠ 可交易**：从未收到某票状态帧时 Halted() 返回 false 但 HasEvidence() 返回 false，
//     闸据此拒绝下单（PLAN §9 的"未确认即拒"姿势），而不是默认放行；
//  2. 证据会过期：TTL 缺省 5min，超时后 HasEvidence()=false（状态是事件流，断流后的旧状态不算数）；
//  3. 状态取值一律**大写原样存**，判定用白名单（明确的交易态）+ 明确的停牌关键词，
//     认不出的取值归入"未知"→ HasEvidence()=false，宁可拒单也不猜。
//
// English: trading-status/tradability evidence store for Binance US equities. Absence of evidence
// is reported as absence (never as "tradeable"), evidence expires on a TTL, and unrecognized status
// values degrade to "no evidence" so the gate refuses rather than guesses.
package data

import (
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// 状态流名（BinanceQuoteFeedOptions.StreamKind 可选值，PLAN §2.4）。
const (
	EquityStreamTradingStatus = "tradingStatus" // {SYM}@tradingStatus
	EquityStreamTradability   = "tradability"   // {SYM}@tradability
)

// BinanceDefaultStatusTTL 状态证据过期时间（超时=未知，不是"沿用旧状态"）。
const BinanceDefaultStatusTTL = 5 * time.Minute

// binanceStatusEvidence 单票一条证据。
type binanceStatusEvidence struct {
	State   string    // 大写原样状态（TRADING/NORMAL/HALTED/LULD…）
	Detail  string    // 附加原因（SSR/limit reason，未实测前只透传不解释）
	SeenAt  time.Time // 本地落地时刻（TTL 判定用）
	EventMs int64     // 源事件时间（epoch 毫秒；0=源未给）
}

// BinanceTradingStatus 交易状态证据库（并发安全；只增不改，读侧拷贝返回值）。
type BinanceTradingStatus struct {
	mu      sync.RWMutex
	ttl     time.Duration
	now     func() time.Time
	trade   map[string]binanceStatusEvidence
	trad    map[string]binanceStatusEvidence // tradability 证据（与 tradingStatus 分表，别互相覆盖）
	misses  map[string]time.Time             // 曾请求但无证据的票（巡检"未确认"计数）
	logAt   time.Time
	watched map[string]bool // 订阅池（判"该来没来"）
}

// NewBinanceTradingStatus 构造证据库（ttl<=0 取缺省 5min；now=nil 用 time.Now）。
func NewBinanceTradingStatus(ttl time.Duration, now func() time.Time) *BinanceTradingStatus {
	if ttl <= 0 {
		ttl = BinanceDefaultStatusTTL
	}
	if now == nil {
		now = time.Now
	}
	return &BinanceTradingStatus{
		ttl:     ttl,
		now:     now,
		trade:   map[string]binanceStatusEvidence{},
		trad:    map[string]binanceStatusEvidence{},
		misses:  map[string]time.Time{},
		watched: map[string]bool{},
	}
}

// Watch 登记订阅池（之后未收到状态帧的票会进 Unconfirmed）。
func (s *BinanceTradingStatus) Watch(symbols ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sym := range symbols {
		if k := normalizeBinanceSymbol(sym); k != "" {
			s.watched[k] = true
		}
	}
}

// classifyBinanceState 状态取值三分法：明确交易态 / 明确停牌态 / 未知。
// 白名单外的取值一律"未知"（→ 消费方按"无证据"拒单），实测扩表只改这里；
// LULD 前缀单独放行——熔断变体（如 LULD_AGG_TIER_x）未实测，但 LULD 根是明确的停牌语义。
func classifyBinanceState(state string) (tradeable, halted bool) {
	switch state {
	case "NORMAL", "TRADING":
		return true, false
	case "HALTED", "BREAK", "PAUSED", "SUSPENDED", "CLOSE", "CLOSED":
		return false, true
	}
	if strings.HasPrefix(state, "LULD") {
		return false, true
	}
	return false, false
}

// liveTradeEvidence 取 tradingStatus 的**可用**证据：存在 + 未过期 + 取值可归类。
// 三关任一不过都算"无证据"（调用方据此拒单），调用方需持读锁。
func (s *BinanceTradingStatus) liveTradeEvidence(sym string) (binanceStatusEvidence, bool, bool) {
	ev, ok := s.trade[sym]
	if !ok || s.now().Sub(ev.SeenAt) > s.ttl {
		return ev, false, false
	}
	tradeable, halted := classifyBinanceState(ev.State)
	if !tradeable && !halted {
		return ev, false, false // 未知状态：证据存在但不作数
	}
	return ev, tradeable, halted
}

// HasEvidence 该票是否有**可据以判定**的新鲜交易状态证据；无证据时顺带记入 misses 巡检计数。
func (s *BinanceTradingStatus) HasEvidence(sym string) bool {
	k := normalizeBinanceSymbol(sym)
	s.mu.Lock()
	defer s.mu.Unlock()
	ev, tradeable, halted := s.liveTradeEvidence(k)
	_ = ev
	if !tradeable && !halted {
		s.misses[k] = s.now()
		return false
	}
	return true
}

// Halted 该票是否处于**有证据的**停牌/熔断态。注意语义：false 只说"没有停牌证据"，
// 不等于可交易——下单闸必须先 HasEvidence() 再 Halted()（§9"未确认即拒"）。
func (s *BinanceTradingStatus) Halted(sym string) bool {
	k := normalizeBinanceSymbol(sym)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _, halted := s.liveTradeEvidence(k)
	return halted
}

// Tradability 透传 tradability 证据（状态串 + 原因 + 是否有新鲜证据）；
// 取值口径未实测（GAP §G-1），这里**只透传不解释**，解释层在闸侧按白名单收紧。
func (s *BinanceTradingStatus) Tradability(sym string) (state, detail string, ok bool) {
	k := normalizeBinanceSymbol(sym)
	s.mu.RLock()
	defer s.mu.RUnlock()
	ev, has := s.trad[k]
	if !has || s.now().Sub(ev.SeenAt) > s.ttl {
		return "", "", false
	}
	return ev.State, ev.Detail, true
}

// Unconfirmed 订阅池中当前无可用 tradingStatus 证据的票（升序；巡检/告警用）。
func (s *BinanceTradingStatus) Unconfirmed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.watched))
	for sym := range s.watched {
		if _, tradeable, halted := s.liveTradeEvidence(sym); !tradeable && !halted {
			out = append(out, sym)
		}
	}
	sort.Strings(out)
	return out
}

// MissCount 曾查询但无证据的票种数（只增不清零，除非该票后来拿到证据被 putStatus 删除）。
func (s *BinanceTradingStatus) MissCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.misses)
}

// logThrottled 状态库专属错误日志节流（60s 一条，与 feed/ws 同惯例）；不落任何凭证。
func (s *BinanceTradingStatus) logThrottled(format string, args ...any) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.logAt.IsZero() && now.Sub(s.logAt) < time.Minute {
		return
	}
	s.logAt = now
	log.Printf("[binance-status] "+format, args...)
}
