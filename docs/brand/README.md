# Logo

The Orbithek logo. The mark is a library in orbit: a planet whose body is three book spines, with a ring around it and a moon on the ring (ADR-0154). The ring passes in front of the books; its far side shows left of the planet. It replaced the question mark above a ticked box (2026-10-23 to 2026-11-01), which had replaced two exclamation marks ("bang bang").

The mark comes in its own colours: black books, a ring and moon in dark petrol blue (`#0E2A38`). On dark ground the books are white and the ring and moon light blue (`#8FB8CC`). The wordmark is black on light ground and white on dark ground.

The wordmark sets "orbithek" in one line, all lower case, in Inter Display SemiBold. In the horizontal logo the mark spans the line, from the top of the ascenders to half the descenders. All artwork is outlines: no file needs the font. Until the product was renamed (ADR-0153) the wordmark read "wikiwiki" over "bangbang" in two lines.

The icon is the mark on a rounded white square (inverted: on dark). It is the app icon and the favicon.

## Files

| Folder | Use |
|---|---|
| `svg/` | vector, the source of every other file; web and further editing |
| `pdf/` | vector for print |
| `png/` | raster with a transparent background, in several widths |
| `favicon/` | the icon: `favicon.ico` (16, 32, 48 px), `favicon.svg` (white square, dark in a dark browser theme), `apple-touch-icon.png` (180 px), `icon-192.png`, `icon-512.png`, `icon-maskable-512.png`, `site.webmanifest` |
| `source/` | the generator (`build.py`), which draws the mark and the wordmark |

Each artwork is named `orbithek-<artwork>` and comes as `-black` (on light ground) and `-white` (on dark ground):

- `horizontal`: the mark beside the wordmark, the default;
- `stacked`: mark above the wordmark, for square spaces;
- `wordmark`: the name alone;
- `mark`: the mark alone;
- `app-icon`, `app-icon-inverted`: the mark on a rounded square, for app launchers; `maskable`: the same on a full square, which the platform cuts to its own shape; `favicon`: the mark larger in its square, for 16 to 48 px.

## Use

- **Clear space:** around the logo, at least the width of the mark's box.
- **Minimum size:** the mark at 24 px or 6 mm; below that, the favicon, where the ringed planet carries it.
- **Do not** recolour, outline, stretch, rotate or add effects. Use the colours above, for light or dark ground.

## Where the product uses it

The web UI embeds copies (ADR-0009): `internal/web/static/favicon.ico`, and in `internal/web/static/assets/brand/` the horizontal logo in black and white for the top bar, `favicon.svg` and `apple-touch-icon.png`. The start page's illustration, `welcome.svg`, is a space station in orbit: a hub holding a page, with docked modules for conversations, tasks, pictures, protection and processes, solar wings, the people on board and the ringed planet of the mark; it holds the horizontal logo too, inline. After a change here, copy them again, and the logo into `welcome.svg`.

## Regenerate

`source/build.py` draws the mark from circles and ellipses with `shapely` (its proportions are the constants at the top of `_mark`) and the wordmark from the Inter outlines, then renders PDF and PNG in Chromium and the ICO with ImageMagick. It is a design tool, not part of the build. It needs Python 3 with `shapely`, `fonttools`, `uharfbuzz` and `playwright`, ImageMagick's `convert`, and Inter (set `INTER_DIR` if it is not in `/usr/share/fonts/opentype/inter`; set `CHROMIUM` to use a particular browser):

```bash
python3 docs/brand/source/build.py /tmp/brand   # then copy svg, pdf, png and favicon over docs/brand
```

## License

The logo is part of this repository and licensed under AGPL-3.0-only like the rest of it ([ADR-0002](../adr/0002-license-agpl-3.0-only.md)). Inter is licensed under the SIL Open Font License 1.1, which allows its use in logos.
