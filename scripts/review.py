#!/usr/bin/env python3
"""Genera una página HTML para revisar a mano los errores del clasificador.

Lee el informe JSON de cmd/classify y el manifest del dataset, y escribe
una galería con los falsos negativos y falsos positivos.

Uso:
  python3 scripts/review.py --report out/informe-decisions.json --out out/revision.html
"""

from __future__ import annotations

import argparse
import html
import json
import os
import sys


def load_manifest(path: str) -> dict[str, str]:
    captions: dict[str, str] = {}
    if not os.path.exists(path):
        return captions
    with open(path, "r", encoding="utf-8") as fh:
        next(fh, None)
        for line in fh:
            parts = line.rstrip("\n").split("\t")
            if len(parts) >= 3:
                captions[parts[0]] = parts[2]
    return captions


def esc(value: object) -> str:
    return html.escape(str(value), quote=True)


def image_src(dataset: str, name: str, out_dir: str) -> str:
    path = os.path.join(dataset, name)
    return os.path.relpath(path, out_dir).replace(os.sep, "/")


def card(item: dict, captions: dict[str, str], dataset: str, out_dir: str, kind: str) -> str:
    name = item.get("name", "")
    score = item.get("score", 0)
    labels = ", ".join(item.get("labels") or []) or "—"
    caption = captions.get(os.path.basename(name), "")
    error = item.get("error", "")

    badge = '<span class="badge badge-fn">FN</span>' if kind == "fn" else '<span class="badge badge-fp">FP</span>'
    if kind == "error":
        badge = '<span class="badge badge-err">ERROR</span>'

    detail = f"score {score:.2f} · labels: {esc(labels)}"
    if error:
        detail = f"error: {esc(error[:180])}"

    caption_html = f'<p class="caption">{esc(caption)}</p>' if caption else ""

    return f"""
      <article class="card {kind}">
        <a class="thumb" href="{esc(image_src(dataset, name, out_dir))}" target="_blank">
          <img src="{esc(image_src(dataset, name, out_dir))}" alt="{esc(name)}" loading="lazy">
        </a>
        <div class="body">
          <div class="head">{badge}<code>{esc(name)}</code></div>
          <p class="detail">{detail}</p>
          {caption_html}
        </div>
      </article>"""


def section(title: str, subtitle: str, items: list[dict], kind: str, captions: dict, dataset: str, out_dir: str) -> str:
    if not items:
        return ""
    cards = "".join(card(item, captions, dataset, out_dir, kind) for item in items)
    return f"""
    <section>
      <h2>{esc(title)} <span class="count">{len(items)}</span></h2>
      <p class="hint">{esc(subtitle)}</p>
      <div class="grid">{cards}
      </div>
    </section>"""


