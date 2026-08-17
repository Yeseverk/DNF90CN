#!/usr/bin/env python3
"""Build the 2017 TGP combat-power panel resource and visual QA preview.

The source bitmap was statically extracted from the supplied 2017 plugin.
The original green upgrade button is retained as the entry point for the
in-game battle-power guide. The obsolete auto-expand footer is merged into the
equipment detail cell so the four runtime rows share one symmetrical frame.
Runtime text and rank art are not baked into the resource, so all characters
can share this exact frame.
"""

from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


ROOT = Path(__file__).resolve().parents[1]
ASSETS = ROOT / "assets"
ORIGINAL = ASSETS / "combat_power_panel_skin_v2_source_original.png"
SOURCE = ASSETS / "combat_power_panel_skin_v2_source.png"
PREVIEW = ASSETS / "combat_power_panel_skin_v2_preview.png"
BGRA = ASSETS / "combat_power_panel_skin_v2.bgra"
RANK_ICON = ASSETS / "combat_rank_martial_mastery_v3.png"

ORIGINAL_PANEL_SIZE = (124, 389)
PANEL_SOURCE_CROP_LEFT = 3
PANEL_SIZE = (118, 386)
GDI_EDGE_BACKGROUND = (4, 8, 12, 255)


def build_four_row_equipment_cell(panel: Image.Image) -> None:
    # Extend the existing detail cell over the obsolete auto-expand footer.
    # Preserve the original outer bottom border while continuing both vertical
    # side rails and the interior texture through y=385. Start at y=356 so the
    # old cell's bright bottom separator is covered too; use a clean, separator-free
    # band across the complete framed width.
    interior = panel.crop((6, 326, 118, 356)).resize(
        (112, 30), Image.Resampling.BICUBIC
    )
    panel.paste(interior, (6, 356))

    # Move the original gold selector arrows from the second row to the new
    # third (equipment) row. Copying the neighbouring texture removes the old
    # arrows without introducing a flat-colour patch.
    left_arrow = panel.crop((14, 305, 27, 321))
    right_arrow = panel.crop((97, 305, 110, 321))
    panel.paste(panel.crop((27, 305, 40, 321)), (14, 305))
    panel.paste(panel.crop((84, 305, 97, 321)), (97, 305))
    panel.paste(left_arrow, (14, 333))
    panel.paste(right_arrow, (97, 333))


def widen_section_headers(panel: Image.Image) -> None:
    # The original two section-caption frames leave a visibly larger gutter
    # than the surrounding value cells. Move only their decorative end caps
    # three pixels outward on each side and fill the added span from the
    # existing interior texture; the original centred Chinese lettering is
    # pasted unchanged, so it neither stretches nor shifts.
    for top, bottom in ((201, 222), (252, 273)):
        left_cap = panel.crop((9, top, 14, bottom))
        left_fill = panel.crop((14, top, 15, bottom)).resize(
            (3, bottom - top), Image.Resampling.BICUBIC
        )
        core = panel.crop((14, top, 110, bottom))
        right_fill = panel.crop((109, top, 110, bottom)).resize(
            (3, bottom - top), Image.Resampling.BICUBIC
        )
        right_cap = panel.crop((110, top, 115, bottom))
        panel.paste(left_cap, (6, top))
        panel.paste(left_fill, (11, top))
        panel.paste(core, (14, top))
        panel.paste(right_fill, (110, top))
        panel.paste(right_cap, (113, top))


def widen_base_score_cell(panel: Image.Image) -> None:
    # Match the base-score value cell to the widened caption frames and the
    # equipment-detail outer rail. Its runtime score text is drawn later, so
    # only the frame texture changes here.
    top, bottom = 223, 252
    left_cap = panel.crop((9, top, 14, bottom))
    left_fill = panel.crop((14, top, 15, bottom)).resize(
        (3, bottom - top), Image.Resampling.BICUBIC
    )
    core = panel.crop((14, top, 110, bottom))
    right_fill = panel.crop((109, top, 110, bottom)).resize(
        (3, bottom - top), Image.Resampling.BICUBIC
    )
    right_cap = panel.crop((110, top, 115, bottom))
    panel.paste(left_cap, (6, top))
    panel.paste(left_fill, (11, top))
    panel.paste(core, (14, top))
    panel.paste(right_fill, (110, top))
    panel.paste(right_cap, (113, top))


def widen_equipment_detail_cell(panel: Image.Image) -> None:
    # Make the white/yellow/equipment/critical detail frame use the same outer
    # rails as the two headings and the base-score cell. The runtime labels
    # stay centred because this operation expands symmetrically around x=62.
    top, bottom = 273, 386
    left_cap = panel.crop((9, top, 14, bottom))
    left_fill = panel.crop((14, top, 15, bottom)).resize(
        (3, bottom - top), Image.Resampling.BICUBIC
    )
    core = panel.crop((14, top, 110, bottom))
    right_fill = panel.crop((109, top, 110, bottom)).resize(
        (3, bottom - top), Image.Resampling.BICUBIC
    )
    right_cap = panel.crop((110, top, 115, bottom))
    panel.paste(left_cap, (6, top))
    panel.paste(left_fill, (11, top))
    panel.paste(core, (14, top))
    panel.paste(right_fill, (110, top))
    panel.paste(right_cap, (113, top))


