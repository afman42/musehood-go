package musehood

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Tool file & gambar untuk folder sandbox. Semua nama file melewati
// sandboxPath (nama saja, tanpa path), hasil keluaran ditulis kembali ke
// sandbox agar bisa dibaca tool lain.

const maxZipBytes = 50 * 1024 * 1024
const maxZipEntries = 100

func splitArgLines(arg string) []string {
	var lines []string
	for _, line := range strings.Split(arg, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func (a *App) toolZipFiles(arg string) string {
	lines := splitArgLines(arg)
	if len(lines) < 2 {
		return "Error: format zip_files — baris pertama nama zip keluaran, baris berikutnya nama file yang dimasukkan."
	}
	outName := lines[0]
	if !strings.HasSuffix(strings.ToLower(outName), ".zip") {
		return "Error: nama keluaran harus berakhiran .zip."
	}
	outPath, err := a.sandboxPath(outName)
	if err != nil {
		return "Error: " + err.Error()
	}
	if err := os.MkdirAll(a.sandboxDir(), 0o755); err != nil {
		return "Error: folder kerja tidak dapat dibuat."
	}
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	var total int64
	for _, name := range lines[1:] {
		path, err := a.sandboxPath(name)
		if err != nil {
			return "Error: " + err.Error()
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Sprintf("Error: file %q tidak dapat dibaca.", name)
		}
		total += int64(len(data))
		if total > maxZipBytes {
			return "Error: total file melebihi batas 50 MB."
		}
		entry, err := writer.Create(name)
		if err != nil {
			return "Error: zip tidak dapat dibuat."
		}
		if _, err := entry.Write(data); err != nil {
			return "Error: zip tidak dapat dibuat."
		}
	}
	if err := writer.Close(); err != nil {
		return "Error: zip tidak dapat dibuat."
	}
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return "Error: zip tidak dapat ditulis."
	}
	return fmt.Sprintf("Zip %s dibuat: %d file, %d byte.", outName, len(lines)-1, buf.Len())
}

func (a *App) toolUnzipFile(arg string) string {
	name := strings.TrimSpace(arg)
	path, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Sprintf("Error: %q bukan zip yang valid.", name)
	}
	defer reader.Close()
	if len(reader.File) > maxZipEntries {
		return fmt.Sprintf("Error: zip berisi %d entri, melebihi batas %d.", len(reader.File), maxZipEntries)
	}
	if err := os.MkdirAll(a.sandboxDir(), 0o755); err != nil {
		return "Error: folder kerja tidak dapat dibuat."
	}
	var extracted []string
	var total int64
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		// Ekstrak rata (basename saja) agar konsisten dengan model sandbox
		// datar dan tidak bisa lolos ke luar folder (zip-slip).
		base := filepath.Base(filepath.ToSlash(entry.Name))
		if base == "." || base == "/" || base == "" {
			continue
		}
		outPath, err := a.sandboxPath(base)
		if err != nil {
			continue
		}
		// Hindari tabrakan nama: tambahkan akhiran -2, -3, ...
		if _, err := os.Stat(outPath); err == nil {
			ext := filepath.Ext(base)
			stem := strings.TrimSuffix(base, ext)
			for i := 2; ; i++ {
				candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
				candidatePath, err := a.sandboxPath(candidate)
				if err != nil {
					break
				}
				if _, err := os.Stat(candidatePath); os.IsNotExist(err) {
					base, outPath = candidate, candidatePath
					break
				}
			}
		}
		rc, err := entry.Open()
		if err != nil {
			return fmt.Sprintf("Error: entri %q tidak dapat dibuka.", entry.Name)
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxZipBytes+1))
		rc.Close()
		if err != nil {
			return fmt.Sprintf("Error: entri %q tidak dapat dibaca.", entry.Name)
		}
		total += int64(len(data))
		if total > maxZipBytes {
			return "Error: total hasil ekstrak melebihi batas 50 MB."
		}
		if err := os.WriteFile(outPath, data, 0o644); err != nil {
			return fmt.Sprintf("Error: %q tidak dapat ditulis.", base)
		}
		extracted = append(extracted, base)
	}
	if len(extracted) == 0 {
		return fmt.Sprintf("Zip %s tidak berisi file.", name)
	}
	return truncateToolResult(fmt.Sprintf("Diekstrak %d file dari %s:\n%s", len(extracted), name, strings.Join(extracted, "\n")))
}

func (a *App) toolFileHash(arg string) string {
	name := strings.TrimSpace(arg)
	path, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("Error: file %q tidak dapat dibaca.", name)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "Error: file tidak dapat dibaca."
	}
	return fmt.Sprintf("SHA-256 %s: %s (%d byte)", name, hex.EncodeToString(hash.Sum(nil)), size)
}

func decodeImageFile(path string) (image.Image, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	img, format, err := image.Decode(file)
	return img, format, err
}

