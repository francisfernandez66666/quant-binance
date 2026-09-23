// 文件职责：binance_ws_std.go（纯标准库 RFC6455 客户端 StdWsDial）的单测。
//
// 关键纪律：测试里的**假服务端完全独立实现**——httptest + Hijack 拿裸 TCP，手写 101 握手，
// 帧的编解码在测试内再写一遍（绝不复用被测代码）。原因：若服务端下发帧也走被测的编码器，
// "编码错 + 解码错"互为镜像时会自我掩盖，掩码位/长度三档/分片这些真正要验的细节就白测了。
//
// 覆盖面（七组必测 + 六组加测）：文本帧读取、ping→自动 pong、出站帧掩码合规（含每帧
// key 随机）、分片重组、服务端 close 帧、坏握手（缺/错 Sec-WebSocket-Accept、非 101 状态码）、
// ctx 取消及时返回；加测长度三档（7/16/64 bit）、协议违规帧（服务器帧带掩码 / RSV 非零 /
// 未知 opcode）拒绝、scheme 白名单与 wss 走 TLS 层、控制帧插在**分片中段**（§5.4 最容易写错
// 的一处）、本地 Close 打断阻塞读、以及"注入 BinanceWS 后端到端出帧"（接缝真能用）。
//
// 端口一律由 httptest 随机分配（不硬编码端口，避免并行机器上的端口争抢）。
package data

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// stdwsTestGUID 握手魔数在测试里**重抄一遍**（不引用被测常量）：被测代码写错魔数必须被测出来。
const stdwsTestGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// stdwsTestServer 内存假 WS 服务端。生命周期：newStdwsTestServer 起 httptest 监听，
// 每次拨号命中 handler 并 Hijack 一条裸连接；stop()（t.Cleanup 注册）先收 handler 再关服务。
type stdwsTestServer struct {
	t      *testing.T
	srv    *httptest.Server
	push   chan []byte // 测试投递给客户端的**原始帧字节**（未经被测代码处理）
	quit   chan struct{}
	broken stdwsBroken

	mu      sync.Mutex
	data    []stdwsTestMsg // 客户端 → 服务端的数据帧（已去掩码）
	ctrl    []stdwsTestMsg // 客户端 → 服务端的控制帧（pong / close）
	handled int            // 命中 handler 的次数（= 握手次数）
}

// newStdwsTestServer 起假服务端并注册收尾。收摊顺序很要紧：必须先 close(quit) 让 handler
// 从 select 里退出，再 srv.Close()——反过来的话 httptest 会等一个永远打转的 handler，测试挂死。
func newStdwsTestServer(t *testing.T, broken stdwsBroken) *stdwsTestServer {
	t.Helper()
	s := &stdwsTestServer{
		push:   make(chan []byte, 16),
		quit:   make(chan struct{}),
		broken: broken,
		t:      t,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.serve)
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.stop)
	return s
}

// stop 幂等收摊（重复调用不 panic）。
func (s *stdwsTestServer) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.quit:
	default:
		close(s.quit)
	}
	s.srv.Close()
}

// wsURL 把 httptest 的 http://127.0.0.1:port 换成 ws://127.0.0.1:port/path。
// 测试链走明文 ws（TLS 分支另有 TestStdWsDialTLSSchemeRouting 覆盖 scheme 路由）。
func (s *stdwsTestServer) wsURL(path string) string {
	return "ws://" + strings.TrimPrefix(s.srv.URL, "http://") + path
}

// serve 处理一次拨号：可选破坏握手 → Hijack → 手写帧下发 → 手写帧解码收帧。
func (s *stdwsTestServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.handled++
	broken := s.broken
	s.mu.Unlock()
	// 先自检请求头：正常用例里客户端必须带 Sec-WebSocket-Key，否则 accept 无从算起（接缝契约没遵守）。
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" && broken == stdwsBrokenNone {
		s.t.Errorf("客户端握手未携带 Sec-WebSocket-Key")
		http.Error(w, "bad handshake", http.StatusBadRequest)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		s.t.Errorf("httptest 不支持 Hijack，假服务端无法继续")
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		s.t.Errorf("Hijack 失败: %v", err)
		return
	}
	defer conn.Close()
	// 握手响应逐字符手写（accept 由 stdwsTestAccept 独立计算），刻意不走被测代码。
	if _, werr := fmt.Fprint(rw, stdwsHandshakeResp(broken, key)); werr != nil {
		return
	}
	if ferr := rw.Flush(); ferr != nil {
		return
	}
	if broken != stdwsBrokenNone {
		return // 坏握手任务的使命已完成：让客户端自己判错并断开
	}
	// 读侧独立协程：客户端每一条出站帧都要能解开（掩码没解对就是乱码，用例立刻红）。
	go s.readLoop(rw.Reader)
	// 写侧留在本协程串行执行，保证下发顺序 == 测试投喂顺序（分片用例强依赖这个顺序）。
	for {
		select {
		case <-s.quit:
			return
		case b := <-s.push:
			if _, werr := conn.Write(b); werr != nil {
				return
			}
		}
	}
}

