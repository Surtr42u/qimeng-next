"""Construct temporary Go overlays on the two pinned CI checkouts."""

import json
import os
from pathlib import Path
import re
import shutil
import sys


class Overlay:
    def __init__(self, server, output, module):
        self.server = server.resolve()
        self.output = output
        self.module = module
        self.sources = {}

    def function(self, path, name, *, enter="", changes=()):
        if path not in self.sources:
            source = (self.server / path).read_text()
            source, n = re.subn(r"(?m)^package \w+\n",
                                r'\g<0>' + f'\nimport scanprobe "{self.module}/internal/scanprobe"\n',
                                source, count=1)
            assert n == 1, path
            self.sources[path] = source
        source = self.sources[path]
        pattern = rf"(?m)^func (?:\([^\n]*?\) )?{re.escape(name)}\([^\n]*\) [^\n]*\{{$|^func (?:\([^\n]*?\) )?{re.escape(name)}\([^\n]*\) \{{$"
        headers = list(re.finditer(pattern, source))
        assert len(headers) == 1, (path, name, len(headers))
        body_start = headers[0].end()
        body_end = source.index("\n}", body_start) + 2
        body = source[body_start:body_end]
        for old, new in changes:
            assert body.count(old) == 1, (path, name, old, body.count(old))
            body = body.replace(old, new, 1)
        if enter:
            body = "\n\t" + enter + body
        self.sources[path] = source[:body_start] + body + source[body_end:]

    def save(self, name):
        package = self.server / "internal/scanprobe"
        package.mkdir()
        shutil.copyfile(Path(__file__).with_name("probe.go.in"), package / "probe.go")
        replace = {}
        for source, code in self.sources.items():
            dest = self.output / name / source
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_text(code)
            replace[str(self.server / source)] = str(dest)
        (self.output / (name + ".json")).write_text(json.dumps({"Replace": replace}))


def scope(label):
    return f'defer scanprobe.End("{label}", scanprobe.Now())'


def around(statement, label, var="stageClock"):
    return statement, f'{var} := scanprobe.Now()\n\t{statement}\n\tscanprobe.End("{label}", {var})'


def lock_wait(statement):
    return around(statement, "writer.wait", "lockClock")


def commit_probe():
    return ('if err := tx.Commit(); err != nil {',
            'commitClock := scanprobe.Now()\n\tcommitErr := tx.Commit()\n'
            '\tscanprobe.End("asset.tx_commit", commitClock)\n\tif err := commitErr; err != nil {')


def add_next(p):
    api = "internal/httpapi/scan_routes.go"
    p.function(api, "StartLibraryScan", enter='scanprobe.ManualPOST()\n\tdefer scanprobe.Point("post.exit")')
    p.function(api, "GetLibraryScan", enter=scope("poll.handler"), changes=[
        ("return a.s.Scan(r.Context(), id)", '''
            stamp := scanprobe.Now()
            job, err := a.s.Scan(r.Context(), id)
            scanprobe.End("poll.read_job", stamp)
            if err == nil { scanprobe.Point("poll.state." + job.State) }
            return job, err
        ''')])

    main = "internal/service/scans.go"
    progress = "internal/service/scan_progress.go"
    missing = "internal/service/scan_missing.go"
    batch = "internal/service/scan_batch.go"
    p.function(main, "startScan", changes=[('now := time.Now().UTC()', 'scanprobe.Point("job.created_clock")\n\tnow := time.Now().UTC()')])
    p.function(progress, "launchScan", enter='scanprobe.Point("job.launch")')
    p.function(main, "scanLibrary", enter='scanprobe.Point("worker.enter")\n\tdefer scanprobe.Point("worker.return")', changes=[
        ("e = s.walkLibrary(ctx, run,", "inventoryClock := scanprobe.Now()\n\t\t\te = s.walkLibrary(ctx, run,"),
        ("if e == nil {\n\t\t\t\te = s.scanMissing", 'scanprobe.End("walk", inventoryClock)\n\t\t\tif e == nil {\n\t\t\t\te = s.scanMissing')])
    p.function(main, "saveScan", changes=[
        ('_, e = q.ExecContext(ctx,', 'var terminalClock int64\n\tif j.State == "completed" { terminalClock = scanprobe.Now() }\n\t_, e = q.ExecContext(ctx,'),
        ('\n\treturn e\n}', '\n\tif e == nil { scanprobe.End("terminal.sql", terminalClock) }\n\treturn e\n}')])
    p.function(main, "finishScan", changes=[lock_wait("s.mu.Lock()"),
        ("j.UpdatedAt = time.Now().UTC()", 'scanprobe.Point("job.updated_clock")\n\tj.UpdatedAt = time.Now().UTC()')])
    p.function("internal/service/settings.go", "admitScan", changes=[lock_wait("s.mu.Lock()")])
    p.function(main, "scanProgress", changes=[lock_wait("s.mu.Lock()")])
    p.function(progress, "skipScannedEntry", changes=[lock_wait("s.mu.Lock()")])
    p.function(progress, "scanSignatures", changes=[
        ("raw, err := json.Marshal(paths)", scope("signatures") + "\n\traw, err := json.Marshal(paths)")])
    p.function("internal/service/scan_known.go", "knownDirectoryEntries", changes=[
        ("if err := ctx.Err(); err != nil {", scope("trusted_dir") + "\n\tif err := ctx.Err(); err != nil {")])
    p.function("internal/service/scan_inspection.go", "inspectScanOriginal", enter=scope("inspection"), changes=[
        around("file.hash, file.size, err = hashReaderContext(ctx, original, run.hashBuffer)", "inspection.hash", "hashClock"),
        ("if err = s.probeMedia(ctx, input, &file.probe); err != nil {",
         'probeClock := scanprobe.Now()\n\terr = s.probeMedia(ctx, input, &file.probe)\n\tscanprobe.End("inspection.probe", probeClock)\n\tif err != nil {')])
    for file, name in ((main, "commitScannedFile"), (batch, "commitFreshScanPrefix")):
        p.function(file, name, enter=scope("publish"), changes=[lock_wait("s.mu.Lock()")])
    p.function(progress, "commitFileProgress", changes=[commit_probe()])
    p.function(main, "saveScannedFile", changes=[
        ("e = store.UpdateScannedFile(ctx, tx, a)", '''rowClock := scanprobe.Now()
            e = store.UpdateScannedFile(ctx, tx, a)
            if moved { scanprobe.End("move.file_update", rowClock) } else { scanprobe.End("rewrite.file_update", rowClock) }''')])
    # The original ExecContext and error path stay intact. Obtain the affected-row
    # count from its returned driver result only after timestamping statement return.
    p.function("internal/store/asset_file.go", "saveAssetFile", changes=[
        ("if _, err := q.ExecContext(ctx, version,", "versionClock := scanprobe.Now()\n\tif result, err := q.ExecContext(ctx, version,"),
        ("\n\t}\n\tvar mtime any", '''
        } else if versionClock != 0 {
            end := scanprobe.Now()
            count, countErr := result.RowsAffected()
            label := "version_sql.one"
            if countErr != nil || (count != 0 && count != 1) { label = "version_sql.unexpected" } else if count == 0 { label = "version_sql.zero" }
            scanprobe.Record(label, versionClock, end)
        }
        var mtime any''')])
    p.function(missing, "scanMissing", enter=scope("reconcile"), changes=[
        ("\n\t\terr := s.DB.DB.QueryRowContext(ctx,", "\n\t\tseekClock := scanprobe.Now()\n\t\terr := s.DB.DB.QueryRowContext(ctx,"),
        (".Scan(&parent)", '.Scan(&parent)\n\t\tscanprobe.End("reconcile.parent_seek", seekClock)')])
    p.function(missing, "scanDirectoryPage", enter=scope("reconcile.page"))
    p.function(missing, "readScanDirectory", enter=scope("reconcile.list"))
    p.function(missing, "confirmMissingPaths", changes=[lock_wait("s.mu.Lock()")])


