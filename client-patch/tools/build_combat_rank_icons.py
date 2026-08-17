#!/usr/bin/env python3
"""Split the generated seven-rank sheet into embedded 80px PBGRA icons."""

from __future__ import annotations

import argparse
from pathlib import Path

from PIL import Image


RANK_SLUGS = (
    "explore",
    "pioneer",
    "fearless",
    "conquer",
    "battle_pinnacle",
    "heroic",
    "martial_mastery",
)


def alpha_x_runs(image: Image.Image, threshold: int = 24) -> list[tuple[int, int]]:
    alpha = image.getchannel("A")
    occupied = []
    for x in range(image.width):
        occupied.append(alpha.crop((x, 0, x + 1, image.height)).getextrema()[1] > threshold)
    runs: list[tuple[int, int]] = []
    start = None
    for x, value in enumerate(occupied + [False]):
        if value and start is None:
            start = x
        elif not value and start is not None:
            if x - start > 24:
                runs.append((start, x))
            start = None
    return runs


def premultiplied_bgra(image: Image.Image) -> bytes:
    output = bytearray()
    for red, green, blue, alpha in image.get_flattened_data():
        output.extend((
            (blue * alpha + 127) // 255,
            (green * alpha + 127) // 255,
            (red * alpha + 127) // 255,
            alpha,
        ))
    return bytes(output)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--size", type=int, default=80)
    args = parser.parse_args()

    sheet = Image.open(args.source).convert("RGBA")
    runs = alpha_x_runs(sheet)
    if len(runs) != len(RANK_SLUGS):
        raise SystemExit(f"expected 7 alpha components, found {len(runs)}: {runs}")

    args.output.mkdir(parents=True, exist_ok=True)
    previews: list[Image.Image] = []
    for slug, (left, right) in zip(RANK_SLUGS, runs):
        alpha = sheet.getchannel("A").crop((left, 0, right, sheet.height))
        box = alpha.getbbox()
        if box is None:
            raise SystemExit(f"empty icon component: {slug}")
        box = (left + box[0], box[1], left + box[2], box[3])
        icon = sheet.crop(box)
        scale = min((args.size - 4) / icon.width, (args.size - 4) / icon.height)
        resized = icon.resize(
            (max(1, round(icon.width * scale)), max(1, round(icon.height * scale))),
            Image.Resampling.LANCZOS,
        )
        canvas = Image.new("RGBA", (args.size, args.size), (0, 0, 0, 0))
        canvas.alpha_composite(
            resized,
            ((args.size - resized.width) // 2, (args.size - resized.height) // 2),
        )
        png_path = args.output / f"combat_rank_{slug}_v3.png"
        raw_path = args.output / f"combat_rank_{slug}_v3.pbgra"
        canvas.save(png_path)
        raw_path.write_bytes(premultiplied_bgra(canvas))
        previews.append(canvas)
        print(f"{slug}: source={box} output={png_path.name} bytes={raw_path.stat().st_size}")

    strip = Image.new("RGBA", (args.size * len(previews), args.size), (0, 0, 0, 0))
    for index, icon in enumerate(previews):
        strip.alpha_composite(icon, (index * args.size, 0))
    strip.save(args.output / "combat_power_rank_icons_v3_preview.png")


if __name__ == "__main__":
    main()
