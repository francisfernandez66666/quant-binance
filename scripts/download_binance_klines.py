#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# 加密历史 K 线下载器（§BINANCE-P4，PLAN Phase 4 第 2 条）：
# 从币安官方公共归档（data.binance.vision 的公开镜像域 data-api.binance.vision）拉取
# SPOT klines 的 daily/monthly zip 归档，解包 CSV 后统一落研究库 `daily` 表
# （ts_code+trade_date 主键，与 A 股日线同表同形状——回测引擎零改动复用）。
#
# 数据契约与坑位（PLAN §2.8/§15.7，2026-09-23 URL 实探修正——三处形态错会静默零入库）：
#   · 归档文件树 **只存在于 data.binance.vision**；data-api.binance.vision 仅镜像 REST
#     （/data/* 全 404），故 BASE 缺省必须是前者（可 BINANCE_DATA_HOST 覆盖）；
#   · 文件名 = {SYMBOL}-{interval}-{后缀}.zip，interval 目录同为周期键（1d/4h/…）：
#       daily 树按【日】出档：BTCUSDT-1d-2026-09-20.zip（当日档 UTC 次日才发布→止于昨日）
#       monthly 树按【月】出档：BTCUSDT-1d-2026-08.zip（整月日线，当月档次月才出）
#     旧版误用 "daily+月后缀 / monthly+年后缀"两种不存在的形态 → 恒 404 全静默；
#   · 现货 kline 时间戳自 2025-01-01 起为【微秒】（历史拼接混单位）——本脚本按位数
#     自适应归一到毫秒再截日期（≥1e14 判微秒，≥1e11 判毫秒，否则秒×1000）；
#   · 每个 zip 伴随 `.CHECKSUM`（SHA-256）文件，下载后先校验再解包，坏包丢弃不入库；
#   · 幂等：INSERT OR REPLACE (ts_code, trade_date)，可重复跑增量补齐；
#   · 防假绿收口：0 个归档命中即 stderr 报错 + exit 1（全部 404 只可能是配置错，
#     绝不允许"完成：共 0 根"顶着一张绿脸收工）；非 1d 周期入库按"当日最后一根"覆盖，
#     不做 OHLC 聚合（回测日线口径请配合 --interval 1d 使用）。
#
# 用法：python3 scripts/download_binance_klines.py <trading.db> <SYMBOL...>
#         [--interval 1d] [--period daily|monthly] [--months N] [--years N] [--out-dir DIR]
#     例：python3 scripts/download_binance_klines.py ~/.quant-trading-v2/trading.db BTCUSDT ETHUSDT --months 6
#   daily 模式逐日文件拉最近 N 月（回溯细、请求多）；monthly 模式按 --years×12 个月
#   逐月归档一次到位（每文件含整月日线）。
import argparse
import csv
import datetime as _dt
import hashlib
import io
import os
import sqlite3
import sys
import urllib.error
import urllib.request
import zipfile

BASE = os.environ.get("BINANCE_DATA_HOST", "data.binance.vision")
UA = {"User-Agent": "quant-binance-kline-downloader/1.0"}


def fetch(url):
    """下载一段字节；404（缺月/缺 symbol）返回 None 由调用方静默跳过。"""
    req = urllib.request.Request(url, headers=UA)
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return r.read()
    except urllib.error.HTTPError as e:
        if e.code == 404:
            return None
        raise


def verify_checksum(name, blob, checksum_txt):
    """官方 .CHECKSUM 为 '<sha256>  <filename>' 格式：逐字节比对，不一致拒收。"""
    if not checksum_txt:
        return True  # 早期归档偶缺 checksum 文件：放行但由调用处打印告警
    want = checksum_txt.strip().split()[0].lower()
    got = hashlib.sha256(blob).hexdigest()
    if want != got:
        print(f"  !! {name} SHA256 不符 want={want[:12]}… got={got[:12]}…，丢弃")
        return False
    return True


def norm_open_time(ts):
    """K 线 open_time 单位归一 → 毫秒。2025 起现货为微秒（位数自适应，PLAN §2.8）。"""
    n = int(ts)
    if n >= 10**14:   # 微秒（µs 现在 ~1.7e15）
        return n // 1000
    if n >= 10**11:   # 毫秒（常态）
        return n
    return n * 1000   # 秒（aggTrades/老周期偶见）


def month_list(months):
    """最近 N 个自然月（含当月，降序拼接由调用处处理）→ [(YYYY, MM), ...] 升序。"""
    from datetime import date
    y, m = date.today().year, date.today().month
    out = []
    for _ in range(months):
        out.append((y, m))
        m -= 1
        if m == 0:
            y, m = y - 1, 12
    return sorted(out)


def parse_kline_csv(text):
    """归档 CSV → [(trade_date, o,h,l,c,vol,amount)]；自动跳过表头行与 open/close 时间列。"""
    rows = []
    for rec in csv.reader(io.StringIO(text)):
        if not rec or len(rec) < 8:
            continue
        try:
            ts = norm_open_time(rec[0])
            o, h, l, c = float(rec[1]), float(rec[2]), float(rec[3]), float(rec[4])
            vol, amount = float(rec[5]), float(rec[7])
        except (ValueError, IndexError):
            continue  # 表头行（open_time,...）与非数值行
        # UTC 毫秒 → YYYYMMDD（与 A 股 daily.trade_date 同为字符串日键；加密 7×24 无交易日历）
        day = _dt.datetime.fromtimestamp(ts / 1000, _dt.timezone.utc).strftime("%Y%m%d")
        rows.append((day, o, h, l, c, vol, amount))
    return rows


