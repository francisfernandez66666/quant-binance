// 文件职责：§BINANCE-CONFIG 测试批（P1-b）——rules.binance 出厂缺省/normalize/validate 三链，
// per-user KV（binance_config_json_v1）读写优先级，BinanceBrokerView 对 BrokerConfig 的映射语义
// （子开关合成、CloseSweepAt=-1、EnforceT1=false、市场档案路由）。
package config

import "testing"

// fakeKV binance 键专用内存 KVStore（与 enhance_test 里的桩同型，独立命名避免互相污染）。
type fakeKV struct {
	data map[string]map[string]string // userID → key → value
}

// SetConfig 实现 KVStore 写路径：按 userID→key 两级懒建 map 后覆盖写（与 auth 落盘语义同形）。
func (f *fakeKV) SetConfig(userID, key, value string) error {
	if f.data == nil {
		f.data = map[string]map[string]string{}
	}
	if f.data[userID] == nil {
		f.data[userID] = map[string]string{}
	}
	f.data[userID][key] = value
	return nil
}

func (f *fakeKV) GetConfig(userID, key string) (string, bool) {
	if f.data == nil || f.data[userID] == nil {
		return "", false
	}
	v, ok := f.data[userID][key]
	return v, ok
}

func TestBinanceDefaultsAndNormalize(t *testing.T) {
	// 发布闸：出厂缺省必须总开关关。
	d := DefaultBinanceConfig()
	if d.Enabled || d.Halted {
		t.Fatalf("出厂缺省必须 enabled=false: %+v", d)
	}
	if d.Stock.TradingSession != "RTH" || d.QuoteAsset != "USDC" {
		t.Fatalf("出厂缺省形状错误: %+v", d)
	}
	// 整段缺失（老文件无 binance 键）→ normalize 回填出厂默认（含 testnet=true）。
	var zero BinanceConfig
	NormalizeBinance(&zero)
	if !zero.Testnet || zero.Mode != "manual" || zero.TimeoutSec != 10 || zero.MissHeartbeatSec != 120 {
		t.Fatalf("缺段回填失败: %+v", zero)
	}
	if !zero.Stock.Enabled || !zero.Spot.Enabled {
		t.Fatal("缺段回填后子开关应为默认开（顶层 Enabled 才是总闸）")
	}
	// 显式 testnet=false 不得被回填翻转（段存在即逐字段 normalize）。
	b := BinanceConfig{Mode: "manual", TimeoutSec: 10, MissHeartbeatSec: 120, QuoteAsset: "USDT", CancelStaleSec: 120}
	NormalizeBinance(&b)
	if b.Testnet {
		t.Fatal("显式 testnet=false 应保留")
	}
}

func TestValidateBinance(t *testing.T) {
	// 缺省段（含出厂默认）必须通过——不启用不阻断启动。
	if err := validateBinance(&BinanceConfig{}); err != nil {
		t.Fatalf("零值段应合法: %v", err)
	}
	d := DefaultBinanceConfig()
	if err := validateBinance(&d); err != nil {
		t.Fatalf("出厂默认应合法: %v", err)
	}
	bad := []struct {
		name string
		mut  func(*BinanceConfig)
	}{
		{"mode 枚举", func(b *BinanceConfig) { b.Mode = "semi" }},
		{"session 枚举", func(b *BinanceConfig) { b.Stock.TradingSession = "PRE" }},
		{"tif 枚举", func(b *BinanceConfig) { b.Stock.TimeInForce = "IOC" }},
		{"负金额", func(b *BinanceConfig) { b.Spot.FixedAmount = -1 }},
		{"max_positions 越界", func(b *BinanceConfig) { b.Stock.MaxPositions = 51 }},
		{"心跳越界", func(b *BinanceConfig) { b.MissHeartbeatSec = 5 }},
		{"撤单秒数非法", func(b *BinanceConfig) { b.CancelStaleSec = 10 }},
		{"启用无凭证", func(b *BinanceConfig) { b.Enabled = true; b.APIKey = ""; b.APISecret = "" }},
		{"启用双子开关全关", func(b *BinanceConfig) {
			b.Enabled = true
			b.APIKey = "k"
			b.APISecret = "s"
			b.Stock.Enabled = false
			b.Spot.Enabled = false
		}},
	}
	for _, c := range bad {
		b := DefaultBinanceConfig()
		b.APIKey, b.APISecret = "k", "s" // 排除凭证干扰，逐例定点触发
		c.mut(&b)
		if err := validateBinance(&b); err == nil {
			t.Fatalf("%s: 应被拒绝但通过了", c.name)
		}
	}
}

