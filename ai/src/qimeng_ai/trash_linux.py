"""Freedesktop home trash. A source on a different volume stays in place."""
from __future__ import annotations

from contextlib import ExitStack, contextmanager
import ctypes
from datetime import datetime
import errno
import hashlib
import os
from pathlib import Path
import stat
from urllib.parse import quote_from_bytes

from .errors import ToolError
from .files import guarded_path, identity

_DIR = os.O_DIRECTORY | os.O_NOFOLLOW | os.O_RDONLY


def _outcome(kind, code):
    return {"outcome": kind, "code": code}


def _proof(permit, binding):
    return {"outcome": "recycled", "providerReceipt": hashlib.sha256(
        f"freedesktop:{permit}:{binding['hash']}:{binding['size']}".encode()).hexdigest()}


def _no_replace():
    try:
        function = ctypes.CDLL(None, use_errno=True).renameat2
    except AttributeError:
        return None
    function.argtypes = (ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint)
    function.restype = ctypes.c_int
    def rename(source_dir, name, dest_dir, dest_name):
        if function(source_dir, os.fsencode(name), dest_dir, os.fsencode(dest_name), 1):
            raise OSError(ctypes.get_errno(), "Atomic no-replace move was refused")
    return rename


def _trash_path():
    data = Path(os.environ.get("XDG_DATA_HOME") or Path.home() / ".local/share")
    if not data.is_absolute():
        raise OSError(errno.EINVAL, "Invalid XDG data directory")
    return data / "Trash"


@contextmanager
def _directories(binding, config, *, create):
    record = binding["record"]
    storage = config.storages[record["storage"]]
    relative = Path(record["relative"])
    trash = _trash_path()
    if relative.is_absolute() or not relative.parts or ".." in relative.parts:
        raise ToolError("PATH_DENIED", "原始来源引用无效")
    if (storage / relative).resolve().is_relative_to(trash.resolve()):
        raise ToolError("PATH_DENIED", "回收站中的文件不能再次作为来源")
    with ExitStack() as stack:
        def open_dir(path, parent=None):
            fd = os.open(path, _DIR, dir_fd=parent)
            stack.callback(os.close, fd)
            return fd
        parent = open_dir(storage)
        for piece in relative.parts[:-1]:
            parent = open_dir(piece, parent)
        if create:
            trash.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
            trash.mkdir(mode=0o700, exist_ok=True)
        root = open_dir(trash)
        dirs = []
        for sub in ("info", "files"):
            if create:
                try:
                    os.mkdir(sub, 0o700, dir_fd=root)
                except FileExistsError:
                    pass
            dirs.append(open_dir(sub, root))
        for fd in (root, *dirs):
            st = os.fstat(fd)
            if st.st_uid != os.getuid() or st.st_mode & 0o077:
                raise OSError(errno.EACCES, "Trash directory is not private")
        if os.fstat(dirs[1]).st_dev != record["identity"][0]:
            raise OSError(errno.EXDEV, "No verified same-volume trash")
        yield parent, relative.name, dirs[0], dirs[1]


def _matches(directory, name, binding):
    try:
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
    except FileNotFoundError:
        return False
    with os.fdopen(fd, "rb") as stream:
        before = os.fstat(fd)
        return (stat.S_ISREG(before.st_mode) and identity(before) == binding["record"]["identity"]
                and hashlib.file_digest(stream, "sha256").hexdigest() == binding["hash"]
                and identity(os.fstat(fd)) == identity(before))


def _original_line(config, binding):
    record = binding["record"]
    absolute = config.storages[record["storage"]] / record["relative"]
    return "Path=" + quote_from_bytes(os.fsencode(absolute), safe="/")


def recover(config, binding, permit):
    """A matching native Trash object and its original-location record prove the move."""
    slot = "qimeng-" + permit
    try:
        with _directories(binding, config, create=False) as (_, _, info, files):
            fd = os.open(slot + ".trashinfo", os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=info)
            with os.fdopen(fd, "rb") as stream:
                if not stat.S_ISREG(os.fstat(fd).st_mode):
                    return None
                lines = stream.read(32_769).decode("ascii").splitlines()
            if (lines[:2] == ["[Trash Info]", _original_line(config, binding)]
                    and _matches(files, slot, binding)):
                return _proof(permit, binding)
    except (OSError, ValueError, ToolError):
        return None
    return None


def move(config, binding, permit, arm, observe):
    record = binding["record"]
    rename = _no_replace()
    if rename is None:
        return _outcome("retained", "atomic_recycle_unavailable")
    slot = "qimeng-" + permit
    side_effect_started = False
    try:
        guarded_path(config, record["storage"], record["relative"])
        with _directories(binding, config, create=True) as (parent, name, info, files):
            if not _matches(parent, name, binding):
                return _outcome("source_changed", "source_identity_changed")
            arm()
            side_effect_started = True
            date = datetime.now().strftime("%Y-%m-%dT%H:%M:%S")
            metadata = f"[Trash Info]\n{_original_line(config, binding)}\nDeletionDate={date}\n".encode("ascii")
            fd = os.open(slot + ".trashinfo", os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600, dir_fd=info)
            with os.fdopen(fd, "wb") as output:
                output.write(metadata)
                output.flush()
                os.fsync(fd)
            os.fsync(info)
            if identity(os.stat(name, dir_fd=parent, follow_symlinks=False)) != record["identity"]:
                os.unlink(slot + ".trashinfo", dir_fd=info)
                return _outcome("source_changed", "source_identity_changed")
            try:
                rename(parent, name, files, slot)
            except OSError:
                # renameat2 did not transfer an object, so the info we just
                # created has no payload. Never unlink a source or Trash file.
                os.unlink(slot + ".trashinfo", dir_fd=info)
                return _outcome("retained", "native_recycle_refused")
            os.fsync(parent)
            os.fsync(files)
            if not _matches(files, slot, binding):
                # The namespace changed between stat and rename. Only return
                # the captured object if the original name is still vacant.
                rename(files, slot, parent, name)
                os.fsync(parent)
                os.fsync(files)
                os.unlink(slot + ".trashinfo", dir_fd=info)
                return _outcome("source_changed", "source_changed_during_move")
            outcome = _proof(permit, binding)
            observe(outcome)
            return outcome
    except OSError:
        if side_effect_started:
            raise ToolError("CLEANUP_OUTCOME_PENDING", "需从本机回收站核对原操作，来源不会被再次移动", uncertain=True) from None
        return _outcome("retained", "safe_native_trash_unavailable")
