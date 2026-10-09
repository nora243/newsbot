package newsbot

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestManagementIntegration(t *testing.T) {
	dsn := os.Getenv("NEWSBOT_SEARCH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated test DB required")
	}
	ctx := context.Background()
	s, err := NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	if err = s.Migrate(ctx, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.Exec(ctx, `TRUNCATE jobs,video_sources,videos,news_articles,news_sources RESTART IDENTITY CASCADE;
 INSERT INTO news_sources(name,rss_url,active) VALUES('Test','https://example.com/rss',true);
 INSERT INTO news_articles(source_id,canonical_url,title,content,content_hash)VALUES(1,'https://example.com/one','Old article','content','one');
 INSERT INTO videos(id,status,script,created_at) SELECT md5(i::text),'pending_review','{"title":"Recent news"}',now() FROM generate_series(1,105)i;
 INSERT INTO videos(id,status,script,created_at)VALUES('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','approved','{"title":"Target title 100%"}',now()-interval '1 day');`)
	if err != nil {
		t.Fatal(err)
	}
	api := API{Store: s, Config: Config{AdminToken: "management-test-admin-token"}}
	h, err := api.Handler()
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer management-test-admin-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := call("DELETE", "/api/sources/1", ""); code != 200 {
		t.Fatal(code)
	}
	sources, err := s.Sources(ctx, false)
	if err != nil || len(sources) != 0 {
		t.Fatal(sources, err)
	}
	articles, err := s.Articles(ctx, []int64{1})
	if err != nil || len(articles) != 1 {
		t.Fatal("lost historical evidence", err)
	}
	if ok, err := s.SaveArticle(ctx, Article{SourceID: 1, URL: "https://example.com/two", Title: "New", Content: "content", Hash: "two"}); err != nil || ok {
		t.Fatal("archived source still collects", ok, err)
	}
	if code := call("DELETE", "/api/sources/1", ""); code != 404 {
		t.Fatal(code)
	}
	if code := call("PATCH", "/api/sources/1", `{"active":false,"license_notes":"Testing rights"}`); code != 404 {
		t.Fatal(code)
	}
	if code := call("POST", "/api/sources", `{"name":"Restored","rss_url":"https://example.com/rss","language":"en","category":"WORLD","license_notes":"Testing rights"}`); code != 201 {
		t.Fatal("restore", code)
	}
	sources, err = s.Sources(ctx, false)
	if err != nil || len(sources) != 1 || sources[0].ID != 1 || sources[0].Active {
		t.Fatal("restore failed", sources, err)
	}
	list, err := s.Videos(ctx, "", "TARGET title", "approved")
	if err != nil || len(list) != 1 {
		t.Fatal("search misses older video", len(list), err)
	}
	list, err = s.Videos(ctx, "", "100%", "pending_review")
	if err != nil || len(list) != 0 {
		t.Fatal("status filter failed", err)
	}
	list, err = s.Videos(ctx, "", "%", "")
	if err != nil || len(list) != 1 {
		t.Fatal("literal query failed", len(list), err)
	}
}