func TestBinancePerUserKV(t *testing.T) {
	m := NewManager("")
	kv := &fakeKV{}
	m.SetStore(kv)
	m.SetOperatorID("op")

	// 无任何落库：全局快照兜底。
	if got := m.GetBinanceConfigFor("u1"); got.Enabled {
		t.Fatal("初始应为出厂关")
	}
	// 账号级写入 → 只影响该账号；他账号回退运营/全局。
	cfg := DefaultBinanceConfig()
	cfg.Enabled = true
	cfg.APIKey, cfg.APISecret = "key-u1", "sec-u1"
	m.SetBinanceConfigFor("u1", &cfg)
	got := m.GetBinanceConfigFor("u1")
	if !got.Enabled || got.APIKey != "key-u1" {
		t.Fatalf("u1 读回错误: %+v", got)
	}
	if m.GetBinanceConfigFor("u2").Enabled {
		t.Fatal("u2 不应继承 u1 的覆盖")
	}
	// 运营账号覆盖对子账号生效（与 GetQMTConfigFor 优先级一致）。
	opCfg := DefaultBinanceConfig()
	opCfg.QuoteAsset = "USDP"
	m.SetBinanceConfigFor("op", &opCfg)
	if m.GetBinanceConfigFor("u3").QuoteAsset != "USDP" {
		t.Fatal("u3 应回退到运营账号覆盖")
	}
	// 坏 JSON → 回退全局不 panic。
	_ = kv.SetConfig("u4", perUserBinanceKey, "{not json")
	if m.GetBinanceConfigFor("u4").Enabled {
		t.Fatal("坏 JSON 应回退全局（关）")
	}
}

func TestBinanceBrokerView(t *testing.T) {
	b := DefaultBinanceConfig()
	b.Enabled = true
	b.Halted = true
	b.Stock.FixedAmount = 500
	b.Spot.FixedAmount = 100
	b.Spot.Blacklist = []string{"300001"}

	us := BrokerConfig(BinanceBrokerView{Cfg: b, Market: "US"})
	ct := BrokerConfig(BinanceBrokerView{Cfg: b, Market: "CRYPTO"})

	if !us.BrokerEnabled() || !ct.BrokerEnabled() {
		t.Fatal("总开关开+子开关开 → BrokerEnabled 应为 true")
	}
	if us.BrokerFixedAmount() != 500 || ct.BrokerFixedAmount() != 100 {
		t.Fatalf("市场档案路由错误: US=%g CRYPTO=%g", us.BrokerFixedAmount(), ct.BrokerFixedAmount())
	}
	if us.BrokerCloseSweepAt() != -1 || ct.BrokerCloseSweepAt() != -1 {
		t.Fatal("币安侧无收盘清单，必须 -1")
	}
	if us.BrokerEnforceT1() {
		t.Fatal("Binance 恒 T+0")
	}
	if !us.BrokerHalted() {
		t.Fatal("Halted 透传错误")
	}
	if us.BrokerMarket("600519.SH") != "US" {
		t.Fatal("视图市场优先：US 视图对任意代码都返回 US（req.Market 直判的兜底不走形态）")
	}
	// 子开关合成：总开关开但 US 子档案关 → US 停用、CRYPTO 不受影响。
	b2 := b
	b2.Stock.Enabled = false
	if (BinanceBrokerView{Cfg: b2, Market: "US"}).BrokerEnabled() {
		t.Fatal("US 子开关关 → BrokerEnabled 必须 false")
	}
	if !(BinanceBrokerView{Cfg: b2, Market: "CRYPTO"}).BrokerEnabled() {
		t.Fatal("US 子开关不得牵连 CRYPTO")
	}
}

// TestBinanceDataPlane §MR-1 行为锁：数据面/交易面拆分。
// ① data_plane=true 无凭证合法（观测链免钥匙——美股/加密货币行情本就公开）；
// ② data_plane=true 双子开关全关仍拒（开了没意义）；
// ③ 视图语义：数据面 BrokerEnabled=true 但 TradingActive=false（真执行器永不因数据面出现）；
// ④ enabled=true 无凭证仍拒（交易面凭证闸不因拆分而松）。
func TestBinanceDataPlane(t *testing.T) {
	b := DefaultBinanceConfig()
	b.DataPlane = true // 凭证留空、spot 子开关出厂为开
	if err := validateBinance(&b); err != nil {
		t.Fatalf("数据面无凭证应合法: %v", err)
	}
	b.Stock.Enabled = false
	b.Spot.Enabled = false
	if err := validateBinance(&b); err == nil {
		t.Fatal("数据面双子开关全关应被拒")
	}
	// 视图双闸语义
	c := DefaultBinanceConfig()
	c.DataPlane = true
	us := BinanceBrokerView{Cfg: c, Market: "US"}
	if !us.BrokerEnabled() || us.TradingActive() {
		t.Fatalf("数据面视图应活跃但非交易态: enabled=%v trading=%v", us.BrokerEnabled(), us.TradingActive())
	}
	c.Enabled = true
	c.APIKey, c.APISecret = "k", "s"
	if v := (BinanceBrokerView{Cfg: c, Market: "US"}); !v.BrokerEnabled() || !v.TradingActive() {
		t.Fatal("交易面视图两闸都须为真")
	}
	// 交易面凭证闸不松
	d := DefaultBinanceConfig()
	d.Enabled = true
	d.APIKey, d.APISecret = "", ""
	if err := validateBinance(&d); err == nil {
		t.Fatal("enabled=true 无凭证必须仍被拒")
	}
}
