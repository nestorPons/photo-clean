# photo-clean

## Qué es
Wapp que recibe un ZIP con imágenes → las clasifica → descarta las que **no** contienen motivo (persona / animal) → devuelve un ZIP con las aceptadas.

## Stack
- **Go** — servidor HTTP + lógica de clasificación
- **CSS** plano — sin frameworks
- **HTMX** — interacción sin recarga
- **Vanilla JS** — solo lo imprescindible
- Sin bundler, sin Node

## Fases

### V1 — Clasificador (script, prioritario)
- Entrada: ZIP de imágenes (`jpg`, `jpeg`, `png`, `webp`)
- Salida: clasificación por imagen → `motivo` (persona/animal) | `paisaje`
- Proveedores:
  1. **OpenAI Decisions** `gpt-6-luna` (prioritario)
  2. **OpenAI Chat Completions** con visión (alternativa / fallback)
- Contrato común:
  ```go
  type Classifier interface {
      Classify(ctx context.Context, img []byte) (Result, error)
  }
  type Result struct {
      HasSubject bool      // true = persona o animal
      Labels     []string
      Score      float64
      Provider   string
  }
  ```
- Dataset de test:
  - `dataset/motivo/` — imágenes con persona y/o animal
  - `dataset/paisaje/` — imágenes solo paisaje
  - Mezcla + ground truth → ejecución del clasificador → informe
- Métricas: matriz de confusión, precision, recall, F1
  - **FN** (motivo descartado) = error grave
  - **FP** (paisaje conservado) = error tolerable
- Objetivos: sistema de clasificación operativo, modelo Decisions como primario y testeo reproducible
- Criterio de aceptación: recall ≥ 0.95 sobre `dataset/motivo/`

### V2 — Web
- Estética buscador: página blanca, contenido centrado, mínima
- Componentes: dropzone ZIP + botón procesar + zona de resultados
- Resultados: contadores **eliminadas** / **conservadas** + listado y descarga del ZIP final
- Progreso con HTMX (polling o SSE); sin estado en cliente

### V3 — Despliegue
- Dockerfile multi-stage + `docker-compose.yml`
- Variable de entorno para la API key (`OPENAI_API_KEY`)
- Subida a producción vía rama `main`

## Estructura prevista
```
cmd/
  photo-clean/        # servidor (V2)
  classify/           # CLI de test (V1)
internal/
  classify/           # interfaz Classifier + implementación decisions / openai
  archive/            # lectura y escritura de ZIP
  web/                # handlers, plantillas, assets
web/
  templates/
  static/
config/
  config.yaml        # rutas de API y constantes de configuración
dataset/
  motivo/
  paisaje/
docs/
  logs/
```

## Reglas de UI
- `border-radius` ≤ **6px** en botones, inputs, cards, secciones, labels y badges (salvo formas circulares o necesidad de diseño)
- **Prohibido** usar `border-left` de color como único acento en cards/avisos/alertas → usar borde completo, fondo, badge/icono o sombra

## Convenciones
- Código en inglés, textos de UI y docs en español
- Errores con `fmt.Errorf("...: %w", err)`
- API keys solo por entorno, nunca en el repo
- `.env.example` versionado, `.env` ignorado
- Sin GitHub Actions salvo petición explícita

## Comandos
```bash
go run ./cmd/classify -in dataset/ -provider decisions -out informe.json
go run ./cmd/photo-clean
docker compose up -d
```

## Logs
Cada cambio de estado → entrada en `docs/logs/YYYY-MM-DD.md`.
