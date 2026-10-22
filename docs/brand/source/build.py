"""Build the wikiwikibangbang logo set (black and white) from geometry.

Everything is generated: the mark from shapely geometry, the wordmark from
Inter Display outlines (OFL-1.1), so every SVG is pure paths with no font
dependency. Output: OUT/svg, OUT/pdf, OUT/png, OUT/favicon.
"""
import os, shutil, subprocess, sys
import uharfbuzz as hb
from fontTools.ttLib import TTFont
from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from shapely.geometry import box, Polygon, LineString
from shapely.ops import unary_union
from shapely import affinity

OUT = sys.argv[1]  # e.g. docs/brand
FONT_DIR = os.environ.get("INTER_DIR", "/usr/share/fonts/opentype/inter")
BLACK, WHITE = "#000000", "#FFFFFF"

# ---------------------------------------------------------------- mark (64 grid)
def rrect(x0, y0, x1, y1, r):
    return box(x0 + r, y0 + r, x1 - r, y1 - r).buffer(r, quad_segs=16)

def page_bar(x0, y0, x1, y1, fold, gap, flap=True):
    body = rrect(x0, y0, x1, y1, 3)
    corner = Polygon([(x1 - fold, y0 - 1), (x1 + 1, y0 - 1), (x1 + 1, y0 + fold)])
    body = body.difference(corner)
    if not flap:
        return body
    f = Polygon([(x1 - fold, y0), (x1 - fold, y0 + fold), (x1, y0 + fold)])
    body = body.difference(f.buffer(gap, join_style="mitre"))
    return unary_union([body, f])

def open_box(x0, y0, s, ring):
    return rrect(x0, y0, x0 + s, y0 + s, 3.5).difference(
        rrect(x0 + ring, y0 + ring, x0 + s - ring, y0 + s - ring, max(3.5 - ring, 0.8)))

def checked_box(x0, y0, s, w):
    k = s / 18
    tick = LineString([(x0 + 4.2 * k, y0 + 9.4 * k), (x0 + 7.7 * k, y0 + 12.9 * k), (x0 + 13.9 * k, y0 + 5.4 * k)])
    return rrect(x0, y0, x0 + s, y0 + s, 3.5).difference(tick.buffer(w / 2, cap_style="flat", join_style="mitre"))

def tall_mark(h):
    """The mark stretched to h units of the 64 grid: the strokes grow, the
    dots keep their size, for the two-line lockup."""
    bars = [page_bar(8, 2, 26, h - 26, 7, 1.6), page_bar(38, 2, 56, h - 26, 7, 1.6)]
    boxes = [open_box(8, h - 20, 18, 3.5), checked_box(38, h - 20, 18, 3.4)]
    return unary_union(bars + boxes)

def mark(small=False):
    """The full mark, or the simplified one for 16-32 px."""
    if small:
        bars = [page_bar(8, 2, 26, 38, 8, 0, flap=False), page_bar(38, 2, 56, 38, 8, 0, flap=False)]
        boxes = [open_box(8, 44, 18, 4.5), checked_box(38, 44, 18, 4.6)]
    else:
        bars = [page_bar(8, 2, 26, 38, 7, 1.6), page_bar(38, 2, 56, 38, 7, 1.6)]
        boxes = [open_box(8, 44, 18, 3.5), checked_box(38, 44, 18, 3.4)]
    return unary_union(bars + boxes)

def geom_path(g, sx=1, sy=1, dx=0, dy=0):
    polys = getattr(g, "geoms", [g])
    out = []
    for p in polys:
        for ring in [p.exterior, *p.interiors]:
            pts = [(x * sx + dx, y * sy + dy) for x, y in ring.coords[:-1]]
            out.append("M" + " L".join(f"{x:.3f},{y:.3f}" for x, y in pts) + "Z")
    return "".join(out)

