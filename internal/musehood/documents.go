package musehood

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Dokumen terlampir per ruang (fitur 26, RAG sederhana): teks diekstrak,
// dipotong menjadi chunk ~1200 karakter (overlap 200), dan chunk paling
// relevan dengan pesan pemilik disisipkan ke system prompt kursi.

type roomDocument struct {
	ID        string   `json:"id"`
	RoomID    string   `json:"roomId"`
	Name      string   `json:"name"`
	Chunks    []string `json:"chunks"`
	CreatedAt string   `json:"createdAt"`
}

const maxDocumentsPerRoom = 10
const maxChunksPerDocument = 200
const documentChunkRunes = 1200
const documentChunkOverlap = 200
const documentUploadLimit = 5 * 1024 * 1024

// chunkDocumentText memotong teks menjadi chunk ~1200 rune dengan
// overlap 200 rune, maksimal 200 chunk.
func chunkDocumentText(text string) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	chunks := []string{}
	step := documentChunkRunes - documentChunkOverlap
	for start := 0; start < len(runes) && len(chunks) < maxChunksPerDocument; start += step {
		end := start + documentChunkRunes
		if end > len(runes) {
			end = len(runes)
		}
		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end == len(runes) {
			break
		}
	}
	return chunks
}

// extractDocumentText mengekstrak teks dari unggahan berdasarkan
// ekstensi file: PDF lewat pdftotext -layout, teks/Markdown/CSV dibaca
// langsung.
func (a *App) extractDocumentText(ctx context.Context, name string, raw []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".txt", ".md", ".markdown", ".csv":
		return string(raw), nil
	case ".pdf":
		bin, err := exec.LookPath("pdftotext")
		if err != nil {
			return "", problem(400, "pdftotext tidak tersedia di server ini; unggah dokumen sebagai teks, Markdown, atau CSV.")
		}
		temp, err := os.CreateTemp(a.dir, "doc-*.pdf")
		if err != nil {
			return "", problem(500, "Gagal menyiapkan file sementara untuk ekstraksi PDF.")
		}
		tempName := temp.Name()
		defer os.Remove(tempName)
		if _, err := temp.Write(raw); err != nil {
			temp.Close()
			return "", problem(500, "Gagal menulis file sementara untuk ekstraksi PDF.")
		}
		if err := temp.Close(); err != nil {
			return "", problem(500, "Gagal menulis file sementara untuk ekstraksi PDF.")
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin, "-layout", tempName, "-").Output()
		if err != nil {
			return "", problem(400, "Teks PDF tidak dapat diekstrak (bukan PDF valid?).")
		}
		return string(out), nil
	default:
		return "", problem(400, "Format dokumen harus PDF, teks (.txt), Markdown (.md), atau CSV (.csv).")
	}
}

// uploadDocument menangani POST /api/rooms/{id}/documents (multipart,
// field file, maks 5 MB).
func (a *App) uploadDocument(w http.ResponseWriter, r *http.Request, roomID string) (int, json.RawMessage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, documentUploadLimit+64*1024)
	if err := r.ParseMultipartForm(documentUploadLimit + 64*1024); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return 0, nil, problem(413, "Ukuran dokumen maksimal 5 MB.")
		}
		return 0, nil, problem(400, "Unggahan dokumen harus berupa multipart yang valid.")
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("file")
	if err != nil {
		return 0, nil, problem(400, "Pilih file dokumen untuk diunggah.")
	}
	defer file.Close()
	raw, large, err := limitedRead(file, documentUploadLimit)
	if large {
		return 0, nil, problem(413, "Ukuran dokumen maksimal 5 MB.")
	}
	if err != nil {
		return 0, nil, err
	}
	name := filepath.Base(header.Filename)
	text, err := a.extractDocumentText(r.Context(), name, raw)
	if err != nil {
		return 0, nil, err
	}
	chunks := chunkDocumentText(text)
	if len(chunks) == 0 {
		return 0, nil, problem(400, "Dokumen tidak mengandung teks yang dapat diekstrak.")
	}
	return a.respondLocked(201, func() (any, error) {
		return a.addDocumentLocked(roomID, name, chunks)
	})
}

