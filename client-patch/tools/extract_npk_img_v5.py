#!/usr/bin/env python3
"""Extract IMG v5 sprites from a DNF NPK archive.

This small development helper implements only the parts needed by the 90CN
combat-power UI asset audit: encrypted NPK paths, IMG v5 atlas/direct sprites,
ARGB 1555/4444/8888 pixels, zlib blocks, links, and labelled contact sheets.
"""

from __future__ import annotations

import argparse
import io
import math
import struct
import zlib
from dataclasses import dataclass
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


NPK_MAGIC = b"NeoplePack_Bill\0"
IMG_MAGIC = b"Neople Img File\0"
LINK = 0x11


def read_i32(stream: io.BufferedIOBase) -> int:
    return struct.unpack("<i", stream.read(4))[0]


def read_i64(stream: io.BufferedIOBase) -> int:
    return struct.unpack("<q", stream.read(8))[0]


def npk_key() -> bytes:
    key = bytearray(256)
    header = b"puchikon@neople dungeon and fighter "
    key[: len(header)] = header
    suffix = b"DNF"
    for index in range(len(header), 255):
        key[index] = suffix[index % 3]
    return bytes(key)


@dataclass
class NpkEntry:
    offset: int
    length: int
    path: str


@dataclass
class Texture:
    version: int
    color_bits: int
    index: int
    length: int
    full_length: int
    width: int
    height: int
    data: bytes = b""


@dataclass
class Sprite:
    index: int
    color_bits: int
    compress_mode: int = 0
    width: int = 0
    height: int = 0
    length: int = 0
    x: int = 0
    y: int = 0
    frame_width: int = 0
    frame_height: int = 0
    target: int | None = None
    texture_index: int | None = None
    left: int = 0
    top: int = 0
    right: int = 0
    bottom: int = 0
    rotate: int = 0
    data: bytes = b""


def read_npk_index(npk_path: Path) -> list[NpkEntry]:
    key = npk_key()
    entries: list[NpkEntry] = []
    with npk_path.open("rb") as stream:
        if stream.read(16) != NPK_MAGIC:
            raise ValueError(f"not a DNF NPK: {npk_path}")
        count = read_i32(stream)
        for _ in range(count):
            offset = read_i32(stream)
            length = read_i32(stream)
            encrypted = stream.read(256)
            decoded = bytes(value ^ key[index] for index, value in enumerate(encrypted))
            path = decoded.split(b"\0", 1)[0].decode("utf-8", errors="replace")
            entries.append(NpkEntry(offset, length, path))
    return entries


def decode_pixels(data: bytes, width: int, height: int, color_bits: int) -> Image.Image:
    pixel_count = width * height
    rgba = bytearray(pixel_count * 4)
    if color_bits == 0x10:  # BGRA 8888
        if len(data) < pixel_count * 4:
            raise ValueError("short ARGB8888 sprite")
        for index in range(pixel_count):
            b, g, r, a = data[index * 4 : index * 4 + 4]
            rgba[index * 4 : index * 4 + 4] = bytes((r, g, b, a))
    elif color_bits in (0x0E, 0x0F):
        if len(data) < pixel_count * 2:
            raise ValueError("short 16-bit sprite")
        for index in range(pixel_count):
            value = struct.unpack_from("<H", data, index * 2)[0]
            if color_bits == 0x0E:  # ARGB 1555
                a = 255 if value & 0x8000 else 0
                r = ((value >> 10) & 0x1F) * 255 // 31
                g = ((value >> 5) & 0x1F) * 255 // 31
                b = (value & 0x1F) * 255 // 31
            else:  # ARGB 4444
                a = ((value >> 12) & 0x0F) * 17
                r = ((value >> 8) & 0x0F) * 17
                g = ((value >> 4) & 0x0F) * 17
                b = (value & 0x0F) * 17
            rgba[index * 4 : index * 4 + 4] = bytes((r, g, b, a))
    else:
        raise ValueError(f"unsupported color bits 0x{color_bits:02X}")
    return Image.frombytes("RGBA", (width, height), bytes(rgba))


def decompress_block(data: bytes, expected: int = 0) -> bytes:
    raw = zlib.decompress(data)
    if expected and len(raw) != expected:
        raise ValueError(f"zlib size mismatch: got {len(raw)}, expected {expected}")
    return raw


def read_img_payload(npk_path: Path, entry: NpkEntry) -> io.BytesIO:
    with npk_path.open("rb") as base:
        base.seek(entry.offset)
        return io.BytesIO(base.read(entry.length))


def extract_img_v2(payload: io.BytesIO, sprite_count: int) -> tuple[list[Sprite], list[Image.Image]]:
    sprites: list[Sprite] = []
    direct_sprites: list[Sprite] = []
    for index in range(sprite_count):
        color_bits = read_i32(payload)
        sprite = Sprite(index=index, color_bits=color_bits)
        sprites.append(sprite)
        if color_bits == LINK:
            sprite.target = read_i32(payload)
            continue
        sprite.compress_mode = read_i32(payload)
        sprite.width = read_i32(payload)
        sprite.height = read_i32(payload)
        sprite.length = read_i32(payload)
        sprite.x = read_i32(payload)
        sprite.y = read_i32(payload)
        sprite.frame_width = read_i32(payload)
        sprite.frame_height = read_i32(payload)
        if sprite.compress_mode == 0x05:
            bytes_per_pixel = 4 if color_bits == 0x10 else 2
            sprite.length = sprite.width * sprite.height * bytes_per_pixel
        direct_sprites.append(sprite)

    images: list[Image.Image | None] = [None] * sprite_count
    for sprite in direct_sprites:
        sprite.data = payload.read(sprite.length)
        raw = zlib.decompress(sprite.data) if sprite.compress_mode == 0x06 else sprite.data
        images[sprite.index] = decode_pixels(raw, sprite.width, sprite.height, sprite.color_bits)
    for sprite in sprites:
        if sprite.target is not None:
            images[sprite.index] = images[sprite.target]
    return sprites, [image if image is not None else Image.new("RGBA", (1, 1)) for image in images]