# ---------------------------------------------------------------- wordmark
def shape(text, path):
    blob = hb.Blob.from_file_path(path)
    font = hb.Font(hb.Face(blob))
    buf = hb.Buffer(); buf.add_str(text); buf.guess_segment_properties()
    hb.shape(font, buf, {"kern": True, "liga": True})
    return buf.glyph_infos, buf.glyph_positions

def text_path(parts, size, x, baseline, tracking=-0.012):
    """parts: [(text, fontfile)]. Returns (svg path d, width) in px."""
    d, pen_x = [], x
    for text, ff in parts:
        path = os.path.join(FONT_DIR, ff)
        tt = TTFont(path); gs = tt.getGlyphSet(); order = tt.getGlyphOrder()
        upm = tt["head"].unitsPerEm; k = size / upm
        infos, pos = shape(text, path)
        for i, p in zip(infos, pos):
            sp = SVGPathPen(gs)
            gs[order[i.codepoint]].draw(TransformPen(sp, (k, 0, 0, -k, pen_x + p.x_offset * k, baseline)))
            d.append(sp.getCommands())
            pen_x += p.x_advance * k + tracking * size
    return "".join(d), pen_x - tracking * size - x

def metrics(ff, size):
    tt = TTFont(os.path.join(FONT_DIR, ff)); upm = tt["head"].unitsPerEm; os2 = tt["OS/2"]
    return os2.sxHeight * size / upm, os2.sCapHeight * size / upm

WORD = [("wikiwiki", "InterDisplay-Medium.otf"), ("bangbang", "InterDisplay-Black.otf")]

# The name is set in two lines: "bangbang" below "wikiwiki", both as wide.
# "wikiwiki" is the narrower line; it is brought to the width of
# "bangbang" by FIT: "scale" enlarges it, "track" spaces its letters,
# "mix" does half of each.
LEADING = 0.98  # baseline to baseline, in font sizes of the second line
FIT = os.environ.get("WWBB_FIT", "track")

def first_line(size, words=WORD):
    """The size and tracking of the first line so it is as wide as the
    second."""
    target = text_path([words[1]], size, 0, 0)[1]
    natural = text_path([words[0]], size, 0, 0)[1]
    n = len(words[0][0])
    if natural > target:  # wider: never tighter than set, only smaller
        return size * target / natural, -0.012
    if FIT == "track":
        return size, -0.012 + (target - natural) / (n - 1) / size
    if FIT == "mix":
        k = (target / natural) ** 0.5
        w = text_path([words[0]], size * k, 0, 0)[1]
        return size * k, -0.012 + (target - w) / (n - 1) / (size * k)
    return size * target / natural, -0.012

def word_lines(size, x, baseline, centre=None, words=WORD):
    """Both lines, as wide, left-aligned at x or centred on centre, the
    first with its baseline at baseline. Returns (svg path d, width, top
    of the first line above baseline, baseline of the second line)."""
    s1, tr1 = first_line(size, words)
    _, cap1 = metrics(words[0][1], s1)
    _, w = text_path([words[1]], size, 0, 0)
    lx = x if centre is None else centre - w / 2
    second = baseline + LEADING * size
    d = text_path([words[0]], s1, lx, baseline, tracking=tr1)[0] + text_path([words[1]], size, lx, second)[0]
    return d, w, cap1 * 1.04, second

# The monogram: "ww" above "bb", as the name is set; the icon of the app,
# the favicon included. The exclamation-mark mark stays in the logo.
INITIALS = [("ww", WORD[0][1]), ("bb", WORD[1][1])]

# ---------------------------------------------------------------- svg writers
def svg(w, h, body, bg=None, title="wikiwikibangbang"):
    rect = f'<rect width="{w}" height="{h}" fill="{bg}"/>' if bg else ""
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w:.2f} {h:.2f}" width="{w:.2f}" height="{h:.2f}" role="img">'
            f'<title>{title}</title>{rect}{body}</svg>\n')

def p(d, fill):
    return f'<path fill="{fill}" fill-rule="evenodd" d="{d}"/>'

