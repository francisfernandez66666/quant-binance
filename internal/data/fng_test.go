// 文件职责：fng.go 的行为锁（§ENH-A1）。用 httptest 假服务器覆盖五条契约：
// ① 正常两行→解析出序列且缓存落到最新一条；② 坏 JSON→报错且旧缓存保留；
// ③ HTTP 500→报错且旧缓存保留（失败不清证据）；④ limit 超 400 截断成 400 下发；
// ⑤ value 越界行单条拒收、不毒化整批。另锁 Age() 的 -1 无缓存哨兵与到达时刻计龄。
//
// English: behavior locks for fng.go against an httptest fake server — happy path,
// malformed JSON, HTTP 500 cache retention, limit clamping and row-wise rejection of
// out-of-range values, plus the Age() no-cache sentinel measured by arrival time.
package data

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// fngBody 拼一段与上游同形态的响应体：rows 为 (value, classification, timestamp秒) 三元组。
func fngBody(rows [][3]string) string {
	out := `{"data":[`
	for i, r := range rows {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf(`{"value":%q,"value_classification":%q,"timestamp":%q}`, r[0], r[1], r[2])
	}
	return out + `],"metadata":{},"error":null}`
}

// newFNGStub 起一个假服务器（handler 由用例现编应答），返回注入 Base/虚拟时钟的客户端。
func newFNGStub(t *testing.T, handler http.HandlerFunc) *FNGClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	now := time.Unix(1800000000, 0).UTC()
	return &FNGClient{Base: srv.URL, Now: func() time.Time { return now }}
}

// TestFNGFetchHappyPath 正常两行：倒序序列原样返回、缓存取时间戳最大的一条、Age 按到达时刻计。
func TestFNGFetchHappyPath(t *testing.T) {
	ts := time.Unix(1700000000, 0)
	c := newFNGStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fng/" {
			t.Errorf("路径应为 /fng/，实际 %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(fngBody([][3]string{
			{"78", "Extreme Greed", "1700000000"},
			{"22", "Extreme Fear", "1699913600"},
		})))
	})
	if _, ok := c.Last(); ok {
		t.Fatal("Fetch 前不应有缓存")
	}
	if age := c.Age(); age != -1 {
		t.Fatalf("无缓存 Age 应为 -1 哨兵，实际 %v", age)
	}
	samples, err := c.Fetch(0) // 0 走缺省条数，不该报错
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(samples) != 2 || samples[0].Value != 78 || samples[1].Classification != "Extreme Fear" {
		t.Fatalf("序列解析错: %+v", samples)
	}
	last, ok := c.Last()
	if !ok || last.Value != 78 || !last.Timestamp.Equal(ts) {
		t.Fatalf("缓存应落最新一条 78@%v，实际 %+v ok=%v", ts, last, ok)
	}
	if age := c.Age(); age != 0 {
		t.Fatalf("虚拟时钟下到达即龄 0，实际 %v", age)
	}
}

// TestFNGFetchBadJSONKeepsCache 坏 JSON：Fetch 报错，旧缓存原样保留（失败不清证据）。
func TestFNGFetchBadJSONKeepsCache(t *testing.T) {
	body := fngBody([][3]string{{"78", "Extreme Greed", "1700000000"}})
	c := newFNGStub(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body)) // 闭包读变量：用例中途改写 body 即改写应答
	})
	if _, err := c.Fetch(5); err != nil {
		t.Fatalf("首拉应成功: %v", err)
	}
	body = `{oops`
	if _, err := c.Fetch(5); err == nil {
		t.Fatal("坏 JSON 应报错")
	}
	if s, ok := c.Last(); !ok || s.Value != 78 {
		t.Fatalf("坏 JSON 后旧缓存必须保留，实际 %+v ok=%v", s, ok)
	}
}

// TestFNGFetchHTTP500KeepsCache HTTP 500：同样报错保缓存——外部源挂了不等于证据没了。
func TestFNGFetchHTTP500KeepsCache(t *testing.T) {
	code := http.StatusOK
	c := newFNGStub(t, func(w http.ResponseWriter, r *http.Request) {
		if code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		_, _ = w.Write([]byte(fngBody([][3]string{{"78", "Extreme Greed", "1700000000"}})))
	})
	if err := c.Refresh(); err != nil {
		t.Fatalf("Refresh 首拉: %v", err)
	}
	code = http.StatusInternalServerError
	if err := c.Refresh(); err == nil {
		t.Fatal("500 应报错")
	}
	if s, ok := c.Last(); !ok || s.Value != 78 {
		t.Fatalf("500 后旧缓存必须保留，实际 %+v ok=%v", s, ok)
	}
}

// TestFNGFetchLimitClamped limit 超上限 400 截断下发；正常值原样透传（上游只认 limit 参数）。
func TestFNGFetchLimitClamped(t *testing.T) {
	seen := make([]string, 0, 2)
	c := newFNGStub(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query().Get("limit"))
		_, _ = w.Write([]byte(fngBody([][3]string{{"50", "Neutral", "1700000000"}})))
	})
	if _, err := c.Fetch(FNGMaxLimit + 100); err != nil {
		t.Fatalf("Fetch(超限): %v", err)
	}
	if _, err := c.Fetch(7); err != nil {
		t.Fatalf("Fetch(正常): %v", err)
	}
	if len(seen) != 2 || seen[0] != strconv.Itoa(FNGMaxLimit) || seen[1] != "7" {
		t.Fatalf("limit 透传错（期望 [%d 7]）: %v", FNGMaxLimit, seen)
	}
}

// TestFNGFetchRejectsOutOfRangeRows value 越界/坏数行单条拒收，合法行照常成批返回；
// 全批越界则视为 Fetch 失败（无有效证据），且不动旧缓存。
func TestFNGFetchRejectsOutOfRangeRows(t *testing.T) {
	c := newFNGStub(t, func(w http.ResponseWriter, r *http.Request) {
		mix := fngBody([][3]string{
			{"120", "Bogus High", "1700000100"}, // 上越界：拒
			{"78", "Extreme Greed", "1700000000"},
			{"-3", "Bogus Low", "1699999900"}, // 下越界：拒
			{"", "Missing", "1699999800"},     // 空值：拒
			{"22", "Extreme Fear", "1699913600"},
		})
		if r.URL.Query().Get("limit") == "1" {
			mix = fngBody([][3]string{{"120", "Bogus High", "1700000100"}}) // 全批越界
		}
		_, _ = w.Write([]byte(mix))
	})
	samples, err := c.Fetch(9)
	if err != nil {
		t.Fatalf("混批应成功: %v", err)
	}
	if len(samples) != 2 || samples[0].Value != 78 || samples[1].Value != 22 {
		t.Fatalf("越界行未被拒收: %+v", samples)
	}
	if _, err := c.Fetch(1); err == nil {
		t.Fatal("全批越界应报错（无有效采样行）")
	}
	if s, ok := c.Last(); !ok || s.Value != 78 {
		t.Fatalf("全批越界后旧缓存必须保留，实际 %+v ok=%v", s, ok)
	}
}
