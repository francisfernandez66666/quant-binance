// 文件职责：§战法批（2026-09-23）XEvent 利多利空打分器——事件腿（EDGAR 8-K / CryptoPanic
// 热帖）只有原始候选（XEvent 不带方向，见 xasset_events.go 文件头），本文件把「标题→方向」
// 这层判定补齐：**关键词基线恒在 + 大模型批量打分可选**，key 没配/调用失败一律回落基线
// （fail-open 到关键词而不是出不了分），按 URL 缓存避免同一帖反复计费。
//
// 语义纪律：
//  1. 方向词表取中文口径（"利好"/"利空"/"中性"），与 newsagent 事件链同词汇——
//     下游 xasset 两条事件源共用一套方向语；
//  2. LLM 只做分类不做交易建议：提示词限定输出 d∈{bullish,bearish,neutral}+置信度，
//     响应解析全程防御式（剥 fenced 代码块、找首尾方括号），解析不出的条目按关键词票，
//     绝不信半截 JSON；
//  3. 密钥红线：api_key 只进请求头，不进日志/错误串（错误里回状态码与截断正文）；
//  4. 零新依赖：纯 stdlib（net/http/encoding json），与全仓纪律一致。
//
// English: §XASSET batch — sentiment scorer for raw XEvent candidates. A keyword baseline is
// always available; an optional OpenAI-compatible LLM refines the batch and fails open to the
// baseline on any error. Directions reuse the newsagent vocabulary (利好/利空/中性) so one
// downstream adapter consumes both event sources. Credentials never enter logs; stdlib only.
package data

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// XEventSentiment 单事件方向判定结果。
//   - Direction：利好 | 利空 | 中性（与 newsagent 方向词汇同源）；
//   - Confidence：0~1 判定强度（关键词基线封顶低于 LLM，见下）；
//   - Source："keyword"（基线）或 "llm"（大模型票）。
type XEventSentiment struct {
	Direction  string  `json:"direction"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source"`
}

// 关键词基线置信度区间：命中越多越信，但基线封顶 0.6——留出让位给 LLM（0.9 级），
// 也让派发闸（dispatch.MinConfidence 缺省 0.6）天然只放强关键词票过线。
const (
	keywordBaseConf = 0.3
	keywordConfStep = 0.1
	keywordConfCap  = 0.6
)

// 中英混合词表（小写匹配英文；中文原文匹配）。EDGAR 8-K 标题高度模板化（"Merger"
// "Bankruptcy" "Material Definitive Agreement"），CryptoPanic 标题带大量市场黑话——
// 两族词都收进来，误伤率由「双向命中相抵」压住。
var (
	bullishKeywords = []string{
		"surge", "rally", "rallies", "soar", "soars", "jump", "jumps", "record high",
		"beat", "upgrade", "upgraded", "buyback", "buybacks",
		"approval", "approved", "clearance", "partnership", "expand", "expands",
		"breakthrough", "wins", "award", "dividend", "acquisition", "merger",
		"inflow", "inflows", "adoption", "etf", "bullish", "gain", "gains", "rebound",
		"回购", "增持", "获批", "合作", "突破", "新高", "超预期", "利好", "上涨", "反弹",
	}
	bearishKeywords = []string{
		"crash", "plunge", "plunges", "tumble", "tumbles", "sink", "sinks", "drop", "drops",
		"fall", "falls", "slump", "miss", "misses", "downgrade", "downgraded",
		"hack", "hacked", "exploit", "exploited", "breach", "stolen", "lawsuit",
		"charges", "investigation", "probe", "delist", "delisting", "bankruptcy",
		"insolven", "liquidation", "default", "fined", "penalty", "outflow", "outflows",
		"bearish", "collapse", "selloff", "sell-off", "dump", "ban", "banned", "fraud",
		"下跌", "暴跌", "被盗", "清算", "违约", "诉讼", "调查", "处罚", "退市", "利空", "爆仓",
	}
)

// ScoreXEventTitle 关键词基线打分（导出：xasset 侧回落与单测直用）。
// 双向计数相抵定方向，命中次数决定强度；零命中/平票=中性（不猜方向）。
func ScoreXEventTitle(title string) XEventSentiment {
	l := strings.ToLower(title)
	bull, bear := 0, 0
	for _, k := range bullishKeywords {
		if strings.Contains(l, k) {
			bull++
		}
	}
	for _, k := range bearishKeywords {
		if strings.Contains(l, k) {
			bear++
		}
	}
	if bull == bear {
		return XEventSentiment{Direction: "中性", Confidence: 0, Source: "keyword"}
	}
	hits, d := bull, "利好"
	if bear > bull {
		hits, d = bear, "利空"
	}
	conf := keywordBaseConf + float64(hits-1)*keywordConfStep
	if conf > keywordConfCap {
		conf = keywordConfCap
	}
	return XEventSentiment{Direction: d, Confidence: conf, Source: "keyword"}
}

