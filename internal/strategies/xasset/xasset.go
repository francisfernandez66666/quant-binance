// Package xasset 新市场最小战法包（PLAN_BINANCE_MULTI_ASSET Phase 3 第 2 项）：
// 面向 US/CRYPTO 的三条最小信号腿——MA 均线交叉、RSI 动量、NewsAgent 事件适配。
//
// 设计纪律：
//  1. **信号产出直接带 Market**（§6.10）：构造期锁定市场键（只收 US/CRYPTO），
//     GenerateSignal 把 Market 原样盖到信号上——下游 dispatch/风控不再从代码格式反推市场；
//  2. 实盘口径只依赖 KLines（data.KLine 收盘序列），不碰 CN 专属字段（资金流/板块/BenchChg）：
//     这些字段在币安行情链路（quotes feed）里不存在，依赖它们的战法在新市场必然退化；
//  3. 数据不足一律 nodata（Pass=false），宁可不发信号也不给猜测分——与 §9"未确认即拒"同姿势；
//  4. !Pass 时 GenerateSignal 返回 (nil, nil)，与 CN 四大形态/因子战法同惯例，调用方零特判。
//
// English: minimal strategies for the new markets (MA cross, RSI momentum, news-event
// adaptation). Each strategy is constructed against one market key (US or CRYPTO) and stamps
// Signal.Market at emission; scoring uses only the daily close series, and insufficient data
// degrades to nodata rather than a guessed score.
package xasset

// *_XASSET_CORE 市场收口/收盘序列/参数容器
import (
	"fmt"
	"time"

	"quant-trading-v2/internal/data"
	"quant-trading-v2/internal/newsagent"
	"quant-trading-v2/internal/strategy"
	"quant-trading-v2/internal/strategy_engine"
)

// normalizeMarketKey 构造期市场收口：归一后必须是 US/CRYPTO，CN 直接拒
// （CN 链路的战法生态已完备，本包绝不参与——"加市场不换市场"的反向保险）。
func normalizeMarketKey(market string) (string, error) {
	switch m := data.NormalizeMarketKey(market); m {
	case "US", "CRYPTO":
		return m, nil
	default:
		return "", fmt.Errorf("xasset: 市场键必须是 US/CRYPTO（收到 %q）", market)
	}
}

// minBars 最低可用 K 线根数：慢窗 20 + 交叉判定需要的前一窗 1，留 1 根余量。
const minBars = 22

// closesOf 从实盘行情里取收盘序列（日K）。md.Price>0 时用最新价覆盖最后一根收盘——
// 盘中决策不能等收盘（币安 24H 市场"今天的收盘"可能就是 3 小时前）。返回 nil=数据不可用。
func closesOf(md *strategy_engine.StockMarketData) []float64 {
	if md == nil || len(md.KLines) < minBars {
		return nil
	}
	out := make([]float64, 0, len(md.KLines))
	for _, k := range md.KLines {
		if k.Close <= 0 {
			continue // 0 收盘=脏行（缺数据占位），整根剔除而不是让它污染均值
		}
		out = append(out, k.Close)
	}
	if md.Price > 0 && len(out) > 0 {
		out[len(out)-1] = md.Price
	}
	if len(out) < minBars {
		return nil
	}
	return out
}

// sma 简单移动平均末值（序列短于 n 时返回 0=不可得，调用方按 nodata 处理）。
func sma(xs []float64, n int) float64 {
	if len(xs) < n || n <= 0 {
		return 0
	}
	sum := 0.0
	for _, v := range xs[len(xs)-n:] {
		sum += v
	}
	return sum / float64(n)
}

// Params MACross/RSI 共用参数容器（<=0 落缺省；构造期归一，运行期不再改）。
type Params struct {
	FastN  int     // MA 快窗（缺省 5）
	SlowN  int     // MA 慢窗（缺省 20）
	RSIN   int     // RSI 周期（缺省 14）
	MinRsi float64 // RSI 超卖回升买点下沿（缺省 30）
	MaxRsi float64 // RSI 过热不追顶闸值（缺省 70）
}

// withDefaults 缺省回填（"零值=未配置"惯例，同 NormalizeBinance）。
func (p Params) withDefaults() Params {
	if p.FastN <= 0 {
		p.FastN = 5
	}
	if p.SlowN <= 0 {
		p.SlowN = 20
	}
	if p.RSIN <= 0 {
		p.RSIN = 14
	}
	if p.MinRsi <= 0 {
		p.MinRsi = 30
	}
	if p.MaxRsi <= 0 {
		p.MaxRsi = 70
	}
	return p
}

// *_XASSET_MA 均线交叉战法