def upsert(conn, symbol, rows):
    """落 `daily` 表：pre_close/change/pct_chg 由表内前收回填（同 A 股装载脚本口径）。"""
    conn.executemany(
        "INSERT OR REPLACE INTO daily (ts_code, trade_date, open, high, low, close, pre_close, change, pct_chg, vol, amount)"
        " VALUES (?,?,?,?,?,?,?,0,0,?,?)",
        [(symbol, r[0], r[1], r[2], r[3], r[4], r[4], r[5], r[6]) for r in rows],
    )
    # 前收/涨跌用已入库序列回填（升序窗口内自我修正，幂等）
    conn.execute(
        "UPDATE daily SET pre_close = ("
        "  SELECT d2.close FROM daily d2 WHERE d2.ts_code = daily.ts_code AND d2.trade_date < daily.trade_date"
        "  ORDER BY d2.trade_date DESC LIMIT 1) WHERE ts_code = ?",
        (symbol,),
    )
    conn.execute(
        "UPDATE daily SET change = close - pre_close, pct_chg = (close - pre_close) * 100.0 / pre_close"
        " WHERE ts_code = ? AND pre_close IS NOT NULL AND pre_close > 0",
        (symbol,),
    )


def main():
    ap = argparse.ArgumentParser(description="币安现货历史K线归档下载入库（data.binance.vision 归档树）")
    ap.add_argument("db", nargs="?", default=os.path.expanduser("~/.quant-trading-v2/trading.db"))
    ap.add_argument("symbols", nargs="+", help="币对，如 BTCUSDT ETHUSDT")
    ap.add_argument("--interval", default="1d", help="K线周期（1d/4h/1h/…；回测日线用 1d）")
    ap.add_argument("--period", default="daily", choices=["daily", "monthly"])
    ap.add_argument("--months", type=int, default=6, help="daily 归档回溯月数（默认 6）")
    ap.add_argument("--years", type=int, default=2, help="monthly 归档回溯年数（默认 2）")
    ap.add_argument("--out-dir", default="", help="可选：归档 zip 落地目录（审计留痕）")
    args = ap.parse_args()

    if not os.path.exists(os.path.dirname(os.path.abspath(args.db))):
        print(f"研究库目录不存在: {args.db}", file=sys.stderr)
        sys.exit(2)
    conn = sqlite3.connect(args.db)
    conn.execute("PRAGMA journal_mode=WAL")

    from datetime import date, timedelta
    files = []  # (period_dir, suffix) 待下载清单（suffix 即文件名日期段）
    yesterday = date.today() - timedelta(days=1)  # 当日档 UTC 次日才发布，多拉只会吃 404
    if args.period == "daily":
        # daily 树按【日】出档：逐日枚举（跳过非法日期与晚于昨日的）
        for (y, m) in month_list(args.months):
            for d in range(1, 32):
                try:
                    day = date(y, m, d)
                except ValueError:
                    continue
                if day > yesterday:
                    break
                files.append(("daily", day.strftime("%Y-%m-%d")))
    else:
        # monthly 树按【月】出档（整月日线在一个文件里）；回溯深度用 --years（换算成月数）
        for (y, m) in month_list(args.years * 12):
            files.append(("monthly", f"{y}-{m:02d}"))

    total = hits = misses = 0
    for sym in args.symbols:
        sym = sym.upper()
        for period, suffix in files:
            stem = f"{sym}-{args.interval}-{suffix}"
            # 官方归档路径（实探钉死）：data/spot/{daily|monthly}/klines/{SYMBOL}/{interval}/{SYMBOL}-{interval}-{日期段}.zip
            base_url = f"https://{BASE}/data/spot/{period}/klines/{sym}/{args.interval}/{stem}"
            blob = fetch(base_url + ".zip")
            if blob is None:
                misses += 1
                continue  # 未出档/缺月：计数放行，全 404 由末尾假绿闸兜底
            hits += 1
            ck = fetch(base_url + ".CHECKSUM")
            ck_txt = ck.decode() if ck else None
            if not verify_checksum(stem + ".zip", blob, ck_txt):
                continue
            if not ck_txt:
                print(f"  .. {stem}.zip 无 CHECKSUM，放行")
            if args.out_dir:
                os.makedirs(args.out_dir, exist_ok=True)
                with open(os.path.join(args.out_dir, stem + ".zip"), "wb") as f:
                    f.write(blob)
            with zipfile.ZipFile(io.BytesIO(blob)) as z:
                name = z.namelist()[0]
                text = z.read(name).decode("utf-8", "replace")
            rows = parse_kline_csv(text)
            if rows:
                upsert(conn, sym, rows)
                total += len(rows)
                print(f"{sym} {stem}: {len(rows)} 根入库")
    conn.commit()
    conn.close()
    print(f"完成：共 {total} 根 K 线入库（归档命中 {hits}/缺档 {misses}）→ {args.db}")
    # 假绿闸：0 命中只可能是主机/周期/币对配置错（比如把归档树指到只镜像 REST 的
    # data-api 域），绝不能"共 0 根"顶绿脸收工——nightly/人工链路都靠退出码判成败。
    if hits == 0:
        print(f"!! 归档 0 命中（候选 {hits + misses} 个文件全 404）：检查 BINANCE_DATA_HOST={BASE}"
              f"、--interval {args.interval}、--period {args.period} 与币对拼写", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
