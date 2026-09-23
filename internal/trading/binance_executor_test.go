// 文件职责：binance_executor_test.go——币安执行器 mock 端到端测试（PLAN §12.1 §BINANCE-EXECUTOR 段）。
// 用 httptest 假柜台覆盖：现货 LIMIT/MARKET 参数面与幂等键、exchangeInfo 本地预检（步长截断/
// MIN_NOTIONAL 拒单）、int64 orderId 精度（§15.5）、美股四格矩阵（§2.2）、486410 落闩、
// 486449 幂等回填、-1021 对时重发、市场守卫 fail-close、现货/美股 State 与撤单路由。
// 签名基线与 binance_sign.go 的 HMAC 规则锁在同一文件（给定固定 offset 逐字节重算）。
// English: mock-server end-to-end tests for BinanceExecutor — spot/equity parameter matrices,
// exchangeInfo pre-checks, int64 order-id precision, disclaimer latch, idempotent backfill,
// -1021 resync-retry and market-guard fail-close.
package trading

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"quant-trading-v2/internal/config"
)

// mockBinance httptest 假币安柜台：按路径记录请求、脚本化响应。
type mockBinance struct {
	srv *httptest.Server
	mu  sync.Mutex
	// calls 路径→请求原始 query 列表（签名参数已含 timestamp/signature）
	calls map[string][]string
	// spotResp/equityResp 下单端点的定制响应（nil 用默认成功体）
	spotResp    func(path string, q string) (int, string)
	equityResp  func(path string, q string) (int, string)
	timeServer  int64 // /api/v3/time 返回的 serverTime（0=不启用）
	accountBody string
	exchangeRsp string
	openOrders  string
	// §P2 回报腿：listenKey 响应体与现货在途集合覆写（空=缺省形状）。
	listenKeyBody  string
	spotOpenOrders string
	// §MR-4B fapi 桩面：path→响应体覆写（空=内置缺省 fixture）+ path→强制 HTTP 状态码
	// （拒单/杠杆失败用例用）。缺省 fixture 覆盖下单/杠杆/规则/持仓/收益/在途/listenKey
	// 七端点，与真 fapi 的字段形状对齐（orderId 大整数、字符串数值族）。
	fapiBodies map[string]string
	fapiStatus map[string]int
}

