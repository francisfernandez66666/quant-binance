// 文件职责：§战法批（2026-09-23）配置面行为锁——派发参数域、事件 LLM 三键齐备闸、
// 纸面盘装配判定 PaperActive 与市场子开关/交易面的一致性约束。
// English: config-side behavior locks for the xasset dispatch batch (dispatch domains,
// all-or-nothing LLM keys, paper-desk predicate).
package config

import "testing"

func baseXAsset() BinanceConfig {
	c := DefaultBinanceConfig()
	c.Testnet = false
	return c
}

// TestValidateDispatchDomain 派发参数域校验：置信度 0-1、节拍 ≥60（0=缺省）、单轮新单帽 0-50。
func TestValidateDispatchDomain(t *testing.T) {
	c := baseXAsset()
	c.Dispatch.MinConfidence = 1.5
	if err := validateBinance(&c); err == nil {
		t.Fatal("min_confidence>1 必须拒")
	}
	c.Dispatch.MinConfidence = 0.7
	c.Dispatch.EverySec = 30
	if err := validateBinance(&c); err == nil {
		t.Fatal("every_sec ∈ (0,60) 必须拒（0=缺省合法）")
	}
	c.Dispatch.EverySec = 0
	c.Dispatch.MaxLiveOrders = 51
	if err := validateBinance(&c); err == nil {
		t.Fatal("max_live_orders>50 必须拒")
	}
	c.Dispatch.MaxLiveOrders = 0
	if err := validateBinance(&c); err != nil {
		t.Fatalf("合法派发参数被拒: %v", err)
	}
}

// TestValidateDispatchNeedsADesk 派发开但既无真交易面（enabled+凭证）也无纸面盘=配置错位，启动即拒。
func TestValidateDispatchNeedsADesk(t *testing.T) {
	c := baseXAsset()
	c.Dispatch.Enabled = true
	if err := validateBinance(&c); err == nil {
		t.Fatal("dispatch=true 且无交易面无纸面盘必须拒（信号无处成交的半截接线）")
	}
	// 纸面盘顶上（交易面保持关，数据面开启=装配门在位）→ 放行
	c.Spot.Paper = true
	c.DataPlane = true
	if err := validateBinance(&c); err != nil {
		t.Fatalf("纸面盘下派发合法: %v", err)
	}
	// §战法批-4 死配置反证：平面全关时 paper 建不出柜台，必须拒
	c.DataPlane = false
	if err := validateBinance(&c); err == nil {
		t.Fatal("paper=true 且数据面/交易面全关必须拒（装配门是平面开关）")
	}
	c.DataPlane = true
	// 真交易面顶上 → 放行
	c2 := baseXAsset()
	c2.Dispatch.Enabled = true
	c2.Enabled = true
	c2.APIKey, c2.APISecret = "k", "s"
	if err := validateBinance(&c2); err != nil {
		t.Fatalf("交易面下派发合法: %v", err)
	}
}

// TestValidatePaperHygiene 纸面盘卫生：paper=true 但市场子开关关=柜台根本不会装配，拒。
func TestValidatePaperHygiene(t *testing.T) {
	c := baseXAsset()
	c.Stock.Enabled = false
	c.Stock.Paper = true
	if err := validateBinance(&c); err == nil {
		t.Fatal("paper=true 且子开关关必须拒")
	}
	c2 := baseXAsset()
	c2.Spot.PaperCash = -1
	if err := validateBinance(&c2); err == nil {
		t.Fatal("paper_cash 负值必须拒")
	}
}

// TestValidateLLMKeysAllOrNothing LLM 三键：全空=关键词基线合法；半截=拒；齐备=过。
func TestValidateLLMKeysAllOrNothing(t *testing.T) {
	c := baseXAsset()
	if err := validateBinance(&c); err != nil {
		t.Fatalf("全空合法（零配置零行为）: %v", err)
	}
	c.Events.LLMBaseURL = "https://api.deepseek.com/v1"
	if err := validateBinance(&c); err == nil {
		t.Fatal("只配 base_url 的半截接线必须拒")
	}
	c.Events.LLMApiKey, c.Events.LLMModel = "sk-x", "deepseek-chat"
	if err := validateBinance(&c); err != nil {
		t.Fatalf("三键齐备必须过: %v", err)
	}
	c.Events.LLMTimeoutSec = 2
	if err := validateBinance(&c); err == nil {
		t.Fatal("llm_timeout_sec ∈ (0,5) 必须拒")
	}
}

// TestPaperActivePredicate 纸面柜台装配判定的三格矩阵：交易面开=恒 false（真面优先），
// 交易面关+子开关开+paper=真，子开关关=假。
func TestPaperActivePredicate(t *testing.T) {
	c := baseXAsset()
	c.Spot.Paper = true
	v := BinanceBrokerView{Cfg: c, Market: "CRYPTO"}
	if !v.PaperActive() {
		t.Fatal("交易面关+纸面开=应装配纸面柜台")
	}
	c.Enabled, c.APIKey, c.APISecret = true, "k", "s"
	v.Cfg = c
	if v.PaperActive() {
		t.Fatal("交易面激活时纸面盘必须让位（绝不静默把真单降级成假成交）")
	}
	c.Spot.Enabled = false
	v.Cfg = c
	if v.PaperActive() {
		t.Fatal("市场子开关关=不装配")
	}
	// US 档案独立：CRYPTO 开纸面不影响 US 视图判定
	c2 := baseXAsset()
	c2.Spot.Paper = true
	if (BinanceBrokerView{Cfg: c2, Market: "US"}).PaperActive() {
		t.Fatal("US 视图不受 CRYPTO 纸面开关影响")
	}
}

// TestDispatchEffectiveDefaults 消费侧缺省回填：门槛 0→0.6、节拍 0→600、单轮帽 0→5。
func TestDispatchEffectiveDefaults(t *testing.T) {
	var d BinanceDispatchConfig
	if d.ConfidenceFloor() != 0.6 || d.EverySecOr() != 600 || d.MaxNewOrdersPerRound() != 5 {
		t.Fatalf("缺省回填错位: %v %v %v", d.ConfidenceFloor(), d.EverySecOr(), d.MaxNewOrdersPerRound())
	}
	d = BinanceDispatchConfig{MinConfidence: 0.8, EverySec: 300, MaxLiveOrders: 2}
	if d.ConfidenceFloor() != 0.8 || d.EverySecOr() != 300 || d.MaxNewOrdersPerRound() != 2 {
		t.Fatal("显式值必须原样生效")
	}
}
