// 文件职责：§BINANCE-CONFIG HTTP 端点回归（P1-b）——GET 脱敏形状 / POST 局部合并 /
// 脱敏哨兵不回写 / 非法值 400 不落库 / 发布闸缺省关。写法镜像 max_order_amount_api_test 最小栈。
package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"quant-trading-v2/internal/config"
)

// newBinanceConfigServer 构造无 store 的最小服务（Get/Set 走全局快照路径），逐用例独立。
// 独立性的关键在于落盘路径：config.NewManager 未 Load 到文件时 Rules 仍指向包级共享的
// DefaultRules 实例——用例间会互相污染（前一用例写入的凭证被后一用例读到）。
// 给每个用例一个真实存在的临时 config.json，Load 反序列化出全新 Rules，实例彻底隔离。
func newBinanceConfigServer(t *testing.T) *Server {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"rules":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Server{cfg: config.NewManager(path)}
}

// binanceGet 直调 GET /api/config/binance 处理函数并解析响应；非 200 直接失败（帮助函数不吞错）。
func binanceGet(t *testing.T, s *Server) map[string]interface{} {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetBinanceConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config/binance", nil))
	if rr.Code != 200 {
		t.Fatalf("GET binance config: %d %s", rr.Code, rr.Body.String())
	}
	var v map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func binancePost(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/config/binance", bytes.NewBufferString(body))
	s.handleSetBinanceConfig(rr, req)
	return rr
}

// TestBinanceConfigGETShape 发布闸+脱敏锁：GET 缺省 enabled=false；响应不得含 api_key/api_secret 明文键。
func TestBinanceConfigGETShape(t *testing.T) {
	s := newBinanceConfigServer(t)
	v := binanceGet(t, s)
	if v["enabled"] != false {
		t.Fatalf("GET 缺省 enabled 必须 false: %v", v["enabled"])
	}
	for _, leak := range []string{"api_key", "api_secret"} {
		if _, ok := v[leak]; ok {
			t.Fatalf("响应泄漏明文凭证键 %q", leak)
		}
	}
	if _, ok := v["api_key_masked"]; !ok {
		t.Fatal("响应缺 api_key_masked 字段")
	}
}

// TestBinanceConfigPatchAndSecrets 局部合并契约：提交明文凭证落库；再提交脱敏哨兵不得覆盖真值；
// 未提交字段（mode/quote_asset）保持原值。
func TestBinanceConfigPatchAndSecrets(t *testing.T) {
	s := newBinanceConfigServer(t)
	if rr := binancePost(t, s, `{"api_key":"abcdef0123456789","api_secret":"sekret0123456789"}`); rr.Code != 200 {
		t.Fatalf("首次 POST 失败: %d %s", rr.Code, rr.Body.String())
	}
	// 二次提交：回显的掩码串（含 …）必须被忽略。
	if rr := binancePost(t, s, `{"api_key":"abcd…6789","api_secret":"***","mode":"auto","testnet":false}`); rr.Code != 200 {
		t.Fatalf("二次 POST 失败: %d %s", rr.Code, rr.Body.String())
	}
	cfg := s.cfg.GetBinanceConfigFor("")
	if cfg.APIKey != "abcdef0123456789" || cfg.APISecret != "sekret0123456789" {
		t.Fatalf("脱敏哨兵覆盖了真值: %q %q", cfg.APIKey, cfg.APISecret)
	}
	if cfg.Mode != "auto" || cfg.Testnet {
		t.Fatalf("指针字段合并错误: %+v", cfg)
	}
	if cfg.QuoteAsset != "USDC" {
		t.Fatalf("未提交字段被误伤: %q", cfg.QuoteAsset)
	}
}

// TestBinanceConfigValidation400 非法输入 400 且不落库：mode 枚举 / 启用无凭证。
func TestBinanceConfigValidation400(t *testing.T) {
	s := newBinanceConfigServer(t)
	if rr := binancePost(t, s, `{"mode":"yolo"}`); rr.Code != 400 {
		t.Fatalf("非法 mode 应 400: %d", rr.Code)
	}
	if rr := binancePost(t, s, `{"enabled":true}`); rr.Code != 400 {
		t.Fatalf("启用但无凭证应 400: %d", rr.Code)
	}
	if s.cfg.GetBinanceConfigFor("").Enabled {
		t.Fatal("400 路径不得产生落库副作用")
	}
}

// TestBinanceConfigDataPlane §MR-1 HTTP 端点回归：数据面开关免凭证可开（200 落库、GET 回显），
// 但永不置起 enabled（交易判定与凭证闸都不看它）；子开关全关的纯 data_plane=true 必须 400。
// English: §MR-1 endpoint regression — data_plane opens credential-free yet never implies
// enabled=true; data_plane with all sub-switches off is rejected 400.
func TestBinanceConfigDataPlane(t *testing.T) {
	s := newBinanceConfigServer(t)
	if rr := binancePost(t, s, `{"data_plane":true,"spot":{"enabled":true}}`); rr.Code != 200 {
		t.Fatalf("数据面免凭证应 200: %d %s", rr.Code, rr.Body.String())
	}
	cfg := s.cfg.GetBinanceConfigFor("")
	if !cfg.DataPlane || cfg.Enabled {
		t.Fatalf("data_plane 落库错误或越权置起 enabled: data_plane=%v enabled=%v", cfg.DataPlane, cfg.Enabled)
	}
	if !cfg.Spot.Enabled {
		t.Fatal("spot 子档案整段替换未生效")
	}
	if v := binanceGet(t, s); v["data_plane"] != true {
		t.Fatalf("GET 视图未回显 data_plane: %v", v["data_plane"])
	}
	// 子开关全关 + 无凭证的纯数据面：validate 拒 400，且不得污染已落库值
	// （出厂缺省 stock/spot.enabled=true，须整档案替换显式关死才触发「≥1 子开关」规则）
	s2 := newBinanceConfigServer(t)
	if rr := binancePost(t, s2, `{"data_plane":true,"stock":{"enabled":false},"spot":{"enabled":false}}`); rr.Code != 400 {
		t.Fatalf("data_plane 无任何子开关应 400: %d %s", rr.Code, rr.Body.String())
	}
	if s2.cfg.GetBinanceConfigFor("").DataPlane {
		t.Fatal("400 路径不得落库")
	}
}

// TestBinanceConfigShortMarginRate §MR-4A 做空档案写入面行为锁：
// ①POST 带 stock.short_margin_rate=0.5 落库并从 GET 档案回显（整档替换腿零改动，字段随行）；
// ②>1 与负值被 validateBinance 拒（400），且不落库（GET 仍是旧值）——0=禁做空的出厂缺省
// 由发布闸测试覆盖（GET 缺省档案无该键=omitempty 零值=禁用）。
func TestBinanceConfigShortMarginRate(t *testing.T) {
	s := newBinanceConfigServer(t)
	if rr := binancePost(t, s, `{"stock":{"enabled":true,"fixed_amount":500,"short_margin_rate":0.5}}`); rr.Code != 200 {
		t.Fatalf("合法保证金率应 200: %d %s", rr.Code, rr.Body.String())
	}
	v := binanceGet(t, s)
	stock, _ := v["stock"].(map[string]interface{})
	if stock == nil || stock["short_margin_rate"] != 0.5 {
		t.Fatalf("GET 应回显 short_margin_rate=0.5, got %v", v["stock"])
	}
	for _, bad := range []string{"1.5", "-0.1"} {
		if rr := binancePost(t, s, `{"stock":{"enabled":true,"short_margin_rate":`+bad+`}}`); rr.Code != 400 {
			t.Fatalf("非法保证金率 %s 必须 400, got %d", bad, rr.Code)
		}
	}
	v = binanceGet(t, s)
	stock, _ = v["stock"].(map[string]interface{})
	if stock["short_margin_rate"] != 0.5 {
		t.Fatalf("非法提交不得污染已落库值, got %v", stock["short_margin_rate"])
	}
}
