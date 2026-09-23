// 文件职责：CryptoPanic **热帖流拉取客户端**（PLAN §ENH-B8，CRYPTO 事件腿上游）。
// CryptoPanic 聚合加密行业新闻/社区热帖，按币种（currencies）拉热点posts，归一为
// []XEvent 候选喂后续 US/CRYPTO 事件链。
//
// ⚠ 响应形状=**公开文档口径，待真 key 实测**：端点 /api/v1/posts/、字段
//
//	（title/url/created_at/source.title/currency/votes）按官方文档构造夹具解析；
//	真 key 到手后只需对齐字段差异，不动客户端骨架。
//
// 惰性装配纪律（仓内「空配置=不装配」惯例）：AuthToken 构造时 TrimSpace，空串→
// Enabled() false、Fetch **直接拒**（一个请求都不发）；key 是否配置由接线批读
// config/env 决定，本客户端不 import config。
//
// 密钥红线：**key 绝不入日志、绝不入错误信息**——所有 error 串均为无密文样（如
// "cryptopanic: HTTP 401（key 无效/配额）"），传输层错误也不带原始 err（url.Error
// 会含带 auth_token 的完整 URL）。
//
// 失败语义：网络失败/非 200/JSON 解不动 → 空集 + error，绝不 panic、绝不编造。
//
// English: CryptoPanic hot-posts puller for the CRYPTO event leg. Empty token makes
// the whole client inert (Enabled false, Fetch refuses to send). The auth token never
// appears in any error string or log; response shape is documented-only pending a
// real-key probe.
package data

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CryptoPanicDefaultBase CryptoPanic 缺省基址（httptest 注入时覆盖）。
const CryptoPanicDefaultBase = "https://cryptopanic.com"

// cryptoPanicPostsPath 热帖流端点路径（公开文档口径，见文件头 ⚠）。
const cryptoPanicPostsPath = "/api/v1/posts/"

// cryptoPanicDefaultTimeout 单次拉取缺省超时。
const cryptoPanicDefaultTimeout = 10 * time.Second

// CryptoPanicClient CryptoPanic REST 客户端（只读公共 posts 面，唯一凭证=auth_token）。
type CryptoPanicClient struct {
	// AuthToken 已 TrimSpace 的 API key；空串=整客户端惰性（见 Enabled）。
	AuthToken string
	// Base 基址，构造后恒带结尾去斜杠（空=缺省 cryptopanic.com）。
	Base string
	// HTTPClient 注入的 http 客户端（nil=缺省 Timeout 构造）。
	HTTPClient *http.Client
}

// NewCryptoPanicClient 构造客户端。token 在此 TrimSpace（空=惰性的唯一判定点）；
// base 空=缺省基址。
func NewCryptoPanicClient(token, base string) *CryptoPanicClient {
	if strings.TrimSpace(base) == "" {
		base = CryptoPanicDefaultBase
	}
	return &CryptoPanicClient{
		AuthToken:  strings.TrimSpace(token),
		Base:       strings.TrimSuffix(base, "/"),
		HTTPClient: &http.Client{Timeout: cryptoPanicDefaultTimeout},
	}
}

// Enabled 是否配置了 key（false=整客户端惰性，Fetch 永不外呼）。
func (c *CryptoPanicClient) Enabled() bool { return c.AuthToken != "" }

// cryptoPanicResponse /api/v1/posts/ 响应外层（results 分页数组）。
type cryptoPanicResponse struct {
	Results []cryptoPanicPost `json:"results"`
}

// cryptoPanicPost 单条热帖（字段=公开文档口径；votes 结构宽容——数值/对象都收，
// 本批不入 XEvent，只保证不因其解不动而拖垮整包解析）。
type cryptoPanicPost struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
	Currency  string `json:"currency"`
	Source    struct {
		Title string `json:"title"`
	} `json:"source"`
	Votes struct {
		Up   int `json:"up"`
		Down int `json:"down"`
	} `json:"votes"`
}

// Fetch 拉取指定币种热帖并归一为 []XEvent（Market="CRYPTO"，Symbol=币种拼 USDT
// 如 BTC→BTCUSDT、空币=Symbol ""；Title/URL/PublishedAt 直投）。
// currencies 去空白去重后逗号拼接；limit>0 附带（<=0 吃服务端缺省）。
// 未配置 key=直接报错不发请求；任何失败=空集+error（错误串不含 token）。
func (c *CryptoPanicClient) Fetch(currencies []string, limit int) ([]XEvent, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("cryptopanic: AuthToken 为空（惰性客户端），拒发请求")
	}
	// 币种清洗：逐个去空白转大写，空串丢弃；手工 O(n²) 去重（保持首现顺序，量小无需 map）。
	cleaned := make([]string, 0, len(currencies))
	for _, cur := range currencies {
		cur = strings.ToUpper(strings.TrimSpace(cur))
		if cur == "" {
			continue
		}
		dup := false
		for _, got := range cleaned {
			if got == cur {
				dup = true
				break
			}
		}
		if !dup {
			cleaned = append(cleaned, cur)
		}
	}
	// 手工拼 query：auth_token 绝不入任何对外可见串（日志/错误）。
	full := c.Base + cryptoPanicPostsPath + "?auth_token=" + c.AuthToken
	if len(cleaned) > 0 {
		full += "&currencies=" + strings.Join(cleaned, ",")
	}
	if limit > 0 {
		full += "&limit=" + strconv.Itoa(limit)
	}
	req, err := http.NewRequest(http.MethodGet, full, nil)
	if err != nil {
		return nil, fmt.Errorf("cryptopanic: 构造请求失败")
	}
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: cryptoPanicDefaultTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		// 不回带原始 err：url.Error 文本含完整 URL（即含 auth_token）。
		return nil, fmt.Errorf("cryptopanic: 请求失败（网络/超时）")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("cryptopanic: HTTP %d（key 无效/配额）", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cryptopanic: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4MB 上限防呆
	if err != nil {
		return nil, fmt.Errorf("cryptopanic: 读取响应失败")
	}
	var parsed cryptoPanicResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("cryptopanic: JSON 解析失败")
	}
	events := make([]XEvent, 0, len(parsed.Results))
	for _, p := range parsed.Results {
		ev := XEvent{
			Market:      "CRYPTO",
			Symbol:      cryptoSymbol(p.Currency),
			Title:       strings.TrimSpace(p.Title),
			URL:         strings.TrimSpace(p.URL),
			PublishedAt: parseCryptoTime(p.CreatedAt),
		}
		events = append(events, ev)
	}
	return events, nil
}

// cryptoSymbol 币种→可交易代码：BTC→BTCUSDT；空币=Symbol ""（如实呈现，不猜基准币）。
func cryptoSymbol(currency string) string {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" {
		return ""
	}
	return cur + "USDT"
}

// parseCryptoTime created_at 宽容解析：RFC3339（含毫秒）→ 无时区 ISO → 纯日期；
// 全失败=零值 time.Time（不编时间）。
func parseCryptoTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