def lockups(fg):
    """Return {name: svg} for every artwork in one colour (fg)."""
    res = {}
    big, small = mark(), mark(small=True)
    res["mark"] = svg(64, 64, p(geom_path(big), fg))
    res["mark-small"] = svg(64, 64, p(geom_path(small), fg))

    size = 100
    xh, cap = metrics("InterDisplay-Medium.otf", size)
    desc = size * 0.24
    # wordmark only: two lines as wide
    d, w, asc, last = word_lines(size, 0, 0)
    res["wordmark"] = svg(w + 4, asc + last + desc, f'<g transform="translate(2 {asc:.2f})">{p(d, fg)}</g>')

    # horizontal: the mark spans both lines, from the top of the first to
    # the baseline of the second; its strokes are as thick as before.
    s = cap * 1.42 / 64
    mh = asc + last; units = mh / s; gap = cap * 1.42 * 0.32
    d, w, _, _ = word_lines(size, 64 * s + gap, 0)
    body = p(geom_path(tall_mark(units), s, s, 0, 0), fg) + f'<g transform="translate(0 {asc:.2f})">{p(d, fg)}</g>'
    res["horizontal"] = svg(64 * s + gap + w + 2, asc + last + desc, body)

    # stacked: mark above, the lines centred below it
    mh2 = size * 1.6; s2 = mh2 / 64
    _, w, asc, _ = word_lines(size, 0, 0)
    W = max(w, mh2) + 4
    ty = mh2 + size * 0.42 + asc
    d, _, _, last = word_lines(size, 0, ty, centre=W / 2)
    body = p(geom_path(big, s2, s2, (W - mh2) / 2, 0), fg) + p(d, fg)
    res["stacked"] = svg(W, last + desc, body)
    return res

def monogram(fg, bg, safe, radius=0):
    """Square icon: the initials centred on a square. safe = the share of
    the square their larger side takes; radius rounds the square's corners
    (0: full bleed)."""
    S = 512
    size = 100
    d, w, top, last = word_lines(size, 0, 0, words=INITIALS)
    k = S * safe / max(w, top + last)  # the initials are taller than wide
    h = (top + last) * k
    tx, ty = (S - w * k) / 2, (S - h) / 2 + top * k
    rect = f'<rect width="{S}" height="{S}" rx="{radius}" fill="{bg}"/>'
    body = rect + f'<g transform="translate({tx:.2f} {ty:.2f}) scale({k:.4f})">{p(d, fg)}</g>'
    return svg(S, S, body)

