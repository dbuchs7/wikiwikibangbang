# Logo

The wikiwikibangbang logo. The mark is a question mark above a ticked box: a question asked and an answer found, as a wiki holds both. It replaced the earlier two exclamation marks ("bang bang") at the maintainer's request (2026-10-23).

The mark comes in its own colours: a black question mark, a box in dark petrol blue (`#0E2A38`) and a dark grey tick (`#333333`). On dark ground the same hues are lighter: a white question mark, a light blue box (`#8FB8CC`) and a light grey tick (`#D0D0D0`). The wordmark is black on light ground and white on dark ground.

The wordmark sets "wikiwiki" in Inter Display Medium and, in a second line below it, "bangbang" in Inter Display Black. Both lines are as wide: "wikiwiki" is letter-spaced to the width of "bangbang". In the horizontal logo the mark spans both lines. All artwork is outlines: no file needs the font.

The icon is the mark on a rounded white square (inverted: on dark). It is the app icon and the favicon.

## Files

| Folder | Use |
|---|---|
| `svg/` | vector, the source of every other file; web and further editing |
| `pdf/` | vector for print |
| `png/` | raster with a transparent background, in several widths |
| `favicon/` | the icon: `favicon.ico` (16, 32, 48 px), `favicon.svg` (white square, dark in a dark browser theme), `apple-touch-icon.png` (180 px), `icon-192.png`, `icon-512.png`, `icon-maskable-512.png`, `site.webmanifest` |
| `source/` | the template (`mark-template.png`), its traced paths (`mark.json`), the tracer (`trace.py`) and the generator (`build.py`) |

Each artwork comes as `-black` (on light ground) and `-white` (on dark ground):

- `horizontal`: the mark beside the two-line wordmark, the default;
- `stacked`: mark above the wordmark, for square spaces;
- `wordmark`: the name alone;
- `mark`: the mark alone;
- `app-icon`, `app-icon-inverted`: the mark on a rounded square, for app launchers; `maskable`: the same on a full square, which the platform cuts to its own shape; `favicon`: the mark larger in its square, for 16 to 48 px.

## Use

- **Clear space:** around the logo, at least the width of the mark's box.
- **Minimum size:** the mark at 24 px or 6 mm; below that, the favicon, where the question mark carries it.
- **Do not** recolour, outline, stretch, rotate or add effects. Use the colours above, for light or dark ground.

## Where the product uses it

The web UI embeds copies (ADR-0009): `internal/web/static/favicon.ico`, and in `internal/web/static/assets/brand/` the horizontal logo in black and white for the top bar, `favicon.svg` and `apple-touch-icon.png`. The start page's illustration, `welcome.svg`, holds the horizontal logo too, inline. After a change here, copy them again, and the logo into `welcome.svg`.

## Regenerate

`source/build.py` places the traced mark and draws the wordmark from the Inter outlines, then renders PDF and PNG in Chromium and the ICO with ImageMagick. It is a design tool, not part of the build. It needs Python 3 with `fonttools`, `uharfbuzz` and `playwright`, ImageMagick's `convert`, and Inter (set `INTER_DIR` if it is not in `/usr/share/fonts/opentype/inter`; set `CHROMIUM` to use a particular browser):

```bash
python3 docs/brand/source/build.py /tmp/brand   # then copy svg, pdf, png and favicon over docs/brand
```

When the template changes, trace it again first; `source/trace.py` needs `numpy`, `Pillow` and `potracer`:

```bash
python3 docs/brand/source/trace.py docs/brand/source/mark-template.png docs/brand/source/mark.json
```

## License

The logo, the mark's template included (supplied by the maintainer), is part of this repository and licensed under AGPL-3.0-only like the rest of it ([ADR-0002](../adr/0002-license-agpl-3.0-only.md)). Inter is licensed under the SIL Open Font License 1.1, which allows its use in logos.
