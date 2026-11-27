package musehood

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"time"
)

// providerHealth adalah hasil pemeriksaan kesehatan satu provider
// (fitur 6). Disimpan hanya di memori; tidak ikut data.json.
type providerHealth struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs"`
	CheckedAt string `json:"checkedAt"`
	Error     string `json:"error,omitempty"`
	// Circuit adalah status circuit breaker provider (fitur 7):
	// "closed", "open", atau "half-open".
	Circuit string `json:"circuit,omitempty"`
}

const healthCheckInterval = 5 * time.Minute
const healthCheckTimeout = 10 * time.Second

// checkProviderHealth memanggil GET /models pada provider dan mencatat
// hasilnya ke peta kesehatan in-memory.
func (a *App) checkProviderHealth(ctx context.Context, p gatewayProvider) providerHealth {
	cfg := settings{BaseURL: p.BaseURL, APIKey: p.APIKey, ProviderName: p.Name, GatewayProtocol: p.Protocol}
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()
	start := time.Now()
	_, err := a.models(ctx, cfg)
	h := providerHealth{OK: err == nil, LatencyMs: time.Since(start).Milliseconds(), CheckedAt: now()}
	if err != nil {
		h.Error = publicError(err)
	}
	a.healthMu.Lock()
	if a.health == nil {
		a.health = map[string]providerHealth{}
	}
	a.health[p.ID] = h
	a.healthMu.Unlock()
	return h
}

// healthLoop memeriksa semua provider setiap 5 menit sampai aplikasi
// ditutup. Pemeriksaan pertama menunggu satu interval.
func (a *App) healthLoop() {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.mu.Lock()
			providers := slices.Clone(a.data.Providers)
			a.mu.Unlock()
			for _, p := range providers {
				a.checkProviderHealth(a.ctx, p)
			}
		}
	}
}

// overlayHealth menyalin hasil kesehatan terakhir ke tampilan provider,
// termasuk status circuit breaker (fitur 7) dari BaseURL provider.
func (a *App) overlayHealth(views []gatewayProviderView) {
	a.healthMu.Lock()
	defer a.healthMu.Unlock()
	for i := range views {
		if h, ok := a.health[views[i].ID]; ok {
			health := h
			health.Circuit = a.breakerState(views[i].BaseURL)
			views[i].Health = &health
		}
	}
}

const maxLatencyModels = 15

// latencyTest menguji model-model satu provider berurutan dengan
// completion kecil dan mengembalikan peringkat tercepat (fitur 10).
func (a *App) latencyTest(ctx context.Context, id string) (int, json.RawMessage, error) {
	a.mu.Lock()
	p := a.data.findProvider(id)
	if p == nil {
		a.mu.Unlock()
		return 0, nil, problem(404, "Provider tidak ditemukan.")
	}
	cfg := a.data.providerConfig(*p)
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	models, err := a.models(ctx, cfg)
	if err != nil {
		return 0, nil, err
	}
	if len(models) > maxLatencyModels {
		models = models[:maxLatencyModels]
	}
	type latencyResult struct {
		Model     string `json:"model"`
		OK        bool   `json:"ok"`
		LatencyMs int64  `json:"latencyMs"`
		Error     string `json:"error,omitempty"`
	}
	results := []latencyResult{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		ms, _, err := a.testOneModel(ctx, cfg, model)
		result := latencyResult{Model: model, OK: err == nil, LatencyMs: ms}
		if err != nil {
			result.Error = publicError(err)
		}
		results = append(results, result)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].OK != results[j].OK {
			return results[i].OK
		}
		return results[i].LatencyMs < results[j].LatencyMs
	})
	raw, err := json.Marshal(map[string]any{"providerId": id, "results": results})
	return 200, raw, err
}
