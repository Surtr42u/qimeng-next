"""Read frozen local inputs. No source directory is opened for writing."""

from contextlib import contextmanager, ExitStack
import hashlib
import os
from pathlib import Path
import stat

from export_facts import MEDIA_TYPES, Rejected, identity, json_bytes, read_json, require, sha256


def absolute(value):
    require(identity(value), "path: an absolute local path is required")
    path = Path(value)
    require(path.is_absolute() and ".." not in path.parts and not str(path).startswith("\\\\"),
            "path: an absolute local path is required")
    return path


def is_link(info):
    return (stat.S_ISLNK(info.st_mode) or
            bool(getattr(info, "st_file_attributes", 0) & stat.FILE_ATTRIBUTE_REPARSE_POINT))


def file_state(info):
    return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


if os.name == "nt":
    import ctypes
    from ctypes import wintypes

    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    create_file = kernel.CreateFileW
    create_file.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, wintypes.DWORD, wintypes.LPVOID,
                            wintypes.DWORD, wintypes.DWORD, wintypes.HANDLE]
    create_file.restype = wintypes.HANDLE
    close_handle = kernel.CloseHandle
    close_handle.argtypes = [wintypes.HANDLE]
    close_handle.restype = wintypes.BOOL


@contextmanager
def pin_directory(path, parent=None):
    # On Windows an ordinary directory handle opened without FILE_SHARE_DELETE
    # pins this component while child paths are in use, including output publication.
    if os.name == "nt":
        handle = create_file(str(path), 0x80, 3, None, 3, 0x02200000, None)
        if handle == wintypes.HANDLE(-1).value:
            raise OSError(ctypes.get_last_error(), "directory unavailable")
        try:
            info = path.lstat()
            require(stat.S_ISDIR(info.st_mode) and not is_link(info), "path: linked or special directory")
            yield Directory(path, None, info)
        finally:
            close_handle(handle)
    else:
        flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
        fd = os.open(path if parent is None else path.name, flags,
                     dir_fd=None if parent is None else parent.fd)
        try:
            yield Directory(path, fd, os.fstat(fd))
        finally:
            os.close(fd)


@contextmanager
def open_directory(path):
    with ExitStack() as stack:
        directory = None
        for part in (*reversed(path.parents), path):
            directory = stack.enter_context(pin_directory(part, directory))
        yield directory


class Directory:
    def __init__(self, path, fd, info):
        self.path = path
        self.fd = fd
        self.info = info

    def stat(self, name):
        if self.fd is None:
            return (self.path / name).lstat()
        return os.stat(name, dir_fd=self.fd, follow_symlinks=False)

    def entry(self, name):
        return self.path / name if self.fd is None else name

    @contextmanager
    def scan(self):
        if self.fd is None:
            with os.scandir(self.path) as entries:
                yield entries
        else:
            fd = os.open(".", os.O_RDONLY | os.O_DIRECTORY, dir_fd=self.fd)
            try:
                with os.scandir(fd) as entries:
                    yield entries
            finally:
                os.close(fd)

    @contextmanager
    def read(self, name):
        before = self.stat(name)
        require(stat.S_ISREG(before.st_mode) and not is_link(before), "file: link or special file")
        flags = os.O_RDONLY | getattr(os, "O_BINARY", 0) | getattr(os, "O_NONBLOCK", 0)
        flags |= getattr(os, "O_NOFOLLOW", 0)
        fd = os.open(self.entry(name), flags, dir_fd=self.fd)
        with os.fdopen(fd, "rb") as handle:
            require(file_state(before) == file_state(os.fstat(fd)), "file: changed before opening")
            yield handle, before
            require(file_state(before) == file_state(os.fstat(fd)) == file_state(self.stat(name)),
                    "file: changed during reading")

    def bytes(self, name):
        with self.read(name) as (handle, info):
            data = handle.read(info.st_size + 1)
            require(len(data) == info.st_size, "file: size changed during reading")
            return data