def main() -> int:
    parser = argparse.ArgumentParser(description="Galería HTML de errores del clasificador.")
    parser.add_argument("--report", default="out/informe-decisions.json", help="informe JSON")
    parser.add_argument("--dataset", default="dataset", help="directorio del dataset")
    parser.add_argument("--out", default="out/revision.html", help="HTML de salida")
    args = parser.parse_args()

    if not os.path.exists(args.report):
        print(f"error: no existe {args.report}", file=sys.stderr)
        return 1

    with open(args.report, "r", encoding="utf-8") as fh:
        report = json.load(fh)

    out_dir = os.path.dirname(os.path.abspath(args.out))
    os.makedirs(out_dir, exist_ok=True)
    captions = load_manifest(os.path.join(args.dataset, "manifest.tsv"))

    items = report.get("items", [])
    fn = sorted([i for i in items if i.get("truth") and not i.get("predicted") and not i.get("error")], key=lambda i: i.get("score", 0))
    fp = sorted([i for i in items if not i.get("truth") and i.get("predicted") and not i.get("error")], key=lambda i: i.get("score", 0), reverse=True)
    errors = [i for i in items if i.get("error")]

    metrics = report.get("metrics", {})
    stats = f"""
      <span><b>{report.get('total', 0)}</b> imágenes</span>
      <span><b>{metrics.get('recall', 0):.3f}</b> recall</span>
      <span><b>{metrics.get('precision', 0):.3f}</b> precision</span>
      <span><b>{metrics.get('accuracy', 0):.3f}</b> accuracy</span>
      <span><b>{len(fn)}</b> FN</span>
      <span><b>{len(fp)}</b> FP</span>"""

    page = f"""<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Revisión · {esc(report.get('provider', 'photo-clean'))}</title>
<style>
  :root {{ --ink:#111827; --muted:#6b7280; --line:#e5e7eb; --bg:#fff; --soft:#f9fafb; }}
  * {{ box-sizing:border-box; }}
  body {{ margin:0; font-family:system-ui,-apple-system,"Segoe UI",Roboto,sans-serif; color:var(--ink); background:var(--bg); }}
  header {{ padding:28px 32px 20px; border-bottom:1px solid var(--line); }}
  h1 {{ margin:0 0 10px; font-size:1.5rem; letter-spacing:-.01em; }}
  .stats {{ display:flex; flex-wrap:wrap; gap:8px 18px; color:var(--muted); font-size:.9rem; }}
  .stats b {{ color:var(--ink); }}
  main {{ padding:24px 32px 60px; }}
  h2 {{ font-size:1.05rem; margin:0 0 4px; }}
  .count {{ color:var(--muted); font-weight:500; }}
  .hint {{ margin:0 0 16px; color:var(--muted); font-size:.85rem; }}
  section {{ margin-bottom:36px; }}
  .grid {{ display:grid; grid-template-columns:repeat(auto-fill,minmax(240px,1fr)); gap:14px; }}
  .card {{ border:1px solid var(--line); border-radius:6px; overflow:hidden; background:var(--soft); display:flex; flex-direction:column; }}
  .card.fn {{ border-color:#fecaca; background:#fff5f5; }}
  .card.fp {{ border-color:#fde68a; background:#fffdf3; }}
  .card.error {{ border-color:#c7d2fe; background:#f5f7ff; }}
  .thumb {{ display:block; aspect-ratio:4/3; background:#e5e7eb; }}
  .thumb img {{ width:100%; height:100%; object-fit:cover; display:block; }}
  .body {{ padding:10px 12px 12px; }}
  .head {{ display:flex; align-items:center; gap:8px; flex-wrap:wrap; }}
  code {{ font-size:.72rem; color:#374151; word-break:break-all; }}
  .detail {{ margin:8px 0 0; font-size:.78rem; color:#374151; }}
  .caption {{ margin:6px 0 0; font-size:.75rem; color:var(--muted); line-height:1.35; }}
  .badge {{ font-size:.68rem; font-weight:700; padding:2px 7px; border-radius:6px; letter-spacing:.02em; }}
  .badge-fn {{ background:#fee2e2; color:#991b1b; }}
  .badge-fp {{ background:#fef3c7; color:#92400e; }}
  .badge-err {{ background:#e0e7ff; color:#3730a3; }}
</style>
</head>
<body>
<header>
  <h1>Revisión de errores · {esc(report.get('provider', ''))}</h1>
  <div class="stats">{stats}</div>
</header>
<main>
  {section("Falsos negativos", "Imágenes con motivo que el clasificador descartó. Es el error grave: revisa cada una.", fn, "fn", captions, args.dataset, out_dir)}
  {section("Falsos positivos", "Imágenes de paisaje que el clasificador conservó. Error tolerable, pero indica sujetos no evidentes.", fp, "fp", captions, args.dataset, out_dir)}
  {section("Errores de clasificación", "Peticiones fallidas: cuentan como descarte.", errors, "error", captions, args.dataset, out_dir)}
</main>
</body>
</html>
"""

    with open(args.out, "w", encoding="utf-8") as fh:
        fh.write(page)

    print(f"Revisión: {args.out} ({len(fn)} FN, {len(fp)} FP, {len(errors)} errores)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
