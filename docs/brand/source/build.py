"""Build the wikiwikibangbang logo set.

Everything is generated: the mark from its traced paths (mark.json, made
by trace.py from mark-template.png), the wordmark from Inter Display
outlines (OFL-1.1), so every SVG is pure paths with no font dependency.
Each artwork comes for light ground ("-black") and dark ground ("-white").
Output: OUT/svg, OUT/pdf, OUT/png, OUT/favicon.
"""
import json, os, shutil, subprocess, sys
import uharfbuzz as hb
from fontTools.ttLib import TTFont
from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen

OUT = sys.argv[1]  # e.g. docs/brand
FONT_DIR = os.environ.get("INTER_DIR", "/usr/share/fonts/opentype/inter")
BLACK, WHITE = "#000000", "#FFFFFF"

# ---------------------------------------------------------------- mark
# A question mark above a ticked box, in three colours: for light ground the
# template's own, for dark ground lighter ones of the same hues.
MARK = json.load(open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "mark.json")))
MX0, MY0, MX1, MY1 = MARK["bbox"]
MW, MH = MX1 - MX0, MY1 - MY0
ON_LIGHT = {"question": "#000000", "box": "#0E2A38", "check": "#333333"}
ON_DARK = {"question": "#FFFFFF", "box": "#8FB8CC", "check": "#D0D0D0"}

def mark_body(colours, height, x=0, y=0, classes=False):
    """The mark, height units tall, its top left at (x, y)."""
    k = height / MH
    paths = "".join(
        f'<path{" class=" + chr(34) + part + chr(34) if classes else ""} fill="{colours[part]}" fill-rule="evenodd" d="{MARK[part]}"/>'
        for part in ("box", "check", "question"))
    return f'<g transform="translate({x - MX0 * k:.3f} {y - MY0 * k:.3f}) scale({k:.5f})">{paths}</g>'

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

# ---------------------------------------------------------------- svg writers
def svg(w, h, body, bg=None, title="wikiwikibangbang"):
    rect = f'<rect width="{w}" height="{h}" fill="{bg}"/>' if bg else ""
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w:.2f} {h:.2f}" width="{w:.2f}" height="{h:.2f}" role="img">'
            f'<title>{title}</title>{rect}{body}</svg>\n')

def p(d, fill):
    return f'<path fill="{fill}" fill-rule="evenodd" d="{d}"/>'

def lockups(fg, colours):
    """Return {name: svg} for every artwork on one ground: fg is the colour
    of the wordmark, colours those of the mark."""
    res = {}
    res["mark"] = svg(MW * 64 / MH + 4, 68, mark_body(colours, 64, 2, 2))

    size = 100
    xh, cap = metrics("InterDisplay-Medium.otf", size)
    desc = size * 0.24
    # wordmark only: two lines as wide
    d, w, asc, last = word_lines(size, 0, 0)
    res["wordmark"] = svg(w + 4, asc + last + desc, f'<g transform="translate(2 {asc:.2f})">{p(d, fg)}</g>')

    # horizontal: the mark spans both lines, from the top of the first to
    # the bottom of the second.
    mh = asc + last + desc * 0.5
    mw = MW * mh / MH
    gap = cap * 0.36
    d, w, _, _ = word_lines(size, mw + gap, 0)
    body = mark_body(colours, mh) + f'<g transform="translate(0 {asc:.2f})">{p(d, fg)}</g>'
    res["horizontal"] = svg(mw + gap + w + 2, asc + last + desc, body)

    # stacked: mark above, the lines centred below it
    mh2 = size * 1.8
    mw2 = MW * mh2 / MH
    _, w, asc, _ = word_lines(size, 0, 0)
    W = max(w, mw2) + 4
    ty = mh2 + size * 0.42 + asc
    d, _, _, last = word_lines(size, 0, ty, centre=W / 2)
    body = mark_body(colours, mh2, (W - mw2) / 2, 0) + p(d, fg)
    res["stacked"] = svg(W, last + desc, body)
    return res

def icon(colours, bg, safe, radius=0, classes=False):
    """Square icon: the mark centred on a square. safe = the share of the
    square its height takes; radius rounds the square's corners (0: full
    bleed)."""
    S = 512
    h = S * safe
    rect = f'<rect width="{S}" height="{S}" rx="{radius}" fill="{bg}"/>'
    return svg(S, S, rect + mark_body(colours, h, (S - MW * h / MH) / 2, (S - h) / 2, classes))

# ---------------------------------------------------------------- render
def main():
    if os.path.exists(OUT): shutil.rmtree(OUT)
    for sub in ("svg", "pdf", "png", "favicon"): os.makedirs(os.path.join(OUT, sub))
    files = {}
    for tone, fg, colours in (("black", BLACK, ON_LIGHT), ("white", WHITE, ON_DARK)):
        for name, s in lockups(fg, colours).items():
            files[f"wwbb-{name}-{tone}"] = s
    # The icons are the mark on a rounded square, white or, inverted, dark;
    # the maskable one is full bleed, as the platform cuts its own shape,
    # with the mark inside the 80 % safe circle.
    DARK_GROUND = "#16181D"
    files["wwbb-app-icon"] = icon(ON_LIGHT, WHITE, 0.72, radius=112)
    files["wwbb-app-icon-inverted"] = icon(ON_DARK, DARK_GROUND, 0.72, radius=112)
    files["wwbb-maskable"] = icon(ON_LIGHT, WHITE, 0.56)
    # For 16 to 48 px the mark fills more of the square.
    files["wwbb-favicon"] = icon(ON_LIGHT, WHITE, 0.88, radius=96)
    for n, s in files.items():
        open(os.path.join(OUT, "svg", n + ".svg"), "w").write(s)

    # favicon.svg follows the tab colour scheme: the mark on white in a
    # light browser theme, in its light colours on dark in a dark one.
    fav_svg = icon(ON_LIGHT, WHITE, 0.88, radius=96, classes=True).replace('role="img">', 'role="img"><style>'
        f'@media (prefers-color-scheme:dark){{rect{{fill:{DARK_GROUND}}}.question{{fill:{ON_DARK["question"]}}}'
        f'.box{{fill:{ON_DARK["box"]}}}.check{{fill:{ON_DARK["check"]}}}}}</style>', 1)
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
            if not n.startswith(("wwbb-app", "wwbb-maskable", "wwbb-favicon")):
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
        '  ],\n  "theme_color": "#0E2A38",\n  "background_color": "#ffffff",\n  "display": "standalone"\n}\n')

if __name__ == "__main__":
    main()
