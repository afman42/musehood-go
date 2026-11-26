package main

import (
	"errors"
	"flag"
	"testing"
)

func fakeEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestParseConfigDefaults(t *testing.T) {
	cfg, err := parseConfig(nil, fakeEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.host != "127.0.0.1" || cfg.port != 3001 || cfg.dataDir != "" || cfg.trustedHosts != "" || cfg.showVersion {
		t.Fatalf("bawaan = %+v", cfg)
	}
}

func TestParseConfigEnv(t *testing.T) {
	cfg, err := parseConfig(nil, fakeEnv(map[string]string{
		"HOST": "0.0.0.0", "PORT": "8080", "DATA_DIR": "/tmp/data", "TRUSTED_HOSTS": "chat.example.org",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.host != "0.0.0.0" || cfg.port != 8080 || cfg.dataDir != "/tmp/data" || cfg.trustedHosts != "chat.example.org" {
		t.Fatalf("env = %+v", cfg)
	}
}

func TestParseConfigFlagsOverrideEnv(t *testing.T) {
	cfg, err := parseConfig(
		[]string{"-host", "192.168.1.10", "-port", "4000", "-data-dir", "/data", "-trusted-hosts", "a.example.org"},
		fakeEnv(map[string]string{"HOST": "0.0.0.0", "PORT": "8080", "DATA_DIR": "/tmp/data", "TRUSTED_HOSTS": "b.example.org"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.host != "192.168.1.10" || cfg.port != 4000 || cfg.dataDir != "/data" || cfg.trustedHosts != "a.example.org" {
		t.Fatalf("flag = %+v", cfg)
	}
}

func TestParseConfigInvalidPort(t *testing.T) {
	if _, err := parseConfig([]string{"-port", "70000"}, fakeEnv(nil)); err == nil {
		t.Fatal("port di luar rentang harus error")
	}
	if _, err := parseConfig(nil, fakeEnv(map[string]string{"PORT": "abc"})); err == nil {
		t.Fatal("PORT env tidak valid harus error")
	}
}

func TestParseConfigVersionAndHelp(t *testing.T) {
	cfg, err := parseConfig([]string{"-version"}, fakeEnv(nil))
	if err != nil || !cfg.showVersion {
		t.Fatalf("version = %+v, %v", cfg, err)
	}
	if _, err := parseConfig([]string{"-h"}, fakeEnv(nil)); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("-h harus ErrHelp, dapat %v", err)
	}
	if _, err := parseConfig([]string{"-tidak-ada"}, fakeEnv(nil)); err == nil {
		t.Fatal("flag tidak dikenal harus error")
	}
}