// MACross 均线交叉战法：快线上穿慢线（金叉）且慢线自身抬头才放行。
// English: moving-average cross strategy — buy on fast-over-slow golden cross with a rising slow MA.
type MACross struct {
	market string
	p      Params
}

// NewMACross 构造（market∈{US,CRYPTO}；参数零值走缺省）。
func NewMACross(market string, p Params) (*MACross, error) {
	m, err := normalizeMarketKey(market)
	if err != nil {
		return nil, err
	}
	p = p.withDefaults()
	if p.FastN >= p.SlowN {
		return nil, fmt.Errorf("xasset: 快窗 %d 必须小于慢窗 %d", p.FastN, p.SlowN)
	}
	return &MACross{market: m, p: p}, nil
}

// Name 策略中文名称（带市场章，前端战法列可直接区分两条市场链）。
func (s *MACross) Name() string { return "均线交叉·" + s.market }

// Type 信号类型。
func (s *MACross) Type() strategy.SignalType { return strategy.SignalMACross }

// Evaluate 金叉判定：本根 fast>slow 且上根 fast<=slow（穿越**事件**，不是位置关系——
// 持续多头不误发新单），慢线斜率>=0 排除"下降途中反抽"。
func (s *MACross) Evaluate(code string, raw interface{}) (*strategy.Evaluation, error) {
	md, _ := raw.(*strategy_engine.StockMarketData)
	closes := closesOf(md)
	if closes == nil {
		// nodata 姿势统一：不报错（引擎按 Level 分流），只给 0 分并关闭闸门。
		return &strategy.Evaluation{Level: "nodata", Reasons: map[string]string{"gate": fmt.Sprintf("日K不足 %d 根", minBars)}}, nil
	}
	n := len(closes)
	if n < s.p.SlowN+2 {
		return &strategy.Evaluation{Level: "nodata", Reasons: map[string]string{"gate": "K 线不足以同时算两根快慢均线"}}, nil
	}
	// 上根窗口 = 去掉最后一根的尾部对齐计算（无需前缀和）。
	curFast, curSlow := sma(closes, s.p.FastN), sma(closes, s.p.SlowN)
	prevFast, prevSlow := sma(closes[:n-1], s.p.FastN), sma(closes[:n-1], s.p.SlowN)
	prevSlowWin := sma(closes[:n-2], s.p.SlowN) // 慢线再前一窗（判抬头的基线）
	golden := curFast > curSlow && prevFast <= prevSlow
	rising := curSlow >= prevSlowWin
	pass := golden && rising
	level := "fail"
	if pass {
		level = "full_chain"
	}
	reasons := map[string]string{
		"cross": fmt.Sprintf("快线 %.4f（上根 %.4f）/ 慢线 %.4f（上根 %.4f）", curFast, prevFast, curSlow, prevSlowWin),
	}
	if golden && !rising {
		reasons["gate"] = "金叉但慢线仍下行：下降途中反抽，不放行"
	} else if !golden {
		reasons["gate"] = "本根无金叉事件（位置关系不等于穿越）"
	}
	conf := 0.0
	if pass {
		conf = 0.6 + 0.2*(curFast-curSlow)/curSlow // 穿越力度越大置信度越高（比值无单位依赖）
		if conf > 0.9 {
			conf = 0.9
		}
	}
	score := 0.0
	if pass {
		score = 100
	}
	return &strategy.Evaluation{
		TotalScore: score,
		Details:    map[string]float64{"fast": curFast, "slow": curSlow, "prev_fast": prevFast, "prev_slow": prevSlow},
		Pass:       pass, Level: level, Confidence: conf, Reasons: reasons,
	}, nil
}

// GenerateSignal 过闸才出信号；Market 章在此盖下（构造期锁定的市场键原样透传）。
func (s *MACross) GenerateSignal(code string, eval *strategy.Evaluation) (*strategy.Signal, error) {
	if eval == nil || !eval.Pass {
		return nil, nil
	}
	return &strategy.Signal{
		Code: code, Type: s.Type(), Action: strategy.ActionBuy, Priority: strategy.P3,
		Reason:     "币安均线金叉（快上穿慢且慢线抬头）",
		Confidence: eval.Confidence, Timestamp: time.Now().Unix(),
		StrategyName: s.Name(), Market: s.market,
	}, nil
}

// *_XASSET_RSI RSI 动量战法

// RSIMomentum RSI 动量战法：超卖区上穿回升买（前一根 < MinRsi、本根 ≥ MinRsi 且 < MaxRsi），
// 过热不追顶。卖出腿交给持仓退出链（本包只产入场信号，与 CN 形态战法分工一致）。
// English: RSI momentum — buy the breakout up out of oversold (prev < floor, now ≥ floor and < ceiling);
// entry signals only, exits belong to the position-exit chain.
type RSIMomentum struct {
	market string
	p      Params
}

