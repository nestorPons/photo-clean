# dataset

Imágenes para testear el clasificador. **No se versionan** (solo `.gitkeep`).

```
dataset/
  motivo/    # personas y/o animales
  paisaje/   # sin persona ni animal
  manifest.tsv
```

## Generación automática (Hugging Face)

Fuente: `nlphuji/mscoco_2014_5k_test_image_text_retrieval` (COCO 2014 test, 5.000 imágenes con captions).

```bash
python3 scripts/fetch_dataset.py --per-class 150
```
- Descarga 150 imágenes por clase (~16 MB) vía la API de datasets-server.
- Etiqueta por captions: menciona persona/animal → `motivo`; si no → `paisaje`.
- Reanudable: cuenta los archivos existentes y no repite.
- Deja `dataset/manifest.tsv` con archivo, etiqueta y captions.

**Las etiquetas son heurísticas.** Antes de sacar conclusiones definitivas, revisa una muestra y corrige a mano los casos dudosos (figurinas, cuadros, fotos de fotos, personas de fondo).

## Otras fuentes útiles
| Dataset | Uso | Tamaño |
|---|---|---|
| `larrygzlb1994/lhq_256` | paisajes puros (90k, 256px) | 10 GB |
| `dpdl-benchmark/Places365-Validation` | escenas naturales (36.5k) | 2.2 GB |
| `Rapidata/Animals-10` | animales, 10 clases (23.5k) | 300 MB |

## Reglas
- Formatos: `jpg`, `jpeg`, `png`, `webp`
- Mínimo recomendado: 100–200 por clase, variadas (interior/exterior, día/noche, sujeto pequeño, contraluz, multitud)
- Casos límite: estatua, maniquí, cartel con una persona, animal de juguete, foto de una foto

## Ejecución
```bash
go run ./cmd/classify -in dataset -provider decisions -out out/informe-decisions.json
```
