#!/usr/bin/env python3
"""Monta un dataset equilibrado para photo-clean desde Hugging Face.

Fuente: COCO 2014 test (5.000 imágenes con 5 captions cada una).
- motivo:  captions mencionan persona o animal.
- paisaje: captions no mencionan ninguno.

Las etiquetas son heurísticas (basadas en captions): sirven para una primera
medición, no sustituyen una revisión manual.

Uso:
  python3 scripts/fetch_dataset.py --per-class 150
  python3 scripts/fetch_dataset.py --per-class 200 --out dataset
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request

DATASET = "nlphuji/mscoco_2014_5k_test_image_text_retrieval"
CONFIG = "TEST"
SPLIT = "test"
PAGE = 100

ROWS_URL = (
    "https://datasets-server.huggingface.co/rows"
    f"?dataset={DATASET.replace('/', '%2F')}&config={CONFIG}&split={SPLIT}"
)

PERSON_RE = re.compile(
    r"\b(person|people|man|woman|men|women|boy|girl|child|children|kid|baby|guy|lady)\b",
    re.IGNORECASE,
)
ANIMAL_RE = re.compile(
    r"\b(dog|dogs|cat|cats|puppy|kitten|horse|horses|cow|cows|sheep|bird|birds|"
    r"elephant|elephants|bear|bears|zebra|giraffe|animal|animals)\b",
    re.IGNORECASE,
)
IMAGE_EXT = (".jpg", ".jpeg", ".png", ".webp")
HEADERS = {"User-Agent": "photo-clean-dataset/0.1"}


def log(msg: str) -> None:
    print(msg, flush=True)


def fetch_json(url: str, attempts: int = 4) -> dict:
    last: Exception | None = None
    for attempt in range(attempts):
        try:
            req = urllib.request.Request(url, headers=HEADERS)
            with urllib.request.urlopen(req, timeout=45) as resp:
                return json.load(resp)
        except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as err:
            last = err
            time.sleep(1.5 * (attempt + 1))
    raise RuntimeError(f"no se pudo leer {url}: {last}")


def download(url: str, path: str, attempts: int = 4) -> None:
    last: Exception | None = None
    for attempt in range(attempts):
        try:
            req = urllib.request.Request(url, headers=HEADERS)
            with urllib.request.urlopen(req, timeout=60) as resp:
                data = resp.read()
            with open(path, "wb") as fh:
                fh.write(data)
            return
        except (urllib.error.URLError, TimeoutError, OSError) as err:
            last = err
            time.sleep(1.5 * (attempt + 1))
    raise RuntimeError(f"no se pudo descargar {url}: {last}")


def count_existing(directory: str) -> int:
    if not os.path.isdir(directory):
        return 0
    return sum(1 for name in os.listdir(directory) if name.lower().endswith(IMAGE_EXT))


def store_manifest(path: str, name: str, label: str, captions: list[str]) -> None:
    new = not os.path.exists(path)
    with open(path, "a", encoding="utf-8") as fh:
        if new:
            fh.write("file\tlabel\tcaptions\n")
        clean = " | ".join(c.strip().replace("\t", " ") for c in captions)
        fh.write(f"{name}\t{label}\t{clean}\n")


def main() -> int:
    parser = argparse.ArgumentParser(description="Descarga un dataset COCO equilibrado desde Hugging Face.")
    parser.add_argument("--per-class", type=int, default=150, help="imágenes por clase (defecto: 150)")
    parser.add_argument("--out", default="dataset", help="directorio de salida (defecto: dataset)")
    parser.add_argument("--max-scan", type=int, default=5000, help="máximo de filas a revisar")
    args = parser.parse_args()

    if args.per_class < 1:
        parser.error("--per-class debe ser >= 1")

    dirs = {"motivo": os.path.join(args.out, "motivo"), "paisaje": os.path.join(args.out, "paisaje")}
    for path in dirs.values():
        os.makedirs(path, exist_ok=True)

    counts = {label: count_existing(path) for label, path in dirs.items()}
    manifest = os.path.join(args.out, "manifest.tsv")

    log(f"Objetivo: {args.per_class} por clase · ya hay motivo={counts['motivo']} paisaje={counts['paisaje']}")

    scanned = 0
    offset = 0
    while offset < args.max_scan and (counts["motivo"] < args.per_class or counts["paisaje"] < args.per_class):
        data = fetch_json(f"{ROWS_URL}&offset={offset}&length={PAGE}")
        rows = data.get("rows", [])
        if not rows:
            break

        for row in rows:
            scanned += 1
            labels = row.get("row", {})
            captions = labels.get("caption") or []
            text = " ".join(captions)
            label = "motivo" if (PERSON_RE.search(text) or ANIMAL_RE.search(text)) else "paisaje"

            if counts[label] >= args.per_class:
                continue

            image = labels.get("image") or {}
            src = image.get("src")
            if not src:
                continue

            name = f"coco-{row.get('row_idx', offset):05d}.jpg"
            dest = os.path.join(dirs[label], name)
            if os.path.exists(dest):
                counts[label] += 1
                continue

            try:
                download(src, dest)
            except RuntimeError as err:
                log(f"  aviso: {err}")
                continue

            counts[label] += 1
            store_manifest(manifest, name, label, captions)

        offset += PAGE
        log(f"  revisadas={scanned} motivo={counts['motivo']} paisaje={counts['paisaje']}")

    log("")
    log(f"Listo: motivo={counts['motivo']} paisaje={counts['paisaje']} en {args.out}/")
    log(f"Manifest: {manifest}")
    if counts["motivo"] < args.per_class or counts["paisaje"] < args.per_class:
        log("Aviso: no se alcanzó el objetivo; aumenta --max-scan.")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
