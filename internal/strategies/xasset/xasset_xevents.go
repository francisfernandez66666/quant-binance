// 文件职责：§战法批（2026-09-23）事件腿信号适配——把 data.XEvent（EDGAR/CryptoPanic
// 原始候选）+ data 侧打分器给出的方向票，折成带 Market 章的 strategy.Signal。
// 这是「新闻→交易」链条上 xasset 的收口件：打分（关键词/LLM）在 data.XEventScorer，
// 方向 fork 在此（利好=买入、利空=空头信号），下单准入在 engine 派发腿（§战法批-4）。
//
// 方向 fork 纪律：
//   - 利空且 BearEnabled=false → ActionSell（只允许它减既有多头，与本批前口径一致）；
//   - 利空且 BearEnabled=true  → ActionShortOpen（开空信号，准入闸在派发腿按市场/借券证据再筛）；
//   - 中性/低置信（< MinConfidence）一律不出信号——事件票宁缺毋滥。
//
// 代码口径：CRYPTO=币对原样（BTCUSDT）；US=裸 ticker（AAPL，与执行器 symbol/状态 feed 同形，
// 研究库 K 线的点分键 AAPL.US 由派发腿查档时换算）。同标的多条事件只留置信度最高一条。
//
// English: §XASSET batch — adapter turning scored XEvents into market-stamped signals.
// Bullish→buy; bearish→short_open when the bear leg is enabled (else plain sell).
// One best-confidence signal per code; neutral/low-confidence events emit nothing.
package xasset

import (
	"fmt"
	"time"

	"quant-trading-v2/internal/data"
	"quant-trading-v2/internal/newsagent"
	"quant-trading-v2/internal/strategy"
)

// XEventSignalOptions 事件信号闸参数。
type XEventSignalOptions struct {
	BearEnabled   bool             // 利空是否可开空（false=利空只转平多卖出，出厂态）
	MinConfidence float64          // 出向最低置信（<=0=不再加闸，按打分器原值）
	NowFn         func() time.Time // 测试注入时钟；nil=time.Now
}

// SignalsFromXEvents 批量 XEvent+方向票 → 信号（senti 以事件 URL 为键）。
// market 必须与事件自带市场章一致（防御跨腿错投喂；归一后仍不等则该条跳过）。
func SignalsFromXEvents(evs []data.XEvent, senti map[string]data.XEventSentiment, market string, opts XEventSignalOptions) ([]strategy.Signal, error) {
	m, err := normalizeMarketKey(market)
	if err != nil {
		return nil, err
	}
	now := time.Now
	if opts.NowFn != nil {
		now = opts.NowFn
	}
	best := map[string]*strategy.Signal{} // code→当前最优（置信最高的一条）
	var order []string                    // 保持首见顺序，输出确定性好（测试/展示都受益）
	for _, ev := range evs {
		if data.NormalizeMarketKey(ev.Market) != m {
			continue // 市场章不符=错腿投喂，整条丢弃
		}
		code := xEventCode(m, ev)
		if code == "" {
			continue // 解析不出标的代码：错标的比漏标的危害大（与 newsagent 适配同纪律）
		}
		s := senti[ev.URL]
		if s.Direction != "利好" && s.Direction != "利空" {
			continue // 中性/未判（打分器没覆盖的 URL 零值也落在这里）
		}
		if opts.MinConfidence > 0 && s.Confidence < opts.MinConfidence {
			continue
		}
		action := strategy.ActionBuy
		if s.Direction == "利空" {
			action = strategy.ActionSell
			if opts.BearEnabled {
				action = strategy.ActionShortOpen
			}
		}
		ts := ev.PublishedAt
		if ts.IsZero() {
			ts = now()
		}
		sig := &strategy.Signal{
			Code: code, Name: code, Type: strategy.SignalNewsX, Action: action,
			Priority:   strategy.P3,
			Reason:     fmt.Sprintf("事件情绪[%s %s %.2f]：%s", s.Source, s.Direction, s.Confidence, ev.Title),
			Confidence: s.Confidence, Timestamp: ts.Unix(),
			StrategyName: "事件情绪·" + m, Market: m,
		}
		if cur, ok := best[code]; !ok {
			best[code] = sig
			order = append(order, code)
		} else if sig.Confidence > cur.Confidence {
			best[code] = sig
		}
	}
	out := make([]strategy.Signal, 0, len(order))
	for _, code := range order {
		out = append(out, *best[code])
	}
	return out, nil
}

// xEventCode 事件→可交易代码：CRYPTO 取 Symbol（币对）；US 取裸 Ticker。
// 拿不到（空/缺段）返回 ""，由调用方整条跳过。
func xEventCode(market string, ev data.XEvent) string {
	if market == "CRYPTO" {
		return ev.Symbol
	}
	return ev.Ticker
}

// SignalsFromEventsBear §战法批 newsagent 事件链（PLAN §6.10 第三条腿）的空头 fork：
// 与 SignalsFromEvents 同三关，唯一差异=利空在 bearEnabled 时改产 short_open。
// 独立签名不动老口子（CN 链路口零调用零变化，xasset 自用面）。
func SignalsFromEventsBear(events []newsagent.NewsEvent, market string, bearEnabled bool) ([]strategy.Signal, error) {
	sigs, err := SignalsFromEvents(events, market)
	if err != nil || !bearEnabled {
		return sigs, err
	}
	for i := range sigs {
		if sigs[i].Action == strategy.ActionSell && sigs[i].Type == strategy.SignalNewsX {
			// 利空票在 newsagent 侧 Direction 已判（适配层不再判向）：卖出→开空一一 fork。
			sigs[i].Action = strategy.ActionShortOpen
		}
	}
	return sigs, nil
}
