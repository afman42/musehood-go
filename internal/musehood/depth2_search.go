package musehood

// Indeks full-text pesan (fitur 35): indeks terbalik in-memory dari token
// huruf kecil ke himpunan ID pesan. Dibangun malas saat kotor (saveLocked
// menandai kotor setiap penyimpanan). Pencarian multi-kata memakai AND
// antar token lewat indeks (digabung dengan kecocokan frasa substring agar
// perilaku lama tidak hilang); kata tunggal yang ada di indeks memakai
// indeks; kata yang tidak ada di indeks jatuh ke pemindaian substring.

import (
	"strings"
	"unicode"
)

func tokenizeFT(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return fields
}

// rebuildFTIndexLocked membangun ulang indeks dari semua pesan.
// Pemanggil wajib memegang a.mu.
func (a *App) rebuildFTIndexLocked() {
	index := map[string]map[string]bool{}
	for _, m := range a.data.Messages {
		if m.Text == "" {
			continue
		}
		for _, token := range tokenizeFT(m.Text) {
			set := index[token]
			if set == nil {
				set = map[string]bool{}
				index[token] = set
			}
			set[m.ID] = true
		}
	}
	a.ftMu.Lock()
	a.ftIndex = index
	a.ftDirty = false
	a.ftMu.Unlock()
}

// markFTDirty menandai indeks perlu dibangun ulang.
func (a *App) markFTDirty() {
	a.ftMu.Lock()
	a.ftDirty = true
	a.ftMu.Unlock()
}

// ftCandidatesLocked mengembalikan himpunan ID kandidat dari indeks dan
// apakah indeks berlaku untuk kueri ini. Pemanggil wajib memegang a.mu.
func (a *App) ftCandidatesLocked(q string) (map[string]bool, bool) {
	a.ftMu.Lock()
	dirty := a.ftDirty
	a.ftMu.Unlock()
	if dirty {
		a.rebuildFTIndexLocked()
	}
	tokens := tokenizeFT(q)
	if len(tokens) == 0 {
		return nil, false
	}
	a.ftMu.Lock()
	defer a.ftMu.Unlock()
	if len(tokens) == 1 {
		set, ok := a.ftIndex[tokens[0]]
		if !ok {
			return nil, false
		}
		out := make(map[string]bool, len(set))
		for id := range set {
			out[id] = true
		}
		return out, true
	}
	var out map[string]bool
	for i, token := range tokens {
		set, ok := a.ftIndex[token]
		if !ok {
			return map[string]bool{}, true
		}
		if i == 0 {
			out = make(map[string]bool, len(set))
			for id := range set {
				out[id] = true
			}
			continue
		}
		for id := range out {
			if !set[id] {
				delete(out, id)
			}
		}
	}
	return out, true
}
