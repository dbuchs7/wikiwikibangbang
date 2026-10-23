"""Trace the mark from its template into docs/brand/source/mark.json.

The template is a raster picture: a black question mark above a ticked
box (dark blue) with a grey tick. Each colour is traced on its own, at four
times the size, with potrace (the potracer package), so the logo is pure
vector paths. A design tool, run once when the template changes:

    python3 docs/brand/source/trace.py docs/brand/source/mark-template.png docs/brand/source/mark.json
"""
import json, sys
import numpy as np
from PIL import Image
import potrace

src, out = sys.argv[1], sys.argv[2]
im = Image.open(src).convert("RGBA")
W, H = im.size
scale = 4
big = im.resize((W*scale, H*scale), Image.LANCZOS)
a = np.asarray(big).astype(int)
r, g, b, al = a[...,0], a[...,1], a[...,2], a[...,3]
opaque = al > 128
navy = opaque & (b - r > 18) & (r + g + b < 360)
grey = opaque & ~navy & (abs(r-g) < 14) & (abs(g-b) < 14) & (r > 34) & (r < 140)
black = opaque & ~navy & (r < 34) & (g < 34) & (b < 34)

def trace(mask):
    bm = potrace.Bitmap(~mask)  # potracer takes False as ink
    plist = bm.trace(turdsize=40, alphamax=1.0, opttolerance=0.4)
    parts = []
    for curve in plist:
        s = curve.start_point
        d = [f"M{s.x/scale:.2f},{s.y/scale:.2f}"]
        for seg in curve.segments:
            if seg.is_corner:
                d.append(f"L{seg.c.x/scale:.2f},{seg.c.y/scale:.2f}L{seg.end_point.x/scale:.2f},{seg.end_point.y/scale:.2f}")
            else:
                d.append(f"C{seg.c1.x/scale:.2f},{seg.c1.y/scale:.2f} {seg.c2.x/scale:.2f},{seg.c2.y/scale:.2f} {seg.end_point.x/scale:.2f},{seg.end_point.y/scale:.2f}")
        d.append("Z")
        parts.append("".join(d))
    return "".join(parts)

ys, xs = np.nonzero(opaque)
res = {"comment": "The mark, traced from mark-template.png by trace.py: a question mark above a ticked box. Coordinates are those of the template's pixels.",
       "bbox": [int(xs.min()) / scale, int(ys.min()) / scale, int(xs.max()) / scale, int(ys.max()) / scale],
       "question": trace(black), "box": trace(navy), "check": trace(grey)}
json.dump(res, open(out, "w"), indent=1)
