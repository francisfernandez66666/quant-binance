// 文件职责：§CN-MASTER 配置段锁——rules.cn.enabled 出厂缺省必须为 false（用户指令：
// 默认关闭 A股相关东西），且只有显式 {"cn":{"enabled":true}} 才开链。
// 缺 cn 段的存量 config.json 反序列化后必须落到零值 false（无迁移、旧文件天然关）。
// English: §CN-MASTER config locks — default off, opt-in only via explicit cn.enabled=true.
package config

import (
	"encoding/json"
	"testing"
)

func TestCNMasterDefaultsOff(t *testing.T) {
	var r Rules
	if r.CN.Enabled {
		t.Fatal("Rules 零值的 cn.enabled 必须为 false（出厂默认关闭 A股链）")
	}
}

func TestCNMasterUnmarshal(t *testing.T) {
	var r Rules
	if err := json.Unmarshal([]byte(`{}`), &r); err != nil {
		t.Fatalf("空对象反序列化失败: %v", err)
	}
	if r.CN.Enabled {
		t.Fatal("缺 cn 段的存量配置反序列化后不得为 true")
	}
	if err := json.Unmarshal([]byte(`{"cn":{"enabled":true}}`), &r); err != nil {
		t.Fatalf("cn 段反序列化失败: %v", err)
	}
	if !r.CN.Enabled {
		t.Fatal(`显式 {"cn":{"enabled":true}} 应开链`)
	}
	// 注：不锁"零值序列化不含 cn 键"——encoding/json 的 omitempty 对结构体字段无效
	// （既有 rules.binance 同款先例），键恒在、值恒 false，对旧文件读写零影响。
}
