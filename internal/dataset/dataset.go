// Package dataset descubre las imágenes de test y su verdad de referencia
// a partir de la estructura dataset/motivo y dataset/paisaje.
package dataset

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"photo-clean/internal/zipset"
)

// Sample es una imagen etiquetada.
type Sample struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Label string `json:"label"`
	Truth bool   `json:"truth"`
	// Data permite clasificar imágenes que ya están en memoria (modo ZIP).
	Data []byte `json:"-"`
}

var labels = map[string]bool{
	"motivo":  true,
	"paisaje": false,
}

// Load recorre las carpetas motivo y paisaje del directorio raíz.
func Load(root string) ([]Sample, error) {
	var samples []Sample

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("leer dataset: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		label := strings.ToLower(entry.Name())
		truth, known := labels[label]
		if !known {
			continue
		}

		dir := filepath.Join(root, entry.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("leer %s: %w", dir, err)
		}
		for _, file := range files {
			if file.IsDir() || strings.HasPrefix(file.Name(), ".") {
				continue
			}
			if !zipset.IsImage(file.Name()) {
				continue
			}
			samples = append(samples, Sample{
				Path:  filepath.Join(dir, file.Name()),
				Name:  filepath.Join(entry.Name(), file.Name()),
				Label: label,
				Truth: truth,
			})
		}
	}

	if len(samples) == 0 {
		return nil, fmt.Errorf("dataset vacío: se esperaban imágenes en %s/motivo y %s/paisaje", root, root)
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i].Name < samples[j].Name })
	return samples, nil
}
