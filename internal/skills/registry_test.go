package skills

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			t.Fatalf("path %s", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "td" {
			t.Fatalf("q=%q", r.URL.Query().Get("q"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"skills":[{"id":"a/b/c","skillId":"c","name":"c","source":"a/b","installs":9}],"count":1}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := c.Search(context.Background(), "td", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a/b/c" || got[0].Installs != 9 {
		t.Fatalf("%+v", got)
	}
}

func TestClientSearchTooShort(t *testing.T) {
	c := &Client{BaseURL: "http://example.invalid"}
	if _, err := c.Search(context.Background(), "a", 10); err != errQueryTooShort {
		t.Fatalf("got %v", err)
	}
}

func TestClientSearchRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.Search(context.Background(), "ab", 10); err != errRateLimited {
		t.Fatalf("got %v", err)
	}
}

func TestClientDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/download/obra/superpowers/brainstorming" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"files":[{"path":"SKILL.md","contents":"---\nname: brainstorming\ndescription: x\n---\nbody"}]}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	files, err := c.Download(context.Background(), "obra/superpowers/brainstorming")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "SKILL.md" {
		t.Fatalf("%+v", files)
	}
}

func TestSplitID(t *testing.T) {
	o, r, s, err := splitID("owner/repo/skill")
	if err != nil || o != "owner" || r != "repo" || s != "skill" {
		t.Fatalf("%s %s %s %v", o, r, s, err)
	}
	if _, _, _, err := splitID("a/b"); err != errInvalidID {
		t.Fatalf("got %v", err)
	}
}
