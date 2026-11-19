"""Generate only synthetic preview fixtures in a test's temporary directory."""

import sys
from pathlib import Path

from PIL import Image, ImageDraw


def make_picture(path, width, height, progressive=False, orientation=1):
    png = path.suffix == ".png"
    mode = "RGBA" if png else "RGB"
    colors = [(216, 43, 42), (25, 177, 80), (38, 69, 215), (235, 163, 41)]
    if png:
        colors = [(*c, a) for c, a in zip(colors, (255, 255, 0, 128))]
    picture = Image.new(mode, (width, height), colors[0])
    draw = ImageDraw.Draw(picture)
    for i, color in enumerate(colors):
        x, y = (i % 2) * (width // 2), (i // 2) * (height // 2)
        draw.rectangle((x, y, x + width // 2 + 1, y + height // 2 + 1), fill=color)
    # A diagonal, concentric ellipses and thin contrasting lines give the
    # compressor actual spatial detail beyond the four sampled color patches.
    light = (247, 239, 225, 255) if png else (247, 239, 225)
    for i in range(16):
        dx, dy = width * (25 + i) // 100, height * (25 + i) // 100
        draw.ellipse((dx, dy, width - dx, height - dy), outline=light, width=max(1, width // 1100))
    draw.line((0, height // 3, width, 2 * height // 3), fill=light, width=max(1, height // 170))
    if png:
        picture.save(path, compress_level=3)
    else:
        exif = picture.getexif()
        exif[274] = orientation
        picture.save(path, quality=90, subsampling=0 if progressive else 2, progressive=progressive, exif=exif)
    picture.close()
    with Image.open(path) as check:
        assert check.size == (width, height)
        check.verify()


target = Path(sys.argv[1])
if len(sys.argv) == 3 and sys.argv[2] == "small":
    make_picture(target / "progressive-patches.jpg", 243, 177, progressive=True)
else:
    make_picture(target / "patches.png", 8003, 6001)
    make_picture(target / "sequential.jpg", 16001, 12503, orientation=6)
    make_picture(target / "progressive.jpg", 12005, 9377, progressive=True)