// pushFrame 编码一帧（服务端→客户端方向**不掩码**，RFC6455 §5.1）后交给 handler 下发。
func (s *stdwsTestServer) pushFrame(opcode byte, fin bool, payload []byte) {
	s.push <- stdwsFrameFull(fin, 0, opcode, false, payload)
}

// stdwsHandshakeResp 按破坏模式生成握手响应文本（三种 101 形态的差别只在 Accept 头）。
func stdwsHandshakeResp(broken stdwsBroken, key string) string {
	// 非切换协议：客户端必须在读到 200 时判错（对面压根不是 WS 服务）。
	if broken == stdwsBrokenStatus200 {
		return "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"
	}
	switch broken {
	case stdwsBrokenNoAccept: // 缺 Accept：最典型的"看着像但不是真 WS"
		return "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n"
	case stdwsBrokenWrongAccept: // Accept 算错（key 被尾部篡改）：逐字节校验必须拦住
		return "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + stdwsTestAccept(key+"-tampered") + "\r\n\r\n"
	default: // 合规响应
		return "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + stdwsTestAccept(key) + "\r\n\r\n"
	}
}

// stdwsTestAccept base64(SHA1(key + GUID))：独立于被测实现算 accept（魔数用测试自带那份）。
func stdwsTestAccept(key string) string {
	sum := sha1.Sum([]byte(key + stdwsTestGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// stdwsBroken 握手破坏方式：坏握手用例靠它把服务端切成"看着像 WS 但协议不合规"。
type stdwsBroken int

const (
	stdwsBrokenNone        stdwsBroken = iota // 正常完成握手
	stdwsBrokenNoAccept                       // 101 但不回 Sec-WebSocket-Accept
	stdwsBrokenWrongAccept                    // 回了算错的 Accept
	stdwsBrokenStatus200                      // 根本不切换协议（200 打回）
)

// stdwsTestMsg 服务端侧观察到的一帧（含"是否带掩码 + 掩码 key"这两个只有独立解码才看得见的位）。
type stdwsTestMsg struct {
	opcode  byte
	fin     bool
	masked  bool
	maskKey [4]byte // 客户端帧的掩码 key（必须是每帧随机的非零值）
	payload []byte
}

// stdwsFrame 正常下行帧编码：单片（fin=1）、无 RSV、服务器侧禁掩码。
// （上方保留 Agent 版 stdwsHandshakeResp/stdwsTestAccept 单一定义，本文件曾出现的
// 第二份重复定义已并入——同名双份会让整个 data 包编译失败。）
func stdwsFrame(opcode byte, payload []byte) []byte {
	return stdwsFrameFull(true, 0, opcode, false, payload)
}

// stdwsFrameFull 全参数编码（仅供违规帧测试拉偏位）：fin / RSV1-3 / 掩码位都可注入非法值。
// 掩码位为真时并不真的掩编码载荷——被测客户端读到两字节头就该判违规断开，后面不再消费。
func stdwsFrameFull(fin bool, rsv byte, opcode byte, maskBit bool, payload []byte) []byte {
	b0 := opcode & 0x0F
	if fin {
		b0 |= 0x80
	}
	b0 |= (rsv & 0x07) << 4
	l := len(payload)
	var out []byte
	switch {
	case l < 126: // 7 位长度直接放
		out = make([]byte, 2)
		out[1] = byte(l)
	case l <= 0xFFFF: // 126 标记 + 16 位扩展长度
		out = make([]byte, 4)
		out[1] = 126
		binary.BigEndian.PutUint16(out[2:], uint16(l))
	default: // 127 标记 + 64 位扩展长度
		out = make([]byte, 10)
		out[1] = 127
		binary.BigEndian.PutUint64(out[2:], uint64(l))
	}
	out[0] = b0
	if maskBit {
		out[1] |= 0x80
		out = append(out, 0, 0, 0, 0) // 伪装的四字节掩码 key
	}
	return append(out, payload...)
}

// readLoop 独立解码客户端 → 服务端帧（客户端帧必须带掩码，这里真解一遍：
// 被测代码若掩码算错，解出来的就是乱码，对应断言立刻红）。
func (s *stdwsTestServer) readLoop(br *bufio.Reader) {
	for {
		select {
		case <-s.quit:
			return
		default:
		}
		head := make([]byte, 2)
		if _, err := io.ReadFull(br, head); err != nil {
			return
		}
		fin := head[0]&0x80 != 0
		opcode := head[0] & 0x0F
		masked := head[1]&0x80 != 0
		n := uint64(head[1] & 0x7F)
		switch n {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(br, ext); err != nil {
				return
			}
			n = uint64(binary.BigEndian.Uint16(ext))
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(br, ext); err != nil {
				return
			}
			n = binary.BigEndian.Uint64(ext)
		}
		var key []byte
		var mk [4]byte // 现 struct 第 282 行已有 maskKey [4]byte 字段：此处只做留档赋值
		if masked {
			key = make([]byte, 4)
			if _, err := io.ReadFull(br, key); err != nil {
				return
			}
			copy(mk[:], key)
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(br, payload); err != nil {
			return
		}
		if masked { // 逐字节 XOR 还原（掩码算法的独立实现）
			for i := range payload {
				payload[i] ^= key[i%4]
			}
		}
		m := stdwsTestMsg{opcode: opcode, fin: fin, masked: masked, maskKey: mk, payload: payload}
		s.mu.Lock()
		switch opcode {
		case 0x8, 0x9, 0xA: // 控制帧另存一类（pong/close 的断言只看这里）
			s.ctrl = append(s.ctrl, m)
		default:
			s.data = append(s.data, m)
		}
		s.mu.Unlock()
		if opcode == 0x8 {
			return // 客户端 Close 的 close 帧收到即收摊
		}
	}
}

// snapshot 取当前观察列表副本（数据帧/控制帧分开），断言时不被并发追加干扰。
func (s *stdwsTestServer) snapshot() (dataMsgs, ctrlMsgs []stdwsTestMsg) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]stdwsTestMsg(nil), s.data...), append([]stdwsTestMsg(nil), s.ctrl...)
}

// waitForMsgs 轮询等"至少 n 条"观察帧到位（假服务端解帧在独立协程，不能假设即时可见）。
func (s *stdwsTestServer) waitForMsgs(t *testing.T, ctrl bool, n int) []stdwsTestMsg {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d, c := s.snapshot()
		got := d
		if ctrl {
			got = c
		}
		if len(got) >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("3s 内未等到 %d 条观察帧（ctrl=%v）", n, ctrl)
	return nil
}

// mustDial 拨号必须成功并返回 transport（失败直接 Fatalf，省掉每个用例重复判 err）。
func mustDial(t *testing.T, s *stdwsTestServer) WsTransport {
	t.Helper()
	tr, err := StdWsDial(context.Background(), s.wsURL("/ws"))
	if err != nil {
		t.Fatalf("StdWsDial 应成功: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return tr
}

// TestStdWsDialLocksTextFrame 最基本的读面：服务端下发一条文本帧，ReadMessage 原样吐出。
func TestStdWsDialLocksTextFrame(t *testing.T) {
	s := newStdwsTestServer(t, stdwsBrokenNone)
	tr := mustDial(t, s)
	body := []byte(`{"e":"miniTicker","s":"BTCUSDT"}`)
	s.push <- stdwsFrame(0x1, body)
	got, err := tr.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage 失败: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("帧内容不符 got=%q want=%q", got, body)
	}
}

// TestStdWsDialLocksLengthTiers 长度三档编码（7/16/64 bit）逐档实测：
// 126 是 7 位上限的边界外第一个值，65536 是 16 位档位之外的第一个值，各差一字节都不行。
func TestStdWsDialLocksLengthTiers(t *testing.T) {
	s := newStdwsTestServer(t, stdwsBrokenNone)
	tr := mustDial(t, s)
	for _, n := range []int{125, 126, 65535, 65536} {
		body := make([]byte, n)
		for i := range body {
			body[i] = byte('A' + i%26)
		}
		s.push <- stdwsFrame(0x2, body)
		got, err := tr.ReadMessage()
		if err != nil {
			t.Fatalf("长度 %d 档读取失败: %v", n, err)
		}
		if len(got) != n || string(got) != string(body) {
			t.Fatalf("长度 %d 档载荷不符（实读 %d）", n, len(got))
		}
	}
}

// TestStdWsDialLocksFragmentReassembly 分片重组：text(fin=0) + 两个 continuation，
// 末片 fin=1 才允许返回整条消息（半截帧绝不提前吐给业务层）。
func TestStdWsDialLocksFragmentReassembly(t *testing.T) {
	s := newStdwsTestServer(t, stdwsBrokenNone)
	tr := mustDial(t, s)
	s.push <- stdwsFrameFull(false, 0, 0x1, false, []byte("part-1|"))
	s.push <- stdwsFrameFull(false, 0, 0x0, false, []byte("part-2|"))
	s.push <- stdwsFrameFull(true, 0, 0x0, false, []byte("part-3"))
	got, err := tr.ReadMessage()
	if err != nil {
		t.Fatalf("分片读取失败: %v", err)
	}
	if string(got) != "part-1|part-2|part-3" {
		t.Fatalf("分片拼接结果不符: %q", got)
	}
}

// TestStdWsDialLocksServerCloseIsEOF 服务端 close 帧必须译成 io.EOF——
// BinanceWS 状态机靠这个哨兵区分"对端告别"和其他 IO 异常。
func TestStdWsDialLocksServerCloseIsEOF(t *testing.T) {
	s := newStdwsTestServer(t, stdwsBrokenNone)
	tr := mustDial(t, s)
	s.push <- stdwsFrame(0x8, []byte{0x03, 0xE8}) // 1000 正常关闭状态码
	if _, err := tr.ReadMessage(); !errors.Is(err, io.EOF) {
		t.Fatalf("close 帧应译成 io.EOF，实得: %v", err)
	}
}

// TestStdWsDialLocksPingAutoPong ping 帧在 ReadMessage 内部被消化：自动回 pong、
// 业务帧照常返回， pong 绝不冒泡成一条"消息"。
func TestStdWsDialLocksPingAutoPong(t *testing.T) {
	s := newStdwsTestServer(t, stdwsBrokenNone)
	tr := mustDial(t, s)
	s.push <- stdwsFrame(0x9, []byte("hb-42"))
	s.push <- stdwsFrame(0x1, []byte("after-ping"))
	got, err := tr.ReadMessage()
	if err != nil {
		t.Fatalf("ping 后读取失败: %v", err)
	}
	if string(got) != "after-ping" {
		t.Fatalf("ping 打断了两条消息: %q", got)
	}
	ctrl := s.waitForMsgs(t, true, 1)
	if ctrl[0].opcode != 0xA || string(ctrl[0].payload) != "hb-42" {
		t.Fatalf("pong 应为 ping 载荷回显，实得 opcode=%#x payload=%q", ctrl[0].opcode, ctrl[0].payload)
	}
	if !ctrl[0].masked {
		t.Fatal("pong 出站帧必须带掩码（§5.3 客户端纪律）")
	}
}

// TestStdWsDialLocksOutboundMasked 出站数据帧三锁：掩码位=1、载荷 XOR 还原后与原文一致、
// 每帧掩码 key 随机（两帧的 key 不同——固定 key 是客户端实现的经典偷懒错误）。
func TestStdWsDialLocksOutboundMasked(t *testing.T) {
	s := newStdwsTestServer(t, stdwsBrokenNone)
	tr := mustDial(t, s)
	first := []byte(`{"method":"SUBSCRIBE","id":1}`)
	if err := tr.WriteMessage(first); err != nil {
		t.Fatalf("WriteMessage 失败: %v", err)
	}
	second := make([]byte, 300) // 走 16 位长度档，顺带锁出站三档编码
	for i := range second {
		second[i] = byte(i)
	}
	if err := tr.WriteMessage(second); err != nil {
		t.Fatalf("第二帧 WriteMessage 失败: %v", err)
	}
	msgs := s.waitForMsgs(t, false, 2)
	if !msgs[0].masked || !msgs[1].masked {
		t.Fatal("客户端出站帧必须一律带掩码")
	}
	if string(msgs[0].payload) != string(first) {
		t.Fatalf("第一帧 XOR 还原失败: %q", msgs[0].payload)
	}
	if string(msgs[1].payload) != string(second) {
		t.Fatalf("16 位长度档载荷还原失败（len=%d）", len(msgs[1].payload))
	}
	// 掩码 key 两锁：非全零（真随机数而非占位）+ 帧间互不相同（禁止固定 key 复用）。
	zero := [4]byte{}
	if msgs[0].maskKey == zero || msgs[1].maskKey == zero {
		t.Fatalf("掩码 key 出现全零占位: %#v / %#v", msgs[0].maskKey, msgs[1].maskKey)
	}
	if msgs[0].maskKey == msgs[1].maskKey {
		t.Fatalf("两帧复用了同一个掩码 key %#v", msgs[0].maskKey)
	}
}

// TestStdWsDialLocksProtocolViolationsRejected 三种非法服务器帧都必须被读出即断：
// 带掩码（§5.1 禁）、RSV 非零（未协商扩展）、未知 opcode（不是行情协议该有的东西）。
func TestStdWsDialLocksProtocolViolationsRejected(t *testing.T) {
	cases := []struct {
		name   string
		frame  []byte
		wantSn string
	}{
		{"服务器帧带掩码", stdwsFrameFull(true, 0, 0x1, true, []byte("x")), "服务器帧带掩码"},
		{"RSV 位非零", stdwsFrameFull(true, 0x4, 0x1, false, []byte("x")), "保留位非零"},
		{"未知 opcode", stdwsFrame(0x3, []byte("x")), "未知 opcode"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStdwsTestServer(t, stdwsBrokenNone)
			tr := mustDial(t, s)
			s.push <- c.frame
			if _, err := tr.ReadMessage(); err == nil || !strings.Contains(err.Error(), c.wantSn) {
				t.Fatalf("应报 %q，实得 %v", c.wantSn, err)
			}
		})
	}
}

// TestStdWsDialLocksBadHandshakes 三种坏握手都必须让拨号阶段就失败，绝不返回半活 transport。
func TestStdWsDialLocksBadHandshakes(t *testing.T) {
	cases := []struct {
		name   string
		broken stdwsBroken
		wantSn string
	}{
		{"缺 Accept", stdwsBrokenNoAccept, "Sec-WebSocket-Accept 不符"},
		{"算错 Accept", stdwsBrokenWrongAccept, "Sec-WebSocket-Accept 不符"},
		{"不升级（200）", stdwsBrokenStatus200, "升级被拒 HTTP 200"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStdwsTestServer(t, c.broken)
			if _, err := StdWsDial(context.Background(), s.wsURL("/ws")); err == nil ||
				!strings.Contains(err.Error(), c.wantSn) {
				t.Fatalf("应报 %q，实得 %v", c.wantSn, err)
			}
		})
	}
}

