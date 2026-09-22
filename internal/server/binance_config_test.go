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
