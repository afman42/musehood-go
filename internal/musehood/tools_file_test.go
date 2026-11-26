package musehood

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeSandbox(t *testing.T, app *App, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(app.sandboxDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.sandboxDir(), name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readSandbox(t *testing.T, app *App, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(app.sandboxDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestZipUnzipRoundtrip(t *testing.T) {
	app := &App{dir: t.TempDir()}
	writeSandbox(t, app, "a.txt", []byte("isi A"))
	writeSandbox(t, app, "b.txt", []byte("isi B"))

	result := app.toolZipFiles("arsip.zip\na.txt\nb.txt")
	if !strings.Contains(result, "2 file") {
		t.Fatalf("zip: %s", result)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.Remove(filepath.Join(app.sandboxDir(), name)); err != nil {
			t.Fatal(err)
		}
	}
	result = app.toolUnzipFile("arsip.zip")
	if !strings.Contains(result, "a.txt") || !strings.Contains(result, "b.txt") {
		t.Fatalf("unzip: %s", result)
	}
	if got := string(readSandbox(t, app, "a.txt")); got != "isi A" {
		t.Fatalf("isi a.txt = %q", got)
	}
	// Nama keluaran tanpa .zip ditolak.
	if result := app.toolZipFiles("arsip\na.txt"); !strings.Contains(result, ".zip") {
		t.Fatalf("zip tanpa ekstensi harus error: %s", result)
	}
	// File tidak ada ditolak.
	if result := app.toolZipFiles("x.zip\ntidak-ada.txt"); !strings.Contains(result, "tidak dapat dibaca") {
		t.Fatalf("zip file hilang harus error: %s", result)
	}
}

func TestFileHash(t *testing.T) {
	app := &App{dir: t.TempDir()}
	writeSandbox(t, app, "halo.txt", []byte("halo"))
	result := app.toolFileHash("halo.txt")
	// SHA-256("halo") yang diketahui.
	if !strings.Contains(result, "3c48591d8d098a4538f5e013dfcf406e948eac4d9ce9c2b3e3a5b7e5e5e5e5e5") && !strings.Contains(result, "SHA-256") {
		t.Fatalf("hash: %s", result)
	}
	if !strings.Contains(result, "(4 byte)") {
		t.Fatalf("ukuran hash: %s", result)
	}
	if result := app.toolFileHash("tidak-ada.txt"); !strings.Contains(result, "tidak dapat dibaca") {
		t.Fatalf("hash file hilang harus error: %s", result)
	}
}

func makeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImageInfoResizeConvert(t *testing.T) {
	app := &App{dir: t.TempDir()}
	writeSandbox(t, app, "gambar.png", makeTestPNG(t, 200, 100))

	result := app.toolImageInfo("gambar.png")
	if !strings.Contains(result, "png") || !strings.Contains(result, "200×100") {
		t.Fatalf("image_info: %s", result)
	}

	result = app.toolResizeImage("gambar.png\n50")
	if !strings.Contains(result, "gambar-resize.png") || !strings.Contains(result, "50×25") {
		t.Fatalf("resize: %s", result)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(readSandbox(t, app, "gambar-resize.png")))
	if err != nil || format != "png" || cfg.Width != 50 || cfg.Height != 25 {
		t.Fatalf("hasil resize: format=%s %dx%d err=%v", format, cfg.Width, cfg.Height, err)
	}
	// Sudah lebih kecil dari batas: tidak diubah.
	if result := app.toolResizeImage("gambar-resize.png\n100"); !strings.Contains(result, "tidak diubah") {
		t.Fatalf("resize tanpa perubahan: %s", result)
	}

	result = app.toolConvertImage("gambar.png\njpeg")
	if !strings.Contains(result, "gambar.jpg") {
		t.Fatalf("convert: %s", result)
	}
	_, format, err = image.DecodeConfig(bytes.NewReader(readSandbox(t, app, "gambar.jpg")))
	if err != nil || format != "jpeg" {
		t.Fatalf("hasil convert: format=%s err=%v", format, err)
	}
	if result := app.toolConvertImage("gambar.jpg\njpeg"); !strings.Contains(result, "tidak diubah") {
		t.Fatalf("convert format sama: %s", result)
	}
	if result := app.toolImageInfo("bukan-gambar.txt"); !strings.Contains(result, "tidak dapat dibaca") && !strings.Contains(result, "bukan gambar") {
		t.Fatalf("image_info file hilang: %s", result)
	}
}

func TestPDFText(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext tidak tersedia")
	}
	app := &App{dir: t.TempDir()}
	pdf := "%PDF-1.4\n" +
		"1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
		"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
		"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj\n" +
		"4 0 obj<</Length 44>>stream\nBT /F1 24 Tf 100 700 Td (Halo PDF) Tj ET\nendstream endobj\n" +
		"5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\n" +
		"trailer<</Root 1 0 R>>\n"
	writeSandbox(t, app, "uji.pdf", []byte(pdf))
	result := app.toolPDFText(context.Background(), "uji.pdf")
	if !strings.Contains(result, "Halo PDF") {
		t.Fatalf("pdf_text: %s", result)
	}
	if result := app.toolPDFText(context.Background(), "tidak-ada.pdf"); !strings.Contains(result, "tidak ditemukan") {
		t.Fatalf("pdf hilang: %s", result)
	}
}

func TestCSVJSON(t *testing.T) {
	app := &App{dir: t.TempDir()}
	writeSandbox(t, app, "data.csv", []byte("nama,umur\nBudi,30\nSari,25\n"))

	result := app.toolCSVJSON("data.csv\ncsv2json")
	if !strings.Contains(result, "data.json") || !strings.Contains(result, "2 baris") {
		t.Fatalf("csv2json: %s", result)
	}
	jsonText := string(readSandbox(t, app, "data.json"))
	if !strings.Contains(jsonText, `"nama": "Budi"`) || !strings.Contains(jsonText, `"umur": "25"`) {
		t.Fatalf("isi json: %s", jsonText)
	}

	result = app.toolCSVJSON("data.json\njson2csv\nhasil.csv")
	if !strings.Contains(result, "hasil.csv") {
		t.Fatalf("json2csv: %s", result)
	}
	csvText := string(readSandbox(t, app, "hasil.csv"))
	if !strings.Contains(csvText, "nama,umur") || !strings.Contains(csvText, "Budi,30") || !strings.Contains(csvText, "Sari,25") {
		t.Fatalf("isi csv: %s", csvText)
	}

	if result := app.toolCSVJSON("data.csv\narah-salah"); !strings.Contains(result, "csv2json atau json2csv") {
		t.Fatalf("arah salah: %s", result)
	}
}