// TestStdWsDialTLSSchemeRouting scheme 白名单：http/https/裸 host 一律拒拨；
// wss 允许通过 scheme 关（真 TLS 握手对明文端口必然失败——错误里必须是握手层而非"不支持"）。
func TestStdWsDialTLSSchemeRouting(t *testing.T) {
	for _, bad := range []string{"http://127.0.0.1:1/ws", "https://127.0.0.1:1/ws"} {
		if _, err := StdWsDial(context.Background(), bad); err == nil ||
			!strings.Contains(err.Error(), "不支持的 scheme") {
			t.Fatalf("scheme 白名单放行非法值 %q（实得 %v）", bad, err)
		}
	}
	// 无 scheme 的裸 host：url.Parse 阶段就崩（"first path segment…colon"），同样拒拨——
	// 锁的是"任何非 ws/wss 输入都拿不到 transport"，不锁具体错误文案。
	if _, err := StdWsDial(context.Background(), "127.0.0.1:1/ws"); err == nil {
		t.Fatal("无 scheme 裸 host 必须拒拨")
	}
	// wss 打到 httptest 的明文端口：过 scheme 关、TCP 也通，死在 TLS 握手——语义即预期。
	s := newStdwsTestServer(t, stdwsBrokenNone)
	wssURL := "wss://" + strings.TrimPrefix(s.srv.URL, "http://") + "/ws"
	if _, err := StdWsDial(context.Background(), wssURL); err == nil ||
		strings.Contains(err.Error(), "不支持的 scheme") {
		t.Fatalf("wss 应走 TLS 握手层报错而非 scheme 拒绝，实得 %v", err)
	}
}

