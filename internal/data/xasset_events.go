// 文件职责：US/CRYPTO 事件腿（PLAN §ENH-A4 + §ENH-B8）的**共用候选事件容器**。
// xasset 的 NewsEvents 战法吃的是 newsagent 已判方向的事件（IsMaterial/Direction/
// CleanedStocks），而 US 面（SEC EDGAR 8-K）与 CRYPTO 面（CryptoPanic 热帖）目前
// 没有事件上游——本批只建「拉取客户端 + 归一容器」，**不改 xasset、不做方向判定**：
// XEvent 是原始候选（哪家公司/哪个币、何时、何题、何链），去重、价值初筛、方向
// 判定、盖市章都属于后续接线批。
// English: candidate event container feeding the future US/CRYPTO news leg. These raw
// events carry no direction; scoring/dedup policy belongs to the wiring batch. Only
// URL+day idempotent dedupe lives here.
package data

import "time"

// XEvent US/CRYPTO 事件腿候选事件（EDGAR、CryptoPanic 等上游统一归一到本结构）。
// 字段口径：
//   - Market：市场章，"US" 或 "CRYPTO"（与 xasset normalizeMarketKey 口径一致）；
//   - Symbol：市场内可交易代码（CRYPTO 面 = 币对如 BTCUSDT；US 面暂留空，接线批定）；
//   - Ticker：上游文本里能提出的股票代码（提不到=空串如实呈现，绝不编造）；
//   - Title/URL：事件标题与原链（URL 兼作去重键的一部分）；
//   - PublishedAt：发布时间（UTC 语义由消费侧再收口，这里只保证解析成功与否）。
type XEvent struct {
	Market      string
	Symbol      string
	Ticker      string
	Title       string
	URL         string
	PublishedAt time.Time
}

// DedupeEvents 按 **URL + 日键**（PublishedAt.UTC() 的 YYYY-MM-DD）幂等去重：
// 同一原链同一天只保留首次出现（跨日重发视为新事件），保持输入顺序、不改动元素。
// 本批只需这层最粗的幂等（滚动拉取窗口重叠时的重复投喂）；更细的指纹去重留给接线批。
func DedupeEvents(events []XEvent) []XEvent {
	if len(events) == 0 {
		return events
	}
	seen := make(map[string]struct{}, len(events))
	out := make([]XEvent, 0, len(events))
	for _, ev := range events {
		key := ev.URL + "|" + ev.PublishedAt.UTC().Format("2006-01-02")
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ev)
	}
	return out
}