def flatten_for_gdi(panel: Image.Image) -> Image.Image:
    # StretchDIBits ignores the source alpha channel. Flattening the antialiased
    # edge onto a near-black prevents formerly transparent white RGB values
    # from appearing as a white fringe. The resulting frame is deliberately
    # opaque: colour-key holes made the game scene show through the original
    # anti-aliased edge and looked like overlapping borders.
    background = Image.new("RGBA", panel.size, GDI_EDGE_BACKGROUND)
    background.alpha_composite(panel)
    return background


def font(size: int, bold: bool = False) -> ImageFont.FreeTypeFont:
    candidates = (
        Path("C:/Windows/Fonts/msyhbd.ttc" if bold else "C:/Windows/Fonts/msyh.ttc"),
        Path("C:/Windows/Fonts/simhei.ttf"),
        Path("C:/Windows/Fonts/simsun.ttc"),
    )
    for candidate in candidates:
        if candidate.exists():
            return ImageFont.truetype(str(candidate), size=size)
    raise FileNotFoundError("no installed Chinese font found")


def centered_text(
    draw: ImageDraw.ImageDraw,
    box: tuple[int, int, int, int],
    value: str,
    text_font: ImageFont.FreeTypeFont,
    fill: tuple[int, int, int, int],
) -> None:
    left, top, right, bottom = box
    bounds = draw.textbbox((0, 0), value, font=text_font, stroke_width=0)
    width = bounds[2] - bounds[0]
    height = bounds[3] - bounds[1]
    x = left + (right - left - width) // 2
    y = top + (bottom - top - height) // 2 - bounds[1]
    draw.text((x + 1, y + 1), value, font=text_font, fill=(0, 0, 0, 230))
    draw.text((x, y), value, font=text_font, fill=fill)


def build_preview(panel: Image.Image) -> Image.Image:
    preview = panel.copy()
    icon = Image.open(RANK_ICON).convert("RGBA")
    preview.alpha_composite(icon, (19, 37))
    draw = ImageDraw.Draw(preview, "RGBA")

    draw.rectangle((35, 102, 82, 117), fill=(78, 52, 25, 255))
    centered_text(draw, (32, 100, 88, 120), "武炼", font(10, True), (245, 226, 171, 255))
    centered_text(draw, (5, 128, 113, 153), "134644", font(18, True), (255, 225, 101, 255))
    centered_text(draw, (5, 153, 113, 177), "剑皇  Lv.90", font(10), (220, 229, 232, 255))
    centered_text(draw, (5, 224, 113, 251), "11064", font(12, True), (103, 218, 255, 255))
    centered_text(draw, (7, 274, 111, 302), "白字  83.00%", font(10), (225, 236, 242, 255))
    centered_text(draw, (7, 302, 111, 330), "黄字  20.00%", font(10), (255, 216, 95, 255))
    centered_text(draw, (7, 330, 111, 358), "装备  253.41%", font(10), (255, 174, 104, 255))
    centered_text(draw, (7, 358, 111, 386), "爆伤  20.00%", font(10), (139, 218, 244, 255))
    return preview


def main() -> None:
    panel = Image.open(ORIGINAL).convert("RGBA")
    if panel.size != ORIGINAL_PANEL_SIZE:
        raise ValueError(
            f"original panel must be {ORIGINAL_PANEL_SIZE}; got {panel.size}"
        )

    widen_section_headers(panel)
    widen_base_score_cell(panel)
    widen_equipment_detail_cell(panel)
    build_four_row_equipment_cell(panel)
    # The original last three rows belong to its retired footer and create a
    # black tail below the real detail frame. Move its one-pixel metal bottom
    # edge up, then crop exactly at that edge so the sidecar is one solid layer
    # with no black footer and no transparent overlap.
    panel.paste(panel.crop((0, 386, 124, 387)), (0, 385))
    panel = flatten_for_gdi(panel)
    # The original's outermost three columns on either side are transparent
    # presentation padding, not part of the metal frame. Crop them away so
    # GDI cannot turn that padding into the reported black side bars.
    panel = panel.crop((
        PANEL_SOURCE_CROP_LEFT,
        0,
        PANEL_SOURCE_CROP_LEFT + PANEL_SIZE[0],
        PANEL_SIZE[1],
    ))
    panel.save(SOURCE, optimize=True)
    build_preview(panel).save(PREVIEW, optimize=True)
    BGRA.write_bytes(panel.tobytes("raw", "BGRA"))

    expected = PANEL_SIZE[0] * PANEL_SIZE[1] * 4
    if BGRA.stat().st_size != expected:
        raise ValueError(f"unexpected BGRA size {BGRA.stat().st_size}; expected {expected}")
    print(f"wrote {SOURCE}")
    print(f"wrote {PREVIEW}")
    print(f"wrote {BGRA} ({BGRA.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