// *_LLM OpenAI 兼容批量打分客户端

// XEventLLMOptions 大模型客户端参数（任一键空=不成器，调用方回落纯关键词）。
type XEventLLMOptions struct {
	BaseURL    string // OpenAI 兼容根（如 https://api.deepseek.com/v1）
	APIKey     string
	Model      string
	TimeoutSec int // <=0 走缺省 30
}

// XEventLLMClient OpenAI 兼容 chat/completions 的最小批量分类客户端（stdlib only）。
type XEventLLMClient struct {
	base    string
	key     string
	model   string
	timeout time.Duration
	http    *http.Client // 测试可换
}

// NewXEventLLMClient 构造；任一键为空返回 (nil,false)=纯关键词模式（调用方零特判）。
func NewXEventLLMClient(opt XEventLLMOptions) (*XEventLLMClient, bool) {
	base := strings.TrimRight(strings.TrimSpace(opt.BaseURL), "/")
	if base == "" || strings.TrimSpace(opt.APIKey) == "" || strings.TrimSpace(opt.Model) == "" {
		return nil, false
	}
	to := time.Duration(opt.TimeoutSec) * time.Second
	if to <= 0 {
		to = 30 * time.Second
	}
	return &XEventLLMClient{base: base, key: strings.TrimSpace(opt.APIKey), model: strings.TrimSpace(opt.Model),
		timeout: to, http: &http.Client{Timeout: to}}, true
}

// llmBatchCap 单次批量标题数上限（一次请求吃完一票事件，超出分批发，控响应截断风险）。
const llmBatchCap = 40

// llmSystemPrompt 限定输出形态的分类器系统提示（只要 JSON、不许解释）。
const llmSystemPrompt = "你是金融新闻情绪分类器。只输出 JSON 数组，每个元素形如 {\"i\":序号,\"d\":\"bullish|bearish|neutral\",\"c\":0到1的置信度}。不要输出任何解释文字。"

// ScoreTitles 批量判定标题（返回与入参等长；LLM 漏答的下标为零值，由调用方按关键词补）。
// 任何链路/解析失败都返回 error，由上层 fail-open 到关键词基线——本函数绝不部分造假。
func (c *XEventLLMClient) ScoreTitles(ctx context.Context, titles []string) ([]XEventSentiment, error) {
	out := make([]XEventSentiment, len(titles))
	if len(titles) == 0 {
		return out, nil
	}
	for start := 0; start < len(titles); start += llmBatchCap {
		end := start + llmBatchCap
		if end > len(titles) {
			end = len(titles)
		}
		batch, err := c.scoreBatch(ctx, titles[start:end])
		if err != nil {
			return nil, err
		}
		copy(out[start:end], batch)
	}
	return out, nil
}

// scoreBatch 单批一次 chat/completions：序号回显对齐（模型漏答/错位按零值留给关键词补）。
func (c *XEventLLMClient) scoreBatch(ctx context.Context, batch []string) ([]XEventSentiment, error) {
	var sb strings.Builder
	for i, t := range batch {
		fmt.Fprintf(&sb, "%d. %s\n", i, t)
	}
	reqBody := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": llmSystemPrompt},
			{"role": "user", "content": sb.String()},
		},
		"temperature": 0,
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.key) // 密钥只进请求头
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm 请求失败: %v", err) // err 面无 URL query/头，不映出密钥
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llm 状态码 %d: %s", res.StatusCode, truncateErrBody(string(body)))
	}
	content, err := chatCompletionContent(body)
	if err != nil {
		return nil, err
	}
	return parseSentimentJSON(content, len(batch))
}

// chatCompletionContent 从 OpenAI 兼容响应里取 choices[0].message.content。
func chatCompletionContent(body []byte) (string, error) {
	var v struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return "", fmt.Errorf("llm 响应解析失败: %w", err)
	}
	if len(v.Choices) == 0 {
		return "", fmt.Errorf("llm 响应无 choices")
	}
	return v.Choices[0].Message.Content, nil
}