// newMockBinance 起一个 httptest 假币安网关：按路径族可编程响应 + 逐路径调用计数，
// 覆盖下单/撤单/查询/exchangeInfo/disclaimer 面（golden 断言与错误码映射都吃这个桩）。
func newMockBinance(t *testing.T) *mockBinance {
	t.Helper()
	m := &mockBinance{calls: map[string][]string{}, fapiBodies: map[string]string{}, fapiStatus: map[string]int{}}
	mux := http.NewServeMux()
	record := func(path, rawQuery string) {
		m.mu.Lock()
		m.calls[path] = append(m.calls[path], rawQuery)
		m.mu.Unlock()
	}
	mux.HandleFunc("/api/v3/order", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path, r.URL.RawQuery)
		if m.spotResp != nil {
			code, body := m.spotResp(r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(code)
			fmt.Fprint(w, body)
			return
		}
		fmt.Fprint(w, `{"orderId":9007199254740993,"clientOrderId":"x","status":"NEW"}`)
	})
	mux.HandleFunc("/sapi/v1/equity/order", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path, r.URL.RawQuery)
		if m.equityResp != nil {
			code, body := m.equityResp(r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(code)
			fmt.Fprint(w, body)
			return
		}
		fmt.Fprint(w, `{"status":"S","orderId":"E-777"}`)
	})
	mux.HandleFunc("/api/v3/exchangeInfo", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path, r.URL.RawQuery)
		if m.exchangeRsp != "" {
			fmt.Fprint(w, m.exchangeRsp)
			return
		}
		fmt.Fprint(w, `{"symbol":"BTCUSDT","filters":[
			{"filterType":"PRICE_FILTER","tickSize":"0.01"},
			{"filterType":"LOT_SIZE","stepSize":"0.001"},
			{"filterType":"MIN_NOTIONAL","minNotional":"10"}]}`)
	})
	mux.HandleFunc("/api/v3/account", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path, r.URL.RawQuery)
		if m.accountBody != "" {
			fmt.Fprint(w, m.accountBody)
			return
		}
		fmt.Fprint(w, `{"balances":[{"asset":"BTC","free":"1.5","locked":"0.5"},{"asset":"USDT","free":"100","locked":"0"},{"asset":"ETH","free":"0","locked":"0"}]}`)
	})
	mux.HandleFunc("/api/v3/time", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path, r.URL.RawQuery)
		ms := m.timeServer
		if ms == 0 {
			ms = time.Now().UnixMilli()
		}
		fmt.Fprintf(w, `{"serverTime":%d}`, ms)
	})
	mux.HandleFunc("/sapi/v1/equity/openOrders", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path, r.URL.RawQuery)
		if m.openOrders != "" {
			fmt.Fprint(w, m.openOrders)
			return
		}
		fmt.Fprint(w, `[{"orderId":"E-1"},{"orderId":"E-2"}]`)
	})
	mux.HandleFunc("/sapi/v1/equity/cancel", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path, r.URL.RawQuery)
		fmt.Fprint(w, `{"status":"S"}`)
	})
	// §P2 回报腿端点：listenKey 开通/续期与现货在途集合（binance_report.go 消费面）。
	mux.HandleFunc("/api/v3/userDataStream", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path+" "+r.Method, r.URL.RawQuery)
		if m.listenKeyBody != "" {
			fmt.Fprint(w, m.listenKeyBody)
			return
		}
		fmt.Fprint(w, `{"listenKey":"LK-TEST"}`)
	})
	mux.HandleFunc("/sapi/v1/equity/listenKey", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path+" "+r.Method, r.URL.RawQuery)
		if m.listenKeyBody != "" {
			fmt.Fprint(w, m.listenKeyBody)
			return
		}
		fmt.Fprint(w, `{"listenKey":"LK-US"}`)
	})
	mux.HandleFunc("/api/v3/openOrders", func(w http.ResponseWriter, r *http.Request) {
		record(r.URL.Path, r.URL.RawQuery)
		if m.spotOpenOrders != "" {
			fmt.Fprint(w, m.spotOpenOrders)
			return
		}
		fmt.Fprint(w, `[]`)
	})
	// §MR-4B 合约假柜台七端点：统一走 fapi 助手（record + 覆写体 + 强制状态码三段式）。
	fapi := func(path, def string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			record(path, r.URL.RawQuery)
			if st := m.fapiStatus[path]; st != 0 {
				w.WriteHeader(st)
			}
			if b, ok := m.fapiBodies[path]; ok {
				fmt.Fprint(w, b)
				return
			}
			fmt.Fprint(w, def)
		}
	}
	mux.HandleFunc("/fapi/v1/order", fapi("/fapi/v1/order",
		`{"orderId":9007199254740993,"clientOrderId":"qt-f","status":"ACCEPTED"}`))
	mux.HandleFunc("/fapi/v1/leverage", fapi("/fapi/v1/leverage",
		`{"leverage":1,"marginType":"isolated","symbol":"BTCUSDT"}`))
	mux.HandleFunc("/fapi/v1/exchangeInfo", fapi("/fapi/v1/exchangeInfo",
		`{"symbols":[{"filters":[
			{"filterType":"PRICE_FILTER","tickSize":"0.10"},
			{"filterType":"LOT_SIZE","stepSize":"0.001"}]}]}`))
	// 缺省持仓 fixture：BTCUSDT 空 0.5（entry 60000/mark 61000/liq 62000）、
	// ETHUSDT 无仓、BNBUSDT 多 2（entry 300）——覆盖方向映射与零仓跳过两支。
	mux.HandleFunc("/fapi/v2/positionRisk", fapi("/fapi/v2/positionRisk",
		`[{"symbol":"BTCUSDT","positionAmt":"-0.5","entryPrice":"60000","markPrice":"61000","liquidationPrice":"62000"},
		  {"symbol":"ETHUSDT","positionAmt":"0","entryPrice":"0","markPrice":"3000","liquidationPrice":"0"},
		  {"symbol":"BNBUSDT","positionAmt":"2","entryPrice":"300","markPrice":"310","liquidationPrice":"0"}]`))
	mux.HandleFunc("/fapi/v1/account", fapi("/fapi/v1/account", `{"totalWalletBalance":"1000"}`))
	mux.HandleFunc("/fapi/v1/openOrders", fapi("/fapi/v1/openOrders", `[]`))
	mux.HandleFunc("/fapi/v1/listenKey", fapi("/fapi/v1/listenKey", `{"listenKey":"LK-FUT"}`))
	mux.HandleFunc("/fapi/v1/income", fapi("/fapi/v1/income", `[]`))
	mux.HandleFunc("/fapi/v1/time", fapi("/fapi/v1/time", `{"serverTime":0}`))
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

// executorFor 构建指向假柜台的执行器（同包白盒改写 signer.base；凭证固定串便于签名基线）。
func executorFor(t *testing.T, m *mockBinance, market string) *BinanceExecutor {
	t.Helper()
	cfg := config.DefaultBinanceConfig()
	cfg.Enabled = true
	cfg.APIKey = "test-api-key"
	cfg.APISecret = "test-api-secret"
	e := NewBinanceExecutor(config.BinanceBrokerView{Cfg: cfg, Market: market})
	e.spot.base = m.srv.URL
	e.equity.base = m.srv.URL
	return e
}

// queries 取某端点的全部已记录 query（拷贝，防竞态）。
func (m *mockBinance) queries(path string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.calls[path]...)
}

// mustParam 断言 query 含键值并回传值。
func mustParam(t *testing.T, raw, key, want string) string {
	t.Helper()
	q := strings.SplitN(raw, "&signature=", 2)[0]
	vals, err := parseQueryForTest(q)
	if err != nil {
		t.Fatalf("解析 query 失败: %v (%s)", err, q)
	}
	got := vals.Get(key)
	if want == "<absent>" {
		if vals.Has(key) {
			t.Fatalf("参数 %s 不应出现（四格矩阵禁填项），实际=%q", key, got)
		}
		return ""
	}
	if got != want {
		t.Fatalf("参数 %s=%q，期望 %q（query=%s）", key, got, want, q)
	}
	return got
}