def add_installed(p):
    api = "internal/httpapi/libraries.go"
    p.function(api, "PostApiV1LibrariesLibraryIdScan", enter='scanprobe.ManualPOST()\n\tdefer scanprobe.Point("post.exit")')
    p.function(api, "set", changes=[("m.m[id] = state", 'm.m[id] = state\n\tif state == gen.LibraryScanStateIdle { scanprobe.Point("terminal.idle") }')])
    p.function(api, "FinishScan", changes=[("s.scanStates.set(libraryID, state)",
        's.scanStates.set(libraryID, state)\n\tscanprobe.Point("idle.unlock_returned")')])
    p.function(api, "GetApiV1Libraries", enter=scope("poll.handler"), changes=[
        around("libs, err := s.q.ListLibraries(r.Context())", "poll.list_libraries", "listClock"),
        around("countRows, err := s.q.CountAllLibrariesMedia(r.Context())", "poll.count_media", "countClock"),
        ("state := s.scanStates.get(l.ID)", 'state := s.scanStates.get(l.ID)\n\t\tscanprobe.Point("poll.state." + string(state))')])
    main = "internal/scanner/scanner.go"
    p.function(main, "beginScan", changes=[lock_wait("g.mu.Lock()")])
    p.function(main, "Scan", enter='scanprobe.Point("worker.enter")\n\tdefer scanprobe.Point("worker.return")', changes=[
        ("existing, err := s.q.ListScanAssetSnapshots(ctx, lib.ID)",
         'snapshotClock := scanprobe.Now()\n\texisting, err := s.q.ListScanAssetSnapshots(ctx, lib.ID)\n\tscanprobe.End("snapshot.query", snapshotClock)'),
        ("totalHint := len(existing)", 'scanprobe.End("signatures", snapshotClock)\n\ttotalHint := len(existing)'),
        ("err = filepath.WalkDir", "walkClock := scanprobe.Now()\n\terr = filepath.WalkDir"),
        ("\n\t})\n\tif err != nil {", '\n\t})\n\tscanprobe.End("walk", walkClock)\n\tif err != nil {')])
    gate = ('err := filing.WithLibraryGate(lib.ID, func() error {',
            'gateClock := scanprobe.Now()\n\terr := filing.WithLibraryGate(lib.ID, func() error {\n\t\tscanprobe.End("writer.wait", gateClock)')
    p.function(main, "ingestFile", enter=scope("publish"), changes=[gate])
    p.function("internal/scanner/reconcile.go", "reconcile", enter=scope("reconcile"), changes=[gate])
    p.function("internal/scanner/enrich.go", "runAssetTx", changes=[commit_probe()])


if __name__ == "__main__":
    assert os.getenv("GITHUB_ACTIONS") == "true"
    next_root, installed_root, output = map(lambda arg: Path(arg).resolve(), sys.argv[1:])
    for root, module, name, instrument in [
        (next_root, "qimeng.local/next/server", "next", add_next),
        (installed_root, "qimeng-media/server", "installed", add_installed),
    ]:
        overlay = Overlay(root, output, module)
        instrument(overlay)
        overlay.save(name)