// TestStdWsDialLocksCtxCancelReturnsFast 已取消的 ctx 必须让拨号立刻返回（不泄漏 goroutine
// 挂在 TCP/握手上）——BinanceWS 重连循环的 Stop 路径依赖这个性质。
func TestStdWsDialLocksCtxCancelReturnsFast(t *testing.T) {
	s := newStdwsTestServer(t, stdwsBrokenNone)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := StdWsDial(ctx, s.wsURL("/ws")); err == nil {
		t.Fatal("取消后的 ctx 拨号必须失败")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("ctx 取消未及时穿透（耗时 %v）", d)
	}
}

// TestStdWsDialLocksBinanceWSEndToEnd 接缝级证明：StdWsDial 注入 BinanceWS 后端到端出帧——
// 状态机连上假服务端、OnMessage 收到帧、MessageCount 增长、Stop 干净收摊。
func TestStdWsDialLocksBinanceWSEndToEnd(t *testing.T) {
	s := newStdwsTestServer(t, stdwsBrokenNone)
	var mu sync.Mutex
	var seen []string
	ws, err := NewBinanceWS(BinanceWSOptions{
		Name: "stdws-e2e",
		URL:  s.wsURL("/stream?streams=btcusdt@miniTicker"),
		Dial: StdWsDial,
		OnMessage: func(payload []byte) {
			mu.Lock()
			seen = append(seen, string(payload))
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("NewBinanceWS 失败: %v", err)
	}
	ws.Start()
	s.push <- stdwsFrame(0x1, []byte(`{"s":"BTCUSDT","c":"1"}`))
	s.push <- stdwsFrame(0x1, []byte(`{"s":"BTCUSDT","c":"2"}`))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && ws.MessageCount() < 2 {
		time.Sleep(20 * time.Millisecond)
	}
	ws.Stop()
	if ws.MessageCount() < 2 {
		t.Fatalf("端到端只收到 %d 帧（期望 ≥2）", ws.MessageCount())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) < 2 || seen[0] != `{"s":"BTCUSDT","c":"1"}` {
		t.Fatalf("OnMessage 顺序/内容不符: %#v", seen)
	}
}

// TestStdWsDialStdPingInsideFragmentedMessage §5.4 专项：控制帧**可以**插在分片消息中间，
// 客户端必须"存好分片现场 → 回 pong → 继续拼同一条消息"。做成分片之间而不是消息边界上，
// 是因为边界上插 ping 的用例（TestStdWsDialLocksPingAutoPong）掩盖不了"分片状态被控制帧清空"
// 这类实现错误——而币安大帧 + 服务端 keepalive ping 并发出现正是这条路径。
func TestStdWsDialStdPingInsideFragmentedMessage(t *testing.T) {
	s := newStdwsTestServer(t, stdwsBrokenNone)
	tr := mustDial(t, s)
	// 首片(fin=0) → ping → 中间片(fin=0) → 末片(fin=1)
	s.pushFrame(0x1, false, []byte("bi"))
	s.pushFrame(0x9, true, []byte("mid-frag"))
	s.pushFrame(0x0, false, []byte("nar"))
	s.pushFrame(0x0, true, []byte("y"))
	got, err := tr.ReadMessage()
	if err != nil {
		t.Fatalf("分片中间插控制帧后读取失败: %v", err)
	}
	if string(got) != "binary" {
		t.Fatalf("分片重组被控制帧污染: %q", got)
	}
	// pong 照样要发出去，且绝不能被当成一条"消息"冒泡给上层。
	ctrl := s.waitForMsgs(t, true, 1)
	if ctrl[0].opcode != 0xA || string(ctrl[0].payload) != "mid-frag" {
		t.Fatalf("分片中的 ping 未自动回 pong: opcode=%#x payload=%q", ctrl[0].opcode, ctrl[0].payload)
	}
	// 消息边界必须复位：下一条完整消息仍要正常读出（残留状态会把它拼脏）。
	s.push <- stdwsFrame(0x1, []byte("next-msg"))
	next, nerr := tr.ReadMessage()
	if nerr != nil {
		t.Fatalf("分片之后的下一帧读取失败: %v", nerr)
	}
	if string(next) != "next-msg" {
		t.Fatalf("下一帧被上一条消息污染: %q", next)
	}
}

// TestStdWsDialStdCloseUnblocksPendingRead 本地 Close 必须立刻叫醒阻塞中的 ReadMessage：
// BinanceWS 的 §6.9 静默看门狗就是"定时器到 → t.Close() → 读循环出错 → 重连"这条链，
// 打不断读就把兜底逻辑变成了永久假活。
func TestStdWsDialStdCloseUnblocksPendingRead(t *testing.T) {
	s := newStdwsTestServer(t, stdwsBrokenNone)
	tr := mustDial(t, s)
	readErr := make(chan error, 1)
	go func() {
		_, err := tr.ReadMessage() // 无帧可读，阻塞在 socket 上
		readErr <- err
	}()
	time.Sleep(50 * time.Millisecond) // 让读协程真正进入阻塞态，避免"读完 EOF 才 Close"的假通过
	_ = tr.Close()
	select {
	case err := <-readErr:
		if err == nil {
			t.Fatal("Close 之后 ReadMessage 仍返回 nil，读协程会永久挂住")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("本地 Close 未能打断阻塞中的 ReadMessage")
	}
}

// TestStdWsDialLocksOrphanContinuationRejected 无进行中消息时直接来 continuation 帧
// （对面撕帧/丢帧后的残局）必须判协议错——静默接受会吐出一条凭空拼出来的"消息"。
func TestStdWsDialLocksOrphanContinuationRejected(t *testing.T) {
	s := newStdwsTestServer(t, stdwsBrokenNone)
	tr := mustDial(t, s)
	s.push <- stdwsFrameFull(true, 0, 0x0, false, []byte("orphan"))
	if _, err := tr.ReadMessage(); err == nil || !strings.Contains(err.Error(), "孤儿 continuation") {
		t.Fatalf("孤儿续帧应被拒绝，实得 %v", err)
	}
}

// TestStdWsDialLocksControlFrameRules 控制帧两规：禁分片（FIN=0 即违规）、载荷 ≤125
// （§5.5 硬数——用 126 标记伪装扩展长度的帧必须被拒）。
func TestStdWsDialLocksControlFrameRules(t *testing.T) {
	big := make([]byte, 200)
	cases := []struct {
		name   string
		frame  []byte
		wantSn string
	}{
		{"ping 分片", stdwsFrameFull(false, 0, 0x9, false, []byte("x")), "控制帧违规"},
		{"pong 超长载荷", stdwsFrame(0xA, big), "控制帧违规"},
		{"保留 opcode 0xC", stdwsFrame(0xC, []byte("x")), "保留 opcode"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStdwsTestServer(t, stdwsBrokenNone)
			tr := mustDial(t, s)
			s.push <- c.frame
			if _, err := tr.ReadMessage(); err == nil || !strings.Contains(err.Error(), c.wantSn) {
				t.Fatalf("应报 %q，实得 %v", c.wantSn, err)
			}
		})
	}
}

// TestStdWsDialLocksCloseIdempotent BinanceWS 状态机会对同一 transport 调两次 Close
// （defer 一路 + 静默看门狗一路）：第二次必须静默成功，不得重发 close 帧、不得吐
// "use of closed"——否则断线日志会被假错误刷屏，真故障淹没在里面。
func TestStdWsDialLocksCloseIdempotent(t *testing.T) {
	s := newStdwsTestServer(t, stdwsBrokenNone)
	tr, err := StdWsDial(context.Background(), s.wsURL("/ws"))
	if err != nil {
		t.Fatalf("拨号失败: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("首次 Close 失败: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("二次 Close 应静默成功: %v", err)
	}
	// 假服务端只应观察到恰好一条客户端 close 帧（重发=状态机误判"对面异常"的源头）。
	s.waitForMsgs(t, true, 1)
	time.Sleep(100 * time.Millisecond) // 再等一拍：若有"第二条 close"在路上，这里也能收到
	_, ctrl := s.snapshot()
	n := 0
	for _, m := range ctrl {
		if m.opcode == 0x8 {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("客户端 close 帧应恰好 1 条，实得 %d（二次 Close 重发了）", n)
	}
}
