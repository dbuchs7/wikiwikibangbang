"""Draw the start page's illustration: a space station in perspective.

The station is a small 3D model (drums, a truss, solar panels) projected
by a perspective camera and drawn back to front as SVG polygons; the
icons of its modules sit on the hull facing the camera. The planet of
the mark, the orbit, the people, the stars and the logo badge are flat.
The logo is read from docs/brand/svg/orbithek-horizontal-black.svg.

    python3 docs/brand/source/welcome.py internal/web/static/assets/brand/welcome.svg
"""
import math, os, re, sys

# ---------------------------------------------------------------- vectors
def sub(a, b): return (a[0]-b[0], a[1]-b[1], a[2]-b[2])
def add(a, b): return (a[0]+b[0], a[1]+b[1], a[2]+b[2])
def mul(a, k): return (a[0]*k, a[1]*k, a[2]*k)
def dot(a, b): return a[0]*b[0]+a[1]*b[1]+a[2]*b[2]
def cross(a, b): return (a[1]*b[2]-a[2]*b[1], a[2]*b[0]-a[0]*b[2], a[0]*b[1]-a[1]*b[0])
def norm(a):
    l = math.sqrt(dot(a, a)); return (a[0]/l, a[1]/l, a[2]/l)

# ---------------------------------------------------------------- camera
EYE = (3.6, 7.0, 15.5)
TARGET = (0.0, 0.0, -0.6)
FWD = norm(sub(TARGET, EYE))
RIGHT = norm(cross(FWD, (0, 1, 0)))
UP = cross(RIGHT, FWD)
F = 640.0           # focal length in px
CX, CY = 240, 132   # screen centre
LIGHT = norm((0.2, 0.9, 0.8))

def cam(p):
    d = sub(p, EYE)
    return (dot(d, RIGHT), dot(d, UP), dot(d, FWD))

def proj(p):
    x, y, z = cam(p)
    return (CX + F * x / z, CY - F * y / z), z

# ---------------------------------------------------------------- scene
faces = []  # (depth, points3d, fill, stroke)

def shade(rgb, n, lo=0.80):
    k = lo + (1 - lo) * max(0.0, dot(n, LIGHT))
    return "#%02x%02x%02x" % tuple(min(255, int(c * k)) for c in rgb)

def face(pts, rgb, n=None, lo=0.80, stroke=True):
    if n is None:
        n = norm(cross(sub(pts[1], pts[0]), sub(pts[2], pts[0])))
    c = sum((p[0] for p in pts)) / len(pts), sum((p[1] for p in pts)) / len(pts), sum((p[2] for p in pts)) / len(pts)
    # back-face culling for closed solids
    if dot(n, sub(EYE, c)) <= 0:
        return
    depth = cam(c)[2]
    fill = shade(rgb, n, lo)
    faces.append((depth, [(pts, fill, fill if stroke else "none")]))

def basis(axis):
    a = norm(axis)
    t = (0, 1, 0) if abs(a[1]) < 0.9 else (1, 0, 0)
    u = norm(cross(a, t)); v = cross(a, u)
    return a, u, v

def cylinder(p0, p1, r, rgb, n=44, caps=True, cap_rgb=None):
    a, u, v = basis(sub(p1, p0))
    ring = lambda base, t: add(base, add(mul(u, r * math.cos(t)), mul(v, r * math.sin(t))))
    for i in range(n):
        t0, t1 = 2 * math.pi * i / n, 2 * math.pi * (i + 1) / n
        tm = (t0 + t1) / 2
        nn = add(mul(u, math.cos(tm)), mul(v, math.sin(tm)))
        face([ring(p0, t0), ring(p0, t1), ring(p1, t1), ring(p1, t0)], rgb, nn)
    if caps:
        cr = cap_rgb or rgb
        face([ring(p1, 2 * math.pi * i / n) for i in range(n)], cr, a)
        face([ring(p0, 2 * math.pi * i / n) for i in range(n)][::-1], cr, mul(a, -1))

