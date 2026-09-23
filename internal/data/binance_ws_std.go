// 文件职责：纯标准库 RFC6455 WebSocket 客户端拨号器（补齐 binance_ws.go 的 WsDialFunc 接缝）。
// 本仓库 go.mod 零第三方 websocket 依赖（PLAN §2.9 依赖面纪律），而 BinanceWS 状态机只认
// WsTransport 三面（ReadMessage/WriteMessage/Close），因此这里手写最小客户端：
// 握手（HTTP Upgrade + Sec-WebSocket-Key/accept 校验）、读帧（分片续帧 + ping/pong/close
// 控制帧消化 + 服务器帧禁掩码）、写帧（客户端帧必须掩码，RFC6455 §5.3）。
// 三条纪律：① ctx 全程穿透，取消/超时不泄漏连接；② 巨帧上限 stdWsMaxFrameBytes 防打爆
// 内存；③ 只接 ws/wss scheme，其余拒拨。
package data

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// stdWsDialTimeout 拨号阶段（TCP+TLS+HTTP upgrade）总超时；ctx 自带 deadline 时以 ctx 为准。
const stdWsDialTimeout = 10 * time.Second

// stdWsReadTimeout 单帧读取上限：超时即对端不再说话，返回 error 让 BinanceWS 走重连。
const stdWsReadTimeout = 200 * time.Second

// stdWsMaxFrameBytes 单消息重组上限（8 MiB）：行情/回报帧都是 KB 级，巨帧只可能是异常
// 或恶意数据，越界立刻断线重连，绝不无限缓冲。
const stdWsMaxFrameBytes = 8 << 20

// wsGUID RFC6455 §1.3 握手魔法串（协议固定值，不是密钥）。
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// StdWsDial 生产 WsDialFunc：解析 wss/ws URL，TCP(+TLS) 后做 HTTP Upgrade，返回 WsTransport。
// 装配层用法：BinanceWSOptions.Dial = data.StdWsDial（registry 唯一真拨号入口）。
func StdWsDial(ctx context.Context, rawURL string) (WsTransport, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("ws dial: URL 解析失败: %w", err)
	}
	var host, port string
	secure := false
	switch u.Scheme {
	case "wss":
		secure = true
		port = "443"
	case "ws":
		port = "80"
	default:
		return nil, fmt.Errorf("ws dial: 不支持的 scheme %q（只接 ws/wss）", u.Scheme)
	}
	host = u.Host
	if h, p, splitErr := net.SplitHostPort(u.Host); splitErr == nil {
		host, port = h, p // URL 显式带端口（httptest 场景）以它为准
	}
	// ctx 已有 deadline 就用它，否则补 stdWsDialTimeout（拨号三阶段共享同一预算）。
	dialCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, stdWsDialTimeout)
		defer cancel()
	}
	d := net.Dialer{}
	conn, err := d.DialContext(dialCtx, "tcp", host+":"+port)
	if err != nil {
		return nil, fmt.Errorf("ws dial: TCP 失败 %s: %w", host, err)
	}
	if secure {
		tconn := tls.Client(conn, &tls.Config{ServerName: u.Hostname()})
		// TLS 握手同样吃 ctx：deadline 到了必须关 TCP，否则调用方挂死在握手上。
		if hErr := tconn.HandshakeContext(dialCtx); hErr != nil {
			conn.Close()
			return nil, fmt.Errorf("ws dial: TLS 握手失败 %s: %w", u.Hostname(), hErr)
		}
		conn = tconn
	}
	ws, err := wsHandshake(dialCtx, conn, u)
	if err != nil {
		conn.Close()
		return nil, err
	}
	// ctx 看门狗：BinanceWS 的 Stop/重连只 cancel ctx、不亲手摸 transport——没有这条，
	// 已判死的连接要等 90s 静默看门狗或 200s 读超时才释放 fd 与读协程。
	// 盯的是入参 ctx 而非 dialCtx（后者带 StdWsDial 自己的 defer cancel，返回即失效）。
	// dead 让连接自然死亡时本协程随之退出，不留悬哨。
	go func() {
		select {
		case <-ctx.Done():
			_ = ws.Close()
		case <-ws.dead:
		}
	}()
	return ws, nil
}

// wsHandshakeReq 拼升级请求文本（单独成函数便于单测逐头检查）。
// Origin 币安不校验，按浏览器惯例带上更稳。
func wsHandshakeReq(u *url.URL, key string) string {
	return fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nOrigin: https://%s\r\n\r\n",
		u.RequestURI(), u.Host, key, u.Host)
}

