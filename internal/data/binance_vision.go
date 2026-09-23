// 文件职责：data.binance.vision **归档日线/月线**下载与校验（Phase 3 第 1 项，PLAN §2.8 历史面）。
// REST /api/v3/klines 只给最近若干根，长历史回测必须走归档分片：
//
//	https://data.binance.vision/data/spot/{daily|monthly}/klines/<SYM>/<interval>/<SYM>-<interval>-<yyyymmdd>.zip
//	同名 + ".CHECKSUM"（内容形如 "<sha256>  <filename>"，PLAN §2.8）
//
// 三条纪律：
//  1. 基址可注入（缺省 BinanceVisionHistoryBase，httptest 能整段回放）——本机不可达时不假装验过；
//  2. **校验和通过才解析**：SHA256 不匹配直接 error，绝不"坏数据凑合用"（回测结果被静默污染
//     比拉不到数据严重一个量级）；
//  3. 归档 CSV 的时间口径与 REST 不同（PLAN §2.8：现货归档用"自 2025-01-01 起的微秒"），
//     统一走 normalizeBinanceEpoch，可用 DateColumnUnit 显式覆盖，禁止靠数量级猜。
//
// English: daily/monthly kline archive downloader (zip + SHA256 .CHECKSUM) with injectable base
// URL. Archives are verified before parsing; timestamps go through the single epoch normalizer.
package data

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BinanceVisionArchive 归档客户端参数。
type BinanceVisionArchive struct {
	Base string
	// Client 注入点（httptest / 代理 / 超时定制）；nil=30s 缺省。
	Client *http.Client
	// DateColumnUnit 归档首列口径；空=BinanceEpochVision（自 2025-01-01 的微秒）。
	DateColumnUnit BinanceEpochUnit
	// VerifyChecksum 关闭则跳过 .CHECKSUM 往返（本机不可达/离线自检时显式关掉，别静默降级）。
	VerifyChecksum bool
}

// NewBinanceVisionArchive 构造归档客户端（base 为空取缺省公共镜像）。
func NewBinanceVisionArchive(base string) *BinanceVisionArchive {
	if strings.TrimSpace(base) == "" {
		base = BinanceVisionHistoryBase
	}
	return &BinanceVisionArchive{
		Base:           strings.TrimSuffix(base, "/"),
		Client:         &http.Client{Timeout: 30 * time.Second},
		VerifyChecksum: true,
	}
}

// ArchiveURL 拼归档 zip 地址：1mo 走 monthly 目录（PLAN §2.8 月线口径是 1mo，不是 1M）。
func (a *BinanceVisionArchive) ArchiveURL(symbol, interval, date string) string {
	freq := "daily"
	if strings.HasPrefix(interval, "1mo") {
		freq = "monthly"
	}
	sym := normalizeBinanceSymbol(symbol)
	return fmt.Sprintf("%s/data/spot/%s/klines/%s/%s/%s-%s-%s.zip",
		a.Base, freq, sym, interval, sym, interval, strings.TrimSpace(date))
}

// FetchKLines 拉单个归档分片 → []KLine（校验不通过/解压失败/全脏行都返回 error）。
func (a *BinanceVisionArchive) FetchKLines(ctx context.Context, symbol, interval, date string) ([]KLine, error) {
	zipURL := a.ArchiveURL(symbol, interval, date)
	body, err := a.getRaw(ctx, zipURL)
	if err != nil {
		return nil, err
	}
	if a.VerifyChecksum {
		sum, serr := a.getRaw(ctx, zipURL+".CHECKSUM")
		if serr != nil {
			return nil, fmt.Errorf("binance vision %s: 校验和拉取失败（拒用未验证数据）: %w", date, serr)
		}
		want := wantSHA256(sum)
		if want == "" {
			return nil, fmt.Errorf("binance vision %s: 校验和格式无法识别（拒用未验证数据）", date)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(body)); got != want {
			return nil, fmt.Errorf("binance vision %s: SHA256 不匹配 want=%s got=%s", date, want, got)
		}
	}
	return parseBinanceVisionZip(body, a.DateColumnUnit)
}

// getRaw 拉原始字节（zip 与 checksum 共用；非 200 直接 error，不吞状态码）。
func (a *BinanceVisionArchive) getRaw(ctx context.Context, full string) ([]byte, error) {
	cl := a.Client
	if cl == nil {
		cl = &http.Client{Timeout: 30 * time.Second}
	}
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, full, nil)
	if err != nil {
		return nil, err
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("binance vision 请求失败 %s: %w", full, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20)) // 64MB 上限（单月分片远小于此）
	if err != nil {
		return nil, fmt.Errorf("binance vision 读体失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance vision HTTP %d: %s", resp.StatusCode, truncateForLog(raw))
	}
	return raw, nil
}

// wantSHA256 从 "<hash>  <filename>" 取 hash；非 64 位十六进制返回 ""（=无法校验）。
func wantSHA256(sum []byte) string {
	fields := strings.Fields(string(sum))
	if len(fields) == 0 {
		return ""
	}
	h := strings.ToLower(fields[0])
	if len(h) != 64 {
		return ""
	}
	for _, r := range h {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return h
}
