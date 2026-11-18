// Package imageprep decodifica imágenes de cualquier formato habitual,
// las reescala dentro de los límites del proveedor y las reencoda en JPEG.
package imageprep

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"math"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// ErrAspectRatio indica que la imagen no puede cumplir min/max a la vez.
var ErrAspectRatio = errors.New("relación de aspecto incompatible con los límites")

// ToJPEG normaliza una imagen a JPEG. minSide puede ser 0 para no forzar
// tamaño mínimo. Devuelve el JPEG, su ancho y su alto finales.
func ToJPEG(data []byte, minSide, maxSide, quality int) ([]byte, int, int, error) {
	if quality <= 0 || quality > 100 {
		quality = 85
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decodificar imagen: %w", err)
	}

	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return nil, 0, 0, errors.New("imagen vacía")
	}

	scale := 1.0
	if maxSide > 0 && maxInt(w, h) > maxSide {
		scale = float64(maxSide) / float64(maxInt(w, h))
	}

	nw, nh := float64(w)*scale, float64(h)*scale
	if minSide > 0 && math.Min(nw, nh) < float64(minSide) {
		up := float64(minSide) / math.Min(nw, nh)
		if maxSide > 0 && math.Max(nw, nh)*up > float64(maxSide) {
			return nil, 0, 0, fmt.Errorf("%w: %dx%d", ErrAspectRatio, w, h)
		}
		scale *= up
	}

	nw = math.Round(float64(w) * scale)
	nh = math.Round(float64(h) * scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}

	outW, outH := int(nw), int(nh)
	if outW == w && outH == h {
		return encodeJPEG(src, quality)
	}

	// Fondo blanco para PNG con transparencia.
	dst := image.NewRGBA(image.Rect(0, 0, outW, outH))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)

	return encodeJPEG(dst, quality)
}

func encodeJPEG(img image.Image, quality int) ([]byte, int, int, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, 0, 0, fmt.Errorf("codificar jpeg: %w", err)
	}
	b := img.Bounds()
	return buf.Bytes(), b.Dx(), b.Dy(), nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
