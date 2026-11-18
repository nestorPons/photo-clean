# photo-clean

ZIP de fotos → clasificación → descarta las que no tienen persona ni animal → ZIP filtrado.

## Estado
| Fase | Contenido | Estado |
|---|---|---|
| V1 | Clasificador + test con dataset | ✅ |
| V2 | Web (dropzone + resultados) | ✅ |
| V3 | Despliegue | pendiente |

## Stack
Go · CSS plano · HTMX · vanilla JS · Docker

## Inicio rápido
```bash
git clone git@github.com:nestorPons/photo-clean.git
cd photo-clean
cp .env.example .env          # añade tu OPENAI_API_KEY
go run ./cmd/photo-clean      # http://127.0.0.1:8080
```
Sin clave de API, para probar la interfaz y el pipeline con el proveedor sintético:
```bash
CLASSIFIER_PROVIDER=mock go run ./cmd/photo-clean
```
Requisitos: Go 1.22+ (desarrollo con 1.27) y, para los scripts de dataset, Python 3 con Pillow.

## Estructura
```
cmd/classify        CLI de clasificación y test
cmd/photo-clean     servidor web
internal/classifier fábrica del proveedor configurado
internal/classify   interfaz + cadena con fallback
  decisions         proveedor OpenAI Decisions (gpt-6-luna)
  openai            proveedor OpenAI (visión)
  mock              proveedor sintético para pruebas
internal/web        handlers, trabajos y pipeline web
web/templates       plantillas HTML
web/static          CSS, JS y HTMX vendorizado
internal/imageprep  decodifica y reescala a JPEG
internal/zipset     lectura/escritura de ZIP
internal/dataset    descubre motivo/ y paisaje/
internal/evaluate   matriz de confusión y métricas
config/             configuración de la aplicación (YAML)
dataset/            imágenes de test (no versionadas)
scripts/            descarga del dataset, revisión y ZIP de prueba
docs/logs/          registro de cambios
```

## Configuración
| Archivo | Contenido |
|---|---|
| `config/config.yaml` | rutas de API, modelos, límites, rutas de salida |
| `.env` | solo secreto (`OPENAI_API_KEY`) |

Precedencia: variables de entorno > `config/config.yaml` > defaults del código.
Ruta alternativa del YAML: `CONFIG_FILE=/ruta/config.yaml`.

Bloques del archivo:
| Bloque | Contenido |
|---|---|
| `app` | dirección y entorno |
| `paths` | dataset, directorio temporal de trabajos, informe y ZIPs de salida |
| `classify` | proveedor, umbral, concurrencia, timeout, recall mínimo |
| `decisions` | `base_url`, `endpoint`, modelo, reintentos, límites de imagen |
| `openai` | `base_url`, `endpoint`, modelo, detalle de imagen, tokens |
| `http` | user-agent y tamaño máximo de respuesta |
| `zip` | tamaño máximo por imagen y número de archivos |
| `web` | tamaño máximo del ZIP, trabajos simultáneos y TTL |
| `prompts` | instrucciones de clasificación (Decisions y OpenAI) |

Los prompts se editan en `config/config.yaml` → `prompts` y se aplican en la siguiente ejecución, sin recompilar. Si se omiten, se usan los valores por defecto del código.

Overrides habituales: `CLASSIFIER_PROVIDER`, `CLASSIFIER_THRESHOLD`, `CLASSIFY_CONCURRENCY`, `CLASSIFY_TIMEOUT`, `DECISIONS_BASE_URL`, `DECISIONS_MODEL`, `OPENAI_MODEL`, `WEB_MAX_UPLOAD_BYTES`, `WEB_MAX_JOBS`, `WEB_JOB_TTL`...

## Web (V2)
```bash
go run ./cmd/photo-clean
# http://127.0.0.1:8080
```
Flujo:
1. Se sube un ZIP (multipart) a `POST /jobs`. La interfaz muestra spinner y porcentaje real de subida y, en cuanto termina la subida, "ZIP subido. Procesando imágenes…".
2. El servidor lo procesa en segundo plano y HTMX consulta `GET /jobs/{id}/progress` cada segundo. El panel indica siempre la fase (`En cola`, `Leyendo ZIP`, `Clasificando`) con spinner y aviso de espera, y se desplaza a la vista al crearse el trabajo.
3. Al terminar aparecen los contadores y las descargas:
   - `GET /jobs/{id}/aceptadas.zip` — conservadas
   - `GET /jobs/{id}/descartadas.zip` — eliminadas
4. Los trabajos viven en memoria y en un directorio temporal del sistema; se borran al terminar o al cumplir `web.job_ttl` (1 h por defecto). No hay base de datos ni volumen persistente.