// NewRSIMomentum 构造（市场收口同 MACross；RSI 窗口至少要有 2 根差值序列可平滑）。
func NewRSIMomentum(market string, p Params) (*RSIMomentum, error) {
	m, err := normalizeMarketKey(market)
	if err != nil {
		return nil, err
	}
	p = p.withDefaults()
	if p.MinRsi >= p.MaxRsi {
		return nil, fmt.Errorf("xasset: RSI 下沿 %.1f 必须小于上沿 %.1f", p.MinRsi, p.MaxRsi)
	}
	return &RSIMomentum{market: m, p: p}, nil
}

// Name 策略中文名称。
func (s *RSIMomentum) Name() string { return "RSI动量·" + s.market }

// Type 信号类型。
func (s *RSIMomentum) Type() strategy.SignalType { return strategy.SignalRSI }

// rsiSeries Wilder 平滑 RSI 序列（长度 len(closes)-1，首项为种子均值）。
// 首窗用简单平均做种子、其后 (prev*(n-1)+x)/n 递推——与标准 Wilder RSI 同式，
// 序列前端若干根是"预热区"，判定只看末两根（预热误差已收敛）。
func rsiSeries(closes []float64, n int) []float64 {
	if len(closes) < n+2 {
		return nil
	}
	gains := make([]float64, 0, len(closes)-1)
	losses := make([]float64, 0, len(closes)-1)
	for i := 1; i < len(closes); i++ {
		d := closes[i] - closes[i-1]
		if d > 0 {
			gains = append(gains, d)
			losses = append(losses, 0)
		} else {
			gains = append(gains, 0)
			losses = append(losses, -d)
		}
	}
	avgG, avgL := sma(gains[:n], n), sma(losses[:n], n) // 种子窗
	out := make([]float64, 0, len(gains))
	out = append(out, rsiOf(avgG, avgL))
	for i := n; i < len(gains); i++ { // Wilder 递推
		avgG = (avgG*float64(n-1) + gains[i]) / float64(n)
		avgL = (avgL*float64(n-1) + losses[i]) / float64(n)
		out = append(out, rsiOf(avgG, avgL))
	}
	return out
}

// rsiOf 由平均涨/跌幅出 RSI（avgL=0 → 100；双零（走平）→ 50 中性，不判超买超卖）。
func rsiOf(avgG, avgL float64) float64 {
	if avgL == 0 {
		if avgG == 0 {
			return 50
		}
		return 100
	}
	return 100 - 100/(1+avgG/avgL)
}

// Evaluate 超卖回升判定：末两根 RSI 穿越 MinRsi 上沿、且未过热。
func (s *RSIMomentum) Evaluate(code string, raw interface{}) (*strategy.Evaluation, error) {
	md, _ := raw.(*strategy_engine.StockMarketData)
	closes := closesOf(md)
	series := rsiSeries(closes, s.p.RSIN)
	if series == nil {
		// 同 MACross 的 nodata 姿势：数据不够就不打分，绝不用半截序列硬凑。
		return &strategy.Evaluation{Level: "nodata", Reasons: map[string]string{"gate": fmt.Sprintf("RSI%d 需要至少 %d 根收盘", s.p.RSIN, s.p.RSIN+2)}}, nil
	}
	prev, cur := series[len(series)-2], series[len(series)-1]
	buy := prev < s.p.MinRsi && cur >= s.p.MinRsi && cur < s.p.MaxRsi
	level := "fail"
	if buy {
		level = "full_chain"
	}
	reasons := map[string]string{"rsi": fmt.Sprintf("上根 %.1f → 本根 %.1f（下沿 %.0f / 上沿 %.0f）", prev, cur, s.p.MinRsi, s.p.MaxRsi)}
	if !buy {
		switch {
		case prev >= s.p.MinRsi:
			reasons["gate"] = "上根未处超卖区：不构成回升事件"
		case cur >= s.p.MaxRsi:
			reasons["gate"] = "回升直冲过热区：不追顶"
		}
	}
	conf := 0.0
	score := 0.0
	if buy {
		score = 100
		conf = 0.55 + 0.25*(cur-s.p.MinRsi)/(s.p.MaxRsi-s.p.MinRsi) // 回升越深置信度越高，封顶 0.8
		if conf > 0.8 {
			conf = 0.8
		}
	}
	return &strategy.Evaluation{
		TotalScore: score,
		Details:    map[string]float64{"rsi": cur, "rsi_prev": prev},
		Pass:       buy, Level: level, Confidence: conf, Reasons: reasons,
	}, nil
}

