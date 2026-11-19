"""Compare both shipped HTTP scanners on one Linux runner and one synthetic corpus."""

import contextlib
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import platform
import random
import re
import secrets
import shutil
import socket
import sqlite3
import statistics
import struct
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
import zlib


FILES, DIRECTORIES = 2048, 16
REWRITES, MOVES, ADDITIONS = 16, 8, 4
EPOCH = 1_700_000_000
POLL = 0.01
PHASES = ("first", "unchanged", "sparse")


def png(seed):
    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data))

    pixels = random.Random(seed).randbytes(32 * 32 * 3)
    pixels = b"".join(b"\0" + pixels[i:i + 96] for i in range(0, len(pixels), 96))
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 32, 32, 8, 2, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(pixels)) + chunk(b"IEND", b""))


def filename(i):
    return f"album-{i % DIRECTORIES:02d}/image-{i:05d}.png"


def put(root, manifest, path, seed):
    data = png(seed)
    target = root / path
    target.parent.mkdir(exist_ok=True)
    target.write_bytes(data)
    stamp = EPOCH + seed
    os.utime(target, (stamp, stamp))
    manifest[path] = (len(data), stamp, hashlib.sha256(data).hexdigest())


def sparse_changes(root, manifest):
    moved = {}
    for i in range(REWRITES):
        put(root, manifest, filename(i), FILES + ADDITIONS + i)
    for i in range(REWRITES, REWRITES + MOVES):
        source = filename(i)
        destination = "moved/" + Path(source).name
        (root / "moved").mkdir(exist_ok=True)
        (root / source).rename(root / destination)
        manifest[destination] = manifest.pop(source)
        moved[source] = destination
    for i in range(FILES, FILES + ADDITIONS):
        put(root, manifest, filename(i), i)
    return moved


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def read_originals(root, manifest):
    for path, (size, stamp, digest) in manifest.items():
        target = root / path
        data = target.read_bytes()
        require((len(data), target.stat().st_mtime_ns, hashlib.sha256(data).hexdigest())
                == (size, stamp * 1_000_000_000, digest), f"fixture differs at {path}")


class API:
    def __init__(self, port, version):
        self.base = f"http://127.0.0.1:{port}"
        self.version = version
        self.token = ""

    def call(self, method, path, payload=None, key=None, text=False):
        headers = {}
        if self.token:
            headers["Authorization"] = "Bearer " + self.token
        if key:
            headers["Idempotency-Key"] = key
        body = None
        if payload is not None:
            body = json.dumps(payload).encode()
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(self.base + path, body, headers, method=method)
        try:
            with urllib.request.urlopen(request, timeout=10) as response:
                raw = response.read()
        except urllib.error.HTTPError as error:
            try:
                problem = json.loads(error.read(4096))
                code = problem.get("code") if isinstance(problem, dict) else None
            except (ValueError, UnicodeError):
                code = None
            if not isinstance(code, str) or not re.fullmatch(r"[a-zA-Z0-9_.-]{1,80}", code):
                code = "unclassified"
            raise RuntimeError(f"{self.version} {method} {path} returned HTTP {error.code} ({code})") from None
        if text:
            return raw.decode()
        return json.loads(raw) if raw else None


def wait_for(check, message, timeout=90):
    until = time.monotonic() + timeout
    while time.monotonic() < until:
        result = check()
        if result:
            return result
        time.sleep(POLL)
    raise TimeoutError(message)


