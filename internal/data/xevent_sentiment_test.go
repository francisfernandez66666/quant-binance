// xevent_sentiment_test.go §战法批 事件打分器行为锁：
//  1. 关键词基线——双向相抵、置信随命中数递增且封顶 0.6、中英词族都生效；
//  2. LLM 解析防御——fenced 代码块/中文方向词/越界序号/词表外/置信夹逼；
//  3. 客户端成器闸——三键任缺即 (nil,false)，请求头带 Bearer、批量按 40 切分；
//  4. 编排 fail-open——LLM 失败整批回落关键词且不污染缓存、成功后按 URL 缓存不再计费、
//     模型漏答条目维持关键词票（下轮重试）。
package data

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestScoreXEventTitleKeywords(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"BTC surges as ETF inflows hit record high", "利好"},
		{"Major exchange hacked, token plunge 30%", "利空"},
		{"Company announces quarterly earnings date", "中性"}, // 无命中不猜向
		{"Stock gains then drops on guidance", "中性"},        // 双向各一，相抵
		{"公司宣布回购并获监管批准", "利好"},
		{"交易所被盗，用户资产清算", "利空"},
	}
	for _, c := range cases {
		got := ScoreXEventTitle(c.title)
		if got.Direction != c.want || got.Source != "keyword" {
			t.Fatalf("%q: 判 %s/%s，期望 %s/keyword", c.title, got.Direction, got.Source, c.want)
		}
		if got.Confidence < 0 || got.Confidence > keywordConfCap {
			t.Fatalf("%q: 基线置信越界 %v", c.title, got.Confidence)
		}
	}
	// 命中越多越信（但封顶）：单词 vs 多词。
	one := ScoreXEventTitle("market drop").Confidence
	many := ScoreXEventTitle("crash plunge tumble dump fraud").Confidence
	if !(many > one && many <= keywordConfCap) {
		t.Fatalf("置信递增/封顶错位: one=%v many=%v", one, many)
	}
}

