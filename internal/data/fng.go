// 文件职责：alternative.me Fear & Greed Index（加密货币恐慌贪婪指数）只读客户端
// （§ENH-A1，XASSET 加密情绪腿的外部证据源）。GET {Base}/fng/?limit=N 返回按时间倒序的
// 日频采样序列；本文件负责 HTTP 拉取、解析（value 0-100 字符串转 int，越界行拒收）与
// 缓存面（Last/Age/Refresh——atomic 持最后一次成功采样含到达时刻，Fetch 失败保留旧缓存，
// 情绪腿读到的永远是"最近一次真证据"而不是崩溃链）。
//
// 设计纪律：
//  1. 零新依赖：只用 net/http + encoding/json（go.mod 不动，与 StdWsDial 同姿势）；
//  2. 行级拒收：value 解析失败或越界 0-100 的单行直接丢弃（上游偶发脏行不毒化整批），
//     全批拒收视为 Fetch 失败——旧缓存保留，绝不返回"部分污染"的序列；
//  3. 缓存龄以**到达时刻**（Now()）计而非样本 timestamp 计：情绪相位判定关心的是
//     "这条证据多新鲜"，上游 timestamp 是日频粒度（每日一个点），拿它算龄会把当天
//     凌晨取到的数据在晚上误判为过期——与 §9 market_halt 的 TTL 证据同口径。
//
// English: read-only client for the alternative.me Fear & Greed Index — the external
// sentiment evidence source for the XASSET crypto leg. Fetch pulls and parses the daily
// series (out-of-range rows are rejected line-wise); Last/Age/Refresh expose an atomic
// cache of the most recent successful sample stamped with its arrival time, and a failed
// Fetch keeps the previous cache instead of erasing the evidence.
package data

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// FNG 采样值的合法域：恐慌贪婪指数定义为 0-100 整数刻度。
const (
	fngValueMin = 0
	fngValueMax = 100
)

// fngDefaultBase 官方免费接口地址（本机实测可达；测试经 Base 注入 httptest 假服务器）。
const fngDefaultBase = "https://api.alternative.me"

// FNGDefaultLimit Fetch()/Refresh() 缺省拉取条数：日频序列取两周足以覆盖周末断供，
// 且远小于上游上限 400，响应体千字节级。
const FNGDefaultLimit = 14

// FNGMaxLimit limit 硬上限（上游文档口径 400，超限截断而不是报错——调用方传错
// 一个巨数不该让情绪腿整条断供，截断即语义完整）。
const FNGMaxLimit = 400

// FNGSample 单条恐慌贪婪采样。Timestamp 来自上游 timestamp 字段（秒级，日频粒度），
// 缓存龄另按到达时刻计（见 fngCache），两者职责不同故分开承载。
type FNGSample struct {
	Value          int       // 0-100，越大越贪婪
	Classification string    // 上游文字档（"Extreme Greed"/"Fear" 等），原样保留供展示
	Timestamp      time.Time // 采样时刻（秒级精度，上游打点）
}

// fngCache 缓存单元：最后一次成功采样 + 到达时刻（Age 的分母）。
type fngCache struct {
	latest    FNGSample
	arrivedAt time.Time
}

// FNGClient 恐慌贪婪指数客户端。零值可用（Base/HTTPClient/Now 走缺省），
// 字段导出仅为测试注入（httptest 地址 / 短超时 / 虚拟时钟）。
type FNGClient struct {
	Base       string       // 空=https://api.alternative.me
	HTTPClient *http.Client // 缺省 10s Timeout（外部免费源，慢不得）
	Now        func() time.Time

	cached atomic.Pointer[fngCache] // 最后一次成功采样（含到达时刻）；Fetch 失败不动它
}

// httpClient 取生效的 HTTP 客户端：注入优先，零值走包级缺省实例（10s 超时）。
// 缺省实例是只读共享对象，并发 Do 由 net/http 自带安全，无需懒建加锁。
func (c *FNGClient) httpClient() *http.Client {
	if h := c.HTTPClient; h != nil {
		return h
	}
	return defaultFNGHTTPClient
}

// defaultFNGHTTPClient 包级缺省客户端（与 hithink 等外部源同一量级超时：10s）。
var defaultFNGHTTPClient = &http.Client{Timeout: 10 * time.Second}

// now 取生效时钟：注入优先，缺省 time.Now（缓存到达时刻的唯一时间源）。
func (c *FNGClient) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// base 取生效接口根地址（去尾斜杠；空走官方生产地址）。
func (c *FNGClient) base() string {
	b := strings.TrimSpace(c.Base)
	if b == "" {
		return fngDefaultBase
	}
	return strings.TrimRight(b, "/")
}