def module(p0, p1, r, band_rgb, band_at=0.25):
    """A capsule-like module: hull, a coloured band, rounded-looking end ring."""
    a = norm(sub(p1, p0)); L = math.dist(p0, p1)
    cylinder(p0, p1, r, HULL, cap_rgb=CAP)
    b0 = add(p0, mul(a, L * band_at)); b1 = add(b0, mul(a, L * 0.12))
    cylinder(b0, b1, r * 1.03, band_rgb, caps=False)
    e0 = add(p1, mul(a, -L * 0.06))
    cylinder(e0, p1, r * 1.04, RING, caps=False)

def box(c, size, rgb):
    hx, hy, hz = size[0] / 2, size[1] / 2, size[2] / 2
    P = lambda sx, sy, sz: (c[0] + sx * hx, c[1] + sy * hy, c[2] + sz * hz)
    quads = [((1, 0, 0), [P(1,-1,-1), P(1,1,-1), P(1,1,1), P(1,-1,1)]),
             ((-1, 0, 0), [P(-1,-1,1), P(-1,1,1), P(-1,1,-1), P(-1,-1,-1)]),
             ((0, 1, 0), [P(-1,1,-1), P(-1,1,1), P(1,1,1), P(1,1,-1)]),
             ((0, -1, 0), [P(-1,-1,1), P(-1,-1,-1), P(1,-1,-1), P(1,-1,1)]),
             ((0, 0, 1), [P(-1,-1,1), P(1,-1,1), P(1,1,1), P(-1,1,1)]),
             ((0, 0, -1), [P(1,-1,-1), P(-1,-1,-1), P(-1,1,-1), P(1,1,-1)])]
    for n, q in quads:
        face(q, rgb, n)

def panel(x0, x1, y0, y1, z, cols=4, rows=6):
    """A solar panel in the plane z: a frame and its cells, sorted as one."""
    frame = [(x0, y0, z), (x1, y0, z), (x1, y1, z), (x0, y1, z)]
    polys = [(frame, "#dfe5f1", "#dfe5f1")]
    for i in range(cols):
        for j in range(rows):
            xa, xb = x0 + (x1 - x0) * i / cols, x0 + (x1 - x0) * (i + 1) / cols
            ya, yb = y0 + (y1 - y0) * j / rows, y0 + (y1 - y0) * (j + 1) / rows
            g = 0.05
            pts = [(xa + g, ya + g, z), (xb - g, ya + g, z), (xb - g, yb - g, z), (xa + g, yb - g, z)]
            k = 0.95
            t = abs(j + 0.5 - rows / 2) / rows
            polys.append((pts, "#%02x%02x%02x" % (int((0x2b + 40 * t) * k), int((0x4a + 50 * t) * k), int((0x8f + 60 * t) * k)), "none"))
    c = ((x0 + x1) / 2, (y0 + y1) / 2, z)
    faces.append((cam(c)[2], polys))

HULL = (250, 251, 255)
CAP = (236, 240, 248)
RING = (190, 199, 216)
PETROL = (14, 42, 56)

# the core: a short upright drum, with a petrol band
cylinder((0, -0.85, 0), (0, 0.85, 0), 1.05, HULL, cap_rgb=CAP)
cylinder((0, 0.25, 0), (0, 0.45, 0), 1.08, PETROL, caps=False)
# docking rings
for d in [((1.0, 0, 0), (1.25, 0, 0)), ((-1.0, 0, 0), (-1.25, 0, 0)), ((0, -0.85, 0), (0, -1.05, 0)), ((-0.551, 0.451, -0.702), (-0.689, 0.564, -0.877)), ((0, 0.85, 0), (0, 1.05, 0))]:
    cylinder(d[0], d[1], 0.42, RING)