Límites: `web.max_upload_bytes` (300 MB), `zip.max_files` (5.000), `zip.max_file_bytes` (25 MiB), `zip.max_total_bytes` (1 GiB descomprimido), `web.max_jobs` (2 en paralelo).

Endpoints auxiliares: `GET /healthz`, `GET /jobs/{id}`, `GET /static/...`.

La interfaz es HTML + CSS plano + HTMX vendorizado en `web/static/vendor/htmx.min.js`; sin bundler, sin Node.

## Uso
```bash
# Test contra el dataset etiquetado
go run ./cmd/classify -in dataset -provider decisions -out out/informe-decisions.json
go run ./cmd/classify -in dataset -provider openai -out out/informe-openai.json

# Prueba del pipeline sin APIs (smoke test)
go run ./cmd/classify -in dataset -provider mock -min-recall 0

# Procesar un ZIP real
go run ./cmd/classify -zip fotos.zip -provider chain \
  -accepted out/aceptadas.zip -rejected out/descartadas.zip
```

Salidas: informe JSON (`out/informe.json`) + resumen en consola con `TP/TN/FP/FN`, precisión, recall, F1 y lista de falsos negativos.

Códigos de salida: `0` ok · `1` error · `2` recall por debajo de `-min-recall` (0.95 por defecto).

## Dataset de prueba
```bash
python3 scripts/fetch_dataset.py --per-class 150   # COCO 2014 test vía Hugging Face
make classify-decisions
```
- `motivo` / `paisaje` etiquetados por captions (heurístico, revisar a mano).
- ~16 MB para 150+150 imágenes; no se versiona.
- Alternativas: LHQ 256 (paisajes), Places365 (escenas), Animals-10 (animales).

### Revisión manual
```bash
make review      # genera out/revision.html
xdg-open out/revision.html
```
- `out/informe-decisions.json`: informe completo (todas las imágenes, score, labels).
- `out/revision.html`: galería de FN, FP y errores con miniatura, caption y score.
- `dataset/manifest.tsv`: archivo, etiqueta y captions.

### ZIP de prueba (50 fotos)
```bash
make testzip           # genera out/e2e-50.zip
make classify-testzip  # informe de referencia out/informe-e2e-50.json
```
- 25 motivos de Flickr30k + 25 paisajes de LHQ 256 (fuentes distintas del COCO del dataset principal).
- Incluye `motivo/`, `paisaje/`, `manifest.tsv` y `LEEME.txt`.
- Referencia actual: 25 conservadas / 25 eliminadas, 0 errores.
- Generador: `scripts/make_test_zip.py`.

## Docker
```bash
docker network create traefik-net     # una sola vez en local (red compartida con Traefik)
docker compose up -d --build          # http://127.0.0.1:8080
docker compose --profile tools run --rm classify -in dataset -provider mock -min-recall 0
```
- `Dockerfile`: imagen de producción (multi-stage, binario estático, usuario sin privilegios).
- `Dockerfile.dev`: solo para el perfil `tools` (compila y ejecuta con `go run`).
- El compose publica el puerto únicamente en `127.0.0.1` y declara las labels de Traefik; el servicio escucha en `:8080` dentro del contenedor.
- Los trabajos viven en el directorio temporal del contenedor; se borran al parar. No hay volumen de persistencia.

## Despliegue (VPS)
```bash
ssh vps-hostinger
cd /root/projects/phot-clean
git pull
docker compose up -d --build
```
- Dominio: `photo-clean.nestorpons.com` (Traefik + `myresolver`, red `traefik-net`).
- `.env` del servidor: `OPENAI_API_KEY`, `APP_PORT=8091`, `ROUTER=photo-clean`, `DOMAIN=photo-clean.nestorpons.com`.
- DNS: registro `A` de `photo-clean` → `93.127.203.142` en Hostinger.

## Proveedores
| Proveedor | API | Uso |
|---|---|---|
| `decisions` | OpenAI Decisions · `POST /v1/decisions` · `gpt-6-luna` | prioritario; imagen + preguntas tipadas |
| `openai` | Chat Completions · `gpt-4o-mini` | alternativa con JSON |
| `chain` | decisions → openai | fallback automático + circuit breaker |
| `mock` | — | smoke tests sin API |

- Ambos proveedores reales usan `OPENAI_API_KEY`.
- `decisions` responde `probability` para `has_subject` y `choice` para `subject` (`person`/`animal`/`both`/`none`).
- El veredicto se decide con `classify.threshold` (0.5 por defecto).

## Tests
```bash
go test ./...
```
