"""Verify one Windows source through a real Go instance, stdio MCP and Shell."""
import asyncio
from contextlib import ExitStack, closing
import hashlib
import json
import os
from pathlib import Path
import secrets
import struct
import subprocess
import sys
import tempfile
import time
import zlib

from mcp import Client, StdioServerParameters

from qimeng_ai.config import Config
from qimeng_ai.errors import ToolError
from qimeng_ai.transport import Transport, load_contract


def sample_png():
    def chunk(kind, payload):
        return struct.pack(">I", len(payload)) + kind + payload + struct.pack(">I", zlib.crc32(kind + payload))

    header = struct.pack(">IIBBBBB", 16, 16, 8, 2, 0, 0, 0)
    pixels = (b"\0" + b"\x39\x72\xb8" * 16) * 16
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", header) + chunk(b"IDAT", zlib.compress(pixels)) + chunk(b"IEND", b"")


def choose_backup_directory(root, stack):
    # Candidate parents are existing runner-owned working or temporary
    # directories. Go verifies the actual target against its database and media.
    parents = [Path(tempfile.gettempdir()), Path(os.environ["GITHUB_WORKSPACE"])]
    if os.environ.get("LOCALAPPDATA"):
        parents.append(Path(os.environ["LOCALAPPDATA"]) / "Temp")
    for parent in parents:
        try:
            if parent.is_dir() and parent.stat().st_dev != root.stat().st_dev:
                return Path(stack.enter_context(tempfile.TemporaryDirectory(prefix="qimeng-proof-", dir=parent)))
        except (FileNotFoundError, PermissionError):
            continue
    target = root / "same-volume-target"
    target.mkdir()
    return target


async def check_chain(root, target, origin, owner, session):
    hooks = {"QIMENG_API_BASE": origin, "QIMENG_API_TOKEN": session["token"], "QIMENG_AI_STATE_DIR": str(root / "executor"),
             "QIMENG_AI_STORAGES": json.dumps({"input": str(root / "source")}), "QIMENG_AI_RECYCLE_STORAGES": '["input"]',
             "QIMENG_AI_PERMISSIONS": "read,ingest,source.cleanup,local.read,local.recycle",
             "PYTHONPATH": str(Path(__file__).resolve().parents[1] / "ai" / "src")}
    params = StdioServerParameters(command=sys.executable, args=["-m", "qimeng_ai.cli", "mcp"], env=hooks)
    source = root / "source" / "chosen.png"
    expected = sample_png()
    source.write_bytes(expected)
    original_stat = source.stat()
    digest = hashlib.sha256(expected).hexdigest()
    batch, library = session["batch"], session["library"]
    subject = {"id": batch}
    with closing(Transport(Config.load(hooks), load_contract())) as scoped:
        def permits(input_id):
            return scoped.call("listSourceCleanupPermits", {"id": batch, "inputId": input_id})["items"]

        async def call(client, name, arguments):
            response = await client.call_tool(name, arguments)
            if response.is_error:
                raise AssertionError(f"{name} failed: {response.structured_content.get('code')}")
            return response.structured_content

        async with Client(params) as client:
            registered = await call(client, "intake.register", {"batchId": batch, "storage": "input", "operationId": "windows-original-selection"})
            item, = registered["inputs"]
            uploaded = await call(client, "upload.file", {"batchId": batch, "libraryId": library, "inputId": item["inputId"],
                                "fileRef": item["fileRef"], "operationId": "windows-actual-upload", "fileName": source.name, "mode": "ordinary"})
            assert uploaded["status"] == "ingested" and uploaded["sha256"] == digest
            asset = uploaded["asset"]
            asset_scope = {"id": asset["id"], "version": asset["contentVersion"]}
            assert owner.binary("getAssetContent", asset_scope, maximum=len(expected))[0] == expected
            info, = owner.call("listBackupTargets", {})["items"]
            assert info["id"] == "proof"
            protect_args = {**subject, "Idempotency-Key": "windows-protect-selected-input"}
            if not info["available"] or not info["distinctVolume"]:
                reason = "backup_target_same_volume" if info["available"] else "backup_target_unavailable"
                for _ in range(2):
                    refused = await client.call_tool("backup.protect", protect_args)
                    assert refused.is_error and refused.structured_content["code"] == reason
                report = await call(client, "task.read", subject)
                assert report["batch"]["backupState"] == "not_covered" and report["batch"]["cleanupState"] == "retained"
                assert report["inputs"][0]["assetId"] == asset["id"] and scoped.call("listBatchProtections", subject)["items"] == []
                assert permits(item["inputId"]) == [] and os.path.samestat(source.stat(), original_stat) and source.read_bytes() == expected
                assert owner.binary("getAssetContent", asset_scope, maximum=len(expected))[0] == expected
                return {"distinctVolume": False, "backup": reason, "sourceOutcome": "retained",
                        "permitCount": 0, "sourceReplay": "no_permit", "serverOriginalVerified": True}

            proof = await call(client, "backup.protect", protect_args)
            path = {**subject, "protectionId": proof["id"]}
            deadline = time.monotonic() + 60
            while time.monotonic() < deadline:
                proof = scoped.call("getBatchProtection", path)
                if proof["state"] not in {"running", "queued"}:
                    break
                await asyncio.sleep(.2)
            proof = await call(client, "backup.protection.read", path)
            assert proof["state"] == "completed" and proof["manifestFingerprint"] == registered["batch"]["manifestFingerprint"]
            protected, = proof["items"]
            assert (protected["inputId"], protected["assetId"], protected["contentVersion"], protected["hash"], protected["size"],
                    protected["state"]) == (item["inputId"], asset["id"], asset["contentVersion"], digest, len(expected), "protected")
            evidence = target / "instances" / proof["instanceId"]
            assert (target / "objects" / "sha256" / digest[:2] / digest).read_bytes() == expected
            assert hashlib.sha256((evidence / "receipts" / (proof["id"] + ".json")).read_bytes()).hexdigest() == proof["proofHash"]
            with (evidence / "snapshots" / (proof["snapshotHash"] + ".sqlite")).open("rb") as snapshot:
                assert hashlib.file_digest(snapshot, "sha256").hexdigest() == proof["snapshotHash"]
            assert source.read_bytes() == expected and permits(item["inputId"]) == []
            args = {"batchId": batch, "inputId": item["inputId"], "fileRef": item["fileRef"],
                    "protectionId": proof["id"], "operationId": "windows-native-recycle"}
            first = await call(client, "source.recycle", args)
            assert first["recorded"] and first["inputId"] == item["inputId"]

        entry, = permits(item["inputId"])
        receipt = entry["receipt"]
        assert entry["state"] == "recorded" and entry["hash"] == digest and entry["size"] == len(expected)
        assert entry["protectionId"] == proof["id"] and entry["contentVersion"] == asset["contentVersion"]
        assert entry["credentialId"] == session["id"] and entry["proofHash"] == proof["proofHash"]
        assert receipt["outcome"] == first["outcome"]
        if receipt["outcome"] == "recycled":
            assert receipt["providerReceipt"] and not source.exists()
            remaining = b"new synthetic file at the same original path"
            source.write_bytes(remaining)
        else:
            assert receipt["outcome"] == "retained" and receipt["code"] and os.path.samestat(source.stat(), original_stat)
            assert source.read_bytes() == expected
            remaining = expected

        # A fresh stdio process must read the same receipt, even when the old
        # filename now belongs to a different file. Never enumerate or empty a user's bin.
        async with Client(params) as restarted:
            replay = await call(restarted, "source.recycle", args)
            assert replay["recorded"] and replay["inputId"] == item["inputId"] and replay["outcome"] == receipt["outcome"]
            report = await call(restarted, "task.read", subject)
        assert report["batch"]["cleanupState"] == receipt["outcome"]
        assert permits(item["inputId"]) == [entry] and source.read_bytes() == remaining
        assert owner.binary("getAssetContent", asset_scope, maximum=len(expected))[0] == expected
        return {"distinctVolume": True, "backup": "snapshot_and_source_verified", "sourceOutcome": receipt["outcome"],
                "reason": receipt.get("code"), "permitCount": 1, "sourceReplay": "same_recorded_receipt", "serverOriginalVerified": True}