# modules
module((1.25, 0, 0), (3.6, 0, 0), 0.62, (20, 184, 166))     # tasks, right
module((-1.25, 0, 0), (-3.6, 0, 0), 0.62, (245, 158, 11))   # pictures, left
module((0, -1.05, 0), (0, -2.0, 0), 0.5, (124, 92, 255))    # protection, below
module((-0.689, 0.564, -0.877), (-1.82, 1.489, -2.316), 0.58, (11, 92, 255))    # conversations, back left
module((0, 1.05, 0), (0, 2.45, 0), 0.42, (236, 72, 153))    # processes, top
# truss behind, along x, through the back module, with the solar wings
TZ = -2.2
box((0, 0, TZ), (10.4, 0.22, 0.22), (200, 207, 222))
for x in (-5.2, 5.2):
    box((x, 0, TZ), (0.3, 0.5, 0.3), (180, 188, 205))
for sx in (-1, 1):
    panel(sx * 4.3, sx * 6.1, 0.35, 2.6, TZ)
    panel(sx * 4.3, sx * 6.1, -0.35, -2.6, TZ)

# ---------------------------------------------------------------- icons on the hull
def anchor(p, axis_dir, r):
    """The point on a module's hull most facing the camera, at p along its axis."""
    a = norm(axis_dir); to_eye = norm(sub(EYE, p))
    side = norm(sub(to_eye, mul(a, dot(to_eye, a))))
    q = add(p, mul(side, r * 1.02))
    (sx, sy), z = proj(q)
    return sx, sy, F / z

ICONS = {
    "tasks": (anchor((2.55, 0, 0), (1, 0, 0), 0.62), "#14b8a6"),
    "pictures": (anchor((-2.65, 0, 0), (1, 0, 0), 0.62), "#f59e0b"),
    "protection": (anchor((0, -1.6, 0), (0, 1, 0), 0.5), "#7c5cff"),
    "conversations": (anchor((-1.516, 1.241, -1.93), (-0.551, 0.451, -0.702), 0.58), "#0b5cff"),
    "processes": (anchor((0, 1.95, 0), (0, 1, 0), 0.42), "#ec4899"),
    "page": (anchor((0, 0.0, 0), (0, 1, 0), 1.05), "#0b5cff"),
}

def icon(kind, x, y, s, colour):
    k = s / 60.0  # icon drawn in a 24-unit box, scaled by distance
    g = f'<g transform="translate({x:.1f} {y:.1f}) scale({k:.3f})">'
    disc = f'<circle r="15" fill="#ffffff" stroke="{colour}" stroke-opacity=".35" stroke-width="1.5"/>'
    body = {
        "tasks": f'<rect x="-9" y="-9" width="18" height="18" rx="4" fill="{colour}"/><path d="M-5 0 l3.5 3.5 7 -7.5" fill="none" stroke="#fff" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/>',
        "pictures": f'<rect x="-10" y="-8" width="20" height="16" rx="3.5" fill="{colour}"/><path d="M-8 6 l5 -5 4 3 3 -2.5 5 4.5 z" fill="#fff"/><circle cx="4" cy="-3.5" r="2" fill="#fff"/>',
        "protection": f'<path d="M0 -10 l8.5 3.5 v5.5 c0 5 -3.6 8.7 -8.5 10 c-4.9 -1.3 -8.5 -5 -8.5 -10 v-5.5 z" fill="{colour}"/><path d="M-3.8 0.5 l2.8 2.8 5 -5" fill="none" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>',
        "conversations": f'<path d="M-9 -8 h18 a3.5 3.5 0 0 1 3.5 3.5 v8 a3.5 3.5 0 0 1 -3.5 3.5 h-10 l-4.5 4 v-4 h-3.5 a3.5 3.5 0 0 1 -3.5 -3.5 v-8 a3.5 3.5 0 0 1 3.5 -3.5 z" fill="{colour}"/><circle cx="-4.5" cy="-0.5" r="1.5" fill="#fff"/><circle cx="0" cy="-0.5" r="1.5" fill="#fff"/><circle cx="4.5" cy="-0.5" r="1.5" fill="#fff"/>',
        "processes": f'<circle cx="-7" cy="0" r="3.2" fill="none" stroke="{colour}" stroke-width="1.8"/><path d="M-3 0 h5 m-2.5 -2.5 l2.5 2.5 -2.5 2.5" fill="none" stroke="{colour}" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/><rect x="3.5" y="-4.5" width="8" height="9" rx="2" fill="{colour}"/>',
        "page": f'<rect x="-8" y="-10" width="16" height="20" rx="2.5" fill="#f5f7fc" stroke="#c7d2fe"/><rect x="-5" y="-6.5" width="9" height="2.6" rx="1.3" fill="url(#bar)"/><rect x="-5" y="-1.5" width="10" height="1.8" rx=".9" fill="#dfe5f1"/><rect x="-5" y="2" width="8" height="1.8" rx=".9" fill="#dfe5f1"/><rect x="-5" y="5.5" width="10" height="1.8" rx=".9" fill="#dfe5f1"/>',
    }[kind]
    return g + disc + body + "</g>"

