#!/usr/bin/env python3
"""Render the native combat-panel layout for screenshot comparison.

This is a visual QA helper only. Runtime drawing remains owned by 90CN.cpp.
"""

from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


ROOT = Path(__file__).resolve().parents[1]
WORKSPACE = ROOT.parent
OUTPUT = WORKSPACE / "runtime" / "diagnostics"
REFERENCE = Path(
    r"C:\Users\ADMINI~1\AppData\Local\Temp\codex-clipboard-aac8b4e3-225b-4b82-9523-03bdd2980fd6.png"
)
FONT = Path(r"C:\Windows\Fonts\msyh.ttc")


def font(size: int, bold: bool = False) -> ImageFont.FreeTypeFont:
    path = Path(r"C:\Windows\Fonts\msyhbd.ttc") if bold else FONT
    return ImageFont.truetype(str(path), size)


def centered(
    draw: ImageDraw.ImageDraw,
    text: str,
    box: tuple[int, int, int, int],
    size: int,
    color: tuple[int, int, int],
    bold: bool = False,
) -> None:
    left, top, right, bottom = box
    position = ((left + right) // 2, (top + bottom) // 2)
    face = font(size, bold)
    draw.text((position[0] + 1, position[1] + 1), text, font=face,
              fill=(0, 0, 0), anchor="mm")
    draw.text(position, text, font=face, fill=color, anchor="mm")


def render_panel() -> Image.Image:
    panel = Image.open(
        ROOT / "assets" / "combat_power_panel_skin_v2_preview.png"
    ).convert("RGBA")
    icon = Image.open(
        ROOT / "assets" / "combat_rank_martial_mastery_v3.png"
    ).convert("RGBA")
    panel.alpha_composite(icon, (24, 29))
    draw = ImageDraw.Draw(panel)

    centered(draw, "我的战斗力值", (7, 5, 113, 28), 13,
             (225, 242, 251), True)
    centered(draw, "?", (105, 25, 122, 42), 11,
             (245, 202, 91), True)
    centered(draw, "200242", (8, 116, 120, 143), 19,
             (255, 225, 101), True)
    centered(draw, "武炼", (34, 96, 94, 114), 9,
             (245, 226, 171), True)
    centered(draw, "基础属性加成", (9, 158, 119, 176), 10,
             (184, 224, 242), True)
    centered(draw, "17074", (9, 174, 119, 195), 12,
             (103, 218, 255), True)
    centered(draw, "装备加成", (9, 198, 119, 225), 10,
             (184, 224, 242), True)

    rows = (
        ("白字  37.00%", (225, 236, 242)),
        ("黄字  20.00%", (255, 216, 95)),
        ("爆伤  20.00%", (255, 174, 104)),
        ("黄追  16.00%", (249, 232, 145)),
        ("爆追  18.00%", (246, 190, 132)),
        ("全攻  79.00%", (161, 216, 241)),
    )
    for index, (label, color) in enumerate(rows):
        top = 227 + index * 25
        centered(draw, label, (12, top, 116, top + 25), 10, color)
    return panel


def main() -> None:
    OUTPUT.mkdir(parents=True, exist_ok=True)
    panel = render_panel()
    panel.save(OUTPUT / "combat-power-v4-layout-preview.png")

    reference = Image.open(REFERENCE).convert("RGB").crop((20, 10, 144, 406))
    scale = 2
    reference = reference.resize(
        (reference.width * scale, reference.height * scale),
        Image.Resampling.NEAREST,
    )
    rendered = panel.convert("RGB").resize(
        (panel.width * scale, panel.height * scale),
        Image.Resampling.NEAREST,
    )
    canvas = Image.new(
        "RGB",
        (reference.width + rendered.width + 72,
         max(reference.height, rendered.height) + 72),
        (23, 31, 39),
    )
    draw = ImageDraw.Draw(canvas)
    draw.text((24, 18), "参考截图", font=font(16, True), fill=(235, 240, 244))
    right_x = reference.width + 48
    draw.text((right_x, 18), "精简排版候选", font=font(16, True),
              fill=(235, 240, 244))
    canvas.paste(reference, (24, 48))
    canvas.paste(rendered, (right_x, 48))
    canvas.save(OUTPUT / "combat-power-v4-layout-comparison.png")


if __name__ == "__main__":
    main()