def extract_img(npk_path: Path, entry: NpkEntry) -> tuple[list[Sprite], list[Image.Image]]:
    payload = read_img_payload(npk_path, entry)

    if payload.read(16) != IMG_MAGIC:
        raise ValueError(f"entry is not an IMG: {entry.path}")
    _index_length = read_i64(payload)
    version = read_i32(payload)
    sprite_count = read_i32(payload)
    if version == 2:
        return extract_img_v2(payload, sprite_count)
    if version != 5:
        raise ValueError(f"expected IMG v2/v5, found v{version}: {entry.path}")

    texture_count = read_i32(payload)
    _album_length = read_i32(payload)
    palette_count = read_i32(payload)
    if palette_count:
        # IMG v5 palettes are four-byte entries in ExtractorSharp.
        payload.seek(palette_count * 4, io.SEEK_CUR)

    textures = [
        Texture(
            version=read_i32(payload),
            color_bits=read_i32(payload),
            index=read_i32(payload),
            length=read_i32(payload),
            full_length=read_i32(payload),
            width=read_i32(payload),
            height=read_i32(payload),
        )
        for _ in range(texture_count)
    ]

    sprites: list[Sprite] = []
    direct_sprites: list[Sprite] = []
    for index in range(sprite_count):
        color_bits = read_i32(payload)
        sprite = Sprite(index=index, color_bits=color_bits)
        sprites.append(sprite)
        if color_bits == LINK:
            sprite.target = read_i32(payload)
            continue
        sprite.compress_mode = read_i32(payload)
        sprite.width = read_i32(payload)
        sprite.height = read_i32(payload)
        sprite.length = read_i32(payload)
        sprite.x = read_i32(payload)
        sprite.y = read_i32(payload)
        sprite.frame_width = read_i32(payload)
        sprite.frame_height = read_i32(payload)
        if color_bits < LINK and sprite.length:
            direct_sprites.append(sprite)
            continue
        _unknown = read_i32(payload)
        sprite.texture_index = read_i32(payload)
        sprite.left = read_i32(payload)
        sprite.top = read_i32(payload)
        sprite.right = read_i32(payload)
        sprite.bottom = read_i32(payload)
        sprite.rotate = read_i32(payload)

    for texture in textures:
        texture.data = payload.read(texture.length)
    for sprite in direct_sprites:
        sprite.data = payload.read(sprite.length)

    texture_images: dict[int, Image.Image] = {}
    for texture in textures:
        raw = decompress_block(texture.data, texture.full_length)
        if texture.color_bits >= LINK:
            raise ValueError(
                f"DDS/DXT atlas 0x{texture.color_bits:02X} is not needed by this audit yet"
            )
        texture_images[texture.index] = decode_pixels(
            raw, texture.width, texture.height, texture.color_bits
        )

    images: list[Image.Image | None] = [None] * sprite_count
    for sprite in sprites:
        if sprite.target is not None:
            continue
        if sprite.color_bits < LINK and sprite.length:
            raw = (
                decompress_block(sprite.data)
                if sprite.compress_mode in (0x06, 0x07)
                else sprite.data
            )
            images[sprite.index] = decode_pixels(raw, sprite.width, sprite.height, sprite.color_bits)
            continue
        atlas = texture_images[sprite.texture_index]
        image = atlas.crop((sprite.left, sprite.top, sprite.right, sprite.bottom))
        if sprite.rotate:
            image = image.transpose(Image.Transpose.TRANSPOSE)
        images[sprite.index] = image

    for sprite in sprites:
        if sprite.target is not None:
            images[sprite.index] = images[sprite.target]

    return sprites, [image if image is not None else Image.new("RGBA", (1, 1)) for image in images]


def save_contact_sheet(images: list[Image.Image], output: Path) -> None:
    cell_width = 220
    cell_height = 180
    columns = 5
    rows = math.ceil(len(images) / columns)
    sheet = Image.new("RGB", (columns * cell_width, rows * cell_height), (28, 31, 38))
    draw = ImageDraw.Draw(sheet)
    font = ImageFont.load_default()
    for index, image in enumerate(images):
        x = (index % columns) * cell_width
        y = (index // columns) * cell_height
        checker = Image.new("RGBA", image.size, (55, 59, 68, 255))
        preview = Image.alpha_composite(checker, image)
        preview.thumbnail((cell_width - 12, cell_height - 28), Image.Resampling.NEAREST)
        sheet.paste(preview.convert("RGB"), (x + 6, y + 22))
        draw.text((x + 6, y + 4), f"#{index}  {image.width}x{image.height}", fill=(245, 220, 148), font=font)
    output.parent.mkdir(parents=True, exist_ok=True)
    sheet.save(output)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("npk", type=Path)
    parser.add_argument("img_path")
    parser.add_argument("output", type=Path)
    args = parser.parse_args()

    entries = read_npk_index(args.npk)
    entry = next((item for item in entries if item.path == args.img_path), None)
    if entry is None:
        raise SystemExit(f"IMG path not found: {args.img_path}")
    sprites, images = extract_img(args.npk, entry)
    args.output.mkdir(parents=True, exist_ok=True)
    for sprite, image in zip(sprites, images):
        image.save(args.output / f"frame-{sprite.index:03d}.png")
    save_contact_sheet(images, args.output / "contact-sheet.png")
    print(f"extracted {len(images)} frames from {entry.path} to {args.output}")


if __name__ == "__main__":
    main()