@contextlib.contextmanager
def server(binary, version, data, media):
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    api = API(port, version)
    secret = secrets.token_urlsafe(40)
    env = {**os.environ, "QIMENG_DATA_DIR": str(data), "QIMENG_LISTEN": f"127.0.0.1:{port}",
           "QIMENG_TIMEZONE": "UTC", "QIMENG_SCANNER_WATCH_ENABLED": "false",
           "QIMENG_SCANNER_POLL_INTERVAL": "24h"}
    if version == "installed":
        env.update(QIMENG_ALLOWED_LIBRARY_ROOTS=str(media), QIMENG_DB_PATH=str(data / "scan.sqlite"),
                   QIMENG_THUMBNAIL_WARMUP_DELAY="30s", QIMENG_BACKUP_ENABLED="false",
                   QIMENG_MAINTENANCE_WORKER_ENABLED="false")
    else:
        env.update(QIMENG_STORAGE_ROOTS=json.dumps({"fixture": str(media.parent)}), QIMENG_ADMIN_TOKEN=secret)
    started = time.monotonic()
    with subprocess.Popen([binary], env=env, cwd=data.parent, stdout=subprocess.DEVNULL,
                          stderr=subprocess.DEVNULL) as proc:
        try:
            def ready():
                require(proc.poll() is None, f"{version} exited before readiness ({proc.returncode})")
                try:
                    api.call("GET", "/healthz", text=True)
                except urllib.error.URLError:
                    return False
                return True

            wait_for(ready, "health check timed out")
            if version == "installed":
                api.token = api.call("POST", "/api/v1/auth/setup", {"password": secret})["token"]
                spec = {"kind": "normal", "rootPath": str(media)}
            else:
                api.token = secret
                spec = {"kind": "regular", "storageId": "fixture", "rootRelativePath": media.name}
            library = api.call("POST", "/api/v1/libraries", {"name": "Synthetic", **spec},
                               key="register-synthetic-library")
            if version != "installed":
                def registration_finished():
                    jobs = api.call("GET", "/api/v1/scans?libraryId=" + library["id"])["items"]
                    return jobs and all(j["state"] == "completed" and j["processed"] == 0 for j in jobs)

                wait_for(registration_finished, "empty-library reconciliation did not finish")
            yield api, library, proc.pid, started
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=25)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
                raise


def proc_number(pid, file, field):
    for line in Path(f"/proc/{pid}/{file}").read_text().splitlines():
        name, _, value = line.partition(":")
        if name == field:
            return int(value.split()[0])
    raise ValueError(f"missing /proc field {field}")


def snapshot(version, data, library_id, manifest, rewritten):
    # Outside the stopwatch. Read the live catalog through a read-only connection.
    if version == "installed":
        db = data / "scan.sqlite"
        sql = "SELECT rel_path,asset_id,size_bytes,mtime,NULL,NULL FROM assets WHERE library_id=?"
    else:
        db = data / "qimeng.sqlite"
        sql = """SELECT relative_path,id,size,json_extract(data,'$.contentMtime'),hash,
                        json_extract(data,'$.contentVersion') FROM assets
                 WHERE library_id=? AND trashed=0"""
    with contextlib.closing(sqlite3.connect(db.as_uri() + "?mode=ro", uri=True)) as connection:
        records = connection.execute(sql, (library_id,)).fetchall()
    require(len(records) == len(manifest), "catalog row count differs from fixture")
    identities = {}
    for path, asset_id, size, stamp, digest, content_version in records:
        expected_size, expected_stamp, expected_hash = manifest[path]
        require((size, datetime.fromisoformat(stamp.replace("Z", "+00:00")).timestamp())
                == (expected_size, expected_stamp), f"catalog metadata differs at {path}")
        if version != "installed":
            require(digest == expected_hash, f"catalog hash differs at {path}")
            expected_version = 2 if path in rewritten else 1
            require(content_version == expected_version,
                    f"content version at {path}: expected {expected_version}, got {content_version}")
        identities[path] = asset_id
    require(set(identities) == set(manifest) and len(set(identities.values())) == len(manifest),
            "catalog paths or identities are not one-to-one")
    return identities


