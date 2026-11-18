#!/usr/bin/env python3
"""Crea un ZIP de prueba con 50 fotos mixtas (motivo / paisaje).

Fuentes (distintas del COCO usado en el dataset principal):
- motivo:  Flickr30k (nlphuji/flickr30k), etiquetado por captions.
- paisaje: LHQ 256 (larrygzlb1994/lhq_256), paisajes sin personas.

El ZIP incluye motivo/, paisaje/, manifest.tsv y LEEME.txt.
Uso:
  python3 scripts/make_test_zip.py --per-class 25 --out out/e2e-50 --zip out/e2e-50.zip
"""

from __future__ import annotations

import argparse
import json
import os
import random
import re
import sys
import time
import urllib.error
import urllib.request
import zipfile

try:
    from PIL import Image
except ImportError:  # pragma: no cover
    Image = None

FLICKR_ROWS = (
    "https://datasets-server.huggingface.co/rows"
    "?dataset=nlphuji%2Fflickr30k&config=TEST&split=test"
)
LHQ_ROWS = (
    "https://datasets-server.huggingface.co/rows"
    "?dataset=larrygzlb1994%2Flhq_256&config=default&split=train"
)
PAGE = 100
LHQ_OFFSETS = (0, 30000, 60000)

HEADERS = {"User-Agent": "photo-clean-testzip/0.1"}

PERSON_RE = re.compile(
    r"\b(person|people|man|men|woman|women|boy|boys|girl|girls|child|children|kid|kids|"
    r"baby|guy|guys|lady|ladies|crowd|player|worker|soldier|police)\b",
    re.IGNORECASE,
)
ANIMAL_RE = re.compile(
    r"\b(dog|dogs|puppy|puppies|cat|cats|kitten|kittens|horse|horses|cow|cows|sheep|"
    r"bird|birds|elephant|elephants|bear|bears|zebra|zebras|giraffe|giraffes|animal|animals|"
    r"lion|lions|tiger|tigers|monkey|monkeys|fish|duck|ducks|goose|geese|swan|swans|"
    r"rabbit|rabbits|mouse|mice|whale|whales|dolphin|dolphins|penguin|penguins|owl|owls|"
    r"fox|foxes|wolf|wolves|deer|goat|goats|pig|pigs|chicken|chickens|snake|snakes|turtle|turtles)\b",
    re.IGNORECASE,
)


def log(message: str) -> None:
    print(message, flush=True)


def fetch_json(url: str, attempts: int = 8) -> dict:
    last: Exception | None = None
    for attempt in range(attempts):
        try:
            req = urllib.request.Request(url, headers=HEADERS)
            with urllib.request.urlopen(req, timeout=60) as resp:
                return json.load(resp)
        except urllib.error.HTTPError as err:
            last = err
            wait = 30 if err.code == 429 else 2 * (attempt + 1)
            if err.code == 429:
                wait = int(err.headers.get("Retry-After", wait))
            log(f"  aviso HTTP {err.code}; espero {wait}s…")
            time.sleep(wait)
        except (urllib.error.URLError, TimeoutError, json.JSONDecodeError, ConnectionError) as err:
            last = err
            time.sleep(1.5 * (attempt + 1))
    raise RuntimeError(f"no se pudo leer {url}: {last}")


def download(url: str, path: str, attempts: int = 4) -> None:
    last: Exception | None = None
    for attempt in range(attempts):
        try:
            req = urllib.request.Request(url, headers=HEADERS)
            with urllib.request.urlopen(req, timeout=90) as resp:
                data = resp.read()
            with open(path, "wb") as fh:
                fh.write(data)
            return
        except (urllib.error.URLError, TimeoutError, OSError, ConnectionError) as err:
            last = err
            time.sleep(1.5 * (attempt + 1))
    raise RuntimeError(f"no se pudo descargar {url}: {last}")


def valid_image(path: str) -> bool:
    if Image is None:
        return os.path.getsize(path) > 1024
    try:
        with Image.open(path) as im:
            im.verify()
        return True
    except Exception:
        return False


def caption_label(captions: list[str]) -> str:
    text = " ".join(captions)
    if PERSON_RE.search(text):
        return "person"
    if ANIMAL_RE.search(text):
        return "animal"
    return ""


def collect_flickr(max_rows: int) -> list[dict]:
    rows: list[dict] = []
    offset = 0
    while len(rows) < max_rows:
        data = fetch_json(f"{FLICKR_ROWS}&offset={offset}&length={PAGE}")
        page = data.get("rows", [])
        if not page:
            break
        rows.extend(page)
        offset += PAGE
        log(f"  flickr: {len(rows)} filas")
        time.sleep(0.3)
    return rows


def collect_lhq(offsets: tuple[int, ...]) -> list[dict]:
    rows: list[dict] = []
    for offset in offsets:
        data = fetch_json(f"{LHQ_ROWS}&offset={offset}&length={PAGE}")
        for row in data.get("rows", []):
            item = row.get("row", {})
            src = (item.get("image") or {}).get("src")
            if not src:
                continue
            rows.append(
                {
                    "src": src,
                    "id": f"lhq-{row.get('row_idx', 0)}",
                    "row_idx": row.get("row_idx", 0),
                    "captions": [],
                }
            )
        log(f"  lhq: {len(rows)} candidatos")
        time.sleep(0.3)
    return rows


