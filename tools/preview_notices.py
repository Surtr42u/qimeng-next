"""Stage and verify the upstream notices distributed with the Go preview decoders."""
import hashlib
from pathlib import Path
import shutil
import sys

DIRECTORY = "third-party/preview-decoders"
DEPLOY = Path(__file__).resolve().parents[1] / "deploy"
FILES = ("README.md", "LICENSE", "LICENSE.GOLANG", "COPYING.IJG", "IJG-README.txt",
         "LICENSE.imaging", "SOURCE-NOTICES.txt")


def copy_to(root: Path) -> None:
    target = root / DIRECTORY
    target.mkdir(parents=True, exist_ok=True)
    for name in FILES:
        shutil.copyfile(DEPLOY / DIRECTORY / name, target / name)


def verify(root) -> dict[str, str]:
    """Check a filesystem or open zipfile.Path and return package-relative SHA-256s."""
    hashes = {}
    for name in FILES:
        relative = f"{DIRECTORY}/{name}"
        data = (root / relative).read_bytes()
        if data != (DEPLOY / relative).read_bytes():
            raise ValueError(f"Packaged decoder notice differs from its source: {relative}")
        hashes[relative] = hashlib.sha256(data).hexdigest()
    return hashes


if __name__ == "__main__":
    copy_to(Path(sys.argv[1]))
