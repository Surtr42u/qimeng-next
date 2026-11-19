"""Export the application artwork from the desktop master using Pillow 13.0."""

from pathlib import Path

from PIL import Image, ImageOps


ROOT = Path(__file__).resolve().parents[1]
WEB = ROOT / "web/public"
ANDROID = ROOT / "android/app/src/main/res/drawable-xxxhdpi"
LANCZOS = Image.Resampling.LANCZOS


def export_web(master):
    icons = WEB / "icons"
    icons.mkdir(exist_ok=True)
    # A tighter crop keeps the landscape and crescent legible in browser tabs.
    favicon = ImageOps.crop(master, border=150)
    favicon.save(WEB / "favicon.ico", sizes=[(16, 16), (32, 32)])
    regular = ImageOps.crop(master, border=75)
    regular.resize((180, 180), LANCZOS).save(WEB / "apple-touch-icon.png")
    for size in (192, 512):
        regular.resize((size, size), LANCZOS).save(icons / f"icon-{size}.png")
        master.resize((size, size), LANCZOS).save(icons / f"maskable-{size}.png")


def export_android(master):
    ANDROID.mkdir(exist_ok=True)
    # The outer columns contain uninterrupted backdrop at every height.
    width, height = master.size
    edges = Image.new("RGB", (2, 432))
    for column, x in enumerate((0, width - 1)):
        strip = master.crop((x, 0, x + 1, height)).resize((1, 432), LANCZOS)
        edges.paste(strip, (column, 0))
    background = edges.resize((432, 432), Image.Resampling.BILINEAR, box=(0.5, 0, 1.5, 432))
    background.save(ANDROID / "ic_launcher_background.png")

    # The ivory symbol and the purple backdrop are distinct in the green channel.
    mask = master.getchannel("G").point(lambda green: (green - 45) * 255 / (244 - 45))
    foreground = Image.new("RGBA", (432, 432), master.getpixel((width // 2, height // 2)) + (0,))
    alpha = Image.new("L", foreground.size)
    alpha.paste(mask.resize((352, 352), LANCZOS), (40, 40))
    foreground.putalpha(alpha)
    foreground.save(ANDROID / "ic_launcher_foreground.png")


if __name__ == "__main__":
    with Image.open(ROOT / "desktop/ui/app-icon.png") as artwork:
        export_web(artwork)
        export_android(artwork)
