# Logo

The wikiwikibangbang logo, black and white only. The mark is two exclamation marks ("bang bang"): the strokes are wiki pages with a folded corner, the dots are tasks, one open and one done. Pages and tasks side by side, as items of one model ([ADR-0001](../adr/0001-unified-item-data-model.md)).

The wordmark sets "wikiwiki" in Inter Display Medium and, in a second line below it, "bangbang" in Inter Display Black. Both lines are as wide: "wikiwiki" is letter-spaced to the width of "bangbang". In the horizontal logo the mark spans both lines: its strokes run from the top of the first line to the baseline of the second, the dots keep their size. All artwork is outlines: no file needs the font.

The icon is the monogram: "ww" above "bb", set as the name is, on a rounded black square. It is the app icon and the favicon.

## Files

| Folder | Use |
|---|---|
| `svg/` | vector, the source of every other file; web and further editing |
| `pdf/` | vector for print, one colour (print as 100 % K or as a black spot colour) |
| `png/` | raster with a transparent background, in several widths |
| `favicon/` | the monogram: `favicon.ico` (16, 32, 48 px), `favicon.svg` (a black square, white in a dark browser theme), `apple-touch-icon.png` (180 px), `icon-192.png`, `icon-512.png`, `icon-maskable-512.png`, `site.webmanifest` |
| `source/` | the generator (`build.py`) |

Each artwork comes as `-black` (on light ground) and `-white` (on dark ground):

- `horizontal`: the tall mark beside the two-line wordmark, the default;
- `stacked`: mark above the wordmark, for square spaces;
- `wordmark`: the name alone;
- `mark`: the mark alone; `mark-small` is a sturdier version for 24 to 48 px;
- `app-icon`, `app-icon-inverted`: the monogram on a rounded square, for app launchers; `maskable`: the same on a full square, which the platform cuts to its own shape; `favicon`: the monogram larger in its square, for 16 to 48 px.

## Use

- **Clear space:** around the logo, at least the width of one of the mark's boxes.
- **Minimum size:** the mark at 24 px or 6 mm; below that, `mark-small` or the monogram.
- **Do not** recolour, outline, stretch, rotate or add effects. Black or white only.

## Where the product uses it

The web UI embeds copies (ADR-0009): `internal/web/static/favicon.ico`, and in `internal/web/static/assets/brand/` the horizontal logo in black and white for the top bar, `favicon.svg` and `apple-touch-icon.png`. The start page's illustration, `welcome.svg`, holds the horizontal logo too, inline. After a change here, copy them again, and the logo into `welcome.svg`.

## Regenerate

`source/build.py` draws the mark from geometry and the wordmark from the Inter outlines, then renders PDF and PNG in Chromium and the ICO with ImageMagick. It is a design tool, not part of the build. It needs Python 3 with `fonttools`, `uharfbuzz`, `shapely` and `playwright`, ImageMagick's `convert`, and Inter (set `INTER_DIR` if it is not in `/usr/share/fonts/opentype/inter`; set `CHROMIUM` to use a particular browser):

```bash
python3 docs/brand/source/build.py /tmp/brand   # then copy svg, pdf, png and favicon over docs/brand
```

## License

The logo is part of this repository and licensed under AGPL-3.0-only like the rest of it ([ADR-0002](../adr/0002-license-agpl-3.0-only.md)). Inter is licensed under the SIL Open Font License 1.1, which allows its use in logos.