// GenerateSignal 过闸才出信号，Market 章同 MACross。
func (s *RSIMomentum) GenerateSignal(code string, eval *strategy.Evaluation) (*strategy.Signal, error) {
	if eval == nil || !eval.Pass {
		return nil, nil
	}
	return &strategy.Signal{
		Code: code, Type: s.Type(), Action: strategy.ActionBuy, Priority: strategy.P3,
		Reason:     "币安 RSI 超卖回升（穿下沿且未过热）",
		Confidence: eval.Confidence, Timestamp: time.Now().Unix(),
		StrategyName: s.Name(), Market: s.market,
	}, nil
}

// *_XASSET_NEWS 新闻事件适配

// NewsEvents NewsAgent 事件适配战法：把已带方向的事件转成 US/CRYPTO 信号
// （PLAN §6.10 第三条腿——CN 的 LLM 事件链产出的 RelatedStocks/CleanedStocks 里出现
// 美股/币安标的时，不再走 A 股分发，而是盖上市章交给对应市场 Controller）。
// 适配层**不做方向再判定**：IsMaterial=false 或中性一律不出信号，事件链说没有价值就没有价值。
// English: adapter turning scored NewsAgent events into market-stamped signals; no re-adjudication
// of direction here — non-material or neutral events emit nothing.
type NewsEvents struct{ market string }

// NewNewsEvents 构造（市场收口同前两法）。
func NewNewsEvents(market string) (*NewsEvents, error) {
	m, err := normalizeMarketKey(market)
	if err != nil {
		return nil, err
	}
	return &NewsEvents{market: m}, nil
}

// Name 策略中文名称。
func (n *NewsEvents) Name() string { return "新闻事件·" + n.market }

// Type 信号类型。
func (n *NewsEvents) Type() strategy.SignalType { return strategy.SignalNewsX }

// Evaluate 事件形态不走近视评分：Level=event、Pass 由调用侧按 SignalsFromEvents 结果决定。
// （保持 strategy.Strategy 接口完整可注册；主用法是 SignalsFromEvents。）
func (n *NewsEvents) Evaluate(code string, raw interface{}) (*strategy.Evaluation, error) {
	return &strategy.Evaluation{Level: "event", Reasons: map[string]string{"how": "事件适配走 SignalsFromEvents，不走单股评分"}}, nil
}

// GenerateSignal 事件战法不走 评分→信号 两步流水（事件即信号），此实现仅为接口完备。
func (n *NewsEvents) GenerateSignal(code string, eval *strategy.Evaluation) (*strategy.Signal, error) {
	return nil, nil
}

// SignalsFromEvents 批量事件→信号：只吃 个股级 + 有方向 + 过价值初筛 的事件；
// 标的取 CleanedStocks（"名称|代码"），解析不出代码的事件整体跳过（错标的比漏标的危害大）。
// 置信度=|Score| 归一（事件强度分通常 0~10，按 /10 夹到 0~1），Priority 统一 P3（人工/风控链再升）。
func SignalsFromEvents(events []newsagent.NewsEvent, market string) ([]strategy.Signal, error) {
	m, err := normalizeMarketKey(market)
	if err != nil {
		return nil, err
	}
	out := make([]strategy.Signal, 0, len(events))
	for _, ev := range events {
		if !ev.IsMaterial || (ev.Direction != "利好" && ev.Direction != "利空") || ev.Level != "个股" {
			continue // 三关：价值初筛 / 明确方向 / 个股级（板块/宏观不落到单票）
		}
		action := strategy.ActionBuy
		if ev.Direction == "利空" {
			action = strategy.ActionSell
		}
		conf := ev.Score
		if conf < 0 {
			conf = -conf
		}
		conf = conf / 10
		if conf > 1 {
			conf = 1
		}
		for _, cs := range ev.CleanedStocks {
			code, name := splitCleanedStock(cs)
			if code == "" {
				continue
			}
			out = append(out, strategy.Signal{
				Code: code, Name: name, Type: strategy.SignalNewsX, Action: action,
				Priority: strategy.P3, Reason: ev.Title, Confidence: conf,
				Timestamp: time.Now().Unix(), StrategyName: "新闻事件·" + m, Market: m,
			})
		}
	}
	return out, nil
}

// splitCleanedStock 解析 "名称|代码"（newsagent 清洗格式）；无竖线/空代码 → ("","")。
func splitCleanedStock(s string) (code, name string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '|' {
			return s[i+1:], s[:i]
		}
	}
	return "", ""
}
