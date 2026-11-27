package musehood

// Tes fitur pendalaman grup A: memori otomatis (3), kloning kursi (4),
// budget harian (5), model per jenis tugas (6).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCloneSeatCopiesConfig(t *testing.T) {
	f := setup(t, nil)
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "memory": "ingat ini", "modelChat": "chat-x"}}}, 200)
	out := f.api("POST", "/api/seats/M01/clone", object{}, 201)
	clone, _ := out["seat"].(object)
	if clone == nil || clone["code"] != "C01" {
		t.Fatal(out)
	}
	original := seatByCode(f.state(), "M01")
	if clone["name"] != original["name"].(string)+" (salinan)" {
		t.Fatal(clone)
	}
	if clone["model"] != "array" || clone["memory"] != "ingat ini" || clone["modelChat"] != "chat-x" {
		t.Fatal(clone)
	}
	// Klon kedua mendapat nama berbeda dan kode berikutnya.
	out = f.api("POST", "/api/seats/M01/clone", object{}, 201)
	clone2, _ := out["seat"].(object)
	if clone2["code"] != "C02" || clone2["name"] != original["name"].(string)+" (salinan) 2" {
		t.Fatal(clone2)
	}
	f.api("POST", "/api/seats/ZZZ/clone", object{}, 404)
	// Konfigurasi klon bisa diubah tanpa memengaruhi asal.
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "C01", "model": "plain"}}}, 200)
	if seatByCode(f.state(), "M01")["model"] != "array" {
		t.Fatal("klon berbagi konfigurasi dengan asal")
	}
}

func TestAutoMemoryAfterSummary(t *testing.T) {
	f := setup(t, nil)
	// Integrator ke kursi gateway agar ringkasan jalan tanpa antrean Muse.
	f.api("POST", "/api/settings", object{
		"settings": object{"integratorSeat": "M10"},
		"seats": []object{
			{"code": "M01", "autoMemory": true},
			{"code": "M02", "autoMemory": true},
			{"code": "M10", "autoMemory": false},
		},
	}, 200)
	f.api("POST", "/api/message", object{"text": "@M01 halo"}, 202)
	f.finish(2)
	roomID := f.state()["activeRoomId"].(string)
	f.api("POST", "/api/rooms/"+roomID+"/summarize", object{}, 202)
	// Tunggu memori M01 terisi oleh goroutine memori otomatis.
	deadline := time.Now().Add(5 * time.Second)
	for {
		s := f.state()
		m01 := seatByCode(s, "M01")
		if m01 != nil {
			if mem, _ := m01["memory"].(string); strings.TrimSpace(mem) != "" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("memori otomatis M01 tidak terisi", s)
		}
		time.Sleep(10 * time.Millisecond)
	}
	s := f.state()
	if mem, _ := seatByCode(s, "M02")["memory"].(string); strings.TrimSpace(mem) == "" {
		t.Fatal("memori otomatis M02 tidak terisi")
	}
	if mem, _ := seatByCode(s, "M10")["memory"].(string); mem != "" {
		t.Fatal("M10 tanpa AutoMemory ikut terisi:", mem)
	}
	// Ringkasan tersemat sebagai pesan berlabel.
	found := false
	for _, m := range messages(s) {
		if m["label"] == summaryLabel {
			found = true
		}
	}
	if !found {
		t.Fatal("pesan ringkasan tidak ditemukan")
	}
}