def read_selected(path):
    with open_directory(path.parent) as parent:
        return parent.bytes(path.name)


def selection(mapping, names):
    values = read_json(mapping, "roots")
    require(isinstance(values, dict) and set(values) == names,
            "roots: exactly one entry per exported library name is required")
    return {name: absolute(value) for name, value in values.items()}


def overlap(first, second):
    return first.is_relative_to(second) or second.is_relative_to(first)


def roots_digest(paths):
    return sha256(json_bytes({name: str(path) for name, path in paths.items()}))


class Inventory:
    def __init__(self, media):
        self.expected = {}
        for ordinal, row in enumerate(media):
            self.expected.setdefault(row["libraryName"], {})[row["relPath"]] = (ordinal, row)
        self.manifest = [None] * len(media)
        self.stamps = {}
        self.physical = set()

    def check(self, roots, *, hashing):
        counts = {"nonMediaFiles": 0, "oldScannerExcludedEntries": 0}
        directories = set()
        found = set()

        def visit(library, directory, prefix):
            directory_id = directory.info.st_dev, directory.info.st_ino
            require(directory_id not in directories, "roots: duplicate physical directory")
            directories.add(directory_id)
            with directory.scan() as entries:
                for entry in entries:
                    rel = prefix + entry.name
                    if entry.name.startswith("."):
                        counts["oldScannerExcludedEntries"] += 1
                        continue
                    info = directory.stat(entry.name)
                    if stat.S_ISDIR(info.st_mode) and entry.name.lower() in {
                            "#recycle", "$recycle.bin", "_gsdata_", "system volume information"}:
                        counts["oldScannerExcludedEntries"] += 1
                        continue
                    location = sha256((library + "\x00" + rel).encode("utf-8"))
                    require(not is_link(info), f"roots: linked entry at locator {location}")
                    if stat.S_ISDIR(info.st_mode):
                        with pin_directory(directory.path / entry.name, directory) as child:
                            require(os.path.samestat(info, child.info), "roots: directory changed")
                            visit(library, child, rel + "/")
                        continue
                    require(stat.S_ISREG(info.st_mode), f"roots: special file at locator {location}")
                    if rel not in self.expected[library]:
                        require(Path(entry.name).suffix.lower() not in MEDIA_TYPES,
                                f"roots: unexported media at locator {location}")
                        counts["nonMediaFiles"] += 1
                        continue
                    ordinal, row = self.expected[library][rel]
                    where = f"mediaFiles[{ordinal}]"
                    require(ordinal not in found, f"{where}: multiple matching files")
                    found.add(ordinal)
                    stamp = file_state(info)
                    require(stamp[2] == row["sizeBytes"] and stamp[3] // 1_000_000 == row["modifiedAtMillis"],
                            f"{where}: original size or millisecond mtime differs")
                    if hashing:
                        physical = info.st_dev, info.st_ino
                        require(physical not in self.physical, f"{where}: multiple IDs refer to one physical file")
                        self.physical.add(physical)
                        self.stamps[ordinal] = stamp
                        with directory.read(entry.name) as (source, opened):
                            require(file_state(opened) == stamp, f"{where}: original changed before hashing")
                            digest = hashlib.file_digest(source, "sha256").hexdigest()
                        record = {key: row[key] for key in (
                            "assetId", "recordKey", "libraryName", "relPath", "sizeBytes", "modifiedAtMillis")}
                        self.manifest[ordinal] = {**record, "sha256": digest}
                    else:
                        require(self.stamps[ordinal] == stamp, f"{where}: original changed after hashing")

        for library, root in roots.items():
            visit(library, root, "")
        if len(found) != len(self.manifest):
            missing = next(i for i in range(len(self.manifest)) if i not in found)
            raise Rejected(f"mediaFiles[{missing}]: original missing from its selected root")
        return {"verifiedFiles": len(found), "verifiedBytes": sum(row["sizeBytes"] for row in self.manifest),
                **counts}