def measure(api, version, library, phase, pid):
    path = "/api/v1/libraries/" + library["id"]
    written_before = proc_number(pid, "io", "write_bytes")
    rss_before = proc_number(pid, "status", "VmRSS")
    peak_before = proc_number(pid, "status", "VmHWM")
    start = time.monotonic()
    if version == "installed":
        api.call("POST", path + "/scan")
    else:
        job = api.call("POST", path + "/scans", {"expectedRevision": library["revision"]},
                       key="scan-comparison-" + phase)
        require(job["trigger"] == "manual", "manual scan joined an automatic run")
    polls = 0

    def finished():
        nonlocal polls
        polls += 1
        if version == "installed":
            item, = api.call("GET", "/api/v1/libraries")
            require(item["scanState"] != "error", "installed scan failed")
            return item if item["scanState"] == "idle" else None
        item = api.call("GET", "/api/v1/scans/" + job["id"])
        require(item["state"] in ("queued", "running", "completed"), f"Next scan ended in {item['state']}")
        return item if item["state"] == "completed" else None

    item = wait_for(finished, "manual scan did not finish")
    elapsed = (time.monotonic() - start) * 1000
    written = proc_number(pid, "io", "write_bytes") - written_before
    high_water = proc_number(pid, "status", "VmHWM")
    total = FILES + (ADDITIONS if phase == "sparse" else 0)
    added, updated, moved = {"first": (FILES, 0, 0), "unchanged": (0, 0, 0),
                             "sparse": (ADDITIONS, REWRITES, MOVES)}[phase]
    if version != "installed":
        require((item["processed"], item["added"], item["changed"], item["moved"], item["skipped"],
                 item["missing"], item["issueCount"]) ==
                (total, added, updated, moved, total - added - updated - moved, 0, 0),
                f"unexpected Next counters: {item}")
        def timestamp(field):
            return datetime.fromisoformat(item[field].replace("Z", "+00:00"))
        internal_ms = (timestamp("updatedAt") - timestamp("createdAt")).total_seconds() * 1000
    else:
        # The legacy list reads SQL counts before fetching the in-memory scan state.
        # A poll crossing the last move merge can combine a pre-merge count with idle.
        # Open a fresh read after observing completion, outside the timed interval.
        settled, = api.call("GET", "/api/v1/libraries")
        require(settled["fileCount"] == total, f"installed library count differs: {settled['fileCount']}")
        metrics = api.call("GET", "/metrics", text=True).splitlines()
        duration, = [line.split()[1] for line in metrics if line.startswith("scan_duration_seconds ")]
        internal_ms = float(duration) * 1000
    return {"phase": phase, "http_ms": elapsed, "backend_ms": internal_ms, "polls": polls,
            "write_bytes": written, "rss_before_kib": rss_before, "process_peak_before_kib": peak_before,
            "process_peak_rss_kib": high_water, "catalog_rows": total}


def trial(version, binary, index, corpus, original, temp):
    with tempfile.TemporaryDirectory(dir=temp) as work:
        root = Path(work)
        data, media = root / "data", root / "storage" / "media"
        data.mkdir()
        media.mkdir(parents=True)
        stage = Path(shutil.copytree(corpus, root / "staged"))
        manifest = original.copy()
        results = []
        with server(binary, version, data, media) as (api, library, pid, started):
            for album in stage.iterdir():
                album.rename(media / album.name)
            before = {}
            moved = {}
            for phase in PHASES:
                if phase == "sparse":
                    moved = sparse_changes(media, manifest)
                # Identical page-cache warming for all three binaries before timing.
                read_originals(media, manifest)
                row = measure(api, version, library, phase, pid)
                # On both fresh processes no thumbnail tick can run before 30 seconds.
                # Reject contaminated comparisons if the whole trial outlives that window.
                end_age = time.monotonic() - started
                require(end_age < 30, f"{version} trial {index} exceeded thumbnail quiet window ({end_age:.1f}s)")
                cache = data / ("thumbs" if version == "installed" else "cache")
                require(not any(p.is_file() for p in cache.rglob("*")), "thumbnail generation overlapped scan")
                rewritten = {filename(i) for i in range(REWRITES)} if phase == "sparse" else set()
                after = snapshot(version, data, library["id"], manifest, rewritten)
                for path, asset_id in before.items():
                    require(after[moved.get(path, path)] == asset_id, f"scan lost identity at {path}")
                if phase == "sparse":
                    # Read the actual originals after the last timed POST, including the
                    # installed scanner which does not store a content digest.
                    physical = {p.relative_to(media).as_posix() for p in media.rglob("*.png") if p.is_file()}
                    require(physical == manifest.keys() and len(physical) == FILES + ADDITIONS,
                            "final physical PNG paths differ from the expected corpus")
                    read_originals(media, manifest)
                    row["physical_originals_verified"] = len(physical)
                    row["physical_bytes_verified"] = sum(size for size, _, _ in manifest.values())
                    if version != "installed":
                        row["versions_verified"] = {
                            "rewritten_advanced": len(rewritten), "moved_unchanged": len(moved),
                            "new_at_one": ADDITIONS, "other_unchanged": FILES - len(rewritten) - len(moved),
                        }
                before = after
                row.update(version=version, trial=index, process_age_s=end_age)
                results.append(row)
                print(json.dumps(row), flush=True)
        return results