// wsHandshake 在已建立的 TCP/TLS 连接上完成 RFC6455 升级；成功返回可读写帧的 transport。
func wsHandshake(ctx context.Context, conn net.Conn, u *url.URL) (*stdWsConn, error) {
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil { // 系统熵源异常：无法生成合规密钥，直接拒拨
		return nil, fmt.Errorf("ws dial: 握手密钥生成失败: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	// 写握手也要能脱身：对端收下不回的半死 TCP 上，裸 Write 会挂穿 ctx deadline。
	type wr struct{ err error }
	wch := make(chan wr, 1)
	go func() {
		_ = conn.SetWriteDeadline(deadlineOf(ctx, stdWsDialTimeout))
		_, werr := conn.Write([]byte(wsHandshakeReq(u, key)))
		wch <- wr{werr}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-wch:
		if r.err != nil {
			return nil, fmt.Errorf("ws dial: 握手请求写失败: %w", r.err)
		}
	}
	br := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(deadlineOf(ctx, stdWsDialTimeout))
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet, URL: u})
	if err != nil {
		return nil, fmt.Errorf("ws dial: 握手响应读取失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("ws dial: 升级被拒 HTTP %d", resp.StatusCode)
	}
	// accept 校验：base64(SHA1(key+GUID)) 必须逐字节相等，不等说明对面根本不是 WS 服务。
	h := sha1.New()
	io.WriteString(h, key+wsGUID)
	want := base64.StdEncoding.EncodeToString(h.Sum(nil))
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != want {
		return nil, fmt.Errorf("ws dial: Sec-WebSocket-Accept 不符 got=%q want=%q", got, want)
	}
	// 扩展/子协议反查：客户端没请求过任何扩展，101 却带回 Sec-WebSocket-Extensions
	// （典型是中间代理硬塞 permessage-deflate）——之后每帧都是解压不了的乱码，宁可拒拨。
	if ext := resp.Header.Get("Sec-WebSocket-Extensions"); ext != "" {
		return nil, fmt.Errorf("ws dial: 服务端协商了未请求的扩展 %q，拒用连接", ext)
	}
	if sub := resp.Header.Get("Sec-WebSocket-Protocol"); sub != "" {
		return nil, fmt.Errorf("ws dial: 服务端协商了未请求的子协议 %q，拒用连接", sub)
	}
	// 握手期 deadline 清零：之后的读写按 stdWsReadTimeout 逐帧管理。
	_ = conn.SetReadDeadline(time.Time{})
	return &stdWsConn{conn: conn, br: br, dead: make(chan struct{})}, nil
}

// deadlineOf 取 ctx 的 deadline；无 deadline 时按 fallback 时长从现在起算。
func deadlineOf(ctx context.Context, fallback time.Duration) time.Time {
	if d, ok := ctx.Deadline(); ok {
		return d
	}
	return time.Now().Add(fallback)
}

// stdWsConn 已升级连接：br 持有缓冲区，除帧读写外不得另起路径消费 conn，
// 否则升级后残留字节会被撕成两半（bufio 独占纪律）。
// writeMu：出站帧原子性锁——自动 pong 在读协程里写、SUBSCRIBE 等业务写在其他协程里写，
// 无锁时两个 goroutine 的 head+payload 会在 TCP 流上互相穿插（半帧接半帧）。
// closeOnce：BinanceWS 状态机会对同一 transport 调两次 Close（defer 一路 + 静默看门狗
// 一路），第二次不得再发 close 帧、也不得把 "use of closed" 错误吐给调用方。
type stdWsConn struct {
	conn      net.Conn
	br        *bufio.Reader
	writeMu   sync.Mutex
	closeOnce sync.Once
	dead      chan struct{} // Close 落定信号：ctx 看门狗协程靠它随连接一起退役
}

// ReadMessage 读一条完整数据消息（自动续分片、消化控制帧）；任何协议/IO 错误都返回
// error，由 BinanceWS 状态机视作断线走重连。
func (c *stdWsConn) ReadMessage() ([]byte, error) {
	var msg []byte
	inMsg := false // "半截消息"唯一权威标志——不能拿 len(msg)>0 顶替（空首片会被吞）
	for {          // 外层：一条消息可能由 首帧+N 个续帧 组成
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case 0x0: // continuation：拼进当前消息
			if !inMsg {
				return nil, errors.New("ws: 孤儿 continuation 帧（无进行中消息）")
			}
			msg = append(msg, payload...)
			if fin {
				return msg, nil
			}
		case 0x1, 0x2: // text/binary：新消息起点
			if inMsg {
				return nil, errors.New("ws: 上一消息未收完就来了新数据帧")
			}
			msg = payload
			if fin {
				return msg, nil
			}
			inMsg = true
		case 0x9: // ping 转立即 pong 回显，继续读（业务帧不该被控制帧打断）
			if err := c.writeFrame(0xA, payload); err != nil {
				return nil, err
			}
		case 0xA: // pong：忽略
		case 0x8: // close：对端告别，透传 io.EOF 让上层重连
			return nil, io.EOF
		default:
			return nil, fmt.Errorf("ws: 未知 opcode 0x%x", opcode)
		}
	}
}