def render():
    out = []
    for depth, polys in sorted(faces, key=lambda f: -f[0]):
      for pts, fill, stroke in polys:
        s = " ".join("%.1f,%.1f" % proj(p)[0] for p in pts)
        out.append(f'<polygon points="{s}" fill="{fill}"' + (f' stroke="{stroke}" stroke-width=".6" stroke-linejoin="round"' if stroke != "none" else "") + "/>")
    icons = []
    for kind, ((x, y, s), colour) in ICONS.items():
        icons.append(icon(kind, x, y, s * 1.15, colour))
    return "\n    ".join(out), "\n    ".join(icons)

LOGO = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "svg", "orbithek-horizontal-black.svg")

def inline_logo():
    """The horizontal logo, centred on the white badge below the station."""
    logo = open(LOGO).read()
    w, h = map(float, re.search(r'viewBox="0 0 ([\d.]+) ([\d.]+)"', logo).groups())
    inner = re.sub(r"^.*?<title>[^<]*</title>", "", logo, flags=re.S).rsplit("</svg>", 1)[0]
    H = 24.0; W = H * w / h
    return f'  <svg x="{240 - W / 2:.1f}" y="{273 + (34 - H) / 2:.1f}" width="{W:.1f}" height="{H:.1f}" viewBox="0 0 {w} {h}">{inner}</svg>'

PEOPLE = """<circle cx="372" cy="118" r="12" fill="#fde68a" stroke="#ffffff" stroke-width="3"/>
    <circle cx="372" cy="114.5" r="4.2" fill="#b45309"/>
    <path d="M364.5 125.5 a7.5 6.5 0 0 1 15 0" fill="#b45309"/>
    <circle cx="160" cy="176" r="12" fill="#bfdbfe" stroke="#ffffff" stroke-width="3"/>
    <circle cx="160" cy="172.5" r="4.2" fill="#1d4ed8"/>
    <path d="M152.5 183.5 a7.5 6.5 0 0 1 15 0" fill="#1d4ed8"/>
    <circle cx="290" cy="196" r="10.5" fill="#fbcfe8" stroke="#ffffff" stroke-width="3"/>
    <circle cx="290" cy="193" r="3.8" fill="#be185d"/>
    <path d="M283.5 202.5 a6.5 5.5 0 0 1 13 0" fill="#be185d"/>"""