func (a *App) toolImageInfo(arg string) string {
	name := strings.TrimSpace(arg)
	path, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("Error: file %q tidak dapat dibaca.", name)
	}
	defer file.Close()
	cfg, format, err := image.DecodeConfig(file)
	if err != nil {
		return fmt.Sprintf("Error: %q bukan gambar yang didukung (PNG/JPEG/GIF).", name)
	}
	info, _ := file.Stat()
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	return fmt.Sprintf("%s: format %s, %d×%d piksel, %d byte.", name, format, cfg.Width, cfg.Height, size)
}

// resizeBilinear memperkecil/memperbesar gambar dengan interpolasi bilinear.
func resizeBilinear(src image.Image, newW, newH int) image.Image {
	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		srcY := float64(y) * float64(srcH) / float64(newH)
		y0 := int(srcY)
		y1 := y0 + 1
		if y1 >= srcH {
			y1 = srcH - 1
		}
		fy := srcY - float64(y0)
		for x := 0; x < newW; x++ {
			srcX := float64(x) * float64(srcW) / float64(newW)
			x0 := int(srcX)
			x1 := x0 + 1
			if x1 >= srcW {
				x1 = srcW - 1
			}
			fx := srcX - float64(x0)
			c00 := color.RGBAModel.Convert(src.At(bounds.Min.X+x0, bounds.Min.Y+y0)).(color.RGBA)
			c10 := color.RGBAModel.Convert(src.At(bounds.Min.X+x1, bounds.Min.Y+y0)).(color.RGBA)
			c01 := color.RGBAModel.Convert(src.At(bounds.Min.X+x0, bounds.Min.Y+y1)).(color.RGBA)
			c11 := color.RGBAModel.Convert(src.At(bounds.Min.X+x1, bounds.Min.Y+y1)).(color.RGBA)
			mix := func(a, b, c, d uint8) uint8 {
				top := float64(a)*(1-fx) + float64(b)*fx
				bottom := float64(c)*(1-fx) + float64(d)*fx
				return uint8(top*(1-fy) + bottom*fy + 0.5)
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: mix(c00.R, c10.R, c01.R, c11.R),
				G: mix(c00.G, c10.G, c01.G, c11.G),
				B: mix(c00.B, c10.B, c01.B, c11.B),
				A: mix(c00.A, c10.A, c01.A, c11.A),
			})
		}
	}
	return dst
}

func encodeImage(img image.Image, format string) ([]byte, error) {
	var buf bytes.Buffer
	switch format {
	case "png":
		err := png.Encode(&buf, img)
		return buf.Bytes(), err
	case "jpeg", "jpg":
		err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
		return buf.Bytes(), err
	case "gif":
		err := gif.Encode(&buf, img, nil)
		return buf.Bytes(), err
	}
	return nil, fmt.Errorf("format %q tidak didukung", format)
}

func imageExt(format string) string {
	switch format {
	case "jpeg", "jpg":
		return ".jpg"
	case "gif":
		return ".gif"
	default:
		return ".png"
	}
}