// paramOf 只读取参数值（形态断言用，不做等值比较）。
func paramOf(t *testing.T, raw, key string) string {
	t.Helper()
	q := strings.SplitN(raw, "&signature=", 2)[0]
	vals, err := parseQueryForTest(q)
	if err != nil {
		t.Fatalf("解析 query 失败: %v (%s)", err, q)
	}
	return vals.Get(key)
}

// parseQueryForTest 把 URL 编码串拆成键值表（与签名链共用 mapVals，避免测试引 net/url 造成口径分叉）。
func parseQueryForTest(q string) (mapVals, error) {
	out := mapVals{v: map[string]string{}}
	for _, kv := range strings.Split(q, "&") {
		if kv == "" {
			continue
		}
		k, val, _ := strings.Cut(kv, "=")
		out.v[k] = val
	}
	return out, nil
}

type mapVals struct{ v map[string]string }

func (m mapVals) Get(k string) string { return m.v[k] }
func (m mapVals) Has(k string) bool   { _, ok := m.v[k]; return ok }

// TestSpotLimitOrderParams 现货 LIMIT 参数面：type/timeInForce/quantity/price + newClientOrderId
// 幂等键（"qt"+sha1(signal_id) hex[:34]）+ 签名三件套齐备。
// futuresExecutorFor 构建指向假柜台的合约执行器（§MR-4B 分叉形态）：
// spot 档案切到 umfutures（FuturesActive=true），三个 signer 的 base 全部钉到 httptest。
// tune 回调可继续改凭证面之外的档案字段（杠杆/阈值/保证金率）。
func futuresExecutorFor(t *testing.T, m *mockBinance, tune func(*config.BinanceConfig)) *BinanceExecutor {
	t.Helper()
	cfg := config.DefaultBinanceConfig()
	cfg.Enabled = true
	cfg.APIKey = "test-api-key"
	cfg.APISecret = "test-api-secret"
	cfg.Spot.Enabled = true
	cfg.Spot.ProductType = "umfutures"
	if tune != nil {
		tune(&cfg)
	}
	e := NewBinanceExecutor(config.BinanceBrokerView{Cfg: cfg, Market: "CRYPTO"})
	e.spot.base = m.srv.URL
	e.equity.base = m.srv.URL
	e.futures.base = m.srv.URL
	return e
}

// TestFuturesPlaceLimitShortOpen 卖出开空 LIMIT 全链：杠杆预设先行（缺省档案=1x 最保守）、
// fapi 参数面（side=SELL 折叠、开空无 reduceOnly、GTC、按 fapi 步长截断）、幂等键复用。
func TestFuturesPlaceLimitShortOpen(t *testing.T) {
	m := newMockBinance(t)
	e := futuresExecutorFor(t, m, nil)
	res, err := e.PlaceSell(OrderRequest{
		Side: SideShortOpen, Code: "BTCUSDT", Market: "CRYPTO",
		PriceType: "limit", Price: 61000.07, Qty: 0.56789, SignalID: "sig-f1",
	})
	if err != nil || !res.OK {
		t.Fatalf("开空下单应成功: %+v %v", res, err)
	}
	if res.OrderID != "9007199254740993" {
		t.Fatalf("orderId 大整数精度丢失: %q", res.OrderID)
	}
	// 杠杆预设：首单前恰好一次（缺省 leverage=0→按 1x 设定）。
	lev := m.queries("/fapi/v1/leverage")
	if len(lev) != 1 {
		t.Fatalf("杠杆预设应 1 次，实际 %d", len(lev))
	}
	mustParam(t, lev[0], "symbol", "BTCUSDT")
	mustParam(t, lev[0], "leverage", "1")
	q := m.queries("/fapi/v1/order")[0]
	mustParam(t, q, "symbol", "BTCUSDT")
	mustParam(t, q, "side", "SELL")
	mustParam(t, q, "type", "LIMIT")
	mustParam(t, q, "timeInForce", "GTC")
	mustParam(t, q, "quantity", "0.567")      // stepSize 0.001 截断
	mustParam(t, q, "price", "61000")         // trimNum 去尾零：61000.0 截 tick 后落串 61000       // tickSize 0.10 截断
	mustParam(t, q, "reduceOnly", "<absent>") // 开空侧禁带 reduceOnly
	mustParam(t, q, "newClientOrderId", spotClientOrderID("sig-f1"))
}

// TestFuturesCoverReduceOnly 平仓两侧（卖出平多/买入平仓平空）必须带 reduceOnly=true——
// 单向持仓模式下这是"只减不加"的线上唯一表达，缺一次就可能把平仓打成反向开仓。
func TestFuturesCoverReduceOnly(t *testing.T) {
	for _, c := range []struct {
		side, wantWire string
	}{
		{SideSell, "SELL"},      // 多头平仓
		{SideShortCover, "BUY"}, // 空头平仓
	} {
		m := newMockBinance(t)
		e := futuresExecutorFor(t, m, nil)
		res, err := e.place(OrderRequest{
			Side: c.side, Code: "BTCUSDT", Market: "CRYPTO", Qty: 0.4, SignalID: "sig-f2",
		})
		if err != nil || !res.OK {
			t.Fatalf("%s 平仓下单应成功: %+v %v", c.side, res, err)
		}
		q := m.queries("/fapi/v1/order")[0]
		mustParam(t, q, "side", c.wantWire)
		mustParam(t, q, "type", "MARKET")
		mustParam(t, q, "reduceOnly", "true")
		mustParam(t, q, "quantity", "0.4")
	}
}

