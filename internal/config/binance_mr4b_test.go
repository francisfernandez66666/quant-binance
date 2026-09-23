// binance_mr4b_test.go — §MR-4B（2026-09-23）合约产品线的配置层单测：
// FuturesBaseURL 域名裁决、product_type/leverage 校验矩阵（含交易面开启时的杠杆红线）、
// FuturesActive 分叉唯一判定点。发布闸缺省形态（spot+无杠杆+enabled=false）由既有
// binance_test.go 全绿兜底，本文件只补新增字段的行为锁。
//
// English: §MR-4B config tests — futures REST root resolution, the product_type/leverage
// validation matrix (including the enabled-trading leverage red line), and the single
// FuturesActive fork predicate.
package config

import (
	"strings"
	"testing"
)

// TestFuturesBaseURL 凭证缺失→空串；testnet→testfuture 域；主网→fapi 域（与现货域名互斥）。
func TestFuturesBaseURL(t *testing.T) {
	c := BinanceConfig{}
	if c.FuturesBaseURL() != "" {
		t.Fatal("无凭证必须返回空串（拒绝探活/装配）")
	}
	c.APIKey, c.APISecret = "k", "s"
	c.Testnet = true
	if got := c.FuturesBaseURL(); got != "https://testnet.binancefuture.com" {
		t.Fatalf("testnet 合约域错: %s", got)
	}
	c.Testnet = false
	if got := c.FuturesBaseURL(); got != "https://fapi.binance.com" {
		t.Fatalf("主网合约域错: %s", got)
	}
	// 现货主网与合约主网域名互不相同——执行器分叉拿错域名会全链 404，配置面先钉死。
	if c.BaseURL() == c.FuturesBaseURL() {
		t.Fatal("现货/合约域名不得混同")
	}
}

// TestValidateMR4BMatrix product_type/leverage/liq 域与红线组合。
func TestValidateMR4BMatrix(t *testing.T) {
	base := func() *BinanceConfig {
		c := DefaultBinanceConfig()
		c.Enabled = false
		return &c
	}
	t.Run("stock_umfutures_rejected", func(t *testing.T) {
		c := base()
		c.Stock.ProductType = "umfutures"
		if err := ValidateBinance(c); err == nil {
			t.Fatal("美股档案不允许 umfutures 产品线")
		}
	})
	t.Run("bad_enum", func(t *testing.T) {
		c := base()
		c.Spot.ProductType = "cmfutures"
		if err := ValidateBinance(c); err == nil {
			t.Fatal("仅允许 spot/umfutures（coin-m 本位不在本仓实施范围）")
		}
	})
	t.Run("leverage_needs_umfutures", func(t *testing.T) {
		c := base()
		c.Spot.ProductType = "spot"
		c.Spot.Leverage = 3
		if err := ValidateBinance(c); err == nil {
			t.Fatal("现货档 leverage>1 必须拒（现货无杠杆语义）")
		}
	})
	t.Run("leverage_cap", func(t *testing.T) {
		c := base()
		c.Spot.ProductType = "umfutures"
		c.Spot.Leverage = 21
		if err := ValidateBinance(c); err == nil {
			t.Fatal("杠杆硬帽 20（超限配置面不放行）")
		}
	})
	t.Run("red_line_enabled_needs_ack", func(t *testing.T) {
		c := base()
		c.Spot.ProductType = "umfutures"
		c.Spot.Leverage = 3
		c.APIKey, c.APISecret = "k", "s" // 让 enabled 一致性检查先过凭证关
		c.Enabled = true
		if err := ValidateBinance(c); err == nil || !strings.Contains(err.Error(), "leverage_acknowledged_at") {
			t.Fatalf("交易面开启+杠杆>1 未确认必须拒: %v", err)
		}
		c.LeverageAcknowledgedAt = "2026-09-23T12:00:00Z"
		if err := ValidateBinance(c); err != nil {
			t.Fatalf("显式确认后应放行: %v", err)
		}
	})
	t.Run("disabled_allows_preconfig", func(t *testing.T) {
		c := base() // Enabled=false
		c.Spot.ProductType = "umfutures"
		c.Spot.Leverage = 5
		if err := ValidateBinance(c); err != nil {
			t.Fatalf("交易面关闭允许预配杠杆（结构上不可能真下单）: %v", err)
		}
	})
	t.Run("liq_dist_domain", func(t *testing.T) {
		c := base()
		c.Spot.LiqDistMinPct = 120
		if err := ValidateBinance(c); err == nil {
			t.Fatal("强平距离阈值域 0-100")
		}
	})
}

// TestFuturesActivePredicate 分叉唯一判定点：CRYPTO+umfutures=true；
// CRYPTO+spot=false；US 即使脏配置带 umfutures 也=false（双保险）。
func TestFuturesActivePredicate(t *testing.T) {
	c := BinanceConfig{}
	c.Spot = BinanceMarketProfile{Enabled: true, ProductType: "umfutures", Leverage: 3, LiqDistMinPct: 2}
	c.Stock = BinanceMarketProfile{Enabled: true, ProductType: "umfutures"}
	ct := BinanceBrokerView{Cfg: c, Market: "CRYPTO"}
	us := BinanceBrokerView{Cfg: c, Market: "US"}
	if !ct.FuturesActive() {
		t.Fatal("CRYPTO+umfutures 应为合约态")
	}
	if us.FuturesActive() {
		t.Fatal("US 视图永远不得进入合约分叉")
	}
	if ct.BrokerLeverage() != 3 || ct.BrokerLiqDistMinPct() != 2 {
		t.Fatalf("档案访问器映射错: lev=%d liq=%.1f", ct.BrokerLeverage(), ct.BrokerLiqDistMinPct())
	}
	c.Spot.ProductType = "" // 存量配置=现货，逐字节兼容
	if (BinanceBrokerView{Cfg: c, Market: "CRYPTO"}).FuturesActive() {
		t.Fatal("空 product_type 必须按现货处理（存量行为零变化）")
	}
}
