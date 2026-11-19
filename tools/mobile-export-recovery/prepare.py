"""Offline preparation/verification of a private source containing only exported facts."""

import argparse
from contextlib import ExitStack
import ctypes
import hashlib
import json
import os
import re
import secrets
import sys

from export_facts import Rejected, coverage, json_bytes, read_json, require, sha256, validate_export
from originals import (Inventory, absolute, open_directory, overlap, pin_directory, read_selected,
                       roots_digest, selection)


FORMAT = "qimeng-mobile-export-source/v1"
PAYLOAD = ("export.json", "originals.jsonl", "coverage.json")
RECEIPT = "receipt.json"


def digest_arg(value):
    if not re.fullmatch("[0-9A-Fa-f]{64}", value):
        raise argparse.ArgumentTypeError("expected a SHA-256 hex digest")
    return value.lower()


def derive(raw, paths, output):
    document = validate_export(raw)
    media = document["data"]["mediaFiles"]
    inventory = Inventory(media)
    selected = selection(paths, set(inventory.expected))
    with ExitStack() as stack:
        roots = {}
        for name, path in selected.items():
            require(not overlap(path, output), "output: overlaps selected originals")
            require(not any(overlap(path, root.path) for root in roots.values()), "roots: overlapping directories")
            roots[name] = stack.enter_context(open_directory(path))
        counts = inventory.check(roots, hashing=True)
        manifest = b"".join(json_bytes(row) for row in inventory.manifest)
        report = coverage(document, sha256(raw), counts)
        require(counts == inventory.check(roots, hashing=False), "roots: inventory changed")
        for root in roots.values():
            with open_directory(root.path) as current:
                require(os.path.samestat(root.info, current.info), "roots: source directory changed")
    return manifest, report, roots_digest(selected)


def source_receipt(raw, manifest, report, mapping_digest):
    return {
        "format": FORMAT, "scope": "exported_facts_only", "sourceId": report["sourceId"],
        "rootSelectionSHA256": mapping_digest,
        "files": {"export.json": sha256(raw), "originals.jsonl": sha256(manifest),
                  "coverage.json": sha256(json_bytes(report))},
    }


def exact_members(directory):
    with directory.scan() as entries:
        require({entry.name for entry in entries} == {*PAYLOAD, RECEIPT},
                "output: incomplete source or unexpected files")


def same_completed(parent, name, files):
    try:
        parent.stat(name)
    except FileNotFoundError:
        return False
    with pin_directory(parent.path / name, parent) as existing:
        exact_members(existing)
        for filename, expected in files.items():
            with existing.read(filename) as (handle, info):
                require(info.st_size == len(expected)
                        and hashlib.file_digest(handle, "sha256").hexdigest() == sha256(expected),
                        "output: existing evidence differs")
    return True


def rename_new_directory(parent, old, new):
    # POSIX rename() is allowed to replace an *empty* destination directory.
    # Use the platform's atomic no-replace primitive even after the existence check.
    if os.name == "nt":
        os.rename(parent.path / old, parent.path / new)
    elif sys.platform.startswith("linux"):
        libc = ctypes.CDLL(None, use_errno=True)
        renameat2 = getattr(libc, "renameat2", None)
        require(renameat2 is not None, "output: filesystem requires atomic no-replace directory rename")
        renameat2.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
        if renameat2(parent.fd, os.fsencode(old), parent.fd, os.fsencode(new), 1) != 0:
            err = ctypes.get_errno()
            raise OSError(err, "exclusive publication failed")
    else:
        raise Rejected("output: atomic publication is supported on Windows and Linux")


def publish(output, files):
    with open_directory(output.parent) as parent:
        if same_completed(parent, output.name, files):
            return "already_prepared"
        stage_name = ".qimeng-mobile-export-" + secrets.token_hex(12)
        os.mkdir(parent.entry(stage_name), mode=0o700, dir_fd=parent.fd)
        owned = []
        published = False
        try:
            with pin_directory(parent.path / stage_name, parent) as stage:
                stage_id = stage.info
                for filename, value in files.items():
                    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_BINARY", 0)
                    fd = os.open(stage.entry(filename), flags, 0o600, dir_fd=stage.fd)
                    owned.append(filename)
                    with os.fdopen(fd, "wb") as handle:
                        handle.write(value)
                        handle.flush()
                        os.fsync(handle.fileno())
                if stage.fd is not None:
                    os.fsync(stage.fd)
            require(os.path.samestat(parent.stat(stage_name), stage_id), "output: staging directory changed")
            try:
                rename_new_directory(parent, stage_name, output.name)
            except OSError:
                if same_completed(parent, output.name, files):
                    return "already_prepared"
                raise
            published = True
            if parent.fd is not None:
                os.fsync(parent.fd)
            return "prepared"
        finally:
            if not published and owned:
                with pin_directory(parent.path / stage_name, parent) as stage:
                    require(os.path.samestat(stage.info, stage_id), "output: staging directory changed")
                    for filename in owned:
                        os.unlink(stage.entry(filename), dir_fd=stage.fd)
                os.rmdir(parent.entry(stage_name), dir_fd=parent.fd)


