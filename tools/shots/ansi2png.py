#!/usr/bin/env python3
"""Rasterize the game's truecolor ANSI frames to PNG (tools/shots emits them).

Cell model mirrors the game's terminal output: one character cell is
1 px wide x 2 px tall once unpacked — the upper half-block U+2580 paints the
top pixel with the cell's foreground color and the bottom with the
background, which is how the pixel-art renderer packs two square pixels per
cell. Anything else is real text (leaderboard screens) and is drawn with a
monospace font.

    python3 tools/shots/ansi2png.py frame.ansi frame.png [scale] [--font TTF]

Scale is pixels per half-cell (default 6 -> a 240x90-px frame becomes
1440x540). Needs Pillow; on NixOS without a global install:

    nix-shell -p python3Packages.pillow --run 'python3 tools/shots/ansi2png.py ...'
"""
import glob
import re
import sys

from PIL import Image, ImageDraw, ImageFont

ESC = 0x1B
HALF_TOP = "\u2580"
HALF_BOTTOM = "\u2588"  # full block, if ever used: whole cell fg


def parse_ansi(data):
    fg = bg = (0, 0, 0)
    bold = False
    x = y = 0
    rows = []
    i, n = 0, len(data)
    while i < n:
        b = data[i]
        if b == ESC:
            m = re.match(rb"\x1b\[([0-9;]*)([A-Za-z])", data[i:])
            if not m:
                i += 1
                continue
            params, final = m.group(1), m.group(2)
            if final == b"H":
                x = y = 0
            elif final == b"m":
                parts = params.split(b";") if params else [b"0"]
                j = 0
                while j < len(parts):
                    v = int(parts[j] or b"0")
                    if v == 0:
                        fg = bg = (0, 0, 0)
                        bold = False
                    elif v == 1:
                        bold = True
                    elif v == 38 and j + 3 < len(parts) and parts[j + 1] == b"2":
                        fg = tuple(int(c) for c in parts[j + 2 : j + 5])
                        j += 4
                    elif v == 48 and j + 3 < len(parts) and parts[j + 1] == b"2":
                        bg = tuple(int(c) for c in parts[j + 2 : j + 5])
                        j += 4
                    j += 1
            i += m.end()
            continue
        if b == 0x0D:
            i += 1
            continue
        if b == 0x0A:
            x = 0
            y += 1
            i += 1
            continue
        ln = 1 if b < 0x80 else 2 if b < 0xE0 else 3 if b < 0xF0 else 4
        ch = data[i : i + ln].decode("utf-8", "replace")
        while len(rows) <= y:
            rows.append([])
        row = rows[y]
        while len(row) <= x:
            row.append((" ", (0, 0, 0), (0, 0, 0), False))
        row[x] = (ch, fg, bg, bold)
        x += 1
        i += ln
    return rows


def default_font():
    for pat in (
        "/run/current-system/sw/share/fonts/**/DejaVuSansMono.ttf",
        "/nix/store/*dejavu*/share/fonts/**/DejaVuSansMono.ttf",
        "/usr/share/fonts/**/DejaVuSansMono.ttf",
    ):
        hits = glob.glob(pat, recursive=True)
        if hits:
            return hits[0]
    return None


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    opts = [a for a in sys.argv[1:] if a.startswith("--")]
    if len(args) < 2:
        sys.exit(__doc__)
    src, dst = args[0], args[1]
    scale = int(args[2]) if len(args) > 2 else 6
    font_path = None
    for o in opts:
        if o.startswith("--font="):
            font_path = o.split("=", 1)[1]
    font_path = font_path or default_font()

    rows = parse_ansi(open(src, "rb").read())
    W = max(len(r) for r in rows)
    H = len(rows)
    cw, ch = scale, 2 * scale
    img = Image.new("RGB", (W * cw, H * ch), (0, 0, 0))
    d = ImageDraw.Draw(img)

    # Text cells: fit the glyph inside the cell (mono advance ~= 0.6 em).
    fonts = {}
    if font_path:
        for size in range(2 * scale, 4, -1):
            probe = ImageFont.truetype(font_path, size)
            if probe.getlength("M") <= cw:
                break
        fonts = {
            False: probe,
            True: ImageFont.truetype(
                font_path.replace("DejaVuSansMono.ttf", "DejaVuSansMono-Bold.ttf"), size
            ),
        }

    for cy, row in enumerate(rows):
        for cx, (ch_, fg, bg, bold) in enumerate(row):
            px, py = cx * cw, cy * ch
            if ch_ == HALF_TOP:
                d.rectangle([px, py, px + cw - 1, py + scale - 1], fill=fg)
                d.rectangle([px, py + scale, px + cw - 1, py + ch - 1], fill=bg)
            elif ch_ == HALF_BOTTOM:
                d.rectangle([px, py, px + cw - 1, py + ch - 1], fill=fg)
            elif ch_ == " ":
                d.rectangle([px, py, px + cw - 1, py + ch - 1], fill=bg)
            else:
                d.rectangle([px, py, px + cw - 1, py + ch - 1], fill=bg)
                f = fonts.get(bold) or fonts.get(False)
                if f:
                    d.text((px, py + (ch - f.size) // 2), ch_, font=f, fill=fg)

    img.save(dst)
    print(f"{dst}: {img.size[0]}x{img.size[0] and img.size[1]} ({W}x{H} cells x{scale})")


if __name__ == "__main__":
    main()
