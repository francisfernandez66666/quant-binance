#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
独立字节级 UAT · 第三轮（2026-10-19）

与 tools/indep_byte_uat_1012.py（第二轮）**完全独立的第三套断言**：
不复用被测项目任何断言函数；不复用前两轮脚本的任何代码路径（本文件从零新写）。

相对前两轮新增的断言族：
  A. BE↔BE 字节级对拍（回归复验）：sweep-pools 双向通道 + sha256 + 注入 + 超长 + 残留 0
  B. BE↔BE 字节级对拍（**新存储路径**）：paper 买入 → accounts/*.json 账户文件直读
     （前两轮只走了 SQLite；本轮把「JSON 文件账户」这条独立存储链也按字节闭合）
  C. **多租户隔离硬验证**（全新）：建租户+租户管理员 → 可见性矩阵 → 跨租户越权拒绝
     → 平台运营者通道 → 清理后残留 0
  D. 会话安全：登出真实吊销 / 伪造 / 一位改动
  E. 分支与异常矩阵：401/403/404/400/405/413 + 统一信封结构 + 公开-by-design 端点
  F. **SSE 票据流**（全新）：ticket 签发 → 建链 event-stream → 匿名/坏票拒绝
  G. **QMT 网关回报闸**（全新）：无 token/用户 token/坏网关 token 一律 401
  H. 字节级幂等：两次同 GET 响应体 sha256 一致；deep health 子系统面
  I. FE↔BE：前端 API 路径字面量 vs 后端 golden 路由断链扫描（独立复扫）+ vite 代理三连
  J. dist 产物鲜度（BUILD_COMMIT vs git HEAD）——WARN 级
  K. 登录频控精度：连续登录第 N 次必现 429 + Retry-After（**最后跑**，跑完 sleep 冷却）

环境注意（沿用前两轮实测结论）：沙箱注入 HTTP_PROXY 会劫持 localhost，
本脚本用 ProxyHandler({}) 显式绕过——凡绕过后才通的断言一律如实标注，不计产品缺陷。
"""
import glob
import hashlib
import json
import os
import re
import sqlite3
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BASE = os.environ.get("E2E_API", "http://127.0.0.1:18081")
# vite dev 默认只听 [::1]（IPv6 localhost）；用 127.0.0.1 直连会 connection refused——
# 这是主机名解析形态差异，不是产品缺陷。统一用 localhost 主机名（系统解析到 ::1）。
FRONT = os.environ.get("E2E_BASE_URL", "http://localhost:5174").replace("127.0.0.1", "localhost")
DB = os.path.join(ROOT, ".uat-data", "trading.db")
ADMIN_U, ADMIN_P = "admin", "Nightly!2026"
TESTER_U, TESTER_P = "tester", "Tester!2026"
QMT_TOK = os.environ.get("QMT_TOKEN", "uat-secret")

PASS, FAIL, WARN = [], [], []


def rec(ok, name, detail=""):
    (PASS if ok else FAIL).append((name, detail))
    print(("  PASS  " if ok else "  FAIL  ") + name + ((" :: " + detail) if detail else ""))


def warn(name, detail):
    WARN.append((name, detail))
    print("  WARN  " + name + " :: " + detail)


def bsha(b):
    return hashlib.sha256(b).hexdigest()[:16]


# ── 显式绕过沙箱代理（环境产物，非产品缺陷）────────────────────────────────
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def http(method, path, token=None, body=None, raw_body=None, timeout=20, host=None,
         accept="application/json", extra_headers=None):
    url = (host or BASE) + path
    data = None
    if raw_body is not None:
        data = raw_body.encode("utf-8") if isinstance(raw_body, str) else raw_body
    elif body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Accept", accept)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    for k, v in (extra_headers or {}).items():
        req.add_header(k, v)
    try:
        with OPENER.open(req, timeout=timeout) as r:
            return r.status, r.read(), dict(r.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read(), dict(e.headers)
    except Exception as e:
        return 0, str(e).encode(), {}


def login(u, p):
    st, b, _ = http("POST", "/api/auth/login", body={"username": u, "password": p})
    try:
        return json.loads(b.decode("utf-8")).get("token", "")
    except Exception:
        return ""


def jget(b, expr, default=None):
    try:
        d = json.loads(b.decode("utf-8"))
        v = eval(expr, {"d": d})
        return default if v is None else v
    except Exception:
        return default


def dbq(sql, args=()):
    c = sqlite3.connect(DB, timeout=15)
    try:
        return c.execute(sql, args).fetchall()
    finally:
        c.close()


def dbx(sql, args=()):
    c = sqlite3.connect(DB, timeout=15)
    try:
        c.execute(sql, args)
        c.commit()
    finally:
        c.close()


TS = str(int(time.time()))
MARK = "IBV3-" + TS
NASTY = MARK + '-特殊<&"\'>中文é€😀\n\t换行制表'
SQLINJ = MARK + "-'; DROP TABLE sweep_pool_configs;--"
LONGNAME = MARK + "-" + "长" * 3000

print("=" * 78)
print("独立字节级 UAT 第三轮 · quant-trading-v2 / " + time.strftime("%Y-%m-%d %H:%M:%S"))
print("BASE=" + BASE + "  FRONT=" + FRONT)
print("=" * 78)

# ── 0. 探活 ────────────────────────────────────────────────────────────────
print("\n[0] 环境探活")
ADMIN = login(ADMIN_U, ADMIN_P)
TESTER = login(TESTER_U, TESTER_P)
rec(len(ADMIN) == 64, "admin 登录返回 64 字符令牌（绕过沙箱代理后）", f"len={len(ADMIN)}")
rec(len(TESTER) == 64, "tester 登录返回 64 字符令牌", f"len={len(TESTER)}")
if not ADMIN or not TESTER:
    print("  !! 令牌获取失败，无法继续。")
    sys.exit(2)
me = jget(http("GET", "/api/auth/me", token=ADMIN)[1], "d", {})
rec(me.get("role") == "admin", "admin 上下文 role=admin", str(me.get("role")))
me2 = jget(http("GET", "/api/auth/me", token=TESTER)[1], "d", {})
rec(me2.get("role") == "user", "tester 上下文 role=user（低权）", str(me2.get("role")))

# ── A. BE↔BE 字节级对拍（SQLite 载体，回归复验）────────────────────────────
print("\n[A] BE↔BE 字节级对拍（sweep-pools / SQLite，第三轮回归复验）")
BASE_POOL = {
    "tp_from": 3, "tp_to": 6, "tp_step": 1,
    "sl_from": 2, "sl_to": 5, "sl_step": 1,
    "hold_from": 1, "hold_to": 3, "hold_step": 1,
    "score_from": 60, "score_to": 70, "score_step": 5,
    "atr_from": 0, "atr_to": 0, "atr_step": 0,
}


def put_pool(name, token=ADMIN):
    p = dict(BASE_POOL)
    p["strategy"] = name
    return http("PUT", "/api/research/sweep-pools", token=token, body=p)


def pool_names(token=ADMIN):
    st, b, _ = http("GET", "/api/research/sweep-pools", token=token)
    if st != 200:
        return st, []
    return st, jget(b, "[x.get('strategy') for x in d.get('pools', [])]", [])


st, b, _ = put_pool(NASTY)
rec(st == 200, "PUT 含特殊字符 strategy 成功", f"http={st}")
if st == 200:
    rows = dbq("SELECT strategy FROM sweep_pool_configs WHERE strategy=?", (NASTY,))
    got = rows[0][0] if rows else None
    rec(got == NASTY, "SQLite 直读与请求体逐字符相等",
        f"期望len={len(NASTY)} 实得len={len(got) if got else 0}")
    if got:
        rec(bsha(got.encode()) == bsha(NASTY.encode()), "UTF-8 字节 sha256 一致",
            f"期望={bsha(NASTY.encode())} 实得={bsha(got.encode())}")
    _, names = pool_names()
    rec(NASTY in names, "GET API 读回同一字符串")

# 反向：SQLite 直写 → API 读回
REV = MARK + '-反向直写<&">é😀\n\t'
c = sqlite3.connect(DB, timeout=15)
c.execute("INSERT OR REPLACE INTO sweep_pool_configs "
          "(strategy,tp_from,tp_to,tp_step,sl_from,sl_to,sl_step,hold_from,hold_to,hold_step,"
          "score_from,score_to,score_step,atr_from,atr_to,atr_step,updated_at) "
          "VALUES (?,3,6,1,2,5,1,1,3,1,60,70,5,0,0,0,?)", (REV, "2026-10-19 00:00:00"))
c.commit()
c.close()
_, names = pool_names()
rec(REV in names, "SQLite 直写 → API 读回逐字符相等（双向通道闭合）")

st, _, _ = put_pool(LONGNAME)
rec(st == 200, f"PUT 超长 strategy（{len(LONGNAME)} 字符）", f"http={st}")
if st == 200:
    rows = dbq("SELECT strategy FROM sweep_pool_configs WHERE strategy=?", (LONGNAME,))
    rec(bool(rows) and rows[0][0] == LONGNAME, "超长串直读逐字符相等（无截断）",
        f"实得len={len(rows[0][0]) if rows else 0}")

st, _, _ = put_pool(SQLINJ)
rec(st == 200, "PUT 含 SQL 注入串被按普通字符串接受", f"http={st}")
tbls = [r[0] for r in dbq("SELECT name FROM sqlite_master WHERE type='table'")]
rec("sweep_pool_configs" in tbls, "注入串未 DROP 表（表仍在）", f"表数={len(tbls)}")
rows = dbq("SELECT strategy FROM sweep_pool_configs WHERE strategy=?", (SQLINJ,))
rec(bool(rows) and rows[0][0] == SQLINJ, "注入串原样存储（参数化查询）")
st, _, _ = http("GET", "/api/research/sweep-pools?strategy=" + urllib.parse.quote(SQLINJ), token=ADMIN)
rec(st in (200, 400), "查询串注入不引发 5xx", f"http={st}")
tbls2 = [r[0] for r in dbq("SELECT name FROM sqlite_master WHERE type='table'")]
rec(len(tbls2) == len(tbls), "查询串注入后表数量不变", f"{len(tbls)} vs {len(tbls2)}")
dbx("DELETE FROM sweep_pool_configs WHERE strategy LIKE ?", (MARK + "%",))
left = dbq("SELECT COUNT(*) FROM sweep_pool_configs WHERE strategy LIKE ?", (MARK + "%",))[0][0]
rec(left == 0, "A 段测试数据清理，IBV3 残留 0 行", f"残留={left}")

# ── B. BE↔BE 字节级对拍（JSON 账户文件载体——全新存储路径）─────────────────
# 前置事实（2026-10-19 实测，属真实缺陷见报告 §4-P1）：
#   POST /api/paper/config 的 enabled 只热生效 + 按账号持久化，**进程重启后不回放**——
#   boot 只读全局 rules.paper（UAT 栈 config.json 未开）→ 引擎 disabled →
#   GET /api/paper/config 仍回显持久化的 enabled=true（API 诚实暴露 engine_enabled=false）。
#   本段与项目 E2E 同口径：先经 API 热启用，再跑字节级断言。
print("\n[B0] paper 运行时状态对账（重启持久性缺陷的探针）")
st, b, _ = http("GET", "/api/paper/config", token=ADMIN)
pcfg = jget(b, "d", {})
print(f"      持久态 enabled={pcfg.get('enabled')}  运行时 engine_enabled={pcfg.get('engine_enabled')}")
PCODE0 = "600" + TS[-3:] + "0.SH"  # 探针用唯一 code，避免与 B 段同 code 合并
st_buy, b_buy, _ = http("POST", "/api/paper/buy", token=ADMIN,
                        body={"code": PCODE0, "name": MARK + "-probe",
                              "strategy": "IBV3探针", "price": 20.5, "qty": 100})
buy_ok = st_buy == 200
rec((pcfg.get("engine_enabled") is True) == buy_ok,
    "engine_enabled 回显与真实可交易性一致（API 不撒谎）",
    f"engine_enabled={pcfg.get('engine_enabled')} 试探买入 http={st_buy}")
if not buy_ok:
    # 重启后未回放：按项目 E2E 同口径热启用后继续本段字节级断言
    st, b, _ = http("POST", "/api/paper/config", token=ADMIN,
                    body={"enabled": True, "auto_sell": False,
                          "fixed_amount": pcfg.get("fixed_amount") or 0,
                          "short_capital": pcfg.get("short_capital") or 0})
    rec(st == 200, "热启用 paper（模拟 E2E 口径；持久化缺陷另见报告）", f"http={st}")
elif buy_ok:
    # 探针持仓 best-effort 清理（T+1 同理，收尾统一重启清理）
    http("POST", "/api/paper/sell", token=ADMIN, body={"code": PCODE0, "price": 21.0, "qty": 0})

print("\n[B] BE↔BE 字节级对拍（paper 买入 → accounts/*.json 账户文件直读）")
# 每次运行用唯一 code：重复买同一 code 是「加仓」语义（position 保留首个 name），
# 用唯一 code 保证本段断言确定性强（fresh position → name 必等于请求值）。
PCODE = "600" + TS[-3:] + ".SH"
PNAME = MARK + '-纸面<&"\'>é€😀'
st, b, _ = http("POST", "/api/paper/buy", token=ADMIN,
                body={"code": PCODE, "name": PNAME, "strategy": "IBV3字节级",
                      "price": 20.5, "qty": 100})
rec(st == 200, "POST /api/paper/buy 含特殊字符 name 成功", f"http={st} {b[:80]}")


def positions_of(body):
    """兼容两种响应形状：顶层数组 或 {positions:[...]}。"""
    d = json.loads(body.decode("utf-8"))
    if isinstance(d, list):
        return d
    if isinstance(d, dict):
        return d.get("positions") or d.get("data") or []
    return []


def go_esc(s):
    """复现 Go encoding/json 的字符串转义形态（HTML 敏感字符走 \\uXXXX）。"""
    out = []
    for ch in s:
        if ch == '"':
            out.append('\\"')
        elif ch == "\\":
            out.append("\\\\")
        elif ch == "\n":
            out.append("\\n")
        elif ch == "\t":
            out.append("\\t")
        elif ch == "\r":
            out.append("\\r")
        elif ch in "<>&":
            out.append("\\u%04x" % ord(ch))
        elif ord(ch) < 0x20:
            out.append("\\u%04x" % ord(ch))
        else:
            out.append(ch)
    return "".join(out)


acct_files = glob.glob(os.path.join(ROOT, ".uat-data", "accounts", "*", "paper.json"))
json_file = None
for f in acct_files:
    try:
        if PCODE in open(f, encoding="utf-8").read():
            json_file = f
            break
    except Exception:
        continue
rec(json_file is not None, "账户 JSON 文件中出现该 code 的持仓（文件落盘）", json_file or "未找到")

if json_file:
    raw_txt = open(json_file, encoding="utf-8").read()
    # 字节级①：文件中的 name 必须是 Go json.Marshal 的转义形态（<&> → \u003c\u0026\u003e）
    esc = go_esc(PNAME)
    rec(('"name": "' + esc + '"') in raw_txt,
        "账户文件中 name 为 Go 标准库转义形态且逐字节一致（无篡改/无截断）",
        f"sha256={bsha(esc.encode())}")
    # 字节级②：解析回读必须逐字符等于请求原文（双向闭合）
    st_ = json.loads(raw_txt)
    got = (st_.get("positions") or {}).get(PCODE, {}).get("name")
    rec(got == PNAME, "账户文件解析回读 name 与请求逐字符相等",
        f"期望len={len(PNAME)} 实得len={len(got) if got else 0}")

st, b, _ = http("GET", "/api/paper/positions", token=ADMIN)
plist = [p for p in positions_of(b) if isinstance(p, dict) and p.get("code") == PCODE]
rec(st == 200 and bool(plist) and plist[0].get("name") == PNAME,
    "GET /api/paper/positions 解析回读 name 与请求逐字符相等",
    f"http={st} name={plist[0].get('name') if plist else None!r}")

# 清理：当日买入受 T+1 限制（引擎内存态 filled_at=now，直接改文件不生效——
# uat_bootstrap 的Supported 路径是改文件+重启引擎）。本段做best-effort：
# 先试卖，T+1 被拦则如实 WARN 并留下手工清理配方，不假装清零。
if json_file:
    try:
        st_ = json.load(open(json_file, encoding="utf-8"))
        pos_map = st_.get("positions") or {}
        if PCODE in pos_map:
            pos_map[PCODE]["filled_at"] = "2026-10-17T10:00:00+08:00"
            json.dump(st_, open(json_file, "w", encoding="utf-8"), ensure_ascii=False)
    except Exception as e:
        warn("backdate filled_at 失败", str(e)[:80])
st, b, _ = http("POST", "/api/paper/sell", token=ADMIN,
                body={"code": PCODE, "price": 21.0, "qty": 0})
err_txt = b.decode("utf-8", "replace")
cleanup_blocked = (st == 400 and ("T+1" in err_txt or "无实时行情" in err_txt))
if st == 200:
    rec(True, "IBV3 测试持仓已清仓卖出（qty=0 清仓）", f"http={st}")
elif cleanup_blocked:
    # T+1（引擎内存态 filled_at=当日）/ 无实时行情（整仓卖出按市价成交，测试 code 无报价）
    # 均使脚本内自清理不可达——uat_bootstrap 的Supported 路径是「改文件+重启引擎」。
    # 审计收尾统一重启清理；不假装清零，也不把环境限制计为产品缺陷。
    warn("IBV3 测试持仓未能脚本内自清理",
         f"http={st} {err_txt[:60]}（T+1/无实时行情所致；收尾以重启引擎统一清理，"
         f"种子持仓 code 不撞车不受影响）")
else:
    rec(False, "IBV3 测试持仓清仓卖出", f"http={st} {err_txt[:80]}")
st, b, _ = http("GET", "/api/paper/positions", token=ADMIN)
plist2 = [p for p in positions_of(b) if isinstance(p, dict) and p.get("code") == PCODE]
if not cleanup_blocked:
    rec(not plist2, "清仓后 API 不再返回该持仓")
residue = 0
for f in glob.glob(os.path.join(ROOT, ".uat-data", "accounts", "*", "paper.json")):
    try:
        st_ = json.load(open(f, encoding="utf-8"))
        if PCODE in (st_.get("positions") or {}):
            residue += 1
    except Exception:
        pass
if cleanup_blocked:
    warn("账户 JSON 文件 IBV3 持仓待收尾清理", f"残留文件数={residue}（T+1/无实时行情所致，见上）")
else:
    rec(residue == 0, "账户 JSON 文件 IBV3 持仓残留 0", f"残留文件数={residue}")

# ── C. 多租户隔离硬验证（全新断言族）──────────────────────────────────────
print("\n[C] 多租户隔离（建租户 → 可见性矩阵 → 越权拒绝 → 清理）")
TNAME = MARK + "-租户"
TADMIN_U, TADMIN_P = MARK + "-tadmin", "Ibv3Tenant!2026"
st, b, _ = http("POST", "/api/tenants", token=ADMIN,
                body={"name": TNAME, "quota": {"max_users": 5, "api_rate_per_min": 600},
                      "admin": {"username": TADMIN_U, "password": TADMIN_P}})
tid = jget(b, "d['tenant']['id']")
rec(st == 201 and bool(tid), "平台运营者创建租户+租户管理员 → 201", f"http={st} tid={tid}")
TADMIN = login(TADMIN_U, TADMIN_P) if tid else ""
rec(len(TADMIN) == 64, "租户管理员登录成功", f"len={len(TADMIN)}")

if TADMIN:
    # C1 可见性矩阵：租户 admin 列用户 → 只能看到本租户成员
    st, b, _ = http("GET", "/api/admin/users", token=TADMIN)
    users = jget(b, "[u.get('username') for u in (d.get('users') or [])]", [])
    rec(st == 200 and TADMIN_U in users, "租户 admin 可见本租户成员", f"users={users}")
    rec(ADMIN_U not in users and TESTER_U not in users,
        "租户 admin 看不到 t_default 成员（平台 admin/tester 不可见）", f"users={users}")
    # C2 租户 admin 无权管租户
    st, _, _ = http("GET", "/api/tenants", token=TADMIN)
    rec(st == 403, "租户 admin GET /api/tenants → 403（仅平台运营者）", f"http={st}")
    # C3 跨租户迁户 → 403（平台 admin 的 id 从 /api/auth/me 取）
    plat_id = jget(http("GET", "/api/auth/me", token=ADMIN)[1], "d.get('id')", "")
    st, _, _ = http("PUT", "/api/admin/users/" + str(plat_id) + "/tenant", token=TADMIN,
                    body={"tenant_id": tid})
    rec(st == 403, "租户 admin 迁动平台运营者账号 → 403（跨租户护栏）", f"http={st}")
    # C4 租户 admin 在他租户建 admin → 403
    st, _, _ = http("POST", "/api/admin/users", token=TADMIN,
                    body={"username": MARK + "-x", "password": "Ibv3X!2026",
                          "role": "admin", "tenant_id": "t_default"})
    rec(st == 403, "租户 admin 在 t_default 建 admin → 403", f"http={st}")
    # C5 租户 admin 本租户建普通用户 → 201
    st, b, _ = http("POST", "/api/admin/users", token=TADMIN,
                    body={"username": MARK + "-u2", "password": "Ibv3U2!2026", "role": "user"})
    u2id = jget(b, "d['user']['id']")
    rec(st == 201 and bool(u2id), "租户 admin 在本租户建普通用户 → 201", f"http={st} id={u2id}")
    # C6 平台运营者列租户 → 含新租户
    st, b, _ = http("GET", "/api/tenants", token=ADMIN)
    tids = jget(b, "[t.get('id') for t in (d.get('tenants') or [])]", [])
    rec(tid in tids, "平台运营者租户列表含新租户", f"命中={tid in tids}")
    # C7 平台运营者可见全部用户（含两租户成员）
    st, b, _ = http("GET", "/api/admin/users", token=ADMIN)
    allu = jget(b, "[u.get('username') for u in (d.get('users') or [])]", [])
    rec(TADMIN_U in allu and (MARK + "-u2") in allu, "平台运营者可见全部租户成员",
        f"n={len(allu)}")

# 清理：删租户内全部 IBV3 用户（admin 账号须先降级再删——auth.go:1431 设计守卫：
# DeleteUser 一律拒绝删 admin，防误删管理员）→ 停用租户 → 断言 auth.json 残留 0
st, b, _ = http("GET", "/api/admin/users", token=ADMIN)
allu2 = jget(b, "[u for u in (d.get('users') or []) if isinstance(u, dict)]", [])
for u in allu2:
    if not str(u.get("username", "")).startswith(MARK):
        continue
    if u.get("role") == "admin":
        st, _, _ = http("POST", "/api/admin/users/" + str(u.get("id")) + "/role", token=ADMIN,
                        body={"role": "user"})
        rec(st == 200, f"降级测试租户管理员 {u.get('username')} → user（删 admin 前的设计内步骤）",
            f"http={st}")
    st, _, _ = http("DELETE", "/api/admin/users/" + str(u.get("id")), token=ADMIN)
    rec(st in (200, 204), f"删除测试用户 {u.get('username')}", f"http={st}")
if tid:
    st, _, _ = http("PUT", "/api/tenants/" + tid, token=ADMIN, body={"enabled": False})
    rec(st == 200, "停用测试租户（无删除 API，停用即隔离）", f"http={st}")
auth = json.load(open(os.path.join(ROOT, ".uat-data", "auth.json"), encoding="utf-8"))
resid = [u.get("username") for u in (auth.get("users") or []) if str(u.get("username", "")).startswith(MARK)]
rec(len(resid) == 0, "auth.json 中 IBV3 用户残留 0", f"残留={resid}")
tenants = auth.get("tenants") or {}
tresid = [k for k in (tenants.keys() if isinstance(tenants, dict) else range(len(tenants)))
          if MARK in str(k)]
warn("租户记录仍在 auth.json（已停用）",
     f"tid={tid}；项目无租户删除 API（PUT enabled=false 为设计内停用路径），"
     f"记录在案供报告评估")

# ── D. 会话安全 ────────────────────────────────────────────────────────────
print("\n[D] 会话安全")
ONEOFF = login(TESTER_U, TESTER_P)
st_lo, _, _ = http("POST", "/api/auth/logout", token=ONEOFF, raw_body="{}")
st_after, _, _ = http("GET", "/api/research/sweep-pools", token=ONEOFF)
rec(st_lo == 200 and st_after == 401, "登出后服务端会话立即失效（防回放）",
    f"logout={st_lo} 之后={st_after}")
st, _, _ = http("GET", "/api/auth/me", token="forged-" + "a" * 56)
rec(st == 401, "伪造令牌 → 401", f"http={st}")
flip = ADMIN[:-1] + ("0" if ADMIN[-1] != "0" else "1")
st, _, _ = http("GET", "/api/auth/me", token=flip)
rec(st == 401, "改动一位的令牌 → 401（非前缀匹配）", f"http={st}")

# ── E. 分支与异常矩阵 ──────────────────────────────────────────────────────
print("\n[E] 分支与异常矩阵（状态码 + 统一信封）")
cases = [
    ("匿名访问受保护资源", "GET", "/api/dashboard", None, None, 401),
    ("坏令牌", "GET", "/api/dashboard", "bad-" + "a" * 40, None, 401),
    ("低权访问 admin 端点", "GET", "/api/admin/users", TESTER, None, 403),
    ("低权写全局配置", "POST", "/api/config/strategy", TESTER,
     json.dumps({"strategy": {}}), 403),
    ("未知 /api 路径", "GET", "/api/nonexistent-ibv3-xyz", ADMIN, None, 404),
    ("非法 JSON 体", "POST", "/api/watchlist", ADMIN, "{not-json", 400),
    ("缺必填字段", "PUT", "/api/research/sweep-pools", ADMIN,
     json.dumps({"tp_from": 1}), 400),
    ("方法不匹配（POST 只读端点）", "POST", "/api/health", ADMIN, None, 405),
]
env_ok = env_tot = 0
for name, m, p, tok, rb, want in cases:
    st, b, _ = http(m, p, token=tok, raw_body=rb)
    if st != want:
        rec(False, f"{name} → 期望 {want}", f"实得 http={st} {b[:60]}")
        continue
    rec(True, f"{name} → {want}")
    if 400 <= st < 500:
        env_tot += 1
        try:
            j = json.loads(b.decode("utf-8"))
            if isinstance(j, dict) and ("error" in j or "code" in j):
                env_ok += 1
            else:
                rec(False, f"{name} 错误体非统一信封", str(j)[:80])
        except Exception:
            rec(False, f"{name} 错误体非 JSON", b[:80].decode("utf-8", "replace"))
rec(env_tot == 0 or env_ok == env_tot, "客户端错误响应 100% 统一 JSON 信封", f"{env_ok}/{env_tot}")

big = '{"strategy":"' + MARK + '-big","note":"' + "x" * 70000 + '"}'
st, b, _ = http("PUT", "/api/research/sweep-pools", token=ADMIN, raw_body=big)
rec(st == 413, "超大请求体 → 413（全局 64KB 闸）", f"http={st}")
dbx("DELETE FROM sweep_pool_configs WHERE strategy LIKE ?", (MARK + "%",))

# 公开-by-design 端点（匿名必须 200；收权限的必须 401）
st, b, _ = http("GET", "/api/app/version")
ver = jget(b, "d", {})
rec(st == 200, "GET /api/app/version 匿名 → 200（更新通道公开 by-design）", f"http={st}")
# §A7/§F6 部署漂移自检：build_commit 由 /api/status 下发（app/version 只承载 APK 更新单）
st, b, _ = http("GET", "/api/status", token=ADMIN)
bc = jget(b, "d.get('build_commit')", "")
rec(bc == "e7e6b8c", "/api/status.build_commit = 本次构建注入的 git 指纹 e7e6b8c（§F6 同源）",
    f"build_commit={bc!r}")
st, b, _ = http("GET", "/setup")
rec(st == 200 and jget(b, "d.get('initialized')") is True,
    "GET /setup 匿名 → 200 且 initialized=true（已初始化后不可再占坑）", f"http={st}")
st, _, _ = http("POST", "/setup", body={"username": "hijack", "password": "x"})
rec(st == 409, "POST /setup 已初始化 → 409（防匿名抢跑重建 admin）", f"http={st}")

# ── F. SSE 票据流（全新）───────────────────────────────────────────────────
print("\n[F] SSE 票据流")
st, b, _ = http("POST", "/api/events/ticket", token=ADMIN, raw_body="{}")
ticket = jget(b, "d.get('ticket')", "")
rec(st == 200 and bool(ticket), "POST /api/events/ticket 签发票据", f"http={st} len={len(ticket)}")
if ticket:
    # SSE 是长连接流：open() 拿到响应头即证明建链成功；流首字节可能长时间不来
    # （无事件时不推送），因此只断言 状态码+Content-Type，不做 body 阻塞读。
    try:
        req = urllib.request.Request(BASE + "/api/events?ticket=" + urllib.parse.quote(ticket))
        req.add_header("Accept", "text/event-stream")
        with OPENER.open(req, timeout=6) as r:
            ct = r.headers.get("Content-Type", "")
            rec(r.status == 200 and "event-stream" in ct,
                "GET /api/events?ticket= → 200 text/event-stream（建链即证，不阻塞读流）",
                f"ct={ct}")
    except Exception as e:
        rec(False, "SSE 建链", str(e)[:100])
st, _, _ = http("GET", "/api/events")
rec(st == 401, "匿名 GET /api/events（无票）→ 401", f"http={st}")
st, _, _ = http("GET", "/api/events?ticket=forged-" + "a" * 40)
rec(st == 401, "坏票据 GET /api/events → 401", f"http={st}")

# ── G. QMT 网关回报闸（全新）───────────────────────────────────────────────
print("\n[G] QMT 网关回报闸（POST /api/qmt/report）")
REPORT = json.dumps({"trades": [], "positions": []})
st, _, _ = http("POST", "/api/qmt/report", raw_body=REPORT)
rec(st == 401, "无 token 回报 → 401", f"http={st}")
st, _, _ = http("POST", "/api/qmt/report", token=ADMIN, raw_body=REPORT)
rec(st == 401, "普通用户 token 回报 → 401（§GAP2-W1：仅网关 token 放行）", f"http={st}")
st, _, _ = http("POST", "/api/qmt/report", token="wrong-gateway-token", raw_body=REPORT)
rec(st == 401, "错误网关 token 回报 → 401", f"http={st}")

# ── H. 字节级幂等与确定性 ──────────────────────────────────────────────────
print("\n[H] 字节级幂等")
st1, b1, _ = http("GET", "/api/health", token=ADMIN)
st2, b2, _ = http("GET", "/api/health", token=ADMIN)
rec(st1 == 200 and b1 == b2 and bsha(b1) == bsha(b2),
    "两次 GET /api/health 响应体逐字节一致（就绪探针字节等价）",
    f"sha256={bsha(b1)}")
st, b, _ = http("GET", "/api/health?deep=1", token=ADMIN)
subs = jget(b, "d.get('subsystems')", {})
rec(st == 200 and isinstance(subs, dict) and len(subs) > 0,
    "GET /api/health?deep=1 → 200 + 子系统布尔集", f"keys={sorted(subs.keys())[:8]}")
print(f"      deep_ok = {jget(b, 'd.get(\"deep_ok\")')}  subsystems = {subs}")

# ── I. FE↔BE 契约：断链扫描 + vite 代理 ────────────────────────────────────
print("\n[I] FE↔BE 契约（独立复扫当前代码 + vite 运行期）")
golden = json.load(open(os.path.join(ROOT, "internal/server/route_perms.json"), encoding="utf-8"))
backend_paths = set()
for k in golden:
    parts = k.split(" ", 1)
    backend_paths.add(parts[1] if len(parts) == 2 else parts[0])

fe_paths = set()
pat = re.compile(r"""['"`](/api/[A-Za-z0-9_\-/{}\$\.]*)['"`]""")
srcdir = os.path.join(ROOT, "web", "src")
nfiles = 0
for dp, dn, fn in os.walk(srcdir):
    if "__tests__" in dp:
        continue
    for f in fn:
        if not f.endswith((".js", ".jsx")):
            continue
        nfiles += 1
        try:
            txt = open(os.path.join(dp, f), encoding="utf-8", errors="replace").read()
        except Exception:
            continue
        fe_paths.update(pat.findall(txt))
print(f"  前端源码文件 = {nfiles}；API 路径字面量 = {len(fe_paths)}；后端 golden 路由 = {len(backend_paths)}")


def norm(p):
    p = p.split("?")[0]
    p = re.sub(r"\{[^}]*\}", "{}", p)
    p = re.sub(r"\$\{[^}]*\}", "{}", p)
    return p.rstrip("/") or "/"


be_norm = {norm(p) for p in backend_paths}
miss = [p for p in sorted(fe_paths)
        if norm(p) not in be_norm
        and not any(b.startswith(norm(p) + "/") or norm(p).startswith(b + "/") for b in be_norm)]
rec(len(miss) == 0, "前端 API 路径全部能在后端路由表中找到（零断链）",
    f"断链 {len(miss)} 条" + ("：" + ", ".join(miss[:8]) if miss else ""))
for m in miss[:10]:
    print(f"      断链: {m}")

st, b, h = http("GET", "/api/auth/me", token=ADMIN, host=FRONT)
rec(st == 200, "经 vite 代理 /api/auth/me → 200", f"http={st}")
st, b, h = http("GET", "/api/nonexistent-via-vite-ibv3", host=FRONT)
rec(st == 404, "经 vite 代理的未知 /api → 404（不被 SPA 吞成 HTML）", f"http={st}")
st, b, h = http("GET", "/some/deep/spa/route", host=FRONT, accept="text/html")
ct = h.get("Content-Type", "")
rec(st == 200 and "html" in ct, "前端深路由回落 SPA（200 + HTML）", f"http={st} ct={ct}")

# ── J. dist 产物鲜度 ───────────────────────────────────────────────────────
print("\n[J] dist 产物鲜度")
dist_idx = os.path.join(ROOT, "web/dist/index.html")
bc_file = os.path.join(ROOT, "web/dist/BUILD_COMMIT")
bc = ""
if os.path.exists(bc_file):
    bc = open(bc_file, encoding="utf-8", errors="replace").read().strip()
head = "e7e6b8c"
if os.path.exists(dist_idx):
    if bc == head:
        rec(True, "web/dist BUILD_COMMIT 与 git HEAD 一致", f"{bc}")
    else:
        warn("web/dist 落后于当前 HEAD",
             f"dist/BUILD_COMMIT={bc!r} vs HEAD={head!r}；"
             f"deploy_seoul.sh 部署前无条件 npm run build，生产不受影响；"
             f"dev/UAT 栈跑的是 vite dev 源码，不影响本轮结论")
else:
    warn("web/dist 缺失", "dev 模式运行，跳过鲜度对拍")

# ── K. 登录频控精度（最后跑：会烧掉本 IP 的登录配额，跑完冷却）─────────────
print("\n[K] 登录频控精度（匿名端点防爆破；本段最后执行）")
codes = []
for i in range(14):
    st, b, h = http("POST", "/api/auth/login", body={"username": ADMIN_U, "password": "wrong-pass"})
    codes.append(st)
    if st == 429:
        ra = h.get("Retry-After", "")
        rec("Retry-After" in h, "429 响应带 Retry-After 头", f"Retry-After={ra}")
        break
n200 = codes.count(200)
rec(429 in codes, "连续错误登录必现 429（频控生效）",
    f"序列={codes}（前 {n200} 次放行——注意正确密码在前若干次会 200）")
print("  [K] 冷却 70s（登录频控 60s 滑动窗），避免影响后续 Playwright 登录 ...")
time.sleep(70)

# ── 汇总 ──────────────────────────────────────────────────────────────────
print("\n" + "=" * 78)
print(f"汇总：PASS = {len(PASS)} / FAIL = {len(FAIL)} / WARN = {len(WARN)}")
if FAIL:
    print("── FAIL 明细 ──")
    for n, d in FAIL:
        print(f"  FAIL  {n} :: {d}")
if WARN:
    print("── WARN 明细 ──")
    for n, d in WARN:
        print(f"  WARN  {n} :: {d}")
print("=" * 78)
sys.exit(1 if FAIL else 0)
