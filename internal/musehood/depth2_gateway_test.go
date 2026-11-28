package musehood

// Tes validateWebhookURL: URL webhook boleh localhost/loopback dan IP
// publik, menolak host privat lain dan skema non-http.

import (
	"context"
	"net"
	"testing"
)

func TestValidateWebhookURL(t *testing.T) {
	oldLookup := lookupIP
	lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
		if ip := net.ParseIP(host); ip != nil {
			return []net.IP{ip}, nil
		}
		switch host {
		case "publik.example":
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		case "privat.example":
			return []net.IP{net.ParseIP("10.0.0.5")}, nil
		default:
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		}
	}
	t.Cleanup(func() { lookupIP = oldLookup })

	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"localhost http", "http://localhost:9000/hook", false},
		{"localhost https", "https://LOCALHOST/hook", false},
		{"loopback literal", "http://127.0.0.1:9000/hook", false},
		{"publik", "https://publik.example/hook", false},
		{"privat", "https://privat.example/hook", true},
		{"tidak terjangkau", "https://tidakada.example/hook", true},
		{"skema ftp", "ftp://publik.example/hook", true},
		{"tanpa host", "https:///hook", true},
		{"kosong", "", true},
		{"bukan url", "bukan-url", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWebhookURL(tc.raw)
			if tc.wantErr && err == nil {
				t.Fatalf("validateWebhookURL(%q) harus error", tc.raw)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("validateWebhookURL(%q) harus lolos: %v", tc.raw, err)
			}
		})
	}
}