# ---------------------------------------------------------------- render
def main():
    if os.path.exists(OUT): shutil.rmtree(OUT)
    for sub in ("svg", "pdf", "png", "favicon"): os.makedirs(os.path.join(OUT, sub))
    files = {}
    for tone, fg in (("black", BLACK), ("white", WHITE)):
        for name, s in lockups(fg).items():
            files[f"wwbb-{name}-{tone}"] = s
    # The icons are the monogram on a rounded square; the maskable one is
    # full bleed, as the platform cuts its own shape, with the initials
    # inside the 80 % safe circle.
    files["wwbb-app-icon"] = monogram(WHITE, BLACK, 0.66, radius=112)
    files["wwbb-app-icon-inverted"] = monogram(BLACK, WHITE, 0.66, radius=112)
    files["wwbb-maskable"] = monogram(WHITE, BLACK, 0.54)
    # For 16 to 48 px the initials fill more of the square.
    files["wwbb-favicon"] = monogram(WHITE, BLACK, 0.84, radius=96)
    for n, s in files.items():
        open(os.path.join(OUT, "svg", n + ".svg"), "w").write(s)

    # favicon.svg follows the tab colour scheme: black square in a light
    # browser theme, white in a dark one.
    fav_svg = files["wwbb-favicon"].replace('role="img">', 'role="img"><style>rect{fill:#000}path{fill:#fff}'
        '@media (prefers-color-scheme:dark){rect{fill:#fff}path{fill:#000}}</style>', 1)
    open(os.path.join(OUT, "favicon", "favicon.svg"), "w").write(fav_svg)

    from playwright.sync_api import sync_playwright
    import re
    def dims(s):
        m = re.search(r'viewBox="0 0 ([\d.]+) ([\d.]+)"', s); return float(m[1]), float(m[2])
    with sync_playwright() as pw:
        br = pw.chromium.launch(executable_path=os.environ.get("CHROMIUM") or None)
        pg = br.new_page()
        def png(s, path, width=None, height=None):
            w, h = dims(s)
            if width: h = h * width / w; w = width
            elif height: w = w * height / h; h = height
            w, h = round(w), round(h)
            pg.set_viewport_size({"width": w, "height": h})
            inner = re.sub(r' width="[\d.]+" height="[\d.]+"', f' width="{w}" height="{h}"', s, count=1)
            pg.set_content(f'<html><body style="margin:0;background:transparent">{inner}</body></html>')
            pg.screenshot(path=path, omit_background=True, clip={"x": 0, "y": 0, "width": w, "height": h})
        def pdf(s, path):
            w, h = dims(s)  # 1 svg unit = 1 css px; scale to a sensible print size
            k = 400 / w if w > 200 else 200 / w
            W, H = w * k, h * k
            inner = re.sub(r' width="[\d.]+" height="[\d.]+"', f' width="{W:.2f}px" height="{H:.2f}px"', s, count=1)
            pg.set_content(f'<html><head><style>@page{{size:{W:.2f}px {H:.2f}px;margin:0}}body{{margin:0}}svg{{display:block}}</style></head><body>{inner}</body></html>')
            pg.pdf(path=path, width=f"{W:.2f}px", height=f"{H:.2f}px", print_background=True, page_ranges="1")

        for n, s in files.items():
            if not n.startswith(("wwbb-app", "wwbb-maskable", "wwbb-favicon")) and "small" not in n:
                pdf(s, os.path.join(OUT, "pdf", n + ".pdf"))
            if n.startswith(("wwbb-app", "wwbb-maskable", "wwbb-favicon")):
                for px in (1024, 512, 192):
                    png(s, os.path.join(OUT, "png", f"{n}-{px}.png"), width=px)
            elif "-mark-" in n:
                for px in (1024, 512, 256):
                    png(s, os.path.join(OUT, "png", f"{n}-{px}.png"), width=px)
            else:
                for px in (2400, 1200, 600):
                    png(s, os.path.join(OUT, "png", f"{n}-{px}w.png"), width=px)

        fav = os.path.join(OUT, "favicon")
        for px in (16, 32, 48):
            png(files["wwbb-favicon"], os.path.join(fav, f"favicon-{px}.png"), width=px)
        png(files["wwbb-app-icon"], os.path.join(fav, "apple-touch-icon.png"), width=180)
        png(files["wwbb-app-icon"], os.path.join(fav, "icon-192.png"), width=192)
        png(files["wwbb-app-icon"], os.path.join(fav, "icon-512.png"), width=512)
        png(files["wwbb-maskable"], os.path.join(fav, "icon-maskable-512.png"), width=512)
        br.close()
    subprocess.run(["convert", *(os.path.join(fav, f"favicon-{px}.png") for px in (16, 32, 48)),
                    os.path.join(fav, "favicon.ico")], check=True)
    open(os.path.join(fav, "site.webmanifest"), "w").write(
        '{\n  "name": "wikiwikibangbang",\n  "short_name": "wwbb",\n  "icons": [\n'
        '    {"src": "/icon-192.png", "sizes": "192x192", "type": "image/png"},\n'
        '    {"src": "/icon-512.png", "sizes": "512x512", "type": "image/png"},\n'
        '    {"src": "/icon-maskable-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"}\n'
        '  ],\n  "theme_color": "#000000",\n  "background_color": "#ffffff",\n  "display": "standalone"\n}\n')

if __name__ == "__main__":
    main()
