package newsbot

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func TestInternationalEditorialBoundaries(t *testing.T) {
	text := strings.Repeat("Verified reporting contains facts from the article itself. ", 12)
	for _, tc := range []struct{ page, body string }{
		{"https://www.bbc.co.uk/news/articles/example", `<article><div data-block="text"><p>` + text + `</p></div><div data-block="relatedLinks"><p>Unrelated story</p><img src="https://example.org/unrelated.jpg"></div></article>`},
		{"https://www.theguardian.com/world/2026/oct/09/example", `<article><div class="article-body-viewer-selector"><p>` + text + `</p><figure><p>Caption</p></figure></div></article>`},
		{"https://www.cnn.com/2023/04/03/example/index.html", `<article><div class="article__content"><p>` + text + `</p><aside><p>Unrelated</p></aside></div></article>`},
		{"https://www.nytimes.com/2026/10/09/world/example.html", `<article><section name="articleBody"><p>` + text + `</p></section></article>`},
	} {
		got, err := extractInternational(strings.NewReader(tc.body), tc.page)
		if err != nil || got != strings.TrimSpace(text) {
			t.Fatalf("%s: %q %v", tc.page, got, err)
		}
	}
	for _, raw := range []string{"https://bbc.com.evil.test/news/articles/x", "https://www.bbc.co.uk/iplayer/episode/x", "https://user@cnn.com/2023/a.html"} {
		if internationalPublisher(raw) != "" {
			t.Fatal(raw)
		}
	}
	if _, err := extractInternational(strings.NewReader(`<p>Please enable JS</p>`), "https://www.nytimes.com/2026/a.html"); err == nil {
		t.Fatal("accepted challenge page")
	}
}

func TestInternationalDownloadedPages(t *testing.T) {
	if os.Getenv("NEWSBOT_INTERNATIONAL_TEST") != "1" {
		t.Skip("optional downloaded public pages")
	}
	for _, tc := range []struct{ name, page string }{
		{"bbc", "https://www.bbc.co.uk/news/articles/cvglw9ryjrylo"},
		{"guardian", "https://www.theguardian.com/technology/2026/oct/09/openai-forecast-revenue-gap"},
		{"cnn", "https://www.cnn.com/2023/04/03/entertainment/shania-twain-equal-diversity/index.html"},
		{"guardian-live", "https://www.theguardian.com/world/live/2026/oct/09/2026-nobel-peace-prize-winner-russia-ukraine-sudan-war-france-protests-poland-latest-news-updates"},
	} {
		data, err := os.ReadFile("/tmp/" + tc.name + ".html")
		if err != nil {
			t.Fatal(err)
		}
		content, err := extractInternational(bytes.NewReader(data), tc.page)
		if err != nil {
			t.Fatal(tc.name, err)
		}
		photos, err := articleImages(bytes.NewReader(data), tc.page)
		if err != nil {
			t.Fatal(tc.name, err)
		}
		groups := mediaGroups(data, tc.page)
		for _, g := range groups {
			t.Logf("%s %s", tc.name, g.Candidates[0])
		}
		t.Logf("%s: %d words, %d image candidates, %d media groups", tc.name, len(strings.Fields(content)), len(photos), len(groups))
	}
	data, err := os.ReadFile("/tmp/nyt.xml")
	if err != nil {
		t.Fatal(err)
	}
	articles, err := ParseFeed(bytes.NewReader(data), Source{ID: 9})
	if err != nil || len(articles) == 0 {
		t.Fatalf("NYT RSS: articles=%d err=%v", len(articles), err)
	}
	t.Logf("NYT RSS: %d articles", len(articles))
}

