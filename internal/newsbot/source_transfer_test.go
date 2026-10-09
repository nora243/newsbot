package newsbot

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSourceTransferValidation(t *testing.T) {
	source := SourceSettings{Name: "News", RSSURL: "https://example.com/feed", Language: "vi", Category: "WORLD", Active: true, LicenseNotes: "Attribution", AttributionRequired: true}
	valid := SourceTransfer{Format: "newsbot-sources", Version: 1, Sources: []SourceSettings{source}}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []SourceTransfer{
		{Format: "other", Version: 1}, {Format: "newsbot-sources", Version: 2},
		{Format: "newsbot-sources", Version: 1, Sources: []SourceSettings{source, source}},
		{Format: "newsbot-sources", Version: 1, Sources: []SourceSettings{{Name: "Bad", RSSURL: "file:///tmp/a"}}},
	} {
		if f.validate() == nil {
			t.Fatal("accepted invalid file")
		}
	}
}

func TestSourceTransferIntegration(t *testing.T) {
	dsn := os.Getenv("NEWSBOT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated test DB required")
	}
	ctx := context.Background()
	store, err := NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB.Close()
	api := &API{Store: store, Config: Config{AdminToken: strings.Repeat("x", 24)}}
	handler, err := api.Handler()
	if err != nil {
		t.Fatal(err)
	}
	url := "https://example.com/transfer-test-" + newID()
	defer store.DB.Exec(ctx, `DELETE FROM news_sources WHERE rss_url=$1`, url)
	f := SourceTransfer{Format: "newsbot-sources", Version: 1, Sources: []SourceSettings{{Name: "Transfer", RSSURL: url, Language: "vi", Category: "WORLD", Active: true, LicenseNotes: "Permission", AllowedImages: true, AllowedVideo: true, AttributionRequired: true}}}
	call := func(method, path string, data any) *httptest.ResponseRecorder {
		var b []byte
		if data != nil {
			b, _ = json.Marshal(data)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+api.Config.AdminToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	for i := 0; i < 2; i++ {
		rec := call("POST", "/api/sources/import", f)
		if rec.Code != 200 {
			t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
		}
	}
	rec := call("GET", "/api/sources/export", nil)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var exported SourceTransfer
	if err = json.Unmarshal(rec.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, s := range exported.Sources {
		if s.RSSURL == url {
			count++
			if s != f.Sources[0] {
				t.Fatalf("lost settings: %+v", s)
			}
		}
	}
	if count != 1 {
		t.Fatalf("duplicates: %d", count)
	}
	f.Sources[0].Name = "Changed"
	f.Sources = append(f.Sources, SourceSettings{RSSURL: "invalid"})
	if rec = call("POST", "/api/sources/import", f); rec.Code != 400 {
		t.Fatal("invalid import accepted")
	}
	var name string
	if err = store.DB.QueryRow(ctx, `SELECT name FROM news_sources WHERE rss_url=$1`, url).Scan(&name); err != nil || name != "Transfer" {
		t.Fatalf("partial import: %s %v", name, err)
	}
}
