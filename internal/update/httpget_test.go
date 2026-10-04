package update

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPGetReadsLargeZip(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), 5<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	c := Client{HTTP: &http.Client{Timeout: 5 * time.Second}}
	got, err := c.httpGet(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(payload) {
		t.Fatalf("got %d bytes, want %d", len(got), len(payload))
	}
}
