package newsbot

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestAdditionalSourceContentBoundaries(t *testing.T) {
	text := strings.Repeat("The article reports independently verified events and facts. ", 12)
	for _, tc := range []struct{ page, body string }{
		{"https://abcnews.com/International/wireStory/example-123", `<div data-testid="prism-article-body"><p>` + text + `</p></div>`},
		{"https://www.aljazeera.com/news/2026/10/9/example", `<div class="wysiwyg wysiwyg--all-content"><p>` + text + `</p><div class="related-articles"><p>Unrelated</p></div></div>`},
		{"https://www.reuters.com/world/europe/example-2026-10-09/", `<div data-testid="article-body"><p>` + text + `</p></div>`},
	} {
		got, err := extractInternational(strings.NewReader(tc.body), tc.page)
		if err != nil || got != strings.TrimSpace(text) {
			t.Fatalf("%s: %q %v", tc.page, got, err)
		}
	}
	if internationalPublisher("https://news.google.com/rss/articles/token") != "" {
		t.Fatal("Google wrapper treated as publisher article")
	}
	if groups := mediaGroups([]byte(`<meta property="og:image" content="https://example.org/google-card.jpg">`), "https://news.google.com/rss/articles/token"); len(groups) != 0 {
		t.Fatal("Google card included", groups)
	}
	if !googleNewsArticle("https://news.google.com/rss/articles/token") || googleNewsArticle("https://news.google.com.evil.test/rss/articles/token") {
		t.Fatal("invalid wrapper classification")
	}
}

func TestAdditionalSourcesDownloadedPages(t *testing.T) {
	if os.Getenv("NEWSBOT_ADDITIONAL_TEST") != "1" {
		t.Skip("optional downloaded pages")
	}
	for _, tc := range []struct{ name, page string }{
		{"abc-new", "https://abcnews.com/International/wireStory/saudi-authorities-3-killed-attacks-riyadh-airport-claimed-137120636"},
		{"aj-new", "https://www.aljazeera.com/news/2026/10/9/three-years-after-october-7-prolonged-war-takes-its-toll-on-israelis"},
	} {
		data, err := os.ReadFile("/tmp/" + tc.name + ".html")
		if err != nil {
			t.Fatal(err)
		}
		text, err := extractInternational(bytes.NewReader(data), tc.page)
		if err != nil {
			t.Fatal(tc.name, err)
		}
		groups := mediaGroups(data, tc.page)
		if len(groups) == 0 {
			t.Fatal(tc.name, "no images")
		}
		client := NewFeedClient()
		for _, g := range groups {
			f := clipFetcher{client: client, remaining: 30 << 20}
			ok := false
			for _, u := range g.Candidates {
				b, _, e := f.get(context.Background(), u, 10<<20)
				if e == nil {
					_, e = normalizePhoto(b)
				}
				if e == nil {
					ok = true
					break
				}
				t.Log(u, e)
			}
			if !ok {
				t.Fatal(tc.name, "image not usable")
			}
		}
		client.CloseIdleConnections()
		t.Logf("%s: %d words, %d media groups, player warning=%v", tc.name, len(strings.Fields(text)), len(groups), embeddedVideoWithoutFile(data, tc.page))
	}
}

func TestAlJazeeraShortVideoSummary(t *testing.T) {
	summary := "International judge was applauded by colleagues as she gave her first public reaction to the award announcement."
	page := `<div class="wysiwyg wysiwyg--all-content"></div><p class="article__subhead">` + summary + `</p><p>Unrelated story</p>`
	got, err := extractInternational(strings.NewReader(page), "https://www.aljazeera.com/video/newsfeed/2026/10/9/example")
	if err != nil || got != summary {
		t.Fatal(got, err)
	}
	if _, err = extractInternational(strings.NewReader(page), "https://www.aljazeera.com/news/2026/10/9/example"); err == nil {
		t.Fatal("accepted a short normal article")
	}
	if os.Getenv("NEWSBOT_ADDITIONAL_TEST") == "1" {
		data, err := os.ReadFile("/tmp/aj-short.html")
		if err != nil {
			t.Fatal(err)
		}
		got, err = extractInternational(bytes.NewReader(data), "https://www.aljazeera.com/video/newsfeed/2026/10/9/is-it-april-fools-navi-pillay-reacts-to-nobel-peace-prize-news")
		if err != nil || !strings.Contains(got, "Navi Pillay") {
			t.Fatal(got, err)
		}
		t.Logf("public video description: %d words", len(strings.Fields(got)))
	}
}
