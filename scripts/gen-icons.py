#!/usr/bin/env python3
"""Draw the app icons with nothing but the standard library.

assets/icon-1024.png        the app icon: dark rounded square, six keys and a knob
internal/app/tray-color.png  44x44 colour tray icon (Windows, Linux)
internal/app/tray-template.png
                             44x44 black-on-transparent template icon (macOS menu bar)
"""
import os, struct, sys, zlib

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def png(width, height, pixels):
    raw = b"".join(b"\x00" + bytes(v for px in row for v in px) for row in pixels)

    def chunk(kind, data):
        body = kind + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)

    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))


def rounded(x, y, x0, y0, x1, y1, r):
    """Distance-free rounded-rect test, with a 1px antialias band via coverage sampling."""
    if x < x0 or x >= x1 or y < y0 or y >= y1:
        return 0.0
    cx = min(max(x + 0.5, x0 + r), x1 - r)
    cy = min(max(y + 0.5, y0 + r), y1 - r)
    d = ((x + 0.5 - cx) ** 2 + (y + 0.5 - cy) ** 2) ** 0.5
    return max(0.0, min(1.0, r - d + 0.5))


def circle(x, y, cx, cy, r):
    d = ((x + 0.5 - cx) ** 2 + (y + 0.5 - cy) ** 2) ** 0.5
    return max(0.0, min(1.0, r - d + 0.5))


def blend(dst, src, a):
    return tuple(int(dst[i] * (1 - a) + src[i] * a) for i in range(3))


def draw(size, background, key, template):
    s = size
    pixels = []
    pad = s * 0.14
    key_w = (s - 2 * pad) * 0.62 / 3
    key_h = (s - 2 * pad) * 0.42 / 2
    gap = key_w * 0.12
    knob_r = key_w * 0.42
    knob_cx = s - pad - knob_r
    knob_cy = s / 2
    keys_x0 = pad
    keys_y0 = s / 2 - key_h - gap / 2
    for y in range(s):
        row = []
        for x in range(s):
            if template:
                a_bg = 0.0
                rgb = (0, 0, 0)
            else:
                a_bg = rounded(x, y, 0, 0, s, s, s * 0.22)
                rgb = background
            a_fg = 0.0
            for ry in range(2):
                for rx in range(3):
                    x0 = keys_x0 + rx * (key_w + gap)
                    y0 = keys_y0 + ry * (key_h + gap)
                    a_fg = max(a_fg, rounded(x, y, x0, y0, x0 + key_w, y0 + key_h, key_w * 0.18))
            a_fg = max(a_fg, circle(x, y, knob_cx, knob_cy, knob_r))
            if template:
                row.append((0, 0, 0, int(255 * a_fg)))
            else:
                color = blend(rgb, key, a_fg)
                row.append((*color, int(255 * max(a_bg, a_fg))))
        pixels.append(row)
    return png(s, s, pixels)


def main():
    out = {
        os.path.join(ROOT, "assets", "icon-1024.png"): draw(1024, (24, 24, 27), (244, 244, 245), False),
        os.path.join(ROOT, "internal", "app", "tray-color.png"): draw(44, (24, 24, 27), (244, 244, 245), False),
        os.path.join(ROOT, "internal", "app", "tray-template.png"): draw(44, None, None, True),
    }
    for path, data in out.items():
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "wb") as f:
            f.write(data)
        print(f"wrote {os.path.relpath(path, ROOT)} ({len(data)} bytes)")


if __name__ == "__main__":
    sys.exit(main())
