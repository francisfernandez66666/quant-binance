// 文件职责：SEC EDGAR **8-K 全量 Atom feed 拉取客户端**（PLAN §ENH-A4，US 事件腿上游）。
// 8-K=重大事件即时披露（盈利/并购/高管变动/退市…），是 US 面最粗颗粒的事件源。
//
// ⚠ 端点形状=**待首尔机/有网机实测项**（本机 api 可达性未测）：
//
//	路径常量 EDGAR8KPath 按公开文档口径定为 browse-edgar 的 getcurrent Atom 导出；
//	老端点 srqsb 形态（?text=form-type=8-K…）仅作背景知识，不在本实现使用。
//	真实字段/条目形状以首台有网机抓取为准，届时只需对齐解析器，不动客户端骨架。
//
// 解析纪律：encoding/xml 按 **Atom XML 宽容解析**——命名空间不敏感（encoding/xml 按
// local name 匹配），published 缺失回退 updated，link 取第一个带 href 的；ticker 从
// 摘要文本宽容提取（"(CIK 0001234567)" 纯 CIK 形态=提不到→空串如实呈现，绝不编造）。
//
// SEC 强制 UA 政策：EDGAR 自动化访问要求带**含联系邮箱的 User-Agent**，空 UA 会被
// 限流/拉黑——故 UA 字段必填，FetchRecent8K 在空 UA 时**直接拒发**（错误信息，不裸奔）。
//
// 失败语义：网络失败/非 200/XML 解不动 → 空集 + error，绝不 panic、绝不返回编造数据。
//
// English: SEC EDGAR 8-K Atom feed puller for the US event leg. Base URL injectable
// (default sec.gov); endpoint path is documented-shape only and pending a live probe.
// UA with contact email is mandatory (SEC policy) — empty UA refuses to send. Atom is
// parsed leniently; ticker extraction is best-effort and yields "" when absent.
package data

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// EDGARDefaultBase EDGAR 缺省基址（httptest 注入时覆盖）。
const EDGARDefaultBase = "https://www.sec.gov"

// EDGAR8KPath 8-K 全量最新提交 Atom 路径（**公开文档口径，待实测**，见文件头 ⚠）。
const EDGAR8KPath = "/cgi-bin/browse-edgar?action=getcurrent&type=8-K&output=atom"

// edgarDefaultTimeout 单次拉取缺省超时（公共摘要接口，10s 足够且防挂死）。
const edgarDefaultTimeout = 10 * time.Second

// EDGARClient SEC EDGAR Atom 拉取客户端（只读公共面，无任何密钥）。
type EDGARClient struct {
	// Base 基址，空串=EDGARDefaultBase；构造后恒带结尾去斜杠。
	Base string
	// UA 必填的含联系邮箱 User-Agent（SEC 自动化访问政策）；空=FetchRecent8K 拒发。
	UA string
	// HTTPClient 注入的 http 客户端（nil=缺省 Timeout 构造）。
	HTTPClient *http.Client
}

// NewEDGARClient 构造 EDGAR 客户端。base 空=缺省 sec.gov；UA 原样存入（不在此处拒，
// 拒发放行决策收在 FetchRecent8K 一处，方便测试与后续包装）。
func NewEDGARClient(base, ua string) *EDGARClient {
	if strings.TrimSpace(base) == "" {
		base = EDGARDefaultBase
	}
	return &EDGARClient{
		Base:       strings.TrimSuffix(base, "/"),
		UA:         ua,
		HTTPClient: &http.Client{Timeout: edgarDefaultTimeout},
	}
}

// *_ATOM 结构体：encoding/xml 按 local name 匹配，atom/媒体命名空间均不敏感。

type edgarFeed struct {
	XMLName xml.Name     `xml:"feed"`
	Entries []edgarEntry `xml:"entry"`
}

type edgarEntry struct {
	Title     string `xml:"title"`
	Published string `xml:"published"`
	Updated   string `xml:"updated"`
	Summary   string `xml:"summary"`
	Links     []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
	} `xml:"link"`
}

