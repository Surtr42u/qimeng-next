"""Use the Windows Shell recycle operation and its per-item completion receipt."""
from __future__ import annotations

from contextlib import ExitStack
import hashlib
import os
from pathlib import Path
import stat

from .errors import ToolError
from .files import guarded_path, identity

OPEN_REPARSE_POINT = 0x00200000  # FILE_FLAG_OPEN_REPARSE_POINT (winbase.h)


def recover(config, binding, permit):
    # Windows chooses the actual Recycle Bin item name. An interrupted call
    # without its PostDeleteItem receipt must be reconciled by the owner.
    return None


def move(config, binding, permit, arm, observe):
    try:
        import msvcrt
        import pythoncom
        import pywintypes
        import win32con
        import win32file
        from win32com.server.exception import COMException
        from win32com.server.util import wrap
        from win32com.shell import shell, shellcon
    except ImportError:
        return {"outcome": "retained", "code": "windows_shell_unavailable"}

    record = binding["record"]
    path = guarded_path(config, record["storage"], record["relative"])
    root = config.storages[record["storage"]]
    holds = config.state_dir / "recycle-holds"
    hold = holds / permit
    outcome = None
    armed = False
    entered = False
    vetoed = False
    approved = False
    if win32file.GetDriveType(path.anchor) != 3:  # DRIVE_FIXED
        return {"outcome": "retained", "code": "recycle_bin_unavailable"}

    class Sink:
        _com_interfaces_ = [shell.IID_IFileOperationProgressSink]
        _public_methods_ = ["StartOperations", "FinishOperations", "PreRenameItem", "PostRenameItem",
                            "PreMoveItem", "PostMoveItem", "PreCopyItem", "PostCopyItem",
                            "PreDeleteItem", "PostDeleteItem", "PreNewItem", "PostNewItem",
                            "UpdateProgress", "ResetTimer", "PauseTimer", "ResumeTimer"]

        def PreDeleteItem(self, flags, item):
            nonlocal entered, vetoed
            try:
                actual = Path(item.GetDisplayName(shellcon.SIGDN_FILESYSPATH))
                allowed = (flags & shellcon.TSF_DELETE_RECYCLE_IF_POSSIBLE and actual == path
                           and identity(actual.lstat()) == record["identity"])
                if not allowed:
                    raise OSError("native provider cannot honor the recycle-only request")
                arm()
                entered = True
            except Exception:
                vetoed = True
                raise COMException(desc="Recycle operation declined", hresult=-2147467260) from None

        def PostDeleteItem(self, flags, item, hr_delete, newly_created):
            nonlocal outcome
            if hr_delete < 0 or newly_created is None:
                return
            try:
                native_path = Path(newly_created.GetDisplayName(shellcon.SIGDN_FILESYSPATH))
                same_original = os.path.samefile(native_path, hold)
            except (OSError, pywintypes.com_error):
                same_original = False
            if not same_original:
                return
            outcome = {"outcome": "recycled", "providerReceipt": hashlib.sha256(
                f"windows-shell:{permit}:{binding['hash']}:{binding['size']}".encode()).hexdigest()}
            observe(outcome)

        def _ignore(self, *unused):
            pass

        StartOperations = FinishOperations = PreRenameItem = PostRenameItem = _ignore
        PreMoveItem = PostMoveItem = PreCopyItem = PostCopyItem = _ignore
        PreNewItem = PostNewItem = UpdateProgress = ResetTimer = PauseTimer = ResumeTimer = _ignore

    # Deny writes to the open original and hold its ancestors against renames.
    # A private hard link remains available even if the Shell cannot confirm
    # the native receipt. Hold removal only ever targets the executor's own file.
    try:
        holds.mkdir(mode=0o700, exist_ok=True)
        if holds.is_symlink() or holds.stat().st_dev != path.stat().st_dev:
            return {"outcome": "retained", "code": "same_volume_recovery_hold_unavailable"}
        with ExitStack() as handles:
            folder = root
            for piece in ("", *Path(record["relative"]).parts[:-1]):
                folder = folder / piece
                lock = win32file.CreateFile(str(folder), 0, win32con.FILE_SHARE_READ | win32con.FILE_SHARE_WRITE,
                                            None, win32con.OPEN_EXISTING, win32con.FILE_FLAG_BACKUP_SEMANTICS
                                            | OPEN_REPARSE_POINT, None)
                handles.callback(lock.Close)
            handle = win32file.CreateFile(str(path), win32con.GENERIC_READ, win32con.FILE_SHARE_READ | win32con.FILE_SHARE_DELETE,
                                          None, win32con.OPEN_EXISTING, OPEN_REPARSE_POINT, None)
            fd = msvcrt.open_osfhandle(handle.Detach(), os.O_RDONLY | os.O_BINARY)
            stream = handles.enter_context(os.fdopen(fd, "rb"))
            info = os.fstat(fd)
            if (not stat.S_ISREG(info.st_mode) or identity(info) != record["identity"]
                    or hashlib.file_digest(stream, "sha256").hexdigest() != binding["hash"]):
                return {"outcome": "source_changed", "code": "source_identity_changed"}
            try:
                os.link(path, hold, follow_symlinks=False)
            except FileExistsError:
                if not os.path.samefile(hold, path):
                    raise ToolError("CLEANUP_HOLD_CONFLICT", "已有恢复链接指向其他对象")
            if identity(hold.lstat()) != record["identity"]:
                return {"outcome": "source_changed", "code": "source_changed_during_hold"}
            pythoncom.CoInitialize()
            handles.callback(pythoncom.CoUninitialize)
            operation = pythoncom.CoCreateInstance(shell.CLSID_FileOperation, None, pythoncom.CLSCTX_INPROC_SERVER,
                                                   shell.IID_IFileOperation)
            # FOFX_RECYCLEONDELETE, FOFX_EARLYFAILURE, FOF_NO_CONNECTED_ELEMENTS.
            operation.SetOperationFlags(0x00080000 | 0x00100000 | 0x2000 | shellcon.FOF_SILENT
                                        | shellcon.FOF_NOERRORUI | shellcon.FOF_NOCONFIRMATION)
            native_item = shell.SHCreateItemFromParsingName(str(path), None, shell.IID_IShellItem)
            operation.DeleteItem(native_item, wrap(Sink(), iid=shell.IID_IFileOperationProgressSink))
            armed = True
            operation.PerformOperations()
            approved = not operation.GetAnyOperationsAborted() and outcome is not None
    except (OSError, pywintypes.com_error):
        pass
    finally:
        known_preserved = False
        try:
            known_preserved = (not entered and os.path.samefile(hold, path)
                               and identity(hold.lstat()) == record["identity"])
            if (approved or known_preserved) and identity(hold.lstat()) == record["identity"]:
                hold.unlink()
        except OSError:
            pass
    if outcome is not None:
        return outcome
    if not armed or (vetoed and not entered):
        return {"outcome": "retained", "code": "native_recycle_refused"}
    raise ToolError("CLEANUP_OUTCOME_PENDING", "Windows 回收站尚未确认去向，保留恢复链接供本人核对", uncertain=True)