func TestDailyBudgetSkipsSeat(t *testing.T) {
	usageGateway := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"m"}]}`)
			return
		}
		var body struct{ Model string }
		json.NewDecoder(r.Body).Decode(&body)
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"balasan %s"}}],"usage":{"prompt_tokens":1000000,"completion_tokens":1000000}}`, body.Model)
	}
	f := setup(t, usageGateway)
	// Harga provider $1 per 1 jt token → tiap panggilan $2.
	providers := asObjects(f.state()["providers"])
	if len(providers) == 0 {
		t.Fatal("tidak ada provider")
	}
	f.api("POST", "/api/providers/"+providers[0]["id"].(string), object{"priceInputPer1M": 1.0, "priceOutputPer1M": 1.0}, 200)
	f.api("POST", "/api/settings", object{"seats": []object{{"code": "M01", "dailyBudgetUsd": 0.5}}}, 200)

	f.api("POST", "/api/message", object{"text": "@M01 pertama"}, 202)
	msgs := f.finish(2)
	if msgs[1]["text"] != "balasan array" {
		t.Fatal(msgs[1])
	}
	// Pemakaian harian tercatat.
	s := f.state()
	daily, _ := s["dailyUsage"].(object)
	day, _ := daily[dailyUsageKey()].(object)
	usage, _ := day["M01"].(object)
	if usage == nil || usage["costUsd"] != 2.0 {
		t.Fatal("pemakaian harian tidak tercatat:", daily)
	}
	// Panggilan kedua: M01 dilewati dengan catatan budget.
	f.api("POST", "/api/message", object{"text": "@M01 kedua"}, 202)
	msgs = f.finish(4)
	skipped := msgs[3]
	if skipped["seat"] != "M01" || skipped["text"] != nil && skipped["text"] != "" {
		t.Fatal(skipped)
	}
	if errText, _ := skipped["error"].(string); !strings.Contains(errText, "budget harian") {
		t.Fatal("catatan budget tidak ada:", skipped)
	}
	// Kursi tanpa budget tetap jalan.
	f.api("POST", "/api/message", object{"text": "@GLM bebas"}, 202)
	msgs = f.finish(6)
	if msgs[5]["text"] != "balasan plain" {
		t.Fatal(msgs[5])
	}
}

func TestModelPerTask(t *testing.T) {
	var mu sync.Mutex
	var models []string
	recordGateway := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			io.WriteString(w, `{"data":[{"id":"m"}]}`)
			return
		}
		var body struct{ Model string }
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		models = append(models, body.Model)
		mu.Unlock()
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"balasan %s"}}]}`, body.Model)
	}
	f := setup(t, recordGateway)
	lastModel := func() string {
		mu.Lock()
		defer mu.Unlock()
		if len(models) == 0 {
			return ""
		}
		return models[len(models)-1]
	}
	f.api("POST", "/api/settings", object{
		"settings": object{"integratorSeat": "M01", "toolsEnabled": false},
		"seats": []object{{"code": "M01", "model": "base-model", "modelChat": "chat-model", "modelTools": "tools-model", "modelSummary": "sum-model"}},
	}, 200)
	// Jalur chat (tool mati) → modelChat.
	f.api("POST", "/api/message", object{"text": "@M01 hai"}, 202)
	msgs := f.finish(2)
	if msgs[1]["text"] != "balasan chat-model" || lastModel() != "chat-model" {
		t.Fatal(msgs[1], lastModel())
	}
	// Jalur tools (tool aktif) → modelTools.
	f.api("POST", "/api/settings", object{"settings": object{"toolsEnabled": true}}, 200)
	f.api("POST", "/api/message", object{"text": "@M01 lagi"}, 202)
	msgs = f.finish(4)
	if msgs[3]["text"] != "balasan tools-model" || lastModel() != "tools-model" {
		t.Fatal(msgs[3], lastModel())
	}
	// Ringkasan ruang → modelSummary.
	roomID := f.state()["activeRoomId"].(string)
	f.api("POST", "/api/rooms/"+roomID+"/summarize", object{}, 202)
	f.wait(func(s object) bool {
		for _, m := range messages(s) {
			if m["label"] == summaryLabel && m["status"] == "completed" {
				return true
			}
		}
		return false
	})
	if lastModel() != "sum-model" {
		t.Fatal("model ringkasan salah:", lastModel())
	}
}
