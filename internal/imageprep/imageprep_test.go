package imageprep

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestToJPEGDownscale(t *testing.T) {
	src := newImage(2000, 1000)

	out, w, h, err := ToJPEG(src, 256, 1536, 80)
	if err != nil {
		t.Fatalf("ToJPEG: %v", err)
	}
	if w != 1536 || h != 768 {
		t.Fatalf("dimensiones = %dx%d, quiero 1536x768", w, h)
	}
	if len(out) == 0 {
		t.Fatal("jpeg vacío")
	}
}

func TestToJPEGUpscaleSmall(t *testing.T) {
	src := newImage(100, 100)

	_, w, h, err := ToJPEG(src, 256, 1536, 80)
	if err != nil {
		t.Fatalf("ToJPEG: %v", err)
	}
	if w != 256 || h != 256 {
		t.Fatalf("dimensiones = %dx%d, quiero 256x256", w, h)
	}
}

func TestToJPEGRejectsExtremePanorama(t *testing.T) {
	src := newImage(4000, 100)

	_, _, _, err := ToJPEG(src, 256, 1568, 80)
	if !errors.Is(err, ErrAspectRatio) {
		t.Fatalf("err = %v, quiero ErrAspectRatio", err)
	}
}

func TestToJPEGAcceptsPNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}

	out, _, _, err := ToJPEG(buf.Bytes(), 256, 1568, 80)
	if err != nil {
		t.Fatalf("ToJPEG: %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("salida no es jpeg: %v", err)
	}
}

func TestToJPEGRejectsGarbage(t *testing.T) {
	if _, _, _, err := ToJPEG([]byte("no soy una imagen"), 0, 1024, 80); err == nil {
		t.Fatal("se esperaba error")
	}
}

func newImage(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	return buf.Bytes()
}
