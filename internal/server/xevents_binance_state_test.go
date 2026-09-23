// xevents_binance_state_test.go §ENH-A4/B8 观测面行为锁：
// ① 未 SetXEventsSource → /api/binance/state 不含 "events" 键（零配置零行为）；
// ② 注入后两市场各成一节：有证据带 events/age_sec，无证据只报 {"ok":false}（不代造空列表）；
// ③ 配置 API：GET 掩码回显（明文 token 绝不出现在响应里），POST 掩码哨兵/空串不回写覆盖真值。
// English: locks for the §ENH-A4/B8 "events" node (absent without the closure; per-market
// ok/entry shape) and the config API token masking + no-clobber merge semantics.
package server

import (
	"encoding/json"
	"strings"
	"testing"

	"quant-trading-v2/internal/config"
	"quant-trading-v2/internal/trading"
)

// TestBinanceStateXEventsAbsentWithoutSource 未注入闭包：响应没有 events 键。
func TestBinanceStateXEventsAbsentWithoutSource(t *testing.T) {
	s := newFNGStateServer()
	s.SetEngineController(fngFakeCtrl{router: trading.NewBrokerRouter(nil)})
	out := callBinanceState(t, s)
	if _, exists := out["events"]; exists {
		t.Fatalf("未注入 XEventsSource 不应出现 events 节: %v", out["events"])
	}
}

// TestBinanceStateXEventsSection US 有证据 / CRYPTO 无证据 两态并存的形状锁。
func TestBinanceStateXEventsSection(t *testing.T) {
	s := newFNGStateServer()
	s.SetEngineController(fngFakeCtrl{router: trading.NewBrokerRouter(nil)})
	s.SetXEventsSource(func(market string) ([]map[string]any, int64, bool) {
		if market != "US" {
			return nil, -1, false
		}
		return []map[string]any{{"title": "8-K", "url": "u"}}, 7, true
	})
	out := callBinanceState(t, s)
	node, ok := out["events"].(map[string]any)
	if !ok {
		t.Fatalf("events 节应为对象: %v", out["events"])
	}
	us, ok := node["US"].(map[string]any)
	if !ok || us["ok"] != true || us["age_sec"] != float64(7) {
		t.Fatalf("US 节形状错: %v", node["US"])
	}
	evs, ok := us["events"].([]any)
	if !ok || len(evs) != 1 {
		t.Fatalf("US events 数组错: %v", us["events"])
	}
	cp, ok := node["CRYPTO"].(map[string]any)
	if !ok || cp["ok"] != false || len(cp) != 1 {
		t.Fatalf("CRYPTO 无证据节应只含 ok=false（不代造空列表）: %v", cp)
	}
}

// TestBinanceConfigEventsMaskingAndMerge 配置面：GET 只回掩码；POST 回显掩码/空串不清真 token；
// 新 token 才覆盖；UA 明文往返（非密钥，供 owner 核对合规 UA）。
func TestBinanceConfigEventsMaskingAndMerge(t *testing.T) {
	base := config.DefaultBinanceConfig()
	base.Events = config.BinanceEventsConfig{
		EdgarUserAgent:   "QuantBot admin@example.com",
		CryptoPanicToken: "secret-token-abcd1234",
	}
	// 出厂视图：明文 token 绝不见于 GET 响应
	view := binanceConfigView(&base)
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-token-abcd1234") {
		t.Fatal("GET 视图泄漏明文 CryptoPanic token")
	}
	ev := view["events"].(map[string]any)
	if ev["has_cryptopanic_token"] != true || !strings.Contains(ev["cryptopanic_token_masked"].(string), "…") {
		t.Fatalf("events 视图掩码形状错: %v", ev)
	}

	// 合并语义（与 api_key 同口径）：掩码哨兵/空串 → 保留真值；新值 → 覆盖
	cfg := base
	echo := maskSecret(base.Events.CryptoPanicToken)
	for _, v := range []*string{&echo, ptr("")} {
		token := *v
		req := setBinanceConfigReq{Events: &config.BinanceEventsConfig{
			EdgarUserAgent: cfg.Events.EdgarUserAgent, CryptoPanicToken: token,
		}}
		merged := mergeBinanceEvents(&cfg, &req)
		if merged.CryptoPanicToken != "secret-token-abcd1234" {
			t.Fatalf("掩码/空串回写覆盖了真 token: %q", merged.CryptoPanicToken)
		}
	}
	req := setBinanceConfigReq{Events: &config.BinanceEventsConfig{CryptoPanicToken: " new-key "}}
	if merged := mergeBinanceEvents(&cfg, &req); merged.CryptoPanicToken != "new-key" {
		t.Fatalf("新 token 应覆盖并 TrimSpace: %q", merged.CryptoPanicToken)
	}
}

func ptr(s string) *string { return &s }