// FetchRecent8K 拉取最近 8-K 提交并归一为 []XEvent（Market="US"；Symbol 留空由
// 接线批定，Ticker=宽容提取的股票代码，提不到=空串）。limit>0 时作为 count 参数
// 附带（<=0 则不传、吃服务端缺省窗口）。
// 任何失败=空集+error（UA 空=拒发；HTTP 非 2xx=报错；XML 解不动=报错）。
func (c *EDGARClient) FetchRecent8K(limit int) ([]XEvent, error) {
	if strings.TrimSpace(c.UA) == "" {
		// SEC 强制 UA 政策：空 UA 裸奔=拉黑风险，宁可不发。
		return nil, fmt.Errorf("edgar: UA 为空（SEC 自动化访问政策要求含联系邮箱的 User-Agent），拒发请求")
	}
	full := c.Base + EDGAR8KPath
	if limit > 0 {
		full += "&count=" + strconv.Itoa(limit)
	}
	req, err := http.NewRequest(http.MethodGet, full, nil)
	if err != nil {
		return nil, fmt.Errorf("edgar: 构造请求失败: %v", err)
	}
	req.Header.Set("User-Agent", strings.TrimSpace(c.UA))
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: edgarDefaultTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("edgar: 请求失败（网络/超时）")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// 不裸读错误体入日志：EDGAR 报错页可能很大；只报状态码。
		return nil, fmt.Errorf("edgar: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8MB 上限防呆
	if err != nil {
		return nil, fmt.Errorf("edgar: 读取响应失败")
	}
	var feed edgarFeed
	// §MR-EDGAR-ENC：EDGAR Atom 头声明 encoding="ISO-8859-1"，encoding/xml 无
	// CharsetReader 时直接拒解（现网 100% 失败：xml: declared but Decoder.CharsetReader is nil）。
	// 零新依赖处置：注入 Latin-1→UTF-8 转换 reader（x/text 属新依赖，红线禁）。
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.CharsetReader = latin1ToUTF8Reader
	if err := dec.Decode(&feed); err != nil {
		return nil, fmt.Errorf("edgar: Atom XML 解析失败: %v", err)
	}
	events := make([]XEvent, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		ev := XEvent{
			Market:      "US",
			Ticker:      extractEDGARTicker(e.Title, e.Summary),
			Title:       strings.TrimSpace(e.Title),
			URL:         firstEDGARLink(e),
			PublishedAt: parseEDGARTime(e.Published, e.Updated),
		}
		events = append(events, ev)
	}
	return events, nil
}

// firstEDGARLink 取 entry 第一个非空 href（Atom 常见 self/alternate 多 link，
// 宽容策略：不挑 rel，有 href 就用——接线批若要按 rel 精选再收口）。
func firstEDGARLink(e edgarEntry) string {
	for _, l := range e.Links {
		if h := strings.TrimSpace(l.Href); h != "" {
			return h
		}
	}
	return ""
}

// parseEDGARTime Atom 时间宽容解析：published 优先、缺失回退 updated；
// RFC3339 → 无时区 ISO → 纯日期 三式轮询，全失败=零值 time.Time（如实呈现）。
func parseEDGARTime(published, updated string) time.Time {
	for _, s := range []string{published, updated} {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
			return t.UTC()
		}
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// EDGAR 摘要里 ticker 的两种公开形态（真实样本待首尔机固化，先按宽容两式）：
//
//	"ACME Industries Inc. (CIK 0001000000)"                 → 纯 CIK，无 ticker → 空串
//	"ACME Industries Inc. (ACME, CIK 0001000000)"           → ticker 在前
//	"ACME Industries Inc. (CIK 0001000000, ACME)"           → ticker 在后（宽容兼收）
var (
	edgarTickerBeforeCIK = regexp.MustCompile(`(?i)\(\s*([^()]+?)\s*,\s*CIK\s*\d{6,10}\s*\)`)
	edgarTickerAfterCIK  = regexp.MustCompile(`(?i)\(\s*CIK\s*\d{6,10}\s*,\s*([^()]+?)\s*\)`)
	// edgarTickerShape 合法 ticker 字符集校验：字母数字加分隔点/杠（BRK.B 类），
	// 含空格/中文/括号的一律视为「不是 ticker」→ 空串（错标的比漏标的危害大）。
	edgarTickerShape = regexp.MustCompile(`^[A-Za-z0-9.\-]{1,6}$`)
)

// extractEDGARTicker 按传入文本顺序（标题优先、摘要兜底）宽容提 ticker；提不到=空串。
func extractEDGARTicker(texts ...string) string {
	for _, t := range texts {
		if tk := edgarTickerIn(t); tk != "" {
			return tk
		}
	}
	return ""
}

func edgarTickerIn(s string) string {
	for _, re := range []*regexp.Regexp{edgarTickerBeforeCIK, edgarTickerAfterCIK} {
		m := re.FindStringSubmatch(s)
		if len(m) < 2 {
			continue
		}
		cand := strings.TrimSpace(strings.ToUpper(m[1]))
		// 公司名尾巴带 "INC"/"CORP" 之类不是 ticker；仅长度/字符集双闸收口，
		// 拿不准就空串——本批只做候选，误标由接线批的方向判定链再拦。
		if edgarTickerShape.MatchString(cand) {
			return cand
		}
	}
	return ""
}

// latin1ToUTF8Reader xml.Decoder 的 CharsetReader 注入件（§MR-EDGAR-ENC）。
// EDGAR 的 Atom 头声明 ISO-8859-1；Go 标准库 encoding/xml 不带转换件时遇非 UTF-8
// 声明直接报错，而 x/text/html/charset 属新依赖（红线禁）——这里手转 Latin-1 单字节
// 为等价码点的 UTF-8 双字节。windows-1252 按 Latin-1 宽容处理（SEC 正文只到重音字母，
// 0x80-0x9F 区间实际不出现）；其余编码如实报不支持。
// English: stdlib-only CharsetReader converting ISO-8859-1 bytes to UTF-8; other
// declared encodings surface as explicit unsupported errors rather than silent garbage.
func latin1ToUTF8Reader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "iso-8859-1", "iso8859-1", "latin-1", "latin1", "windows-1252":
	default:
		return nil, fmt.Errorf("edgar: 不支持的 XML 编码 %q", charset)
	}
	raw, err := io.ReadAll(io.LimitReader(input, 8<<20))
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(raw)+64)
	for _, b := range raw {
		if b < 0x80 {
			out = append(out, b)
			continue
		}
		out = append(out, 0xC0|b>>6, 0x80|b&0x3F)
	}
	return bytes.NewReader(out), nil
}