// readFrame 读一个 RFC6455 帧：保留位/掩码位校验 + 7/16/64 三档长度。
// 服务器到客户端帧禁掩码（§5.1），带了即协议违规；巨帧越界直接断线。
func (c *stdWsConn) readFrame() (fin bool, opcode byte, payload []byte, err error) {
	_ = c.conn.SetReadDeadline(time.Now().Add(stdWsReadTimeout))
	head := make([]byte, 2)
	if _, err = io.ReadFull(c.br, head); err != nil {
		return
	}
	fin = head[0]&0x80 != 0
	opcode = head[0] & 0x0F
	if head[0]&0x70 != 0 { // 保留位（RSV1-3）：未协商任何扩展，出现即协议错
		err = errors.New("ws: 保留位非零（未协商扩展）")
		return
	}
	if head[1]&0x80 != 0 {
		err = errors.New("ws: 服务器帧带掩码，协议违规")
		return
	}
	// 载荷长度三档编码（§5.2）：7 位直接值；126=后随 2 字节 16 位长度；
	// 127=后随 8 字节 64 位长度；10 字节以内不读扩展（币安状态帧远小于此）。
	n := uint64(head[1] & 0x7F)
	switch n {
	case 126:
		ext := make([]byte, 2)
		if _, err = io.ReadFull(c.br, ext); err != nil {
			return
		}
		n = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err = io.ReadFull(c.br, ext); err != nil {
			return
		}
		n = binary.BigEndian.Uint64(ext)
	}
	if n > stdWsMaxFrameBytes {
		err = fmt.Errorf("ws: 帧长 %d 超上限 %d，断线重连", n, stdWsMaxFrameBytes)
		return
	}
	// 控制帧三条硬校验（§5.5）：不得分片（FIN 必须为 1）、载荷 ≤125 字节（长度字段不许
	// 用扩展档伪装）、opcode 0xB-0xF 保留位禁用——任何一条不过都是对面没按协议说话。
	if opcode >= 0x8 {
		if !fin || n > 125 {
			err = fmt.Errorf("ws: 控制帧违规（opcode=%#x fin=%v len=%d）", opcode, fin, n)
			return
		}
		if opcode >= 0xB {
			err = fmt.Errorf("ws: 保留 opcode %#x 不得使用", opcode)
			return
		}
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return
	}
	return fin, opcode, payload, nil
}

// WriteMessage 发一条未分片的二进制数据帧（SUBSCRIBE 帧等业务写全走这里）。
func (c *stdWsConn) WriteMessage(payload []byte) error {
	return c.writeFrame(0x2, payload)
}

// writeFrame 编码并写出一帧：客户端帧必须带掩码（§5.3），掩码 key 每帧随机。
// 整帧在 writeMu 内完成——pong 与业务帧可能来自不同协程，锁保证 TCP 流上帧不互咬。
func (c *stdWsConn) writeFrame(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(stdWsDialTimeout))
	var head []byte
	l := len(payload)
	switch {
	case l < 126:
		head = []byte{0x80 | opcode, 0x80 | byte(l)}
	case l <= 0xFFFF:
		head = []byte{0x80 | opcode, 0x80 | 126, byte(l >> 8), byte(l)}
	default:
		head = make([]byte, 10)
		head[0] = 0x80 | opcode
		head[1] = 0x80 | 127
		binary.BigEndian.PutUint64(head[2:], uint64(l))
	}
	key := make([]byte, 4)
	if _, err := rand.Read(key); err != nil {
		return fmt.Errorf("ws: 掩码密钥生成失败: %w", err)
	}
	masked := make([]byte, l)
	for i := 0; i < l; i++ {
		masked[i] = payload[i] ^ key[i%4]
	}
	_, err := c.conn.Write(append(append(head, key...), masked...))
	return err
}

// Close 幂等告别：BinanceWS 状态机对同一 transport 至少调两次（defer 一路 + 静默看门狗
// 一路），第二次直接成功返回——重发 close 帧没意义，"use of closed" 也不该冒给调用方。
func (c *stdWsConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		defer close(c.dead)        // 先注册退役信号（panic 也不留哨）
		_ = c.writeFrame(0x8, nil) // 尽力而为告别（写失败也不纠结）
		err = c.conn.Close()       // 底层连接必须关死，不留半成品 socket
	})
	return err
}

// 编译期锁：StdWsDial 的返回值必须满足 WsTransport 三面（签名漂移当场红）。
var _ WsTransport = (*stdWsConn)(nil)
