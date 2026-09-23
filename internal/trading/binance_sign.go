// 文件职责：币安 REST 查询串签名（PLAN_BINANCE_MULTI_ASSET §2.1/§15.1 -1021）。
// HMAC-SHA256(queryString, secret) → &signature=<hex>；timestamp/recvWindow 统一注入；
// 服务器校时（GET /api/v3/time）维护本地时钟偏移 offsetMs——-1021（时间戳越界）时用
// signedRetryAfterResync 重同步后仅重签一次。密钥只驻内存，不落日志（maskKey 脱敏）。
// English: Binance REST query signing — HMAC-SHA256 with timestamp/recvWindow injection and
// server-time drift correction (offset from /api/v3/time, one resync-and-resign on -1021).
// Secrets stay in memory and are never logged (masked via maskKey).
package trading

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// binanceRecvWindow 签名请求的时间窗（毫秒）——§2.1 缺省 5000、上限 60000，取缺省值。
const binanceRecvWindow = 5000

// binanceSigner 单个 REST 根地址的签名器：api key/secret + 校时偏移。
// QMT 链的"网关 token"在币安侧的对应物；HTTP 客户端由执行器共享注入。
type binanceSigner struct {
	base     string       // REST 根（https://api.binance.com 或 testnet 现货域）
	apiKey   string       // X-MBX-APIKEY
	secret   string       // HMAC 密钥（只用于签名，绝不入日志/回显）
	httpc    *http.Client // 共享 HTTP 客户端（超时来自 BinanceConfig.TimeoutSec）
	offsetMs atomic.Int64 // 服务器时间 − 本地时间（毫秒）；-1021 校时后更新
}

func newBinanceSigner(base, apiKey, secret string, httpc *http.Client) *binanceSigner {
	return &binanceSigner{base: strings.TrimSuffix(base, "/"), apiKey: apiKey, secret: secret, httpc: httpc}
}

// signQuery 组装签名查询串：params + timestamp(+offset) + recvWindow，按 Encode() 的稳定序
// 生成待签原文，再追加 signature=<hex>。签名原文与实际发送串逐字节一致（同一 Encode 结果）。
// English: builds the signed query — params + timestamp(+server offset) + recvWindow, HMAC over the
// exact encoded string that goes on the wire.
func (s *binanceSigner) signQuery(params url.Values) string {
	q := url.Values{}
	for k, vs := range params {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	q.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli()+s.offsetMs.Load(), 10))
	q.Set("recvWindow", strconv.Itoa(binanceRecvWindow))
	base := q.Encode()
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(base))
	return base + "&signature=" + hex.EncodeToString(mac.Sum(nil))
}

// syncTimeOffset 拉取服务器校时端点并更新偏移：GET {spotBase}/api/v3/time → {"serverTime":ms}。
// 启动预热与 -1021 后重签前各用一次；失败仅返回错误，偏移保持旧值（fail-safe：下一次请求
// 仍按旧偏移签名，最坏再触发一次 -1021 重同步，不会静默漂移）。
// English: fetch /api/v3/time and refresh the clock offset; failures keep the old offset (worst case
// is one more -1021 resync, never a silent drift).
func (s *binanceSigner) syncTimeOffset(spotBase string) error {
	spotBase = strings.TrimSuffix(spotBase, "/")
	resp, err := s.httpc.Get(spotBase + "/api/v3/time")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("binance time sync: HTTP %d", resp.StatusCode)
	}
	var body struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<10)).Decode(&body); err != nil {
		return err
	}
	if body.ServerTime <= 0 {
		return fmt.Errorf("binance time sync: serverTime=%d 非法", body.ServerTime)
	}
	s.offsetMs.Store(body.ServerTime - time.Now().UnixMilli())
	return nil
}

// maskKey 日志脱敏：只保留前 4 后 4（同 qmt token_masked 惯例）。
// English: log-safe key mask (first/last 4 chars), mirroring the qmt token_masked convention.
func maskKey(k string) string {
	if len(k) <= 8 {
		return "****"
	}
	return k[:4] + "****" + k[len(k)-4:]
}
