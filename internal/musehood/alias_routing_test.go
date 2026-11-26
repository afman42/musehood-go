package musehood

import "testing"

// Model kursi berformat "alias/model" diarahkan ke provider dengan alias itu
// dan dikirim tanpa awalan; nama model tanpa awalan alias tetap utuh ke
// provider aktif.
func TestResolveModelProviderAlias(t *testing.T) {
	d := &data{Providers: []gatewayProvider{
		{ID: "p1", Name: "cm-cli", Alias: "cmc", BaseURL: "http://127.0.0.1:7199/v1", APIKey: "local", Protocol: "openai"},
		{ID: "p2", Name: "rs", Alias: "rs", BaseURL: "https://api.samp.team/v1", APIKey: "k2", Protocol: "openai"},
		{ID: "p3", Name: "tanpa-alias", BaseURL: "https://example.com/v1", APIKey: "k3", Protocol: "openai"},
	}}
	active := settings{BaseURL: "http://127.0.0.1:7199/v1", APIKey: "local", ProviderName: "cm-cli", GatewayProtocol: "openai"}

	// Alias provider aktif: awalan dibuang, konfigurasi provider itu dipakai.
	cfg, model := d.resolveModelProviderLocked(active, "cmc/meta/muse-spark-open")
	if model != "meta/muse-spark-open" || cfg.ProviderName != "cm-cli" {
		t.Fatalf("alias aktif: model=%q provider=%q", model, cfg.ProviderName)
	}
	// Alias provider lain: panggilan diarahkan ke provider tersebut.
	cfg, model = d.resolveModelProviderLocked(active, "rs/qwen3.8-omni-flash")
	if model != "qwen3.8-omni-flash" || cfg.ProviderName != "rs" || cfg.BaseURL != "https://api.samp.team/v1" {
		t.Fatalf("alias lain: model=%q provider=%q base=%q", model, cfg.ProviderName, cfg.BaseURL)
	}
	// Nama model mengandung "/" tetapi awalannya bukan alias: utuh ke provider aktif.
	cfg, model = d.resolveModelProviderLocked(active, "deepseek/deepseek-v4-flash")
	if model != "deepseek/deepseek-v4-flash" || cfg.ProviderName != "cm-cli" {
		t.Fatalf("tanpa alias: model=%q provider=%q", model, cfg.ProviderName)
	}
	// Tanpa garis miring: utuh.
	cfg, model = d.resolveModelProviderLocked(active, "gpt-6.1-sol")
	if model != "gpt-6.1-sol" || cfg.ProviderName != "cm-cli" {
		t.Fatalf("polos: model=%q provider=%q", model, cfg.ProviderName)
	}
	// Awalan alias tetapi sisa kosong: utuh (tidak diarahkan).
	cfg, model = d.resolveModelProviderLocked(active, "rs/")
	if model != "rs/" || cfg.ProviderName != "cm-cli" {
		t.Fatalf("sisa kosong: model=%q provider=%q", model, cfg.ProviderName)
	}
}