def report(evidence, metadata, rows):
    (evidence / "results.json").write_text(json.dumps({**metadata, "runs": rows}, indent=2) + "\n")
    lines = ["## 完整手动扫描", "", f"旧版 \x60{metadata['installed_sha']}\x60，Next \x60{metadata['next_sha']}\x60。",
             f"同一 {metadata['platform']} 运行器，Go {metadata['go']}，CGO=0。每轮使用新进程和空数据库。",
             f"{FILES} 张不同的 32×32 RGB PNG，{DIRECTORIES} 个目录，合计 {metadata['bytes']:,} 字节。",
             f"少量变动包含 {REWRITES} 张原位改写、{MOVES} 张移动、{ADDITIONS} 张新增。每轮核对全部路径、大小、mtime 和已有 ID。"
             f"末次计时结束后再读全部 {FILES + ADDITIONS:,} 份物理原件核对长度和 SHA-256。Next 同时逐项核对目录中的摘要与内容版本。",
             "", "| 阶段 | 版本 | HTTP 毫秒（3 次） | HTTP 中位数 ms | 后端中位数 ms | 进程写入中位数 MiB | 截至该阶段 RSS 高水位中位数 MiB | 入库总数 |",
             "|---|---|---|---:|---:|---:|---:|---:|"]
    if metadata.get("before_next_sha"):
        lines.insert(3, f"同机附测优化前 Next 提交 {metadata['before_next_sha']}，表中记为 before。")
    for phase in PHASES:
        for version in dict.fromkeys(row["version"] for row in rows):
            samples = [row for row in rows if row["phase"] == phase and row["version"] == version]
            if not samples:
                continue
            def median(key):
                return statistics.median(r[key] for r in samples)
            times = ", ".join(f"{r['http_ms']:.2f}" for r in samples)
            lines.append(f"| {phase} | {version} | {times} | {median('http_ms'):.2f} | {median('backend_ms'):.2f} | "
                         f"{median('write_bytes') / 2**20:.2f} | {median('process_peak_rss_kib') / 1024:.1f} | {samples[0]['catalog_rows']} |")
    lines += ["", "计时从手动 POST 发出到首次观察到成功终态。两版均每 10 毫秒轮询一次，HTTP 时间包含轮询间隔和请求开销。"
              "后端辅助时间分别取旧版扫描函数指标和 Next 持久化任务创建至终态时间。进程写入取 Linux /proc/io 的 write_bytes 差值。"
              "RSS 高水位从进程启动累计。旧版在扫描前设置密码时执行 64 MiB Argon2id 派生，Next 使用配置令牌。JSON 另记每次提交扫描前的 RSS 和高水位。",
              "每阶段在计时前顺序读取并核对全部原件，磁盘页缓存已热。文件监听关闭、轮询间隔 24 小时，先让 Next 的登记对账扫完空库。"
              "两版缩略图首次回填均在启动 30 秒后，计时结束核验仍在静默期且缓存中无产物。"
              "安装版启动时查找 ffmpeg/ffprobe 路径，准备环境和工具检测均在 POST 前。PNG 没有每文件外部探测，Next 的图片头校验和内容哈希计入扫描。",
              "旧版 README 的两万行基准分别测索引构建或 SQL 快照装载。本次测量涵盖遍历、变更处理、持久化和处理结果的 HTTP 观察。"]
    output = "\n".join(lines) + "\n"
    (evidence / "report.md").write_text(output)
    if summary := os.environ.get("GITHUB_STEP_SUMMARY"):
        with open(summary, "a") as f:
            f.write(output)


def main():
    old, new, evidence, *previous = [Path(p).resolve() for p in sys.argv[1:]]
    evidence.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory() as work:
        temp = Path(work)
        corpus = temp / "corpus"
        corpus.mkdir()
        manifest = {}
        for i in range(FILES):
            put(corpus, manifest, filename(i), i)
        metadata = {"installed_sha": os.environ["REFERENCE_SHA"], "next_sha": os.environ["GITHUB_SHA"],
                    "platform": platform.platform(), "go": subprocess.check_output(["go", "version"], text=True).strip(),
                    "cpu_count": len(os.sched_getaffinity(0)), "files": FILES, "directories": DIRECTORIES,
                    "bytes": sum(v[0] for v in manifest.values()),
                    "manifest_sha256": hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest(),
                    "media_tools_on_path": {name: bool(shutil.which(name)) for name in ("ffmpeg", "ffprobe")}}
        rows = []
        versions = [("installed", old), ("next", new)]
        if previous:
            metadata["before_next_sha"] = os.environ["BEFORE_NEXT"]
            versions.insert(1, ("before", previous[0]))
        for index in range(3):
            offset = index % len(versions)
            for version, binary in versions[offset:] + versions[:offset]:
                rows.extend(trial(version, binary, index + 1, corpus, manifest, temp))
        report(evidence, metadata, rows)


if __name__ == "__main__":
    main()
