"""Report every locked advisory and gate the published Windows Rust graph."""

import json
import os
from pathlib import Path
import re
import subprocess
import sys


LOCKS = (
    "server/go.mod", "requirements.txt:ai/requirements.lock",
    "tools/mcp-compat/package-lock.json", "web/package-lock.json", "desktop/package-lock.json",
    "desktop/src-tauri/Cargo.lock",
)
WINDOWS = "x86_64-pc-windows-msvc"
MANIFEST = "desktop/src-tauri/Cargo.toml"


def tree(target, features, *options):
    command = ["cargo", "tree", "--locked", "--manifest-path", MANIFEST, "--target", target,
               "--edges", "normal,build", "--features", features, "--charset", "ascii", "--color", "never", *options]
    return subprocess.check_output(command, text=True, encoding="utf-8")


def packages(target, features):
    selected = set()
    for line in tree(target, features, "--prefix", "none").splitlines():
        if not line.strip() or line.strip() == "[build-dependencies]":
            continue
        match = re.fullmatch(r"([\w-]+) v(\S+)(?: .*)?", line.strip())
        if not match:
            raise ValueError(f"Unexpected cargo tree row: {line}")
        selected.add(match.groups())
    if not any(name == "tauri" for name, _ in selected):
        raise ValueError(f"Tauri is missing from the {target} graph")
    return selected


def maintenance_notice(record, crate):
    if not record["id"].startswith("RUSTSEC-"):
        return False
    affected = [item for item in record.get("affected", [])
                if item.get("package", {}).get("ecosystem") == "crates.io" and item["package"].get("name") == crate]
    # OSV's native JSON and the scanner's protobuf JSON use different field spellings.
    return bool(affected) and all(
        (item.get("database_specific") or item.get("databaseSpecific") or {}).get("informational") == "unmaintained"
        for item in affected
    )


def main(scanner, output):
    output.mkdir(parents=True, exist_ok=True)
    report_file = output / "all-locks.osv.json"
    scan = subprocess.run([scanner, "scan", "source", "--no-call-analysis=go", "--all-packages",
                           "--format=json", f"--output-file={report_file}",
                           *(f"--lockfile={source}" for source in LOCKS)])
    if scan.returncode not in (0, 1):
        raise SystemExit(f"OSV-Scanner failed with exit code {scan.returncode}")
    report = json.loads(report_file.read_text(encoding="utf-8"))
    sources = {Path(source["source"]["path"]).resolve() for source in report["results"] if source["packages"]}
    expected = {Path(source.split(":")[-1]).resolve() for source in LOCKS}
    if not expected <= sources:
        raise ValueError(f"Missing lock inventory: {expected - sources}")

    config = json.loads(Path("desktop/src-tauri/tauri.conf.json").read_text(encoding="utf-8"))
    # Tauri CLI build_options adds custom-protocol to the configured build features.
    features = ",".join(["tauri/custom-protocol", *(config["build"].get("features") or [])])
    host = subprocess.check_output(["rustc", "-vV"], text=True)
    if f"host: {WINDOWS}" not in host.splitlines():
        raise ValueError(f"Run this check on the native {WINDOWS} release host")
    windows = packages(WINDOWS, features)
    all_targets = packages("all", features)
    (output / "windows-tree.txt").write_text(host + "\n" + tree(WINDOWS, features), encoding="utf-8")
    (output / "all-targets-tree.txt").write_text(tree("all", features), encoding="utf-8")

    lines = ["## 锁定依赖公告", "", f"Windows 目标 {WINDOWS}，Cargo 普通及构建依赖共 {len(windows)} 个包版本。",
             "", "| 对象 | 版本 | 公告 | 处置 |", "|---|---|---|---|"]
    blocked = 0
    records = 0
    paths = {}
    for source in report["results"]:
        for item in source["packages"]:
            pkg = item["package"]
            name, version = pkg["name"], pkg["version"]
            for finding in item.get("vulnerabilities", []):
                records += 1
                key = name, version
                reject = True
                disposition = "发布或工程依赖，需处置"
                if pkg["ecosystem"] == "crates.io":
                    if key not in windows:
                        reject = False
                        disposition = "其他目标" if key in all_targets else "仅在锁文件，当前特性未选中"
                    elif maintenance_notice(finding, name):
                        reject = False
                        disposition = "Windows 维护状态提醒"
                    else:
                        disposition = "Windows 普通或构建依赖，需处置"
                    if key not in paths:
                        target = WINDOWS if key in windows else "all"
                        paths[key] = tree(target, features, "--invert", f"{name}@{version}") if key in all_targets or key in windows else "No selected normal or build dependency path.\n"
                blocked += int(reject)
                advisory = finding["id"]
                lines.append(f"| {name} | {version} | [{advisory}](https://osv.dev/vulnerability/{advisory}) | {disposition} |")
    (output / "rust-advisory-paths.txt").write_text("\n".join(
        f"{name} {version}\n{paths[name, version]}" for name, version in sorted(paths)), encoding="utf-8")
    if report.get("experimental_generic_findings") or (scan.returncode == 1 and records == 0):
        raise ValueError("OSV reported findings outside the supported package result format; review the full report")
    lines += ["", f"共 {records} 条原始记录，{blocked} 条阻断。相互引用的公告分别列出。"]
    summary = "\n".join(lines) + "\n"
    (output / "summary.md").write_text(summary, encoding="utf-8")
    with open(os.environ["GITHUB_STEP_SUMMARY"], "a", encoding="utf-8") as dest:
        dest.write(summary)
    print(f"{records} advisory records, {blocked} blocking")
    return int(blocked > 0)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1], Path(sys.argv[2])))