def pick_motive(rows: list[dict], per_class: int, seed: int) -> tuple[list[dict], list[dict]]:
    people: list[dict] = []
    animals: list[dict] = []
    seen: set[str] = set()

    for row in rows:
        item = row.get("row", {})
        src = (item.get("image") or {}).get("src")
        img_id = str(item.get("img_id") or row.get("row_idx"))
        if not src or img_id in seen:
            continue
        seen.add(img_id)

        captions = item.get("caption") or []
        label = caption_label(captions)
        candidate = {
            "src": src,
            "id": img_id,
            "row_idx": row.get("row_idx", 0),
            "captions": captions,
            "kind": label,
        }
        if label == "person":
            people.append(candidate)
        elif label == "animal":
            animals.append(candidate)

    rng = random.Random(seed)
    rng.shuffle(people)
    rng.shuffle(animals)

    half = per_class // 2
    selected = animals[:half] + people[: per_class - half]
    if len(selected) < per_class:
        used = {item["id"] for item in selected}
        for candidate in animals + people:
            if len(selected) >= per_class:
                break
            if candidate["id"] not in used:
                selected.append(candidate)
                used.add(candidate["id"])

    log(f"  flickr: personas={len(people)} animales={len(animals)}")
    return selected[:per_class], people


def main() -> int:
    parser = argparse.ArgumentParser(description="ZIP de prueba con Flickr30k + LHQ256.")
    parser.add_argument("--per-class", type=int, default=25, help="imágenes por clase")
    parser.add_argument("--out", default="out/e2e-50", help="carpeta de salida")
    parser.add_argument("--zip", default="out/e2e-50.zip", help="ZIP de salida")
    parser.add_argument("--rows", type=int, default=1200, help="filas de Flickr30k a explorar")
    parser.add_argument("--seed", type=int, default=42)
    args = parser.parse_args()

    if args.per_class < 1:
        parser.error("--per-class debe ser >= 1")

    motivo_dir = os.path.join(args.out, "motivo")
    paisaje_dir = os.path.join(args.out, "paisaje")
    for path in (motivo_dir, paisaje_dir):
        os.makedirs(path, exist_ok=True)

    log("Buscando motivos en Flickr30k…")
    flickr_rows = collect_flickr(args.rows)
    motive, _ = pick_motive(flickr_rows, args.per_class, args.seed)

    log("Buscando paisajes en LHQ256…")
    lhq_rows = collect_lhq(LHQ_OFFSETS)
    random.Random(args.seed + 1).shuffle(lhq_rows)

    if len(motive) < args.per_class or len(lhq_rows) < args.per_class:
        log(
            f"Aviso: candidatos insuficientes (motivo={len(motive)}, paisaje={len(lhq_rows)}). "
            "Aumenta --rows."
        )
        return 1

    manifest = ["file\tlabel\tcaptions"]
    downloaded = {"motivo": 0, "paisaje": 0}

    def save(candidate: dict, label: str) -> bool:
        name = f"{candidate['id']}.jpg"
        folder = motivo_dir if label == "motivo" else paisaje_dir
        dest = os.path.join(folder, name)
        try:
            download(candidate["src"], dest)
            if not valid_image(dest):
                os.remove(dest)
                log(f"  aviso: imagen inválida {name}")
                return False
        except RuntimeError as err:
            log(f"  aviso: {err}")
            return False
        captions = candidate.get("captions") or []
        clean = " | ".join(c.strip().replace("\t", " ") for c in captions)
        if not clean:
            clean = "LHQ256 (paisaje)"
        manifest.append(f"{label}/{name}\t{label}\t{clean}")
        downloaded[label] += 1
        return True

    for candidate in motive:
        if downloaded["motivo"] >= args.per_class:
            break
        save(candidate, "motivo")
    for candidate in lhq_rows:
        if downloaded["paisaje"] >= args.per_class:
            break
        save(candidate, "paisaje")

    if downloaded["motivo"] < args.per_class or downloaded["paisaje"] < args.per_class:
        log(f"Aviso: descargadas motivo={downloaded['motivo']} paisaje={downloaded['paisaje']}.")
        return 1

    with open(os.path.join(args.out, "manifest.tsv"), "w", encoding="utf-8") as fh:
        fh.write("\n".join(manifest) + "\n")

    with open(os.path.join(args.out, "LEEME.txt"), "w", encoding="utf-8") as fh:
        fh.write(
            "photo-clean · ZIP de prueba\n\n"
            f"motivo: {downloaded['motivo']}\n"
            f"paisaje: {downloaded['paisaje']}\n\n"
            "Fuentes (distintas del COCO del dataset principal):\n"
            "- motivo: Flickr30k (nlphuji/flickr30k, split test), etiquetas por captions.\n"
            "- paisaje: LHQ 256 (larrygzlb1994/lhq_256), paisajes sin personas.\n\n"
            "Sube este ZIP a photo-clean y compara los contadores con estas cifras.\n"
        )

    os.makedirs(os.path.dirname(os.path.abspath(args.zip)), exist_ok=True)
    with zipfile.ZipFile(args.zip, "w", zipfile.ZIP_DEFLATED) as zf:
        for root, _, files in os.walk(args.out):
            for name in sorted(files):
                if name.lower().endswith((".jpg", ".jpeg", ".png", ".webp")) or name in (
                    "manifest.tsv",
                    "LEEME.txt",
                ):
                    full = os.path.join(root, name)
                    rel = os.path.relpath(full, args.out)
                    zf.write(full, rel)

    size_mb = os.path.getsize(args.zip) / (1024 * 1024)
    log("")
    log(f"Listo: motivo={downloaded['motivo']} paisaje={downloaded['paisaje']}")
    log(f"Carpeta: {args.out}")
    log(f"ZIP: {args.zip} ({size_mb:.1f} MB)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
