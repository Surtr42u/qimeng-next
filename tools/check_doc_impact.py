"""Compare a PR's changed paths with the guides responsible for the changed behavior."""

import json
import os
from pathlib import Path
import re
import subprocess
import sys


def affected_guides(paths):
    matches = {}
    if "api/openapi.yaml" in paths:
        matches["协议"] = {"docs/api.md", "docs/author-profiles.md", "docs/migration.md", "docs/mcp-compatibility.md", "docs/analytics.md"}
    if "server/internal/architecture/boundaries_test.go" in paths:
        matches["模块边界"] = {"docs/architecture.md", "server/README.md"}
    if any(p.startswith("server/internal/store/migrations/") and p.endswith(".sql") or
           (p.startswith("server/internal/migration/") or p.startswith("server/internal/service/migration_"))
           and p.endswith(".go") and not p.endswith("_test.go") for p in paths):
        matches["数据迁移"] = {"docs/migration.md", "docs/author-profiles.md", "server/README.md"}
    return matches


def evaluate(paths, body):
    reports, missing = [], []
    for area, guides in affected_guides(paths).items():
        edited = sorted(guides.intersection(paths))
        if edited:
            reports.append(f"- {area}已更新 " + "、".join(f"`{guide}`" for guide in edited))
        elif re.search(rf"^文档无需更新\[{area}\][ \t]+\S.*$", body, re.MULTILINE):
            reports.append(f"- {area}已在 PR 正文说明沿用现有文档的原因。")
        else:
            missing.append(area)
            reports.append(f"- {area}需核对 " + "、".join(f"`{guide}`" for guide in sorted(guides)))
    return reports, missing


def main():
    if os.environ.get("GITHUB_ACTIONS") != "true":
        raise SystemExit("Run documentation impact checks in cloud CI.")
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text(encoding="utf-8"))
    pr = event.get("pull_request")
    base = sys.argv[1]
    if not base:
        return 0
    revision = f"{base}...{pr['head']['sha']}" if pr else f"{base}..HEAD"
    changed = subprocess.check_output(["git", "diff", "--no-renames", "--name-only", "-z", revision], text=False)
    paths = {os.fsdecode(p) for p in changed.split(b"\0") if p}
    reports, missing = evaluate(paths, (pr.get("body") or "") if pr else "")
    with Path(os.environ["GITHUB_STEP_SUMMARY"]).open("a", encoding="utf-8") as summary:
        summary.write("### 文档影响\n\n" + "\n".join(reports or ["本次差异没有触及协议、模块边界或数据迁移入口。"]) + "\n")
    if missing and pr:
        print("请更新对应专题，或在 PR 正文按专题说明文档为何已经准确。见 CONTRIBUTING.md。涉及 " + "、".join(missing))
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
