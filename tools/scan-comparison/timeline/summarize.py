"""Derive disjoint worker totals and separate concurrent HTTP observations."""

import json
import os
from pathlib import Path
from statistics import median


def bounds(row):
    return row["start_ns"], row["end_ns"]


def overlap(first, second):
    return max(0, min(first[1], second[1]) - max(first[0], second[0]))


def inside(inner, outer):
    return outer[0] <= inner[0] <= inner[1] <= outer[1]


def disjoint(intervals):
    ordered = sorted(intervals)
    assert all(a[1] <= b[0] for a, b in zip(ordered, ordered[1:])), "overlapping worker partition"


def analyze(raw):
    version, mode = raw["version"], raw["mode"]
    calls = raw["client"]
    post, *polls = list(map(bounds, calls))
    start, stop = post[0], polls[-1][1]
    client = (start, stop)
    disjoint([post, *polls])
    assert calls[0]["method"] == "POST" and all(r["method"] == "GET" for r in calls[1:])
    assert calls[-1]["state"] == ("idle" if version == "installed" else "completed")
    fixture, = [r for r in raw["fixture"] if r["phase"] == "sparse"]
    assert fixture["physical_originals_verified"] == 2052 and fixture["catalog_rows"] == 2052
    ms = {
        "http": stop - start, "post": post[1] - post[0],
        "polls_in_flight": sum(b - a for a, b in polls),
    }
    ms["between_http"] = ms["http"] - ms["post"] - ms["polls_in_flight"]
    row = {"pair": raw["pair"], "mode": mode, "version": version, "gets": len(polls),
           "fixture_http_ms": fixture["http_ms"], "existing_backend_ms": fixture["backend_ms"],
           "write_bytes": fixture["write_bytes"], "peak_rss_kib": fixture["process_peak_rss_kib"]}
    if mode == "probe":
        events = raw["server"]["events"]
        def spans(name):
            return [bounds(e) for e in events if e["name"] == name]

        def only(name):
            item, = spans(name)
            return item

        def total(name, window=None):
            ranges = spans(name)
            disjoint(ranges)
            return sum(overlap(x, window) if window else x[1] - x[0] for x in ranges)

        enter = only("post.enter")[0]
        worker = only("worker.enter")[0]
        mark = only("terminal.sql" if version == "next" else "terminal.idle")[1]
        assert start <= enter <= worker <= mark <= stop, "cross-process clock or terminal observation mismatch"
        tail = (mark, stop)
        work = (worker, mark)
        ms.update({
            "post_to_worker": worker - start, "worker": mark - worker, "terminal_to_client": stop - mark,
            "tail_http": sum(overlap(request, tail) for request in [post, *polls]),
            "handler_to_worker": worker - enter, "post_handler": only("post.exit")[1] - enter,
        })
        ms["tail_between_http"] = ms["terminal_to_client"] - ms["tail_http"]
        assert ms["post_to_worker"] + ms["worker"] + ms["terminal_to_client"] == ms["http"]

        walk = only("walk")
        assert inside(walk, work)
        parts = ["signatures", "trusted_dir", "inspection", "publish", "reconcile", "terminal.sql"]
        stages = [span for name in parts for span in spans(name)]
        assert all(inside(span, work) for span in stages)
        disjoint(stages)
        for name in parts:
            ms[name] = total(name)
        ms["walk_other"] = walk[1] - walk[0] - sum(overlap(v, walk) for v in stages)
        ms["worker_other"] = ms["worker"] - ms["walk_other"] - sum(ms[name] for name in parts)
        assert ms["walk_other"] >= 0 and ms["worker_other"] >= 0

        for name in ("writer.wait", "asset.tx_commit", "snapshot.query", "inspection.hash", "inspection.probe",
                     "reconcile.parent_seek", "reconcile.page", "reconcile.list", "version_sql.zero", "version_sql.one"):
            ms[name] = total(name, work)
        if version == "next":
            moves, rewrites = spans("move.file_update"), spans("rewrite.file_update")
            zero, one = spans("version_sql.zero"), spans("version_sql.one")
            assert len(moves) == len(zero) == 8 and len(rewrites) == 16 and len(one) == 20
            assert all(sum(inside(v, move) for v in zero) == 1 for move in moves)
            assert all(sum(inside(v, rewrite) for v in one) == 1 for rewrite in rewrites)
            assert not spans("version_sql.unexpected") and len(spans("inspection")) == 28
        for request, client_call in zip(polls, calls[1:]):
            states = [e["name"] for e in events if e["name"].startswith("poll.state.") and inside(bounds(e), request)]
            assert states == ["poll.state." + client_call["state"]], "client and server poll states disagree"
        for name in ("poll.handler", "poll.list_libraries", "poll.count_media", "poll.read_job"):
            ms[name] = total(name, client)
        row["counts"] = {name: len(spans(name)) for name in
                         ("signatures", "trusted_dir", "inspection", "publish", "reconcile", "writer.wait",
                          "asset.tx_commit", "version_sql.zero", "version_sql.one", "reconcile.page", "reconcile.list")}
        row["calibration_per_empty_span_ns"] = raw["server"]["calibration_ns"] / raw["server"]["calibration_samples"]
        row["trace_events"] = len(events)
    row.update({name + "_ms": ns / 1e6 for name, ns in ms.items()})
    return row