func TestInternationalVideoMetadataMatching(t *testing.T) {
	page := "https://www.cnn.com/videos/us/2023/example.cnn"
	data := []byte(`<script type="application/ld+json">[{"@type":"VideoObject","mainEntityOfPage":{"url":"` + page + `"},"contentUrl":"https://cdn.example.org/current.mp4"},{"@type":"VideoObject","mainEntityOfPage":{"url":"https://www.cnn.com/videos/other.cnn"},"contentUrl":"https://cdn.example.org/unrelated.mp4"}]</script>`)
	videos := articleVideoURLs(data, page)
	if len(videos) != 1 || videos[0] != "https://cdn.example.org/current.mp4" {
		t.Fatal(videos)
	}
	if !embeddedVideoWithoutFile([]byte(`<script>{"@type":"VideoObject"}</script>`), "https://www.bbc.co.uk/news/videos/a") {
		t.Fatal("missing unsupported player warning")
	}
}

func TestRSSAtomLinkDoesNotOverwriteArticleURL(t *testing.T) {
	for _, ns := range []string{"", ` xmlns="http://backend.userland.com/rss2"`} {
		data := `<rss` + ns + ` xmlns:atom="http://www.w3.org/2005/Atom"><channel><item><title>Example</title><link>https://www.nytimes.com/2026/a.html</link><atom:link href="https://www.nytimes.com/2026/a.html" rel="standout"/><description>Summary</description></item></channel></rss>`
		articles, err := ParseFeed(strings.NewReader(data), Source{ID: 9})
		if err != nil || len(articles) != 1 || articles[0].URL != "https://www.nytimes.com/2026/a.html" {
			t.Fatalf("%+v %v", articles, err)
		}
	}
}

func TestImageSrcsetWithCDNCommas(t *testing.T) {
	raw := "https://media.cnn.com/img.jpg?q=w_860,c_fill/f_avif"
	got := imageSrcsetURLs(raw)
	if len(got) != 1 || got[0] != raw {
		t.Fatal(got)
	}
	got = imageSrcsetURLs(raw + " 860w, https://media.cnn.com/img.jpg?q=w_1280,c_fill 1280w")
	if len(got) != 2 || got[0] != raw {
		t.Fatal(got)
	}
}

func TestInternationalPublicMedia(t *testing.T) {
	if os.Getenv("NEWSBOT_INTERNATIONAL_TEST") != "1" {
		t.Skip("optional public media downloads")
	}
	client := NewFeedClient()
	defer client.CloseIdleConnections()
	for _, tc := range []struct{ name, page string }{
		{"bbc", "https://www.bbc.co.uk/news/articles/cvglw9ryjrylo"},
		{"guardian", "https://www.theguardian.com/technology/2026/oct/09/openai-forecast-revenue-gap"},
		{"cnn", "https://www.cnn.com/2023/04/03/entertainment/shania-twain-equal-diversity/index.html"},
	} {
		data, err := os.ReadFile("/tmp/" + tc.name + ".html")
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range mediaGroups(data, tc.page) {
			f := clipFetcher{client: client, remaining: 30 << 20}
			ok := false
			for _, candidate := range g.Candidates {
				b, _, err := f.get(context.Background(), candidate, 10<<20)
				if err == nil {
					_, err = normalizePhoto(b)
				}
				if err == nil {
					ok = true
					break
				}
			}
			if !ok {
				t.Fatalf("%s: no usable image candidate", tc.name)
			}
		}
		t.Log(tc.name, "all photo groups downloaded and decoded")
	}
	page := "https://www.cnn.com/videos/us/2023/03/31/snowboarder-rescue-mt-baker-washington-cprog-lon-orig-na.cnn"
	data, err := os.ReadFile("/tmp/cnn-video.html")
	if err != nil {
		t.Fatal(err)
	}
	urls := articleVideoURLs(data, page)
	if len(urls) != 1 {
		t.Fatal(urls)
	}
	t.Log("CNN playable MP4 found:", urls[0])
	if os.Getenv("NEWSBOT_RENDER_TEST") == "1" {
		f := clipFetcher{client: client, remaining: 120 << 20}
		clip, err := downloadClipURLs(context.Background(), &f, urls, t.TempDir(), 1)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("CNN clip decoded: %.2fs", clip.Duration)
	}
}