// addDocumentLocked menyimpan dokumen baru ke ruang. Pemanggil wajib
// memegang a.mu.
func (a *App) addDocumentLocked(roomID, name string, chunks []string) (any, error) {
	if a.data.findRoom(roomID) == nil {
		return nil, problem(404, "Ruang tidak ditemukan.")
	}
	count := 0
	for _, doc := range a.data.Documents {
		if doc.RoomID == roomID {
			count++
		}
	}
	if count >= maxDocumentsPerRoom {
		return nil, problem(400, "Maksimal 10 dokumen per ruang.")
	}
	doc := roomDocument{ID: uuid(), RoomID: roomID, Name: name, Chunks: chunks, CreatedAt: now()}
	previous := a.data.Documents
	a.data.Documents = append(a.data.Documents, doc)
	if err := a.saveLocked(); err != nil {
		a.data.Documents = previous
		return nil, err
	}
	return documentView(doc), nil
}

// documentView adalah tampilan dokumen tanpa isi chunk.
func documentView(doc roomDocument) map[string]any {
	return map[string]any{"id": doc.ID, "roomId": doc.RoomID, "name": doc.Name, "chunks": len(doc.Chunks), "createdAt": doc.CreatedAt}
}

// listDocumentsLocked mengembalikan daftar dokumen satu ruang (tanpa
// chunk). Pemanggil wajib memegang a.mu.
func (a *App) listDocumentsLocked(roomID string) (any, error) {
	if a.data.findRoom(roomID) == nil {
		return nil, problem(404, "Ruang tidak ditemukan.")
	}
	views := []map[string]any{}
	for _, doc := range a.data.Documents {
		if doc.RoomID == roomID {
			views = append(views, documentView(doc))
		}
	}
	return map[string]any{"documents": views}, nil
}

// deleteDocumentLocked menghapus satu dokumen dari ruang. Pemanggil
// wajib memegang a.mu.
func (a *App) deleteDocumentLocked(roomID, docID string) error {
	for i, doc := range a.data.Documents {
		if doc.ID == docID && doc.RoomID == roomID {
			previous := a.data.Documents
			a.data.Documents = append(a.data.Documents[:i], a.data.Documents[i+1:]...)
			if err := a.saveLocked(); err != nil {
				a.data.Documents = previous
				return err
			}
			return nil
		}
	}
	return problem(404, "Dokumen tidak ditemukan.")
}

// documentKeywords mengekstrak kata kunci dari pertanyaan: kata unik
// (huruf saja) dengan panjang minimal 4 huruf, huruf kecil.
func documentKeywords(query string) []string {
	seen := map[string]bool{}
	keywords := []string{}
	for _, field := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if len([]rune(field)) < 4 || seen[field] {
			continue
		}
		seen[field] = true
		keywords = append(keywords, field)
	}
	return keywords
}

// relevantDocumentChunks memilih chunk dokumen ruang yang paling relevan
// dengan pertanyaan: skor = total kemunculan kata kunci (≥4 huruf) di
// chunk. Mengembalikan maksimal limit chunk berurutan skor menurun
// (seri: urutan dokumen/chunk asli).
func relevantDocumentChunks(d data, roomID, query string, limit int) []string {
	keywords := documentKeywords(query)
	if len(keywords) == 0 || limit <= 0 {
		return nil
	}
	type scored struct {
		chunk string
		score int
		order int
	}
	var candidates []scored
	order := 0
	for _, doc := range d.Documents {
		if doc.RoomID != roomID {
			continue
		}
		for _, chunk := range doc.Chunks {
			lower := strings.ToLower(chunk)
			score := 0
			for _, keyword := range keywords {
				score += strings.Count(lower, keyword)
			}
			if score > 0 {
				candidates = append(candidates, scored{chunk, score, order})
			}
			order++
		}
	}
	slices.SortStableFunc(candidates, func(a, b scored) int { return cmp.Compare(b.score, a.score) })
	out := []string{}
	for i, candidate := range candidates {
		if i >= limit {
			break
		}
		out = append(out, candidate.chunk)
	}
	return out
}