// TestFuturesMarketBuyNeedsQty 合约无 quoteOrderQty：只有金额的市价买单必须本地拒
// （不烧请求配额），并把换算指引写进拒因。
func TestFuturesMarketBuyNeedsQty(t *testing.T) {
	m := newMockBinance(t)
	e := futuresExecutorFor(t, m, nil)
	res, err := e.PlaceBuy(OrderRequest{Side: SideBuy, Code: "BTCUSDT", Market: "CRYPTO", Amount: 500})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Err, "quoteOrderQty") {
		t.Fatalf("按金额市价应被拒且带指引: %+v", res)
	}
	if n := len(m.queries("/fapi/v1/order")); n != 0 {
		t.Fatalf("本地拒单不得发请求，实际 %d 发", n)
	}
}

// TestFuturesLeveragePresetCacheAndFailClose 杠杆 per-symbol 缓存（同 symbol 两单只设一次；
// 换 symbol 再设一次）；杠杆设定失败=fail-close 不发单（传输错误上抛让 controller 标发送失败）。
func TestFuturesLeveragePresetCacheAndFailClose(t *testing.T) {
	m := newMockBinance(t)
	e := futuresExecutorFor(t, m, func(c *config.BinanceConfig) { c.Spot.Leverage = 3 })
	for i := 0; i < 2; i++ {
		if _, err := e.PlaceBuy(OrderRequest{Side: SideBuy, Code: "BTCUSDT", Market: "CRYPTO", Qty: 0.1, SignalID: fmt.Sprintf("sig-l%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.PlaceBuy(OrderRequest{Side: SideBuy, Code: "ETHUSDT", Market: "CRYPTO", Qty: 1, SignalID: "sig-l2"}); err != nil {
		t.Fatal(err)
	}
	lev := m.queries("/fapi/v1/leverage")
	if len(lev) != 2 {
		t.Fatalf("杠杆预设应 2 次（BTC/ETH 各一），实际 %d", len(lev))
	}
	mustParam(t, lev[0], "leverage", "3")
	// 杠杆端点炸（-4028 形态）：下一单（新 symbol）fail-close，order 端点不涨。
	m.fapiStatus["/fapi/v1/leverage"] = http.StatusBadRequest
	m.fapiBodies["/fapi/v1/leverage"] = `{"code":-4028,"msg":"Leverage is invalid"}`
	before := len(m.queries("/fapi/v1/order"))
	_, err := e.PlaceBuy(OrderRequest{Side: SideBuy, Code: "SOLUSDT", Market: "CRYPTO", Qty: 1, SignalID: "sig-l3"})
	if err == nil || !strings.Contains(err.Error(), "杠杆预设失败") {
		t.Fatalf("杠杆失败必须 fail-close 上抛: %v", err)
	}
	if len(m.queries("/fapi/v1/order")) != before {
		t.Fatal("杠杆预设失败时不得发出委托")
	}
}

// TestFuturesForkIsolation 分叉负锁①：product_type 空（现货档案）时 /fapi 端点零调用；
// 负锁②：FuturesActive 但 fapi base 缺失（无凭证装配错位）→ 本地 fail-close 拒单不外发。
func TestFuturesForkIsolation(t *testing.T) {
	m := newMockBinance(t)
	spotExec := executorFor(t, m, "CRYPTO")
	if _, err := spotExec.PlaceBuy(OrderRequest{Side: SideBuy, Code: "BTCUSDT", Market: "CRYPTO", Qty: 0.1}); err != nil {
		t.Fatal(err)
	}
	if n := len(m.queries("/fapi/v1/order")); n != 0 {
		t.Fatalf("现货视图不得触达 fapi，实际 %d 发", n)
	}
	e := futuresExecutorFor(t, m, nil)
	e.futures.base = ""
	res, err := e.PlaceBuy(OrderRequest{Side: SideBuy, Code: "BTCUSDT", Market: "CRYPTO", Qty: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Err, "fail-close") {
		t.Fatalf("fapi base 缺失应本地拒: %+v", res)
	}
}

// TestFuturesStateAndCancel 合约对账与撤单：positionRisk→持仓行（方向/成本/计价币/零仓跳过）、
// 撤单走 fapi 复合单号、裸单号本地拒。
func TestFuturesStateAndCancel(t *testing.T) {
	m := newMockBinance(t)
	e := futuresExecutorFor(t, m, nil)
	st, err := e.State()
	if err != nil || !st.Connected {
		t.Fatalf("合约 State 应连通: %+v %v", st, err)
	}
	if len(st.Positions) != 2 {
		t.Fatalf("零仓符号必须跳过，应 2 行，实际 %d", len(st.Positions))
	}
	btc := st.Positions[0]
	if btc.TsCode != "BTCUSDT" || btc.Side != "short" || btc.Qty != 0.5 || btc.Currency != "USDT" || btc.CostPrice != 60000 {
		t.Fatalf("空头持仓映射错: %+v", btc)
	}
	bnb := st.Positions[1]
	if bnb.TsCode != "BNBUSDT" || bnb.Side != "long" || bnb.Qty != 2 || bnb.CostPrice != 300 {
		t.Fatalf("多头持仓映射错: %+v", bnb)
	}
	if err := e.Cancel("BTCUSDT:123"); err != nil {
		t.Fatal(err)
	}
	q := m.queries("/fapi/v1/order")
	last := q[len(q)-1]
	if !strings.Contains(last, "symbol=BTCUSDT") || !strings.Contains(last, "orderId=123") {
		t.Fatalf("撤单参数错: %s", last)
	}
	if err := e.Cancel("123"); err == nil {
		t.Fatal("裸单号撤单必须拒（无法定位 symbol）")
	}
	if ok, err := e.Health(); !ok || err != nil {
		t.Fatalf("健康探测应过: %v %v", ok, err)
	}
	if n := len(m.queries("/fapi/v1/account")); n == 0 {
		t.Fatal("合约健康探测必须走 fapi account")
	}
}

// TestLiqEvidence liq_distance 证据源三形态：距离跌破阈值=拦（带数字理由）、
// 达标=放行、无既有空头=放行（新开仓尚无强平价）。
func TestLiqEvidence(t *testing.T) {
	m := newMockBinance(t)
	e := futuresExecutorFor(t, m, func(c *config.BinanceConfig) { c.Spot.LiqDistMinPct = 5 })
	// mark 61000 / liq 62000 → 距离 1.64% < 5% → 拦。
	ok, detail := e.LiqEvidence("CRYPTO", "BTCUSDT")
	if ok || !strings.Contains(detail, "距强平") {
		t.Fatalf("距离不足应拦: %v %s", ok, detail)
	}
	// 阈值调到 1%：达标放行。
	e2 := futuresExecutorFor(t, m, func(c *config.BinanceConfig) { c.Spot.LiqDistMinPct = 1 })
	if ok2, _ := e2.LiqEvidence("CRYPTO", "BTCUSDT"); !ok2 {
		t.Fatal("距离达标应放行")
	}
	// 无空头持仓的符号：放行（新开仓无强平价可证）。
	if ok3, d3 := e.LiqEvidence("CRYPTO", "SOLUSDT"); !ok3 || !strings.Contains(d3, "无既有仓位") {
		t.Fatalf("无仓位应放行并给原因: %v %s", ok3, d3)
	}
}

func TestSpotLimitOrderParams(t *testing.T) {
	m := newMockBinance(t)
	e := executorFor(t, m, "CRYPTO")
	res, err := e.PlaceBuy(OrderRequest{SignalID: "sig-1", Code: "btcusdt", Side: SideBuy, PriceType: "limit", Price: 65000.00, Qty: 0.01, Market: "CRYPTO"})
	if err != nil || !res.OK {
		t.Fatalf("下单应成功: res=%+v err=%v", res, err)
	}
	qs := m.queries("/api/v3/order")
	if len(qs) != 1 {
		t.Fatalf("应发出一次下单请求，实际 %d", len(qs))
	}
	q := qs[0]
	mustParam(t, q, "symbol", "BTCUSDT")
	mustParam(t, q, "side", "BUY")
	mustParam(t, q, "type", "LIMIT")
	mustParam(t, q, "timeInForce", "GTC")
	mustParam(t, q, "quantity", "0.01")
	mustParam(t, q, "price", "65000")
	if got := paramOf(t, q, "newClientOrderId"); !strings.HasPrefix(got, "qt") || len(got) != 36 {
		t.Fatalf("newClientOrderId 形态错误: %q", got)
	}
	if !strings.Contains(q, "&signature=") || !strings.Contains(q, "recvWindow=5000") {
		t.Fatalf("签名/时间窗参数缺失: %s", q)
	}
	// §15.5 int64 精度锁：9007199254740993（>2^53）必须原样回传，不经 float64 舍入。
	if res.OrderID != "9007199254740993" {
		t.Fatalf("orderId 精度丢失（§15.5 UseNumber 失效？）: %q", res.OrderID)
	}
}

// TestSpotMarketBuyQuoteQty 现货 MARKET 买按金额：quoteOrderQty 出现且 price/quantity 禁出现。
func TestSpotMarketBuyQuoteQty(t *testing.T) {
	m := newMockBinance(t)
	e := executorFor(t, m, "CRYPTO")
	if _, err := e.PlaceBuy(OrderRequest{SignalID: "s2", Code: "ETHUSDT", Side: SideBuy, PriceType: "market", Amount: 100.5, Market: "CRYPTO"}); err != nil {
		t.Fatal(err)
	}
	q := m.queries("/api/v3/order")[0]
	mustParam(t, q, "type", "MARKET")
	mustParam(t, q, "quoteOrderQty", "100.5")
	mustParam(t, q, "price", "<absent>")
	mustParam(t, q, "quantity", "<absent>")
}

// TestSpotRulesTruncationAndMinNotional exchangeInfo 本地预检：stepSize 截断 + MIN_NOTIONAL 拒单
// （拒单路径不得发出 /api/v3/order 请求）。
func TestSpotRulesTruncationAndMinNotional(t *testing.T) {
	m := newMockBinance(t)
	e := executorFor(t, m, "CRYPTO")
	// 截断：qty 1.23456（step 0.001）→ 1.234；price 65000.999（tick 0.01）→ 65000.99
	if _, err := e.PlaceBuy(OrderRequest{SignalID: "s3", Code: "BTCUSDT", Side: SideBuy, PriceType: "limit", Price: 65000.999, Qty: 1.23456, Market: "CRYPTO"}); err != nil {
		t.Fatal(err)
	}
	q := m.queries("/api/v3/order")[0]
	mustParam(t, q, "quantity", "1.234")
	mustParam(t, q, "price", "65000.99")
	// MIN_NOTIONAL=10：0.001×1000=1 < 10 → 本地拒，无网络请求（数量取合法步长倍，排除截断干扰）
	m2 := newMockBinance(t)
	e2 := executorFor(t, m2, "CRYPTO")
	res, err := e2.PlaceBuy(OrderRequest{SignalID: "s4", Code: "BTCUSDT", Side: SideBuy, PriceType: "limit", Price: 1000, Qty: 0.001, Market: "CRYPTO"})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Err, "MIN_NOTIONAL") {
		t.Fatalf("应本地拒 MIN_NOTIONAL，实际 %+v", res)
	}
	if n := len(m2.queries("/api/v3/order")); n != 0 {
		t.Fatalf("本地拒单不应发出下单请求（%d 次）", n)
	}
}

// TestEquityFourCellMatrix §2.2 四格矩阵逐格锁定（必填齐、禁填缺席）。
func TestEquityFourCellMatrix(t *testing.T) {
	cases := []struct {
		name   string
		req    OrderRequest
		want   map[string]string
		forbid []string
	}{
		{"BUY+LIMIT", OrderRequest{Side: SideBuy, PriceType: "limit", Price: 199.99, Qty: 2, TradingSession: "EXTENDED"},
			map[string]string{"type": "LIMIT", "price": "199.99", "quantity": "2", "tradingSession": "EXTENDED", "timeInForce": "DAY"},
			[]string{"notional"}},
		{"BUY+MARKET", OrderRequest{Side: SideBuy, PriceType: "market", Notional: 500},
			map[string]string{"type": "MARKET", "notional": "500", "walletType": "CARD"},
			[]string{"price", "quantity", "tradingSession"}},
		{"SELL+LIMIT", OrderRequest{Side: SideSell, PriceType: "limit", Price: 100.5, Qty: 3, TradingSession: "RTH", TimeInForce: "GTC"},
			map[string]string{"type": "LIMIT", "price": "100.5", "quantity": "3", "tradingSession": "RTH", "timeInForce": "GTC", "walletType": "<absent>"},
			[]string{"notional"}},
		{"SELL+MARKET", OrderRequest{Side: SideSell, PriceType: "market", Qty: 1.5},
			map[string]string{"type": "MARKET", "quantity": "1.5", "walletType": "CARD"},
			[]string{"price", "notional", "tradingSession"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMockBinance(t)
			e := executorFor(t, m, "US")
			tc.req.SignalID = "sig-" + tc.name
			tc.req.Code = "AAPL"
			tc.req.Market = "US"
			if _, err := e.PlaceBuy(tc.req); err != nil { // PlaceBuy/PlaceSell 同链路，按 Side 字段决定
				t.Fatal(err)
			}
			qs := m.queries("/sapi/v1/equity/order")
			if len(qs) != 1 {
				t.Fatalf("请求次数=%d", len(qs))
			}
			for k, v := range tc.want {
				mustParam(t, qs[0], k, v)
			}
			for _, k := range tc.forbid {
				mustParam(t, qs[0], k, "<absent>")
			}
			mustParam(t, qs[0], "quoteAsset", "USDC")
			// Q3 未实测：clientOrderId 不下发（幂等留本地 signal_id 键）
			mustParam(t, qs[0], "clientOrderId", "<absent>")
		})
	}
}

// TestEquityFractionalGTCDowngrade 碎股 GTC+RTH 非法组合 → 自动降级 DAY（486441/486442 族预防）。
func TestEquityFractionalGTCDowngrade(t *testing.T) {
	m := newMockBinance(t)
	e := executorFor(t, m, "US")
	if _, err := e.PlaceBuy(OrderRequest{SignalID: "fx", Code: "MSFT", Side: SideBuy, PriceType: "limit", Price: 400.25, Qty: 0.4, TimeInForce: "GTC", TradingSession: "RTH", Market: "US"}); err != nil {
		t.Fatal(err)
	}
	mustParam(t, m.queries("/sapi/v1/equity/order")[0], "timeInForce", "DAY")
}

// TestEquityPricePrecisionReject 美股限价超 2 位小数本地拒（486418 族预防）。
func TestEquityPricePrecisionReject(t *testing.T) {
	m := newMockBinance(t)
	e := executorFor(t, m, "US")
	res, err := e.PlaceBuy(OrderRequest{SignalID: "pp", Code: "NVDA", Side: SideBuy, PriceType: "limit", Price: 123.456, Qty: 1, Market: "US"})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Err, "2 位小数") {
		t.Fatalf("应本地拒精度: %+v", res)
	}
	if len(m.queries("/sapi/v1/equity/order")) != 0 {
		t.Fatal("精度拒单不应发出请求")
	}
}

// TestEquityDisclaimerLatch 486410 → 业务拒单 + 闩锁：后续下单不再触网。
func TestEquityDisclaimerLatch(t *testing.T) {
	m := newMockBinance(t)
	m.equityResp = func(string, string) (int, string) { return 400, `{"code":486410,"msg":"equity disclaimer not signed"}` }
	e := executorFor(t, m, "US")
	res, _ := e.PlaceBuy(OrderRequest{SignalID: "d1", Code: "AAPL", Side: SideBuy, PriceType: "market", Notional: 100, Market: "US"})
	if res.OK || !strings.Contains(res.Err, "已自锁") {
		t.Fatalf("486410 应拒单并提示自锁: %+v", res)
	}
	if !e.DisclaimerUnsigned() {
		t.Fatal("闩锁位应置真")
	}
	res2, _ := e.PlaceBuy(OrderRequest{SignalID: "d2", Code: "AAPL", Side: SideBuy, PriceType: "market", Notional: 100, Market: "US"})
	if res2.OK || len(m.queries("/sapi/v1/equity/order")) != 1 {
		t.Fatalf("闩锁后不得再触网: %+v calls=%d", res2, len(m.queries("/sapi/v1/equity/order")))
	}
}

// TestEquityIdempotentHitBackfill 486449 幂等命中 → 查在途委托回填单号（OK=true）。
func TestEquityIdempotentHitBackfill(t *testing.T) {
	m := newMockBinance(t)
	var n int
	m.equityResp = func(string, string) (int, string) {
		n++
		if n == 1 {
			return 400, `{"code":486449,"msg":"order already exists"}`
		}
		return 200, `{"status":"S"}`
	}
	e := executorFor(t, m, "US")
	res, err := e.PlaceBuy(OrderRequest{SignalID: "id1", Code: "AAPL", Side: SideBuy, PriceType: "market", Notional: 50, Market: "US"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.OrderID != "E-2" {
		t.Fatalf("应回填最近一笔在途单号 E-2: %+v", res)
	}
}

// TestSpotMinus1021ResyncRetry -1021 时间戳出窗 → 对时一次后重发成功（共两次下单请求）。
func TestSpotMinus1021ResyncRetry(t *testing.T) {
	m := newMockBinance(t)
	// 假服务器"快 10 分钟"——首次签名（offset=0）被拒，对时后第二次成功
	m.timeServer = time.Now().Add(10 * time.Minute).UnixMilli()
	var n int
	m.spotResp = func(string, string) (int, string) {
		n++
		if n == 1 {
			return 400, `{"code":-1021,"msg":"Timestamp for this request is outside of the recvWindow."}`
		}
		return 200, `{"orderId":42,"status":"NEW"}`
	}
	e := executorFor(t, m, "CRYPTO")
	res, err := e.PlaceBuy(OrderRequest{SignalID: "ts", Code: "BTCUSDT", Side: SideBuy, PriceType: "market", Amount: 100, Market: "CRYPTO"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.OrderID != "42" {
		t.Fatalf("-1021 重发应成功: %+v", res)
	}
	if len(m.queries("/api/v3/order")) != 2 {
		t.Fatal("应恰重发一次")
	}
	if off := e.spot.offsetMs.Load(); off <= 0 {
		t.Fatalf("对时偏移未更新: %d", off)
	}
}

// TestMarketGuardFailClose 市场守卫：CN 单/跨市场单一律 error 拒（绝不派错通道）。
func TestMarketGuardFailClose(t *testing.T) {
	m := newMockBinance(t)
	eCN := executorFor(t, m, "US")
	if _, err := eCN.PlaceBuy(OrderRequest{Code: "600000.SH", Side: SideBuy, Market: "CN"}); err == nil {
		t.Fatal("CN 单必须拒")
	}
	if _, err := eCN.PlaceBuy(OrderRequest{Code: "BTCUSDT", Side: SideBuy, Market: "CRYPTO"}); err == nil {
		t.Fatal("跨市场误投必须拒")
	}
}

// TestStateSpotAndUS State：现货余额→持仓（含非正余额过滤）；美股恒 Connected=false 防空快照清账。
func TestStateSpotAndUS(t *testing.T) {
	m := newMockBinance(t)
	spot := executorFor(t, m, "CRYPTO")
	st, err := spot.State()
	if err != nil || !st.Connected {
		t.Fatalf("现货 State 应成功: %v %+v", err, st)
	}
	if len(st.Positions) != 2 {
		t.Fatalf("应过滤零余额后剩 2 行: %+v", st.Positions)
	}
	btc := st.Positions[0]
	if btc.Market != "CRYPTO" || btc.TsCode != "BTC" || btc.Qty != 2.0 || btc.CostPrice != 0 {
		t.Fatalf("BTC 持仓行形态错误: %+v", btc)
	}
	us := executorFor(t, m, "US")
	st2, err := us.State()
	if err != nil || st2.Connected || len(st2.Positions) != 0 {
		t.Fatalf("美股 State 必须 Connected=false 防空快照: %+v err=%v", st2, err)
	}
}

// TestCancelRouting 撤单路由：现货复合格拆 symbol+orderId；美股数字号走 orderId、非数字走 clientOrderId。
func TestCancelRouting(t *testing.T) {
	m := newMockBinance(t)
	spot := executorFor(t, m, "CRYPTO")
	if err := spot.Cancel("btcusdt:12345"); err != nil {
		t.Fatal(err)
	}
	q := m.queries("/api/v3/order")[0]
	mustParam(t, q, "symbol", "BTCUSDT")
	mustParam(t, q, "orderId", "12345")
	if err := spot.Cancel("12345"); err == nil {
		t.Fatal("现货裸单号必须拒撤（缺 symbol 无法撤）")
	}
	us := executorFor(t, m, "US")
	if err := us.Cancel("E-9"); err != nil {
		t.Fatal(err)
	}
	mustParam(t, m.queries("/sapi/v1/equity/cancel")[0], "clientOrderId", "E-9")
}

// TestSpotClientOrderIDDeterminism 幂等键确定性 + 长度上限（36 ≤ newClientOrderId 上限）。
func TestSpotClientOrderIDDeterminism(t *testing.T) {
	a := spotClientOrderID("sig-abc")
	b := spotClientOrderID("sig-abc")
	if a != b || len(a) != 36 || !strings.HasPrefix(a, "qt") {
		t.Fatalf("幂等键形态错误: %s", a)
	}
	if spotClientOrderID("other") == a {
		t.Fatal("不同信号必须产生不同键")
	}
}

// TestSignerGolden 签名基线锁：固定 secret + 固定参数（timestamp 偏移注入）逐字节重算 HMAC。
func TestSignerGolden(t *testing.T) {
	s := newBinanceSigner("https://example.invalid", "k", "golden-secret", &http.Client{})
	s.offsetMs.Store(1000)
	fixed := time.Now().UnixMilli() + 1000
	// signQuery 入参是标准 url.Values（修正首稿自造类型）——签名串按其稳定序生成。
	q := s.signQuery(url.Values{"symbol": {"BTCUSDT"}})
	base := strings.SplitN(q, "&signature=", 2)[0]
	mac := hmac.New(sha256.New, []byte("golden-secret"))
	mac.Write([]byte(base))
	want := "&signature=" + hex.EncodeToString(mac.Sum(nil))
	if !strings.HasSuffix(q, want) {
		t.Fatalf("签名与独立实现不一致:\n%s", q)
	}
	if !strings.Contains(base, "symbol=BTCUSDT") || !strings.Contains(base, "recvWindow=5000") {
		t.Fatalf("参数面缺项: %s", base)
	}
	ts, perr := strconv.ParseInt(strings.Split(strings.Split(base, "timestamp=")[1], "&")[0], 10, 64)
	if perr != nil || ts < fixed-2000 || ts > fixed+2000 {
		t.Fatalf("timestamp 未按 offset 注入: %d vs %d", ts, fixed)
	}
}

// mapValsBuild 已移除——signQuery 直接使用标准 url.Values。

// TestEquityRateGateCap 200/min 滑窗本地帽：第 201 次拒发且不发网络请求。
func TestEquityRateGateCap(t *testing.T) {
	m := newMockBinance(t)
	e := executorFor(t, m, "US")
	for i := 0; i < 200; i++ {
		if err := e.equityRateGate(); err != nil {
			t.Fatalf("第 %d 次不应限流: %v", i, err)
		}
	}
	if err := e.equityRateGate(); err == nil || !strings.Contains(err.Error(), "200 次/分钟") {
		t.Fatalf("第 201 次应触发滑窗帽: %v", err)
	}
}

// TestRouterFailClose BrokerRouter：未注册市场 fail-close；已注册市场按键分发。
func TestRouterFailClose(t *testing.T) {
	cn := NewController(NoopExecutor{}, nil, "u", config.DefaultQMTConfig(), nil)
	r := NewBrokerRouter(cn)
	if _, err := r.PlaceOrder(OrderRequest{Code: "AAPL", Market: "US", Side: SideBuy}); err == nil {
		t.Fatal("US 未注册必须拒")
	}
	bn := NewController(NoopExecutor{}, nil, "u", config.BinanceBrokerView{Cfg: config.DefaultBinanceConfig(), Market: "US"}, nil, "binance")
	r.Register("US", bn)
	if c := r.ControllerFor("US"); c != bn {
		t.Fatal("注册后应命中 US 控制器")
	}
	if c := r.ControllerFor(""); c != cn {
		t.Fatal("空市场键必须归一 CN")
	}
	if n := len(r.All()); n != 2 {
		t.Fatalf("All 应含 2 控制器: %d", n)
	}
}

var _ = json.Marshal // 预留给后续断言扩展
