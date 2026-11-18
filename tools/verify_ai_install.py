"""Install exactly what a new user downloads, then read the installed API contract."""
import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def main():
    if os.environ.get("CI") != "true":
        raise SystemExit("Package installation checks run in cloud CI.")
    root = Path(__file__).resolve().parents[1]
    artifact = root / "build/ai-install"
    expected = hashlib.sha256((root / "api/openapi.yaml").read_bytes()).hexdigest()
    env = os.environ.copy()
    env.pop("PYTHONPATH", None)
    with tempfile.TemporaryDirectory() as temporary:
        venv = Path(temporary) / "client"
        subprocess.run([sys.executable, "-m", "venv", str(venv)], check=True, env=env)
        windows = os.name == "nt"
        scripts = venv / ("Scripts" if windows else "bin")
        python = scripts / ("python.exe" if windows else "python")
        qai = scripts / ("qai.exe" if windows else "qai")
        install = [str(python), "-m", "pip", "install", "--only-binary=:all:", "--require-hashes", "-r", "install.lock"]
        if windows:
            install.extend(["-r", "windows-recycle.lock"])
        subprocess.run(install, cwd=artifact, env=env, check=True)
        subprocess.run([str(python), "-m", "pip", "check"], cwd=temporary, env=env, check=True)
        help_text = subprocess.run([str(qai), "worker", "--help"], cwd=temporary, env=env, check=True,
                                   capture_output=True, encoding="utf-8").stdout
        if "--once" not in help_text or "批次" not in help_text:
            raise SystemExit("The installed worker did not emit its UTF-8 command help.")
        result = subprocess.run([str(python), "-I", "-c",
                                 "from qimeng_ai.transport import load_contract; print(load_contract()['openapiSha256'])"],
                                cwd=temporary, env=env, check=True, text=True, capture_output=True)
        if result.stdout.strip() != expected:
            raise SystemExit("The installed wheel contains a different API contract.")


if __name__ == "__main__":
    main()