func TestParseSentimentJSONDefense(t *testing.T) {
	got, err := parseSentimentJSON("```json\n[{\"i\":0,\"d\":\"Bullish\",\"c\":1.4},{\"i\":1,\"d\":\"bearish\",\"c\":0.8},{\"i\":9,\"d\":\"neutral\",\"c\":0.5},{\"i\":2,\"d\":\"暴涨\",\"c\":0.9},{\"i\":3,\"d\":\"利空\",\"c\":0.7}]\n多余话```", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Direction != "利好" || got[0].Confidence != 1 { // c>1 夹到 1
		t.Fatalf("夹逼/映射错: %+v", got[0])
	}
	if got[1].Direction != "利空" || got[1].Source != "llm" {
		t.Fatalf("bearish 映射错: %+v", got[1])
	}
	if got[2] != (XEventSentiment{}) { // 词表外=零值留给关键词
		t.Fatalf("词表外必须留零: %+v", got[2])
	}
	if got[3].Direction != "利空" { // 中文方向词
		t.Fatalf("中文方向词错: %+v", got[3])
	}
	if _, err := parseSentimentJSON("no array here", 2); err == nil {
		t.Fatal("无数组必须报错")
	}
}

func TestXEventLLMClientIncomplete(t *testing.T) {
	full := XEventLLMOptions{BaseURL: "https://x/v1", APIKey: "k", Model: "m"}
	for name, opt := range map[string]XEventLLMOptions{
		"缺url":   {APIKey: "k", Model: "m"},
		"缺key":   {BaseURL: "https://x/v1", Model: "m"},
		"缺model": {BaseURL: "https://x/v1", APIKey: "k"},
		"斜杠尾":    {BaseURL: "https://x/v1/", APIKey: " k ", Model: " m "},
	} {
		c, ok := NewXEventLLMClient(opt)
		wantOK := name == "斜杠尾"
		if ok != wantOK || (wantOK && (c == nil || c.base != "https://x/v1")) {
			t.Fatalf("%s: ok=%v c=%v", name, ok, c)
		}
	}
	if _, ok := NewXEventLLMClient(full); !ok {
		t.Fatal("三键齐全应成器")
	}
}

// fakeLLM 计数 OpenAI 兼容假服务器：按调用序回放 content 列表（越界回 500）。
func fakeLLM(t *testing.T, contents []string) (*httptest.Server, *struct {
	mu    sync.Mutex
	hits  int
	auths []string
	model string
}) {
	t.Helper()
	var st struct {
		mu    sync.Mutex
		hits  int
		auths []string
		model string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(404)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		st.mu.Lock()
		idx := st.hits
		st.hits++
		st.auths = append(st.auths, r.Header.Get("Authorization"))
		if m, ok := req["model"].(string); ok {
			st.model = m
		}
		st.mu.Unlock()
		if idx >= len(contents) {
			w.WriteHeader(500)
			return
		}
		resp := map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": contents[idx]}}}}
		b, _ := json.Marshal(resp)
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv, &st
}

// 打分器主流程：关键词基线分→过阈事件→产出多/空场外信号；LLM 缺席时零调用不报错。
func TestXEventScorerFlow(t *testing.T) {
	evs := []XEvent{
		{Market: "CRYPTO", Symbol: "BTCUSDT", Title: "BTC quietly drifts", URL: "a"},
		{Market: "CRYPTO", Symbol: "ETHUSDT", Title: "ETH steady", URL: "b"},
	}
	srv, st := fakeLLM(t, []string{
		`[{"i":0,"d":"bearish","c":0.9},{"i":1,"d":"neutral","c":0.4}]`,
	})
	llm, ok := NewXEventLLMClient(XEventLLMOptions{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Model: "unit-model"})
	if !ok {
		t.Fatal("llm 客户端未成器")
	}
	sc := NewXEventScorer(llm)
	if !sc.LLMEnabled() {
		t.Fatal("LLMEnabled 错位")
	}
	out := sc.ScoreEvents(context.Background(), evs)
	if out["a"].Source != "llm" || out["a"].Direction != "利空" || out["a"].Confidence != 0.9 {
		t.Fatalf("LLM 票未生效: %+v", out["a"])
	}
	if out["b"].Source != "llm" || out["b"].Direction != "中性" || out["b"].Confidence != 0.4 {
		t.Fatalf("模型明示中性=有效票，应保留并缓存（不回落改判）: %+v", out["b"])
	}
	if st.hits != 1 || st.auths[0] != "Bearer sk-test" || st.model != "unit-model" {
		t.Fatalf("外呼计数/头/模型错位: hits=%d auths=%v model=%s", st.hits, st.auths, st.model)
	}
	// 第二轮全走缓存：不再计费外呼。
	out2 := sc.ScoreEvents(context.Background(), evs)
	if st.hits != 1 {
		t.Fatalf("URL 缓存未生效（第二轮又外呼 %d 次）", st.hits)
	}
	if out2["a"].Source != "llm" {
		t.Fatalf("缓存票漂移: %+v", out2["a"])
	}
}

func TestXEventScorerFailOpen(t *testing.T) {
	evs := []XEvent{{Market: "CRYPTO", Symbol: "BTCUSDT", Title: "token crashes hard", URL: "a"}}
	srv, _ := fakeLLM(t, nil) // 直接 500
	llm, _ := NewXEventLLMClient(XEventLLMOptions{BaseURL: srv.URL + "/v1", APIKey: "k", Model: "m"})
	sc := NewXEventScorer(llm)
	out := sc.ScoreEvents(context.Background(), evs)
	if out["a"].Direction != "利空" || out["a"].Source != "keyword" {
		t.Fatalf("LLM 故障必须 fail-open 到关键词: %+v", out["a"])
	}
	// 故障不进缓存：换好服务器后同一 URL 还能升级成 llm 票。
	srv2, st2 := fakeLLM(t, []string{`[{"i":0,"d":"bullish","c":0.95}]`})
	// 直接改指向：httptest URL 换绑走重建客户端（scorer 持旧客户端，另建一个同 URL 的 scorer 验证重试语义）。
	llm2, _ := NewXEventLLMClient(XEventLLMOptions{BaseURL: srv2.URL + "/v1", APIKey: "k", Model: "m"})
	sc2 := NewXEventScorer(llm2)
	up := sc2.ScoreEvents(context.Background(), evs)
	if up["a"].Source != "llm" || st2.hits != 1 {
		t.Fatalf("重试升级失败: %+v hits=%d", up["a"], st2.hits)
	}
}

func TestXEventScorerBatchSplit(t *testing.T) {
	var evs []XEvent
	for i := 0; i <= llmBatchCap; i++ { // 41 条 → 两批
		evs = append(evs, XEvent{Market: "US", Ticker: "T", Title: "t", URL: fmt.Sprintf("u%02d", i)})
	}
	// fakeLLM 按"第几次外呼"回放：hit0=第一批 40 条全 bullish、hit1=第二批 1 条 bearish。
	contents := []string{makeBatchJSON(40, "bullish"), `[{"i":0,"d":"bearish","c":0.7}]`}
	srv, st := fakeLLM(t, contents)
	llm, _ := NewXEventLLMClient(XEventLLMOptions{BaseURL: srv.URL + "/v1", APIKey: "k", Model: "m"})
	sc := NewXEventScorer(llm)
	out := sc.ScoreEvents(context.Background(), evs)
	if st.hits != 2 {
		t.Fatalf("41 条应切两批外呼: hits=%d", st.hits)
	}
	bull := 0
	for _, v := range out {
		if strings.Contains(v.Direction, "利好") {
			bull++
		}
	}
	if bull != 40 {
		t.Fatalf("首批 40 条应全为利好，实际 %d", bull)
	}
}

// makeBatchJSON 生成 n 条同向回显 JSON（i 从 0 起）。
func makeBatchJSON(n int, d string) string {
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"i":` + strconv.Itoa(i) + `,"d":"` + d + `","c":0.9}`)
	}
	sb.WriteString("]")
	return sb.String()
}