if __name__ == "__main__":
    st, icons = render()
    st = f"  <g>\n    {st}\n  </g>\n  <g filter=\"url(#lift)\">\n    {icons}\n  </g>\n"
    logo = inline_logo()
    svg = f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 480 320" role="img" aria-labelledby="t">
      <title id="t">A space station in orbit, seen in perspective: a central drum holding a page, with docked modules for conversations, tasks, pictures, protection and processes, solar wings on a truss, the people on board, and the Orbithek logo below</title>
      <!-- The welcome illustration of the start page: no text, no external resources. The station is a small 3D model projected in perspective (see docs/brand/README.md). -->
      <defs>
        <radialGradient id="glow" cx="50%" cy="50%" r="50%">
          <stop offset="0" stop-color="#7c9dff" stop-opacity=".40"/>
          <stop offset=".6" stop-color="#a78bfa" stop-opacity=".14"/>
          <stop offset="1" stop-color="#a78bfa" stop-opacity="0"/>
        </radialGradient>
        <linearGradient id="bar" x1="0" x2="1">
          <stop offset="0" stop-color="#0b5cff"/>
          <stop offset="1" stop-color="#7c5cff"/>
        </linearGradient>
        <mask id="spines" maskUnits="userSpaceOnUse" x="-40" y="230" width="160" height="120">
          <rect x="-40" y="230" width="160" height="120" fill="#ffffff"/>
          <path d="M22 230 V350 M58 230 V350" stroke="#000000" stroke-width="5"/>
        </mask>
        <filter id="lift" x="-20%" y="-20%" width="140%" height="150%">
          <feDropShadow dx="0" dy="4" stdDeviation="4" flood-color="#1e2a55" flood-opacity=".16"/>
        </filter>
        <filter id="soft" x="-10%" y="-10%" width="120%" height="130%">
          <feDropShadow dx="0" dy="8" stdDeviation="9" flood-color="#1e2a55" flood-opacity=".18"/>
        </filter>
      </defs>
    
      <!-- glow, the planet of the logo below the station, and the station's orbit -->
      <circle cx="240" cy="150" r="170" fill="url(#glow)"/>
      <g opacity=".9">
        <circle cx="40" cy="312" r="58" fill="#24546b" mask="url(#spines)"/>
        <ellipse cx="40" cy="312" rx="92" ry="22" transform="rotate(-24 40 312)" fill="none" stroke="#8FB8CC" stroke-width="6"/>
      </g>
      <ellipse cx="240" cy="150" rx="214" ry="104" transform="rotate(-8 240 150)" fill="none" stroke="#9fb4ff" stroke-width="1.5" stroke-dasharray="3 7" opacity=".7"/>
    
      <!-- the station -->
      <g filter="url(#soft)">
    {st}  </g>
    
      <!-- people on board, at the windows -->
      <g filter="url(#lift)">
        {PEOPLE}
      </g>
    
      <!-- the product's logo on a white badge, readable on light and dark cards -->
      <g filter="url(#lift)">
        <rect x="169.5" y="266.0" width="141.0" height="48.0" rx="12" fill="#ffffff"/>
      </g>
    {logo}
    
      <!-- stars -->
      <path d="M150 26 l3.5 9 9 3.5 -9 3.5 -3.5 9 -3.5 -9 -9 -3.5 9 -3.5 z" fill="#7c5cff"/>
      <path d="M462 258 l2.5 6 6 2.5 -6 2.5 -2.5 6 -2.5 -6 -6 -2.5 6 -2.5 z" fill="#14b8a6"/>
      <path d="M372 24 l2.5 6 6 2.5 -6 2.5 -2.5 6 -2.5 -6 -6 -2.5 6 -2.5 z" fill="#f59e0b"/>
      <circle cx="330" cy="256" r="3" fill="#f43f5e" opacity=".8"/>
      <circle cx="96" cy="40" r="3" fill="#7c5cff" opacity=".6"/>
      <circle cx="440" cy="30" r="2" fill="#7c5cff" opacity=".5"/>
      <circle cx="20" cy="30" r="2" fill="#14b8a6" opacity=".5"/>
    </svg>
    '''
    svg = "\n".join(l[4:] if l.startswith("    ") else l for l in svg.split("\n"))  # the template is indented in this file
    open(sys.argv[1], "w").write(svg)