def paired(deltas):
    center = median(deltas)
    mad = median(abs(value - center) for value in deltas)
    positive = sum(value > 0 for value in deltas)
    return {"deltas": deltas, "median": center, "mad": mad, "positive_of_five": positive,
            "passes_numerical_part": positive >= 4 and center > 10 and center > 2 * mad}


def report(rows, metadata, evidence):
    by = {(r["pair"], r["mode"], r["version"]): r for r in rows}
    pairs = range(1, 6)
    assert len(by) == 20
    stats = {}
    for mode in ("plain", "probe"):
        sample = by[1, mode, "next"]
        stats[mode] = {}
        for field in sample:
            if field.endswith("_ms"):
                stats[mode][field] = paired([by[i, mode, "next"][field] - by[i, mode, "installed"][field] for i in pairs])
    overhead = {version: paired([by[i, "probe", version]["http_ms"] - by[i, "plain", version]["http_ms"]
                                 for i in pairs]) for version in ("installed", "next")}
    (evidence / "paired.json").write_text(json.dumps({"next_minus_installed": stats, "probe_minus_plain": overhead,
                                                      "rows": rows}, indent=2) + "\n")
    lines = [
        "## 少量改动复扫的单调时钟测量", "",
        f"安装版 {metadata['installed_sha']}，Next {metadata['next_sha']}，探针来源 {metadata['experiment_sha']}。",
        f"同一 Linux 运行器，{metadata['cpus']} vCPU，{metadata['go']}，{metadata['python']} Python。",
        "每种组合五个新进程和数据库，依次运行首扫、原样复扫、少量变化。每轮预热文件页缓存并核验物理原件及身份。",
        "下表 HTTP 从 Python 调用提交到终态 GET 解析完成，单位为毫秒。两种模式均沿用同一客户端和 10 ms 轮询间隔。", "",
        "| 轮 | 安装版原测量 | Next 原测量 | 安装版探针 | Next 探针 |",
        "|---:|---:|---:|---:|---:|",
    ]
    for i in pairs:
        values = [by[i, mode, version]["http_ms"] for mode in ("plain", "probe") for version in ("installed", "next")]
        lines.append("| " + str(i) + " | " + " | ".join(f"{v:.3f}" for v in values) + " |")

    def table(title, columns):
        lines.extend(["", "### " + title, "", "| 轮 | 版本 | " + " | ".join(label for label, _ in columns) + " |",
                      "|---:|---|" + "---:|" * len(columns)])
        for i in pairs:
            for version in ("installed", "next"):
                row = by[i, "probe", version]
                values = [row[key] for _, key in columns]
                lines.append(f"| {i} | {version} | " + " | ".join(f"{v:.3f}" for v in values) + " |")

    table("端到端分段（前三列相加等于 HTTP）", [
        ("提交→worker", "post_to_worker_ms"), ("worker→终态", "worker_ms"), ("终态→客户端", "terminal_to_client_ms"),
        ("尾段 HTTP 在途", "tail_http_ms"), ("尾段请求间隙", "tail_between_http_ms"), ("GET 数", "gets"),
        ("提交 HTTP", "post_ms"), ("全部 GET 在途", "polls_in_flight_ms"),
        ("GET 中读库列表", "poll.list_libraries_ms"), ("GET 中分组计数", "poll.count_media_ms"), ("GET 中读任务", "poll.read_job_ms"),
    ])
    table("worker 的互斥分段（同一行各列相加等于 worker→终态）", [
        ("读签名和建索引", "signatures_ms"), ("受信目录", "trusted_dir_ms"), ("原件检验", "inspection_ms"),
        ("资产发布", "publish_ms"), ("遍历其余", "walk_other_ms"), ("缺件/移动对账", "reconcile_ms"),
        ("终态 SQL", "terminal.sql_ms"), ("其余", "worker_other_ms"),
    ])
    table("上述阶段内的子区间（单独阅读）", [
        ("写锁等待", "writer.wait_ms"), ("Commit 调用", "asset.tx_commit_ms"),
        ("内容哈希", "inspection.hash_ms"), ("媒体头探测", "inspection.probe_ms"),
        ("缺件父目录 seek", "reconcile.parent_seek_ms"), ("缺件索引页", "reconcile.page_ms"),
        ("缺件目录枚举", "reconcile.list_ms"), ("8 次版本零行语句", "version_sql.zero_ms"),
        ("单个空探针 ns", "calibration_per_empty_span_ns"),
    ])
    lines.extend(["", "全部绝对时间戳、观察到的每次状态、原件校验及进程写入在各轮 JSON。paired.json 给出逐对差值、中位数、MAD、方向数。",
                  "安装版终态为持锁设置 idle，Next 终态为成功的终态 SQL 调用返回。后台工作、POST 响应及各 GET 可重叠。",
                  "安装版把移动合并放在对账，Next 在资产发布中保存同一身份。安装版遍历其余包含逐文件路径、stat 与签名比较。Next 的受信目录单列。",
                  "同机无探针原测量覆盖整体扰动和轮次差异，空探针校准在各轮身份检查后执行。各阶段为经过的墙钟时间，包含当时的等待或调度。",
                  "工程门槛要求同一可消除工作至少四对方向相同，中位差大于 10 ms 且大于同段配对差值 MAD 的两倍。数值过线还需业务等价和冗余证据。"])
    output = "\n".join(lines) + "\n"
    (evidence / "report.md").write_text(output)
    print(output)
    with Path(os.environ["GITHUB_STEP_SUMMARY"]).open("a") as stream:
        stream.write(output)
