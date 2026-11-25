package musehood

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type unreadableRequestBody struct{}

func (unreadableRequestBody) Read([]byte) (int, error) {
	return 0, errors.New("request body read failed")
}

func TestReadObject(t *testing.T) {
	t.Parallel()
	const limit = 256 * 1024
	valid := `{"text":"hello"}`
	for _, tc := range []struct {
		name    string
		body    io.Reader
		status  int
		message string
	}{
		{name: "object at size limit", body: strings.NewReader(valid + strings.Repeat(" ", limit-len(valid)))},
		{name: "empty", body: strings.NewReader(" \n "), status: 400, message: "JSON permintaan tidak valid."},
		{name: "null", body: strings.NewReader("null"), status: 400, message: "JSON permintaan tidak valid."},
		{name: "array", body: strings.NewReader("[]"), status: 400, message: "JSON permintaan tidak valid."},
		{name: "partial object", body: strings.NewReader(`{"text":`), status: 400, message: "JSON permintaan tidak valid."},
		{name: "trailing object", body: strings.NewReader(valid + `{}`), status: 400, message: "JSON permintaan tidak valid."},
		{name: "wrong field type", body: strings.NewReader(`{"text":false}`), status: 400, message: "JSON permintaan tidak valid."},
		{name: "one byte over size limit", body: strings.NewReader(valid + strings.Repeat(" ", limit+1-len(valid))), status: 413, message: "Isi permintaan terlalu besar."},
		{name: "read failure", body: unreadableRequestBody{}, status: 400, message: "Isi permintaan tidak dapat dibaca."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodPost, "http://localhost/api/message", tc.body)
			defer r.Body.Close()
			var body struct{ Text string }
			err := readObject(httptest.NewRecorder(), r, &body)
			if tc.status == 0 {
				if err != nil || body.Text != "hello" {
					t.Fatalf("valid object at limit: text=%q, error=%v", body.Text, err)
				}
				return
			}
			var apiErr *apiError
			if !errors.As(err, &apiErr) || apiErr.status != tc.status || apiErr.message != tc.message {
				t.Fatalf("want HTTP %d, %q; got %v", tc.status, tc.message, err)
			}
		})
	}
}