def prepare(args):
    source, mapping, output = map(absolute, (args.export, args.roots, args.output))
    require(output.name and not overlap(source, output) and not overlap(mapping, output),
            "output: overlaps selected inputs")
    raw, mappings = read_selected(source), read_selected(mapping)
    require(sha256(raw) == args.export_sha256, "export: independent SHA-256 differs")
    manifest, report, mapping_digest = derive(raw, mappings, output)
    require(read_selected(mapping) == mappings, "roots: selected mapping changed")
    # The selected source may have been replaced while originals were hashed.
    with open_directory(source.parent) as parent:
        with parent.read(source.name) as (handle, _):
            require(hashlib.file_digest(handle, "sha256").hexdigest() == args.export_sha256,
                    "export: selected source changed")
    receipt = json_bytes(source_receipt(raw, manifest, report, mapping_digest))
    files = dict(zip(PAYLOAD, (raw, manifest, json_bytes(report)), strict=True))
    state = publish(output, {**files, RECEIPT: receipt})
    return {"state": state, "scope": "exported_facts_only", "receiptSHA256": sha256(receipt),
            "verifiedFiles": report["originals"]["verifiedFiles"]}


def verify(args):
    source, mapping = map(absolute, (args.source, args.roots))
    with open_directory(source) as directory:
        exact_members(directory)
        encoded = directory.bytes(RECEIPT)
        require(sha256(encoded) == args.receipt_sha256, "receipt: independent SHA-256 differs")
        receipt = read_json(encoded, "receipt")
        require(isinstance(receipt, dict) and receipt.get("format") == FORMAT
                and isinstance(receipt.get("files"), dict) and set(receipt["files"]) == set(PAYLOAD),
                "receipt: format or member list differs")
        files = {name: directory.bytes(name) for name in PAYLOAD}
        for name, value in files.items():
            require(sha256(value) == receipt["files"][name], "receipt: member SHA-256 differs")
        mappings = read_selected(mapping)
        manifest, report, mapping_digest = derive(files["export.json"], mappings, source)
        require(read_selected(mapping) == mappings, "roots: mapping changed")
        require(manifest == files["originals.jsonl"], "originals: identity or SHA-256 differs")
        old_report = read_json(files["coverage.json"], "coverage")
        same_roots = mapping_digest == receipt.get("rootSelectionSHA256")
        if not same_roots:
            # A relocated copy may omit ancillary files or scanner-excluded cache directories.
            # Their old counts remain observations of the original selected roots.
            for key in ("nonMediaFiles", "oldScannerExcludedEntries"):
                require(isinstance(old_report, dict) and isinstance(old_report.get("originals"), dict)
                        and type(old_report["originals"].get(key)) is int and old_report["originals"][key] >= 0,
                        "coverage: invalid inventory")
                report["originals"][key] = old_report["originals"][key]
        require(json_bytes(report) == files["coverage.json"], "coverage: differs from exported facts")
        expected = source_receipt(files["export.json"], manifest, report, receipt.get("rootSelectionSHA256"))
        require(re.fullmatch("[a-f0-9]{64}", str(expected["rootSelectionSHA256"]))
                and json_bytes(expected) == encoded, "receipt: source metadata differs")
    return {"state": "verified", "scope": "exported_facts_only", "receiptSHA256": args.receipt_sha256,
            "verifiedFiles": report["originals"]["verifiedFiles"], "sameRootSelection": same_roots}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="action", required=True)
    make = commands.add_parser("prepare", help="prepare in a new private directory")
    make.add_argument("--export", required=True, help="complete exported v2 JSON")
    make.add_argument("--export-sha256", required=True, type=digest_arg, help="independent export SHA-256")
    make.add_argument("--roots", required=True, help="local JSON mapping exported library names to absolute roots")
    make.add_argument("--output", required=True, help="new output directory outside original roots")
    check = commands.add_parser("verify", help="reread the evidence and all selected originals")
    check.add_argument("--source", required=True)
    check.add_argument("--receipt-sha256", required=True, type=digest_arg)
    check.add_argument("--roots", required=True)
    args = parser.parse_args()
    try:
        result = prepare(args) if args.action == "prepare" else verify(args)
    except Rejected as error:
        print(str(error), file=sys.stderr)
        return 1
    except OSError as error:
        # OS errors can contain absolute paths or private media filenames.
        print(f"filesystem operation failed (errno={error.errno})", file=sys.stderr)
        return 1
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