def main():
    if sys.platform != "win32" or os.environ.get("GITHUB_ACTIONS") != "true":
        raise SystemExit("This check requires an isolated Windows GitHub Actions runner.")
    repository = Path(__file__).resolve().parents[1]
    origin = "http://127.0.0.1:18481"
    with ExitStack() as stack:
        root = Path(stack.enter_context(tempfile.TemporaryDirectory(prefix="qimeng-windows-", dir=os.environ["RUNNER_TEMP"])))
        target = choose_backup_directory(root, stack)
        (root / "source").mkdir()
        token = secrets.token_urlsafe(32)
        env = {k: v for k, v in os.environ.items() if not k.startswith("QIMENG_")}
        env.update(QIMENG_DATA_DIR=str(root / "data"), QIMENG_LISTEN=origin.removeprefix("http://"), QIMENG_ADMIN_TOKEN=token,
                   QIMENG_DEFAULT_BACKUP_TARGET="proof", QIMENG_BACKUP_TARGETS=json.dumps({"proof": {"path": str(target), "failureDomain": "cloud-runner-fixture"}}))
        process = subprocess.Popen([repository / "build" / "qimeng-next.exe"], env=env, cwd=root,
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, creationflags=subprocess.CREATE_NO_WINDOW)
        try:
            config = Config.load({"QIMENG_API_BASE": origin, "QIMENG_API_TOKEN": token, "QIMENG_AI_STATE_DIR": str(root / "owner")})
            with closing(Transport(config, load_contract())) as owner:
                deadline = time.monotonic() + 30
                while True:
                    if process.poll() is not None:
                        raise RuntimeError("The isolated Go instance exited before authentication")
                    try:
                        owner.call("getCurrentSession", {})
                        break
                    except ToolError as exc:
                        if exc.code != "NETWORK_ERROR" or time.monotonic() >= deadline:
                            raise
                        time.sleep(.2)
                library = owner.call("createLibrary", {"Idempotency-Key": "windows-test-library", "body": {"name": "Windows source", "kind": "regular"}})["id"]
                batch = owner.call("createBatch", {"Idempotency-Key": "windows-test-batch", "body": {"label": "Source recovery", "mode": "ordinary", "libraryId": library}})["id"]
                credential = owner.call("createCredential", {"body": {"label": "Synthetic source executor", "batchId": batch,
                                        "scopes": ["read", "ingest", "source.cleanup"]}})
                try:
                    result = asyncio.run(check_chain(root, target, origin, owner, {**credential, "batch": batch, "library": library}))
                finally:
                    owner.call("revokeCredential", {"id": credential["id"]})
        finally:
            process.terminate()
            process.wait(timeout=15)
    print("Windows source recovery:", json.dumps(result, sort_keys=True))
    (repository / "build" / "windows-source-recovery.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
