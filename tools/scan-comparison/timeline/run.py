"""Use the existing first / unchanged / sparse fixture and its outcome checks."""

import contextlib
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import compare
from summarize import analyze, report


# Keep the two versions adjacent in each pair. Rotate which mode and version
# go first. Every entry calls trial(), which creates a new process and database.
ORDERS = (
    (("plain", "installed", "next"), ("probe", "next", "installed")),
    (("probe", "installed", "next"), ("plain", "next", "installed")),
    (("plain", "next", "installed"), ("probe", "installed", "next")),
    (("probe", "next", "installed"), ("plain", "installed", "next")),
    (("plain", "installed", "next"), ("probe", "next", "installed")),
)


def one_trial(binary, mode, version, index, corpus, manifest, temp, evidence):
    dump = temp / f"trace-{mode}-{version}-{index}.json"
    original_server, original_measure, original_call = compare.server, compare.measure, compare.API.call
    calls = []
    measuring = False
    completed = False

    @contextlib.contextmanager
    def recorder_server(*args, **kwargs):
        with original_server(*args, **kwargs) as process:
            yield process
            if mode == "probe":
                os.kill(process[2], signal.SIGUSR2)
                compare.wait_for(dump.is_file, "server did not flush the requested trace", timeout=10)

    def record_http(api, method, path, *args, **kwargs):
        nonlocal completed
        if not measuring or completed:
            return original_call(api, method, path, *args, **kwargs)
        start = time.clock_gettime_ns(time.CLOCK_MONOTONIC)
        result = original_call(api, method, path, *args, **kwargs)
        end = time.clock_gettime_ns(time.CLOCK_MONOTONIC)
        if method == "POST":
            state = "submitted"
        elif version == "next":
            state = result["state"]
            completed = state == "completed"
        else:
            item, = result
            state = item["scanState"]
            completed = state == "idle"
        calls.append({"method": method, "state": state, "start_ns": start, "end_ns": end})
        return result

    def recorded_measure(api, version, library, phase, pid):
        nonlocal measuring
        measuring = phase == "sparse"
        try:
            return original_measure(api, version, library, phase, pid)
        finally:
            measuring = False

    with contextlib.ExitStack() as stack:
        if mode == "probe":
            stack.enter_context(patch.dict(os.environ, QIMENG_SCAN_STUDY_DUMP=str(dump)))
        stack.enter_context(patch.object(compare, "server", recorder_server))
        stack.enter_context(patch.object(compare, "measure", recorded_measure))
        stack.enter_context(patch.object(compare.API, "call", record_http))
        fixture_rows = compare.trial(version, binary, index, corpus, manifest, temp)
    sparse, = [row for row in fixture_rows if row["phase"] == "sparse"]
    compare.require(completed and sparse["polls"] == len(calls) - 1, "poll recorder disagrees with normal client")
    raw = {"mode": mode, "version": version, "pair": index, "client": calls, "fixture": fixture_rows}
    if mode == "probe":
        raw["server"] = json.loads(dump.read_text())
    (evidence / f"{index}-{mode}-{version}.json").write_text(json.dumps(raw, indent=2) + "\n")
    return analyze(raw)


def main():
    compare.require(os.getenv("GITHUB_ACTIONS") == "true", "use the isolated cloud job")
    bins, evidence = (Path(arg).resolve() for arg in sys.argv[1:])
    evidence.mkdir(parents=True, exist_ok=True)
    binaries = {(mode, version): bins / (version + ("-probe" if mode == "probe" else "-server"))
                for mode in ("plain", "probe") for version in ("installed", "next")}
    with tempfile.TemporaryDirectory() as folder:
        temp = Path(folder)
        corpus = temp / "corpus"
        corpus.mkdir()
        manifest = {}
        for i in range(compare.FILES):
            compare.put(corpus, manifest, compare.filename(i), i)
        metadata = {
            "installed_sha": os.environ["REFERENCE_SHA"], "next_sha": os.environ["NEXT_BASE"],
            "experiment_sha": os.environ["GITHUB_SHA"], "platform": platform.platform(),
            "go": subprocess.check_output(["go", "version"], text=True).strip(),
            "cpus": len(os.sched_getaffinity(0)), "python": platform.python_version(),
            "ffmpeg": bool(shutil.which("ffmpeg")), "ffprobe": bool(shutil.which("ffprobe")),
            "clock": "Linux CLOCK_MONOTONIC; absolute nanoseconds for Python HTTP and Go records",
            "files": len(manifest), "initial_bytes": sum(v[0] for v in manifest.values()),
            "initial_manifest_sha256": hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest(),
            "order": ORDERS, "binary_sha256": {},
        }
        for key, binary in binaries.items():
            with binary.open("rb") as stream:
                metadata["binary_sha256"]["/".join(key)] = hashlib.file_digest(stream, "sha256").hexdigest()
        (evidence / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n")
        rows = []
        for index, orders in enumerate(ORDERS, 1):
            for mode, *versions in orders:
                for version in versions:
                    row = one_trial(binaries[mode, version], mode, version, index, corpus, manifest, temp, evidence)
                    rows.append(row)
                    print(json.dumps(row), flush=True)
    report(rows, metadata, evidence)


if __name__ == "__main__":
    main()
