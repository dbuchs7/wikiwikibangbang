"""Pixel-drawn 16 px favicon: the mark aligned to whole pixels."""
import sys
from PIL import Image
G = """\
.###......###...
.####.....####..
.#####....#####.
.#####....#####.
.#####....#####.
.#####....#####.
.#####....#####.
.#####....#####.
.#####....#####.
.#####....#####.
................
.#####....#####.
.#...#....###.#.
.#...#....#.#.#.
.#...#....##.##.
.#####....#####.""".split("\n")
img = Image.new("RGBA", (16, 16), (0, 0, 0, 0))
for y, row in enumerate(G):
    for x, c in enumerate(row):
        if c == "#":
            img.putpixel((x, y), (0, 0, 0, 255))
img.save(sys.argv[1])
