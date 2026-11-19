import base64
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from export_facts import Rejected, validate_export


COMMAND = Path(__file__).with_name("prepare.py")
IMAGE = base64.b64decode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=")
STAMP = 1_761_956_812_123
FIRST = "same.png @ a"
SECOND = "same.png @ b"


def raw_json(value):
    return (json.dumps(value, ensure_ascii=False, indent=2) + "\n").encode()


def digest(value):
    return hashlib.sha256(value).hexdigest()


def full_export():
    def original(asset, key, rel):
        return {"assetId": asset, "recordKey": key, "fileName": "same.png", "libraryName": "合成资料",
                "relPath": rel, "mediaType": "image", "sizeBytes": len(IMAGE), "modifiedAtMillis": STAMP,
                "indexedAtMillis": STAMP - 3600_000, "isCosFile": False}

    key = {"recordKey": FIRST}
    stamp = datetime.fromtimestamp(STAMP // 1000, timezone.utc).replace(microsecond=123000)
    at = stamp.isoformat(timespec="milliseconds").replace("+00:00", "Z")
    data = {
        "mediaFiles": [original("old-asset-b", SECOND, "b/same.png"), original("old-asset-a", FIRST, "a/same.png")],
        "viewEvents": [dict(key, eventId="portable:synthetic-original", kind="open", sessionId="源会话",
                            startedAt=at, seconds=None), dict(key, eventId="legacy:synthetic-original", kind="dwell",
                                                             sessionId="", startedAt=at, seconds=11, day="2025-11-01")],
        "authors": [{"authorId": "illustrator", "displayName": "示例作者", "profileJson": json.dumps({
            "authorId": "illustrator", "avatarUrl": "https://example.org/avatar.png", "biography": "合成档案"}),
                     "avatarData": base64.b64encode(IMAGE).decode()}],
        "authorMediaRefs": [dict(key, authorId="illustrator", fileName="same.png", origin="legacy")],
        "tags": [{"name": "测试标签"}], "mediaTagRefs": [dict(key, tagName="测试标签")],
        "favorites": [SECOND], "followedAuthorIds": ["illustrator"],
        "likes": [dict(key, likeCount=4, lastLikeDate="2025-11-01")],
        "history": [dict(key, fileName="same.png", mediaType="image", openedAtMillis=STAMP)],
        "mediaStats": [dict(key, fileName="same.png", viewCount=1, playCount=0, totalBrowseSeconds=11)],
        "dailyBrowse": [dict(key, fileName="same.png", mediaType="image", dayStartMillis=STAMP - 1000,
                             viewCount=1, playCount=0, totalBrowseSeconds=11)],
        "txtFragments": [{"filename": "合成.txt", "content": "精确保留\r\n末行", "importedAtMillis": 0}],
        "authorMasterTxt": "合成总表\n", "timelineTags": [],
        "settings": [], "scanSources": [], "albumRules": [], "cosWorks": [],
        "appPrefs": {"version": 1, "recommendationPrefs": {"freshness": 0.4}},
        "futureExtension": [{"opaque": [None, True, {"precise": 18446744073709551618}]}],
    }
    return {"format": "qimeng_backup", "schemaVersion": 2, "appIdentifier": "com.qimeng.media",
            "exportedAtMillis": STAMP + 86400000, "data": data}


class RecoveryCLI(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.temp = Path(self.scratch.name)
        self.root = self.temp / "media"
        self.output = self.temp / "verified-source"
        self.export = self.temp / "old.json"
        self.mapping = self.temp / "roots.json"
        self.raw = raw_json(full_export())
        self.export.write_bytes(self.raw)
        self.mapping.write_bytes(raw_json({"合成资料": str(self.root)}))
        for folder in ("a", "b"):
            child = self.root / folder
            child.mkdir(parents=True)
            path = child / "same.png"
            path.write_bytes(IMAGE)
            nanos = STAMP * 1_000_000 + 100
            os.utime(path, ns=(nanos, nanos))
        (self.root / "ancillary.txt").write_text("synthetic ancillary")
        (self.root / ".hidden").mkdir()
        (self.root / ".hidden" / "cache.png").write_bytes(IMAGE)

    def run_command(self, args, *, ok):
        result = subprocess.run([sys.executable, str(COMMAND), *map(str, args)], capture_output=True,
                                text=True, encoding="utf-8", env={**os.environ, "PYTHONUTF8": "1"})
        if ok:
            self.assertEqual(result.returncode, 0, result.stderr)
            return json.loads(result.stdout)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(str(self.temp), result.stderr)
        self.assertNotIn("Traceback", result.stderr)
        return result.stderr

    def prepare(self, ok=True, **changes):
        options = {"--export": self.export, "--export-sha256": digest(self.raw), "--roots": self.mapping,
                   "--output": self.output}
        options.update(changes)
        args = [part for item in options.items() for part in item]
        return self.run_command(["prepare", *args], ok=ok)

    def verify(self, receipt, ok=True):
        return self.run_command(["verify", "--source", self.output, "--roots", self.mapping,
                                 "--receipt-sha256", receipt], ok=ok)

    def test_readback_raw_sections_ids_and_idempotence(self):
        result = self.prepare()
        self.assertEqual(result["state"], "prepared")
        self.assertEqual(result["verifiedFiles"], 2)
        self.assertEqual((self.output / "export.json").read_bytes(), self.raw)
        raw = json.loads((self.output / "export.json").read_bytes())["data"]
        self.assertEqual([row["eventId"] for row in raw["viewEvents"]],
                         ["portable:synthetic-original", "legacy:synthetic-original"])
        self.assertNotIn("createdAtMillis", raw["authorMediaRefs"][0])
        files = [json.loads(line) for line in (self.output / "originals.jsonl").read_bytes().splitlines()]
        self.assertEqual([(row["assetId"], row["recordKey"], row["sha256"]) for row in files],
                         [("old-asset-b", SECOND, digest(IMAGE)), ("old-asset-a", FIRST, digest(IMAGE))])
        self.assertEqual(digest((self.output / "receipt.json").read_bytes()), result["receiptSHA256"])
        report = json.loads((self.output / "coverage.json").read_bytes())
        self.assertEqual(report["oldInstanceId"], None)
        self.assertEqual(report["nativeArchiveCreated"], False)
        self.assertEqual(set(report["sections"]), set(raw))
        self.assertEqual(report["sections"]["futureExtension"]["disposition"], "unknown_section_sealed_verbatim")
        self.assertEqual(report["originals"]["nonMediaFiles"], 1)
        self.assertEqual(report["originals"]["oldScannerExcludedEntries"], 1)
        self.assertIn("trash_files_and_deletion_history", report["notRecoverableFromThisExport"])
        for path in self.output.iterdir():
            self.assertNotIn(str(self.temp).encode(), path.read_bytes())
        before = {p.name: (p.stat().st_mtime_ns, p.read_bytes()) for p in self.output.iterdir()}
        self.assertEqual(self.prepare(), {**result, "state": "already_prepared"})
        self.assertEqual(self.verify(result["receiptSHA256"])["state"], "verified")
        self.assertEqual(before, {p.name: (p.stat().st_mtime_ns, p.read_bytes()) for p in self.output.iterdir()})

    def test_empty_export_empty_roots_and_optional_preferences(self):
        data = full_export()
        data["data"] = {k: [] for k in data["data"] if k not in ("appPrefs", "futureExtension")}
        data["data"].update({"authorMasterTxt": "", "followedAuthorIds": None})
        self.raw = raw_json(data)
        self.export.write_bytes(self.raw)
        self.mapping.write_bytes(b"{}")
        result = self.prepare()
        self.assertEqual(self.verify(result["receiptSHA256"])["verifiedFiles"], 0)
        report = json.loads((self.output / "coverage.json").read_bytes())
        self.assertEqual(report["sections"]["followedAuthorIds"]["representation"], "null")
        self.assertEqual(report["sections"]["appPrefs"]["disposition"], "not_present_in_export")

    def test_damaged_or_ambiguous_export_refused(self):
        data = full_export()["data"]
        variants = {
            "recordKey": ("mediaFiles", [data["mediaFiles"][0], dict(data["mediaFiles"][1], recordKey=SECOND)]),
            "assetId": ("mediaFiles", [data["mediaFiles"][0], dict(data["mediaFiles"][1], assetId="old-asset-b")]),
            "target": ("mediaFiles", [data["mediaFiles"][0], dict(data["mediaFiles"][1], relPath="b/same.png")]),
            "traversal": ("mediaFiles", [dict(data["mediaFiles"][0], relPath="../same.png")]),
            "eventId": ("viewEvents", data["viewEvents"] * 2),
            "eventRef": ("viewEvents", [dict(data["viewEvents"][0], recordKey="missing")]),
            "session": ("viewEvents", [dict(data["viewEvents"][0], sessionId=None)]),
            "time": ("viewEvents", [dict(data["viewEvents"][0], startedAt="invalid")]),
            "aggregate": ("history", []),
            "tag": ("mediaTagRefs", [{"recordKey": FIRST, "tagName": "unknown"}]),
            "author": ("authorMediaRefs", [dict(data["authorMediaRefs"][0], authorId="unknown")]),
            "avatar": ("authors", [dict(data["authors"][0], avatarData="!!!")]),
        }
        for reason, (section, change) in variants.items():
            with self.subTest(reason=reason), self.assertRaises(Rejected):
                document = full_export()
                document["data"][section] = change
                validate_export(raw_json(document))
        for section in ("mediaFiles", "viewEvents", "likes", "authorMasterTxt"):
            with self.subTest(section=section), self.assertRaises(Rejected):
                document = full_export()
                del document["data"][section]
                validate_export(raw_json(document))
        damaged = self.raw.replace(b'"schemaVersion": 2', b'"schemaVersion": 2, "schemaVersion": 2')
        self.export.write_bytes(damaged)
        self.assertIn("duplicate JSON", self.prepare(False, **{"--export-sha256": digest(damaged)}))
        self.assertFalse(self.output.exists())

    def test_original_evidence_failure_and_untouched_destination(self):
        self.assertIn("independent SHA-256", self.prepare(False, **{"--export-sha256": "0" * 64}))
        extra = self.root / "extra.gif"
        extra.write_bytes(b"GIF89a")
        self.assertIn("unexported media", self.prepare(False))
        extra.unlink()
        selected = self.root / "a/same.png"
        selected.unlink()
        self.assertIn("missing", self.prepare(False))
        os.link(self.root / "b/same.png", selected)
        self.assertIn("physical file", self.prepare(False))
        selected.unlink()
        selected.write_bytes(IMAGE)
        self.assertIn("mtime differs", self.prepare(False))
        os.utime(selected, ns=(STAMP * 1_000_000,) * 2)
        self.assertIn("overlaps selected originals", self.prepare(False, **{"--output": self.root / "new"}))
        self.output.mkdir()
        self.assertIn("incomplete source", self.prepare(False))
        self.assertEqual(list(self.output.iterdir()), [])

    def test_relocated_originals_and_same_metadata_tampering(self):
        hash_value = self.prepare()["receiptSHA256"]
        (self.root / "ancillary.txt").unlink()
        relocated = self.temp / "relocated"
        self.root.rename(relocated)
        self.mapping.write_bytes(raw_json({"合成资料": str(relocated)}))
        self.assertFalse(self.verify(hash_value)["sameRootSelection"])
        altered = relocated / "a/same.png"
        stamp = altered.stat().st_mtime_ns
        changed = bytearray(altered.read_bytes())
        changed[-1] ^= 1
        altered.write_bytes(changed)
        os.utime(altered, ns=(stamp, stamp))
        self.assertIn("SHA-256 differs", self.verify(hash_value, False))
        self.assertIn("existing evidence differs", self.prepare(False))
        (self.output / "originals.jsonl").write_bytes(b"{}\n")
        self.assertIn("member SHA-256 differs", self.verify(hash_value, False))

    def test_reject_directory_symlink(self):
        link = self.temp / "linked"
        try:
            link.symlink_to(self.root, target_is_directory=True)
        except OSError:
            if os.name != "nt":
                raise
            subprocess.run(["cmd.exe", "/d", "/c", "mklink", "/J", str(link), str(self.root)],
                           check=True, capture_output=True)
        try:
            self.mapping.write_bytes(raw_json({"合成资料": str(link)}))
            self.prepare(False)
            self.assertFalse(self.output.exists())
        finally:
            if link.is_symlink():
                link.unlink()
            else:
                link.rmdir()

    def test_concurrent_publish_same_input(self):
        with ThreadPoolExecutor(2) as pool:
            results = [future.result() for future in [pool.submit(self.prepare), pool.submit(self.prepare)]]
        self.assertEqual({r["state"] for r in results}, {"prepared", "already_prepared"})
        self.assertEqual(results[0]["receiptSHA256"], results[1]["receiptSHA256"])
        self.assertEqual(self.verify(results[0]["receiptSHA256"])["verifiedFiles"], 2)


if __name__ == "__main__":
    unittest.main()
