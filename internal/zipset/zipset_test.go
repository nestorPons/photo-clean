package zipset

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "fotos.zip")

	input := []Item{
		{Name: "playa/a.jpg", Data: []byte("jpg")},
		{Name: "montana/b.png", Data: []byte("png")},
	}
	if err := Write(zipPath, input); err != nil {
		t.Fatalf("Write: %v", err)
	}

	items, skipped, err := Read(zipPath, 1<<20, 10)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v", skipped)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, quiero 2", len(items))
	}
	if items[0].Name != "playa/a.jpg" || string(items[0].Data) != "jpg" {
		t.Fatalf("item inesperado: %+v", items[0])
	}
}

func TestReadSkipsJunk(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "fotos.zip")

	input := []Item{
		{Name: "ok.jpg", Data: []byte("x")},
		{Name: "__MACOSX/._ok.jpg", Data: []byte("x")},
		{Name: "notas.txt", Data: []byte("x")},
		{Name: ".DS_Store", Data: []byte("x")},
	}
	if err := Write(zipPath, input); err != nil {
		t.Fatalf("Write: %v", err)
	}

	items, _, err := Read(zipPath, 1<<20, 10)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(items) != 1 || items[0].Name != "ok.jpg" {
		t.Fatalf("items = %+v, quiero solo ok.jpg", items)
	}
}

func TestReadEmptyZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "vacio.zip")
	if err := Write(zipPath, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, _, err := Read(zipPath, 1<<20, 10); err == nil {
		t.Fatal("se esperaba error con zip sin imágenes")
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("stat: %v", err)
	}
}

func TestReadTotalLimit(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "fotos.zip")

	input := []Item{
		{Name: "a.jpg", Data: []byte("0123456789")},
		{Name: "b.jpg", Data: []byte("0123456789")},
	}
	if err := Write(zipPath, input); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, _, err := ReadLimit(zipPath, 1<<20, 10, 15); err == nil {
		t.Fatal("se esperaba error al superar el total descomprimido")
	}
	if _, _, err := ReadLimit(zipPath, 1<<20, 10, 20); err != nil {
		t.Fatalf("ReadLimit con total suficiente: %v", err)
	}
}

func TestIsImage(t *testing.T) {
	cases := map[string]bool{
		"a.JPG": true, "b.jpeg": true, "c.png": true, "d.webp": true,
		"e.gif": false, "f.txt": false, "sin-extension": false,
	}
	for name, want := range cases {
		if got := IsImage(name); got != want {
			t.Errorf("IsImage(%q) = %v, quiero %v", name, got, want)
		}
	}
}