// fngRow 上游单行的原始 JSON 形态：value/timestamp 都是**字符串**（上游如此，
// 别按数字解——json.Number 解码实测对 "78" 可行但对空串/缺字段脆，显式字符串最稳）。
type fngRow struct {
	Value          string `json:"value"`
	Classification string `json:"value_classification"`
	Timestamp      string `json:"timestamp"`
}

// fngEnvelope 上游响应信封。error 字段非 null 时视为业务失败（HTTP 200 也要拒），
// 缓存保留旧值——外部源"活着但报错"和"挂了"对情绪腿是同一件事：没有新证据。
type fngEnvelope struct {
	Data  []fngRow        `json:"data"`
	Error json.RawMessage `json:"error"`
}

// Fetch 拉取最近 limit 条采样（limit≤0 走 FNGDefaultLimit，超 FNGMaxLimit 截断）。
// 成功（至少一条有效行）即刷新缓存；任何失败返回 error 且**保留旧缓存**。
func (c *FNGClient) Fetch(limit int) ([]FNGSample, error) {
	if limit <= 0 {
		limit = FNGDefaultLimit
	}
	if limit > FNGMaxLimit {
		limit = FNGMaxLimit // 超限截断：语义是"最多要这些"，不是错误
	}
	url := fmt.Sprintf("%s/fng/?limit=%d", c.base(), limit)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fng: 构造请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fng: 请求 %s 失败: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fng: HTTP %d", resp.StatusCode)
	}
	var env fngEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("fng: 响应解析失败: %w", err)
	}
	if len(env.Error) > 0 && string(env.Error) != "null" {
		return nil, fmt.Errorf("fng: 上游返回错误: %s", strings.TrimSpace(string(env.Error)))
	}
	samples := make([]FNGSample, 0, len(env.Data))
	for _, row := range env.Data {
		s, ok := parseFNGRow(row)
		if !ok {
			continue // 越界/坏行单条拒收，不毒化整批（纪律 2）
		}
		samples = append(samples, s)
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("fng: 响应无有效采样行（收到 %d 行全部拒收）", len(env.Data))
	}
	// 缓存写入取时间戳最大的一条（上游按倒序返回，但"最新"以数据自身为准，不赌序）。
	latest := samples[0]
	for _, s := range samples[1:] {
		if s.Timestamp.After(latest.Timestamp) {
			latest = s
		}
	}
	c.cached.Store(&fngCache{latest: latest, arrivedAt: c.now()})
	return samples, nil
}

// parseFNGRow 单行解析：value 必须是 0-100 整数字符串，timestamp 必须是正秒级整数，
// 任一不合格即拒收（返回 false）。classification 缺失不拒——文字档是展示件不是证据本体。
func parseFNGRow(row fngRow) (FNGSample, bool) {
	v, err := strconv.Atoi(strings.TrimSpace(row.Value))
	if err != nil || v < fngValueMin || v > fngValueMax {
		return FNGSample{}, false
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(row.Timestamp), 10, 64)
	if err != nil || secs <= 0 {
		return FNGSample{}, false
	}
	return FNGSample{Value: v, Classification: row.Classification, Timestamp: time.Unix(secs, 0).UTC()}, true
}

// Refresh 以缺省条数拉一次并刷新缓存（失败返回 error、旧缓存原样保留）。
// 调用节奏归装配层（§ENH-A1 约定：本客户端不起后台轮询）。
func (c *FNGClient) Refresh() error {
	_, err := c.Fetch(FNGDefaultLimit)
	return err
}

// Last 返回缓存中最后一次成功采样；从未成功过返回 (零值, false)——
// 下游据此走 SentimentPhase 的 ("unknown", false)，无证据绝不落中性档。
func (c *FNGClient) Last() (FNGSample, bool) {
	if p := c.cached.Load(); p != nil {
		return p.latest, true
	}
	return FNGSample{}, false
}

// Age 返回缓存龄（到达时刻至今）；无缓存返回 -1 哨兵（与 FeedStat 负数失活标记同姿势，
// 调用方拿 -1 必须走 unknown 分支而不是"龄 0 很新鲜"）。
func (c *FNGClient) Age() time.Duration {
	p := c.cached.Load()
	if p == nil {
		return -1
	}
	d := c.now().Sub(p.arrivedAt)
	if d < 0 {
		return 0 // 注入时钟回拨的防御：龄不为负
	}
	return d
}