func (a *App) toolResizeImage(arg string) string {
	lines := splitArgLines(arg)
	if len(lines) < 2 {
		return "Error: format resize_image — baris 1 nama file, baris 2 lebar maksimum (px), baris 3 (opsional) nama keluaran."
	}
	name := lines[0]
	maxW, err := strconv.Atoi(lines[1])
	if err != nil || maxW < 1 || maxW > 10000 {
		return "Error: lebar maksimum harus angka 1–10000."
	}
	path, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	img, format, err := decodeImageFile(path)
	if err != nil {
		return fmt.Sprintf("Error: %q bukan gambar yang didukung (PNG/JPEG/GIF).", name)
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= maxW {
		return fmt.Sprintf("%s sudah %d piksel lebar (≤ %d), tidak diubah.", name, w, maxW)
	}
	newH := h * maxW / w
	if newH < 1 {
		newH = 1
	}
	resized := resizeBilinear(img, maxW, newH)
	data, err := encodeImage(resized, format)
	if err != nil {
		return "Error: gambar tidak dapat ditulis ulang."
	}
	outName := ""
	if len(lines) > 2 {
		outName = lines[2]
	} else {
		ext := filepath.Ext(name)
		outName = strings.TrimSuffix(name, ext) + "-resize" + imageExt(format)
	}
	outPath, err := a.sandboxPath(outName)
	if err != nil {
		return "Error: " + err.Error()
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return "Error: hasil resize tidak dapat ditulis."
	}
	return fmt.Sprintf("%s (%d×%d) diperkecil menjadi %s (%d×%d, %d byte).", name, w, h, outName, maxW, newH, len(data))
}

func (a *App) toolConvertImage(arg string) string {
	lines := splitArgLines(arg)
	if len(lines) < 2 {
		return "Error: format convert_image — baris 1 nama file, baris 2 format tujuan (png atau jpeg)."
	}
	name := lines[0]
	target := strings.ToLower(lines[1])
	if target != "png" && target != "jpeg" && target != "jpg" {
		return "Error: format tujuan harus png atau jpeg."
	}
	path, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	img, format, err := decodeImageFile(path)
	if err != nil {
		return fmt.Sprintf("Error: %q bukan gambar yang didukung (PNG/JPEG/GIF).", name)
	}
	if (format == "jpeg" && (target == "jpeg" || target == "jpg")) || format == target {
		return fmt.Sprintf("%s sudah berformat %s, tidak diubah.", name, format)
	}
	data, err := encodeImage(img, target)
	if err != nil {
		return "Error: gambar tidak dapat dikonversi."
	}
	ext := filepath.Ext(name)
	outName := strings.TrimSuffix(name, ext) + imageExt(target)
	outPath, err := a.sandboxPath(outName)
	if err != nil {
		return "Error: " + err.Error()
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return "Error: hasil konversi tidak dapat ditulis."
	}
	return fmt.Sprintf("%s (%s) dikonversi menjadi %s (%s, %d byte).", name, format, outName, target, len(data))
}

func (a *App) toolPDFText(ctx context.Context, arg string) string {
	name := strings.TrimSpace(arg)
	path, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Sprintf("Error: file %q tidak ditemukan.", name)
	}
	bin, err := exec.LookPath("pdftotext")
	if err != nil {
		return "Error: pdftotext tidak tersedia di server ini."
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-layout", path, "-").Output()
	if err != nil {
		return fmt.Sprintf("Error: teks %q tidak dapat diekstrak (bukan PDF valid?).", name)
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return fmt.Sprintf("%s tidak mengandung teks yang dapat diekstrak (mungkin hasil pindaian gambar).", name)
	}
	return truncateToolResult(text)
}

func (a *App) toolCSVJSON(arg string) string {
	lines := splitArgLines(arg)
	if len(lines) < 2 {
		return "Error: format csv_json — baris 1 nama file, baris 2 arah (csv2json atau json2csv), baris 3 (opsional) nama keluaran."
	}
	name, direction := lines[0], strings.ToLower(lines[1])
	path, err := a.sandboxPath(name)
	if err != nil {
		return "Error: " + err.Error()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("Error: file %q tidak dapat dibaca.", name)
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)

	switch direction {
	case "csv2json":
		reader := csv.NewReader(bytes.NewReader(data))
		records, err := reader.ReadAll()
		if err != nil || len(records) == 0 {
			return fmt.Sprintf("Error: %q bukan CSV yang valid.", name)
		}
		headers := records[0]
		var objects []map[string]string
		for _, record := range records[1:] {
			object := make(map[string]string, len(headers))
			for i, header := range headers {
				value := ""
				if i < len(record) {
					value = record[i]
				}
				object[header] = value
			}
			objects = append(objects, object)
		}
		encoded, err := json.MarshalIndent(objects, "", "  ")
		if err != nil {
			return "Error: JSON tidak dapat dibuat."
		}
		outName := stem + ".json"
		if len(lines) > 2 {
			outName = lines[2]
		}
		outPath, err := a.sandboxPath(outName)
		if err != nil {
			return "Error: " + err.Error()
		}
		if err := os.WriteFile(outPath, encoded, 0o644); err != nil {
			return "Error: hasil konversi tidak dapat ditulis."
		}
		preview := string(encoded)
		if len(preview) > 1200 {
			preview = preview[:1200] + "\n…(dipotong)"
		}
		return fmt.Sprintf("%s dikonversi menjadi %s (%d baris data, kolom: %s).\n%s", name, outName, len(objects), strings.Join(headers, ", "), preview)
	case "json2csv":
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		var objects []map[string]any
		if err := decoder.Decode(&objects); err != nil {
			return fmt.Sprintf("Error: %q bukan JSON array objek yang valid.", name)
		}
		keySet := map[string]bool{}
		for _, object := range objects {
			for key := range object {
				keySet[key] = true
			}
		}
		headers := make([]string, 0, len(keySet))
		for key := range keySet {
			headers = append(headers, key)
		}
		slices.Sort(headers)
		var buf bytes.Buffer
		writer := csv.NewWriter(&buf)
		_ = writer.Write(headers)
		for _, object := range objects {
			row := make([]string, len(headers))
			for i, header := range headers {
				if value, ok := object[header]; ok && value != nil {
					row[i] = fmt.Sprint(value)
				}
			}
			_ = writer.Write(row)
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return "Error: CSV tidak dapat dibuat."
		}
		outName := stem + ".csv"
		if len(lines) > 2 {
			outName = lines[2]
		}
		outPath, err := a.sandboxPath(outName)
		if err != nil {
			return "Error: " + err.Error()
		}
		if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
			return "Error: hasil konversi tidak dapat ditulis."
		}
		return fmt.Sprintf("%s dikonversi menjadi %s (%d baris data, kolom: %s).", name, outName, len(objects), strings.Join(headers, ", "))
	default:
		return "Error: arah harus csv2json atau json2csv."
	}
}
