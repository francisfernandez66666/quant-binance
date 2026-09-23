// 文件职责：§MR-EDGAR-TKR EDGAR CIK→ticker 映射兜底（edgar.go 主链路的 fallback 腿）。
// 现网 8-K 标题真实形态 "8-K - ACME CORP (0001234567) (Filer)" 只带零补齐 10 位 CIK，
// 逗号式 extractEDGARTicker 提不到 ticker → 本文件从 SEC 公开的 company_tickers.json
// 建 CIK(去零十进制串)→ticker 映射。纪律：
//
//	· 进程级 load-once 缓存（成功才记忆；失败不记忆=下次可重试），映射故障绝不
//	  升级为事件腿故障——未命中如实留空串，绝不编造；
//	· UA 政策同主链路：空 UA 拒发（SEC 自动化访问政策）；
//	· stdlib only（encoding/json + regexp），零新依赖。
//
// English: CIK→ticker fallback map for the 8-K feed, loaded once from SEC's public
// company_tickers.json. Load failures never break the event leg: miss = empty ticker.
package data

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// EDGARTickersPath company_tickers.json 路径（公开文档口径，httptest 注入时随 Base 覆盖）。
const EDGARTickersPath = "/files/company_tickers.json"

// edgarPaddedCIKRe 标题中的零补齐 10 位 CIK 形态 "(0001234567)"（映射兜底的入口钥匙）。
var edgarPaddedCIKRe = regexp.MustCompile(`\((\d{10})\)`)

// edgarPaddedCIKKey 从 8-K 标题提取查表钥匙：匹配补齐 CIK、去前导零后的十进制串
// （company_tickers.json 的 cik 是裸整数）。无匹配/全零系统条目(CIK 0)一律空串=不查表。
// English: the lookup key — leading zeros stripped decimal CIK, or "" (no query).
func edgarPaddedCIKKey(title string) string {
	m := edgarPaddedCIKRe.FindStringSubmatch(title)
	if m == nil {
		return ""
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n <= 0 {
		return ""
	}
	return strconv.FormatInt(n, 10)
}

// edgarTickerCache 进程级 load-once 缓存：只缓存成功结果（失败可重试——映射拉取失败
// 通常是瞬时网络问题，缓存失败会把整进程剩余寿命的 ticker 全钉死为空）。
var edgarTickerCache struct {
	mu     sync.Mutex
	loaded bool
	m      map[string]string
}

// EDGARTickerMap 取 CIK→ticker 映射（键=去零十进制 CIK；永不返回 nil，最坏=空表）。
// base 空=EDGARDefaultBase；UA 空或拉取/解析失败=返回空表并留日志线索（error 只作
// 诊断，不影响调用方继续出事件）。
// English: the shared CIK→ticker map; any failure yields an empty map (event leg alive).
func EDGARTickerMap(base, ua string, client *http.Client) map[string]string {
	edgarTickerCache.mu.Lock()
	defer edgarTickerCache.mu.Unlock()
	if edgarTickerCache.loaded {
		return edgarTickerCache.m
	}
	m, err := fetchEDGATickerMap(base, ua, client)
	if err != nil {
		return map[string]string{} // 失败不记忆：下次调用可重试
	}
	edgarTickerCache.m = m
	edgarTickerCache.loaded = true
	return m
}

// ResetEDGARTickerCacheForTest 仅供测试重置进程级缓存（httptest 基址逐用例不同，
// 不重置会把第一个用例的表漏给第二个）。生产代码不得调用。
// English: test-only cache reset; production must never touch it.
func ResetEDGARTickerCacheForTest() {
	edgarTickerCache.mu.Lock()
	defer edgarTickerCache.mu.Unlock()
	edgarTickerCache.loaded = false
	edgarTickerCache.m = nil
}

// fetchEDGATickerMap 单次拉取+解析 company_tickers.json：
// {"0":{"company_name":"...","ticker":"AAPL","cik":320193},...} → map["320193"]="AAPL"。
func fetchEDGATickerMap(base, ua string, client *http.Client) (map[string]string, error) {
	if strings.TrimSpace(ua) == "" {
		return nil, fmt.Errorf("edgar: UA 为空，ticker 映射请求拒发（SEC 自动化访问政策）")
	}
	if strings.TrimSpace(base) == "" {
		base = EDGARDefaultBase
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSuffix(base, "/")+EDGARTickersPath, nil)
	if err != nil {
		return nil, fmt.Errorf("edgar: ticker 映射构造请求失败: %v", err)
	}
	req.Header.Set("User-Agent", strings.TrimSpace(ua))
	if client == nil {
		client = &http.Client{Timeout: edgarDefaultTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("edgar: ticker 映射请求失败（网络/超时）")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("edgar: ticker 映射 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16MB 防呆（全量表约 1MB 级）
	if err != nil {
		return nil, fmt.Errorf("edgar: ticker 映射读取失败")
	}
	var raw map[string]struct {
		Ticker string `json:"ticker"`
		CIK    int64  `json:"cik"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("edgar: ticker 映射 JSON 解析失败: %v", err)
	}
	out := make(map[string]string, len(raw))
	for _, r := range raw {
		if r.CIK <= 0 || strings.TrimSpace(r.Ticker) == "" {
			continue // 脏行跳过：宁缺勿错
		}
		out[strconv.FormatInt(r.CIK, 10)] = strings.ToUpper(strings.TrimSpace(r.Ticker))
	}
	return out, nil
}
