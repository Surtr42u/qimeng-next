"""Collect the unmodified installed application's presentation on the dedicated hosted AVD."""

import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import urllib.request
import xml.etree.ElementTree as ET

from PIL import Image


PACKAGE = "media.qimeng.app"
SERIAL = "emulator-5556"
SOURCE = "745974081716a9d69a95002a41220e35e743d764"
PICTURE = "android-synthetic.png"
VIDEO = "android-synthetic.mp4"
TAG = "安卓云端标签"
NAV = ["首页", "相册", "数据", "我的"]


def adb(*arguments):
    return subprocess.check_output(["adb", "-s", SERIAL, *arguments], timeout=25)


def shell(*arguments):
    return adb("shell", *arguments).decode().strip()


def sha256(path):
    with path.open("rb") as handle:
        return hashlib.file_digest(handle, "sha256").hexdigest()


def api(method, route, token="", data=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    body = None if data is None else json.dumps(data, ensure_ascii=False).encode()
    request = urllib.request.Request("http://127.0.0.1:18483/api/v1/" + route, body, headers, method=method)
    with urllib.request.urlopen(request, timeout=8) as response:
        content = response.read(2_000_000)
    return json.loads(content) if content else None


def seed(media):
    # Matches AndroidRealServerTest's 160×240 Canvas, rectangular halves and solid white circle.
    picture = Image.new("RGB", (160, 240))
    picture.putdata([
        (255, 255, 255) if (x + 0.5 - 80) ** 2 + (y + 0.5 - 120) ** 2 < 46 ** 2 else
        (72, 112, 190) if x < 80 else (224, 180, 112)
        for y in range(240) for x in range(160)
    ])
    picture.save(media / PICTURE)
    token = api("POST", "auth/dev-login")["token"]
    library = api("POST", "libraries", token, {"name": "Android 云端合成影库", "rootPath": str(media)})
    api("POST", f"libraries/{library['id']}/scan", token)
    expires = time.monotonic() + 75
    while time.monotonic() < expires:
        assets = api("GET", "assets?libraryId=" + library["id"], token)["items"] or []
        if {item["fileName"] for item in assets} == {PICTURE, VIDEO}:
            break
        time.sleep(1)
    else:
        raise RuntimeError("The legacy Go scan did not expose both synthetic assets")
    api("POST", "tags", token, {"name": TAG})
    return {name: sha256(media / name) for name in (PICTURE, VIDEO)}


def tree():
    # UIAutomator's success text is separate from the file, including on API 35.
    path = "/data/local/tmp/qimeng-baseline-window.xml"
    result = shell("uiautomator", "dump", path)
    if "dumped to" not in result.lower():
        return []
    xml = ET.fromstring(adb("exec-out", "cat", path))
    return [node for node in xml.iter("node") if node.get("package") == PACKAGE]


def bounds(node):
    return tuple(map(int, re.findall(r"-?\d+", node.get("bounds", ""))))


def matching(nodes, label):
    def matches(node):
        for attribute in ("text", "content-desc"):
            words = re.split(r"[,\n]", node.get(attribute, ""))
            if any(word.strip() == label for word in words):
                return True
        return False
    return [node for node in nodes if matches(node) and len(bounds(node)) == 4]


def wait_for(label, *, scroll=False, match=None, timeout=35, stable=False):
    deadline = time.monotonic() + timeout
    previous = None
    while time.monotonic() < deadline:
        nodes = tree()
        candidates = match(nodes) if match else matching(nodes, label)
        for node in candidates:
            rect = bounds(node)
            left, top, right, bottom = rect
            if right <= left or bottom - top < 10 or not (0 <= (left + right) // 2 < 320 and 0 <= (top + bottom) // 2 < 640):
                continue
            if not stable or rect == previous:
                return node
            previous = rect
            break
        else:
            previous = None
            if scroll and nodes:
                shell("input", "swipe", "160", "475", "160", "210", "350")
        time.sleep(0.4)
    raise RuntimeError(f"Expected production UI control was not visible: {label}")


def tap(label, **options):
    left, top, right, bottom = bounds(wait_for(label, **options))
    shell("input", "tap", str((left + right) // 2), str((top + bottom) // 2))


def capture_image():
    image = Image.open(io.BytesIO(adb("exec-out", "screencap", "-p"))).convert("RGB")
    if image.size != (320, 640):
        raise RuntimeError(f"Unexpected screenshot dimensions: {image.size}")
    return image


def has_picture(image, box):
    colors = list(image.crop(box).getdata())
    blue = sum(b > r + 50 and b > g + 35 for r, g, b in colors)
    sand = sum(r > b + 50 and g > b + 30 for r, g, b in colors)
    return blue >= 20 and sand >= 20


def capture_home(output, material):
    wait_for(PICTURE)
    wait_for(VIDEO)
    deadline = time.monotonic() + 35
    while time.monotonic() < deadline:
        nodes = tree()
        names = [PICTURE, VIDEO, *NAV]
        found = {name: matching(nodes, name) for name in names}
        if all(found.values()):
            geometry = {name: bounds(found[name][-1]) for name in names}
            frame = capture_image()
            if has_picture(frame, geometry[PICTURE]):
                frame.save(output / f"installed-home-{material}.png")
                frame.crop((0, 508, 320, 640)).save(output / f"installed-navigation-{material}.png")
                return geometry
        time.sleep(0.6)
    raise RuntimeError(f"{material} home did not display the real two-color fixture and four tab labels")


def boot_and_login():
    shell("wm", "size", "320x640")
    shell("wm", "density", "160")
    shell("settings", "put", "system", "font_scale", "1.0")
    shell("cmd", "uimode", "night", "no")
    shell("am", "start", "-W", "-n", PACKAGE + "/.MainActivity")
    tap("NAS / 电脑")
    tap("手动填写地址", scroll=True)
    tap("server address", scroll=True, match=lambda nodes: [
        n for n in nodes if n.get("class") == "android.widget.EditText" and n.get("password") != "true"
    ])
    shell("input", "keycombination", "113", "29")
    shell("input", "keyevent", "67")
    shell("input", "text", "http://127.0.0.1:18483")
    wait_for("http://127.0.0.1:18483")
    if re.search(r"(?:mInputShown|inputShown)=true", shell("dumpsys", "input_method")):
        shell("input", "keyevent", "BACK")
    # Discovery disables the button while its label remains visible. The IME and the discovery
    # result can also move the form between a UIAutomator dump and a coordinate tap.
    tap("连接影库", scroll=True, stable=True, match=lambda nodes: [
        node for node in nodes if node.get("enabled") == "true" and node.get("clickable") == "true"
        and matching(node.iter("node"), "连接影库")
    ])
    wait_for(PICTURE, timeout=75)


def run(work, output, mode):
    manifest = {
        "sourceRevision": SOURCE,
        "sourceAndroidTree": subprocess.check_output(
            ["git", "-C", str(work / "source"), "rev-parse", "HEAD:android"], text=True
        ).strip(),
        "installedBuild": mode,
        "installedApkSha256": (work / "installed.sha256").read_text().strip(),
        "releaseArtifactId": 14228034776,
        "releaseRun": 43873579739,
        "runId": os.environ["GITHUB_RUN_ID"],
        "apiLevel": shell("getprop", "ro.build.version.sdk"),
        "systemImage": shell("getprop", "ro.build.fingerprint"),
        "abis": shell("getprop", "ro.product.cpu.abilist"),
        "syntheticMediaSha256": seed(work / "media"),
        "geometry": {},
    }
    stage = "login"
    try:
        boot_and_login()
        manifest["display"] = {
            "size": shell("wm", "size"), "density": shell("wm", "density"),
            "fontScale": shell("settings", "get", "system", "font_scale"),
            "navigationMode": shell("settings", "get", "secure", "navigation_mode"),
            "night": shell("cmd", "uimode", "night"),
            "animatorScale": shell("settings", "get", "global", "animator_duration_scale"),
        }
        stage = "default-home"
        manifest["geometry"]["liquid"] = capture_home(output, "liquid")
        stage = "tag-sheet"
        tap(PICTURE)
        tap("标签")
        tap(TAG)
        wait_for("标签管理")
        wait_for("已选 1", match=lambda nodes: [n for n in nodes if re.search(r"已选\s*1", n.get("text", ""))])
        capture_image().save(output / "installed-tag-sheet.png")
        tap("保存")
        wait_for("标签")
        shell("input", "keyevent", "BACK")
        wait_for(PICTURE)
        stage = "classic-appearance"
        tap("我的")
        tap("外观", scroll=True)
        tap("经典", scroll=True)
        # Appearance is an inline subpage of the profile tab in this version.
        shell("input", "keyevent", "BACK")
        wait_for("外观", scroll=True)
        tap("首页")
        stage = "classic-home"
        manifest["geometry"]["classic"] = capture_home(output, "classic")
        if (output / "installed-navigation-liquid.png").read_bytes() == (output / "installed-navigation-classic.png").read_bytes():
            raise RuntimeError("The two navigation material crops were identical")
        manifest["state"] = "complete"
        manifest["screenshots"] = {p.name: sha256(p) for p in sorted(output.glob("installed-*.png"))}
    except Exception:
        manifest["state"] = "failed"
        manifest["failedStage"] = stage
        try:
            capture_image().save(output / ("unverified-" + stage + ".png"))
        except (OSError, subprocess.SubprocessError, RuntimeError):
            pass
        raise
    finally:
        (output / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    if (os.getenv("GITHUB_ACTIONS"), os.getenv("RUNNER_ENVIRONMENT"), os.getenv("ANDROID_SERIAL")) != ("true", "github-hosted", SERIAL):
        raise SystemExit("This capture requires the dedicated GitHub-hosted AVD")
    run(Path(sys.argv[1]), Path(sys.argv[2]), sys.argv[3])
