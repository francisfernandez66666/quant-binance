// 文件职责：binance_contract_test.go——币安下单参数面与错误码分类的 golden 契约锁
// （PLAN_BINANCE_MULTI_ASSET §12.1 §15.1）。两份基线 JSON 落在 qmt_gateway/contract/：
//   - binance_order_fields.json：7 格（现货 LIMIT/市价买/市价卖 × 美股四格）实际下发到
//     柜台的"业务键集合"（鉴权键 timestamp/recvWindow/signature/apiKey 不计）——防参数面
//     漂移（多加/漏发/改名键即红），与 §2.2 四格矩阵禁填项互补：矩阵锁"该发什么"，
//     本锁"整组实际发了什么"。
//   - binance_error_codes.json：binance_errors.go 分类表全量快照——防误改处置语义
//     （可重试/致命/幂等命中三类位翻转即红）。
//
// 再生成约定与 order_fields.json 同族：ORDER 面用 BINANCE_ORDER_FIELDS_UPDATE=1、
// 错误码面用 BINANCE_ERROR_CODES_UPDATE=1 触发重写并 t.Skip，人工 review diff 后提交。
// English: golden contract locks for the Binance order parameter surface (7 cells, business
// keys only) and the error-code classification table; regenerate via env triggers with manual review.
package trading

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"quant-trading-v2/internal/config"
)

// binanceContractPath golden 契约 JSON 的仓库相对路径（internal/trading 测试工作目录）。
func binanceContractPath(name string) string {
	return filepath.Join("..", "..", "qmt_gateway", "contract", name)
}

// authParamKeys 鉴权层参数（签名器统一附加，不属于业务契约面）。
var authParamKeys = map[string]bool{"timestamp": true, "recvWindow": true, "signature": true, "apiKey": true}

