// Package zipset lee y escribe los ZIP de imágenes.
package zipset

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var imageExt = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".webp": true,
}

// Item es una imagen extraída del ZIP, con su ruta original.
type Item struct {
	Name string
	Data []byte
}

// IsImage indica si el nombre tiene una extensión admitida.
func IsImage(name string) bool {
	return imageExt[strings.ToLower(filepath.Ext(name))]
}

// Read extrae las imágenes del ZIP. Omite carpetas, metadatos de macOS y
// archivos que superen maxFileBytes.
func Read(zipPath string, maxFileBytes int64, maxFiles int) (items []Item, skipped []string, err error) {
	return ReadLimit(zipPath, maxFileBytes, maxFiles, 0)
}

// ReadLimit es como Read, pero además limita el total descomprimido. Un
// maxTotalBytes <= 0 desactiva el límite.
func ReadLimit(zipPath string, maxFileBytes int64, maxFiles int, maxTotalBytes int64) (items []Item, skipped []string, err error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, nil, fmt.Errorf("abrir zip: %w", err)
	}
	defer r.Close()

	var total int64
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if shouldSkip(name) || !IsImage(name) {
			continue
		}
		if maxFiles > 0 && len(items) >= maxFiles {
			skipped = append(skipped, name+": límite de archivos alcanzado")
			continue
		}
		if maxFileBytes > 0 && int64(f.UncompressedSize64) > maxFileBytes {
			skipped = append(skipped, name+": supera el tamaño máximo")
			continue
		}
		if maxTotalBytes > 0 && total+int64(f.UncompressedSize64) > maxTotalBytes {
			return nil, skipped, fmt.Errorf("el zip descomprimido supera el límite de %d MiB", maxTotalBytes>>20)
		}

		rc, openErr := f.Open()
		if openErr != nil {
			skipped = append(skipped, name+": "+openErr.Error())
			continue
		}

		data, readErr := io.ReadAll(io.LimitReader(rc, maxFileBytes+1))
		rc.Close()
		if readErr != nil {
			skipped = append(skipped, name+": "+readErr.Error())
			continue
		}
		if maxFileBytes > 0 && int64(len(data)) > maxFileBytes {
			skipped = append(skipped, name+": supera el tamaño máximo")
			continue
		}
		if maxTotalBytes > 0 && total+int64(len(data)) > maxTotalBytes {
			return nil, skipped, fmt.Errorf("el zip descomprimido supera el límite de %d MiB", maxTotalBytes>>20)
		}
		total += int64(len(data))

		items = append(items, Item{Name: name, Data: data})
	}

	if len(items) == 0 {
		return nil, skipped, fmt.Errorf("el zip no contiene imágenes admitidas (jpg, jpeg, png, webp)")
	}
	return items, skipped, nil
}

// Write guarda los items en un ZIP nuevo conservando sus rutas.
func Write(zipPath string, items []Item) error {
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		return fmt.Errorf("crear directorio de salida: %w", err)
	}

	f, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("crear zip: %w", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for _, item := range items {
		entry, err := w.Create(item.Name)
		if err != nil {
			return fmt.Errorf("añadir %s al zip: %w", item.Name, err)
		}
		if _, err := entry.Write(item.Data); err != nil {
			return fmt.Errorf("escribir %s en el zip: %w", item.Name, err)
		}
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("cerrar zip: %w", err)
	}
	return nil
}

func shouldSkip(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "__macosx/") || strings.Contains(lower, "/__macosx/") {
		return true
	}
	base := strings.ToLower(path.Base(name))
	return base == ".ds_store" || strings.HasPrefix(base, "._")
}
