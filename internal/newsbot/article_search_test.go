package newsbot

import (
	"context"
	"os"
	"testing"
)

func TestArticleSearchIntegration(t *testing.T) {
	dsn := os.Getenv("NEWSBOT_SEARCH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated search test database required")
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
 INSERT INTO news_sources(name,rss_url,category) VALUES ('Source A','https://a.test/rss','WORLD'),('Source B','https://b.test/rss','SPORTS');
 INSERT INTO news_articles(source_id,canonical_url,title,content,content_hash,collected_at)
 SELECT 1,'https://a.test/'||i,'Recent '||i,'',md5(i::text),now() FROM generate_series(1,110) i;
 INSERT INTO news_articles(source_id,canonical_url,title,content,content_hash,collected_at)
 VALUES(2,'https://b.test/old','Unique 100%_ news','',md5('old'),now()-interval '10 days')`)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.SearchArticles(ctx, "", nil, "", 0)
	if err != nil || len(rows) != 101 {
		t.Fatalf("first page %d %v", len(rows), err)
	}
	rows, err = s.SearchArticles(ctx, "", nil, "", 100)
	if err != nil || len(rows) != 11 {
		t.Fatalf("second page %d %v", len(rows), err)
	}
	rows, err = s.SearchArticles(ctx, "unique", []int64{2}, "SPORTS", 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("old matching row missing %v %v", rows, err)
	}
	rows, err = s.SearchArticles(ctx, "unique", []int64{1}, "", 0)
	if err != nil || len(rows) != 0 {
		t.Fatal("source filter failed", err)
	}
	rows, err = s.SearchArticles(ctx, "", []int64{2}, "WORLD", 0)
	if err != nil || len(rows) != 0 {
		t.Fatal("category filter failed", err)
	}
	rows, err = s.SearchArticles(ctx, "%_", nil, "", 0)
	if err != nil || len(rows) != 1 {
		t.Fatal("literal special characters failed", err)
	}
	rows, err = s.SearchArticles(ctx, "", []int64{1, 2}, "", 0)
	if err != nil || len(rows) != 101 {
		t.Fatal("multiple sources failed", err)
	}

	// Verify newest published_at appears first
	_, err = s.DB.Exec(ctx, `INSERT INTO news_articles(source_id,canonical_url,title,content,content_hash,published_at,collected_at)
 VALUES(1,'https://a.test/newest','Brand Newest Article','',md5('newest'),now()+interval '1 hour',now()-interval '5 days'),
       (1,'https://a.test/older','Older Article','',md5('older'),now()-interval '1 hour',now())`)
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := s.SearchArticles(ctx, "Article", nil, "", 0)
	if err != nil || len(ordered) < 2 {
		t.Fatalf("search for ordered articles failed: %v", err)
	}
	if ordered[0].Title != "Brand Newest Article" {
		t.Fatalf("expected newest article first, got %s", ordered[0].Title)
	}
}