// emittedBizKeys 从记录的原始 query 提取业务键集合（升序，去鉴权键）。
// 与 mustParam 同规则：签名总在尾部，截 "&signature=" 前段再解析。
func emittedBizKeys(t *testing.T, raw string) []string {
	t.Helper()
	q, _, _ := strings.Cut(raw, "&signature=")
	vals, err := url.ParseQuery(q)
	if err != nil {
		t.Fatalf("解析 query 失败: %v (%s)", err, q)
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		if !authParamKeys[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// equalStrings 有序切片逐元素相等。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// binanceOrderFieldsDoc 契约文件结构：来源说明 + 格名→业务键集合。
type binanceOrderFieldsDoc struct {
	Source string              `json:"source"`
	Cells  map[string][]string `json:"cells"`
}

// TestBinanceOrderFieldsGolden 用 mock 柜台实际下 7 单，逐格比对业务键集合。
func TestBinanceOrderFieldsGolden(t *testing.T) {
	m := newMockBinance(t)
	cells := []struct {
		name, market, side, path string
		req                      OrderRequest
	}{
		{"spot_limit", "CRYPTO", "buy", "/api/v3/order",
			OrderRequest{Market: "CRYPTO", SignalID: "golden-sl", Code: "btcusdt", Side: "buy", PriceType: "limit", Price: 65000.999, Qty: 1.2345}},
		{"spot_market_buy", "CRYPTO", "buy", "/api/v3/order",
			OrderRequest{Market: "CRYPTO", SignalID: "golden-smb", Code: "BTCUSDT", Side: "buy", PriceType: "market", Amount: 100}},
		{"spot_market_sell", "CRYPTO", "sell", "/api/v3/order",
			OrderRequest{Market: "CRYPTO", SignalID: "golden-sms", Code: "BTCUSDT", Side: "sell", PriceType: "market", Qty: 0.5}},
		{"us_buy_limit", "US", "buy", "/sapi/v1/equity/order",
			OrderRequest{Market: "US", SignalID: "golden-ubl", Code: "AAPL", Side: "buy", PriceType: "limit", Price: 231.5, Qty: 10, TradingSession: "RTH", TimeInForce: "DAY"}},
		{"us_buy_market", "US", "buy", "/sapi/v1/equity/order",
			OrderRequest{Market: "US", SignalID: "golden-ubm", Code: "AAPL", Side: "buy", PriceType: "market", Amount: 500}},
		{"us_sell_limit", "US", "sell", "/sapi/v1/equity/order",
			OrderRequest{Market: "US", SignalID: "golden-usl", Code: "AAPL", Side: "sell", PriceType: "limit", Price: 231.5, Qty: 10}},
		{"us_sell_market", "US", "sell", "/sapi/v1/equity/order",
			OrderRequest{Market: "US", SignalID: "golden-usm", Code: "AAPL", Side: "sell", PriceType: "market", Qty: 10}},
	}
	got := map[string][]string{}
	for _, c := range cells {
		e := executorFor(t, m, c.market)
		var res *OrderResult
		var err error
		if c.side == "buy" {
			res, err = e.PlaceBuy(c.req)
		} else {
			res, err = e.PlaceSell(c.req)
		}
		if err != nil || res == nil || !res.OK {
			t.Fatalf("格子 %s 下单应成功: err=%v res=%+v", c.name, err, res)
		}
		qs := m.queries(c.path)
		if len(qs) == 0 {
			t.Fatalf("格子 %s 未记录到 %s 请求", c.name, c.path)
		}
		got[c.name] = emittedBizKeys(t, qs[len(qs)-1])
	}
	path := binanceContractPath("binance_order_fields.json")
	if os.Getenv("BINANCE_ORDER_FIELDS_UPDATE") == "1" {
		doc := binanceOrderFieldsDoc{Source: "internal/trading/binance_contract_test.go (mock 柜台 7 格实发键面)", Cells: got}
		b, _ := json.MarshalIndent(doc, "", "  ")
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatalf("写 golden 契约失败: %v", err)
		}
		t.Skip("BINANCE_ORDER_FIELDS_UPDATE=1: 已重新生成 binance_order_fields.json，请人工 review diff")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 golden 契约失败（首跑请用 BINANCE_ORDER_FIELDS_UPDATE=1 生成）: %v", err)
	}
	var want binanceOrderFieldsDoc
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("解析 golden 契约失败: %v", err)
	}
	if len(want.Cells) != len(got) {
		t.Fatalf("格子数漂移: 契约 %d 实际 %d", len(want.Cells), len(got))
	}
	for name, gk := range got {
		wk, ok := want.Cells[name]
		if !ok {
			t.Fatalf("契约缺少格子 %s（新增参数面需显式再生成并 review）", name)
		}
		if !equalStrings(gk, wk) {
			t.Fatalf("格子 %s 键面漂移:\n  契约=%v\n  实发=%v", name, wk, gk)
		}
	}
}

// binanceErrCodeRow 错误码契约单行（omitempty：false/空串不落盘，diff 只呈现语义变化）。
type binanceErrCodeRow struct {
	Code          int    `json:"code"`
	Name          string `json:"name"`
	Retryable     bool   `json:"retryable,omitempty"`
	Fatal         bool   `json:"fatal,omitempty"`
	IdempotentHit bool   `json:"idempotent_hit,omitempty"`
	UserAction    string `json:"user_action,omitempty"`
}

// binanceErrCodesDoc 错误码契约文件结构。
type binanceErrCodesDoc struct {
	Source string              `json:"source"`
	Rows   []binanceErrCodeRow `json:"rows"`
}

// errCodeRowsFromTable 把分类表按码升序展平成契约行。
func errCodeRowsFromTable() []binanceErrCodeRow {
	codes := make([]int, 0, len(binanceErrTable))
	for code := range binanceErrTable {
		codes = append(codes, code)
	}
	sort.Ints(codes)
	rows := make([]binanceErrCodeRow, 0, len(codes))
	for _, code := range codes {
		c := binanceErrTable[code]
		rows = append(rows, binanceErrCodeRow{
			Code: c.Code, Name: c.Name, Retryable: c.Retryable,
			Fatal: c.Fatal, IdempotentHit: c.IdempotentHit, UserAction: c.UserAction,
		})
	}
	return rows
}

// TestBinanceErrorCodesGolden 锁死错误码表全量快照与关键处置语义位。
func TestBinanceErrorCodesGolden(t *testing.T) {
	rows := errCodeRowsFromTable()
	path := binanceContractPath("binance_error_codes.json")
	if os.Getenv("BINANCE_ERROR_CODES_UPDATE") == "1" {
		doc := binanceErrCodesDoc{Source: "internal/trading/binance_errors.go binanceErrTable 展平（码升序）", Rows: rows}
		b, _ := json.MarshalIndent(doc, "", "  ")
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatalf("写 golden 契约失败: %v", err)
		}
		t.Skip("BINANCE_ERROR_CODES_UPDATE=1: 已重新生成 binance_error_codes.json，请人工 review diff")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 golden 契约失败（首跑请用 BINANCE_ERROR_CODES_UPDATE=1 生成）: %v", err)
	}
	var want binanceErrCodesDoc
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("解析 golden 契约失败: %v", err)
	}
	if len(want.Rows) != len(rows) {
		t.Fatalf("错误码行数漂移: 契约 %d 代码 %d（增删码需显式再生成并 review）", len(want.Rows), len(rows))
	}
	for i := range rows {
		if rows[i] != want.Rows[i] {
			t.Fatalf("第 %d 行分类漂移:\n  契约=%+v\n  代码=%+v", i, want.Rows[i], rows[i])
		}
	}
	// 语义抽查（即使再生成也不许翻转的三条铁律）：
	if !classifyBinanceError(486410).Fatal {
		t.Fatal("486410 披露函未签必须是 Fatal（需人签署）")
	}
	if !classifyBinanceError(486449).IdempotentHit {
		t.Fatal("486449 重复 clientOrderId 必须走幂等回填，不得重下单")
	}
	if !classifyBinanceError(-1021).Retryable {
		t.Fatal("-1021 时间戳偏移必须可自动校时重发")
	}
}

// TestBinanceDefaultDisabled 默认配置必须关闭币安通道（防止无凭证环境误启用外连）。
func TestBinanceDefaultDisabled(t *testing.T) {
	cfg := config.DefaultBinanceConfig()
	if cfg.Enabled {
		t.Fatal("DefaultBinanceConfig 不得默认 Enabled=true")
	}
}