// parseSentimentJSON 防御式解析模型输出：剥 fenced 代码块、截取首尾方括号再 unmarshal；
// 方向词收 bullish/bearish/neutral 与中文利好/利空/中性两套；越界/词表外/零置信=当作没答，
// 留零值给关键词补位。
func parseSentimentJSON(content string, n int) ([]XEventSentiment, error) {
	s := content
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		if j := strings.Index(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	l, r := strings.Index(s, "["), strings.LastIndex(s, "]")
	if l < 0 || r <= l {
		return nil, fmt.Errorf("llm 输出无 JSON 数组")
	}
	var items []struct {
		I int     `json:"i"`
		D string  `json:"d"`
		C float64 `json:"c"`
	}
	if err := json.Unmarshal([]byte(s[l:r+1]), &items); err != nil {
		return nil, fmt.Errorf("llm 数组解析失败: %w", err)
	}
	out := make([]XEventSentiment, n)
	for _, it := range items {
		if it.I < 0 || it.I >= n {
			continue
		}
		d := ""
		switch strings.ToLower(strings.TrimSpace(it.D)) {
		case "bullish", "利好":
			d = "利好"
		case "bearish", "利空":
			d = "利空"
		case "neutral", "中性":
			d = "中性"
		}
		if d == "" || it.C <= 0 {
			continue
		}
		conf := it.C
		if conf > 1 {
			conf = 1
		}
		out[it.I] = XEventSentiment{Direction: d, Confidence: conf, Source: "llm"}
	}
	return out, nil
}

// truncateErrBody 错误面正文截断（防大响应进日志；不含请求头所以无密钥面）。
func truncateErrBody(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if utf8.RuneCountInString(s) > 160 {
		return string([]rune(s)[:160]) + "…"
	}
	return s
}

// *_SCORER 编排面：关键词打底 + LLM 精修 + URL 缓存

// XEventScorer 事件打分编排器（进程级一份，挂在引擎装配面）：
//   - 每个 URL 只判一次（LLM 票按 URL 缓存；关键词票零成本每轮现算保证新鲜）；
//   - LLM 批量只喂"本轮未缓存"的 URL，失败整批回落关键词——
//     计费外呼永远排在证据链最后面。
type XEventScorer struct {
	mu      sync.Mutex
	llm     *XEventLLMClient
	cache   map[string]XEventSentiment // URL→LLM 票（只存成功判过的）
	callMu  sync.Mutex                 // 串行化 LLM 外呼（多市场腿共一条车道，防并发双计费）
	timeout time.Duration
}

// NewXEventScorer llm 可空=纯关键词模式。
func NewXEventScorer(llm *XEventLLMClient) *XEventScorer {
	s := &XEventScorer{llm: llm, cache: map[string]XEventSentiment{}}
	if llm != nil {
		s.timeout = llm.timeout
	}
	return s
}

// LLMEnabled 大模型精修是否在位（观测面展示判定源构成）。
func (s *XEventScorer) LLMEnabled() bool { return s != nil && s.llm != nil }

// ScoreEvents 批量出向（key=URL）：全量先落关键词基线，LLM 在位时对本轮未缓存 URL
// 批量精修并回写缓存；LLM 失败不影响返回（拿到的就是关键词票，fail-open）。
func (s *XEventScorer) ScoreEvents(ctx context.Context, evs []XEvent) map[string]XEventSentiment {
	out := make(map[string]XEventSentiment, len(evs))
	var pending []string // 未缓存、待 LLM 精修的标题（与 pendingURL 同序登记）
	var pendingURL []string
	for _, ev := range evs {
		s.mu.Lock()
		cached, hit := s.cache[ev.URL]
		s.mu.Unlock()
		if hit {
			out[ev.URL] = cached
			continue
		}
		out[ev.URL] = ScoreXEventTitle(ev.Title)
		if s.LLMEnabled() && ev.URL != "" {
			pending = append(pending, ev.Title)
			pendingURL = append(pendingURL, ev.URL)
		}
	}
	if len(pending) == 0 {
		return out
	}
	cctx := ctx
	if s.timeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, s.timeout+5*time.Second) // 客户端超时之外的兜底伞
		defer cancel()
	}
	s.callMu.Lock() // 多腿并发只留一条外呼车道
	defer s.callMu.Unlock()
	votes, err := s.llm.ScoreTitles(cctx, pending)
	if err != nil || len(votes) != len(pending) {
		return out // fail-open：out 已是关键词票；缓存未回写，下轮自然重试
	}
	for i, url := range pendingURL {
		v := votes[i]
		if v.Direction == "" {
			continue // 模型漏答该条：维持关键词票、不进缓存（下轮还会重试）
		}
		s.mu.Lock()
		s.cache[url] = v
		s.mu.Unlock()
		out[url] = v
	}
	return out
}
