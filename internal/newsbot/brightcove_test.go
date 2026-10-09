package newsbot

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestBrightcoveArticleIdentity(t *testing.T) {
	page := `<h1>Current video</h1><script type="application/ld+json">[{"@type":"VideoObject","name":"Current video","embedUrl":"https://players.brightcove.net/123/abc_default/index.html?videoId=456"},{"@type":"VideoObject","name":"Other video","embedUrl":"https://players.brightcove.net/123/abc_default/index.html?videoId=789"}]</script>`
	got := articleBrightcoveEmbeds([]byte(page), "https://www.aljazeera.com/video/2026/10/9/example")
	if len(got) != 1 || !strings.Contains(got[0], "456") {
		t.Fatal(got)
	}
	for _, raw := range []string{"https://players.brightcove.net.evil.test/123/abc_default/index.html?videoId=456", "https://players.brightcove.net/123/abc_default/index.html?videoId=../456", "http://players.brightcove.net/123/abc_default/index.html?videoId=456"} {
		if _, _, _, ok := brightcoveEmbed(raw); ok {
			t.Fatal(raw)
		}
	}
	data := []byte(`{"id":"456","sources":[{"src":"https://cdn.example/a.mp4","height":720},{"src":"https://cdn.example/drm.mp4","key_systems":{"com.widevine.alpha":{}}},{"src":"http://cdn.example/a.mp4"},{"src":"https://cdn.example/master.m3u8"}]}`)
	urls, err := playableBrightcoveSources(data, "456")
	if err != nil || len(urls) != 2 || urls[0] != "https://cdn.example/a.mp4" {
		t.Fatal(urls, err)
	}
	if _, err = playableBrightcoveSources(data, "999"); err == nil {
		t.Fatal("accepted wrong video")
	}
}

func TestBrightcovePublicPlayback(t *testing.T) {
	if os.Getenv("NEWSBOT_BRIGHTCOVE_TEST") != "1" {
		t.Skip("optional public playback")
	}
	data, err := os.ReadFile("/tmp/aj-short.html")
	if err != nil {
		t.Fatal(err)
	}
	client := NewFeedClient()
	defer client.CloseIdleConnections()
	f := clipFetcher{client: client, remaining: maxArticleBytes}
	groups, err := resolvedArticleMedia(context.Background(), &f, data, "https://www.aljazeera.com/video/newsfeed/2026/10/9/is-it-april-fools-navi-pillay-reacts-to-nobel-peace-prize-news")
	if err != nil {
		t.Fatal(err)
	}
	var urls []string
	videos := 0
	for _, g := range groups {
		if g.Kind == "video" {
			videos++
			urls = g.Candidates
		}
	}
	if videos != 1 || len(urls) == 0 {
		t.Fatal("expected one selectable video", videos, len(urls))
	}
	t.Logf("found %d video with %d playable variants", videos, len(urls))
	if os.Getenv("NEWSBOT_RENDER_TEST") == "1" {
		f.remaining = 120 << 20
		clip, err := downloadClipURLs(context.Background(), &f, urls, t.TempDir(), 90617)
		if err != nil || clip.Duration < 60 {
			t.Fatal(clip, err)
		}
		t.Logf("downloaded and decoded %.2fs", clip.Duration)
	}
}

func TestBrightcoveSecondPublicVideo(t *testing.T) {
	if os.Getenv("NEWSBOT_BRIGHTCOVE_TEST") != "1" {
		t.Skip("optional public playback")
	}
	data, e := os.ReadFile("/tmp/aj-second.html")
	if e != nil {
		t.Fatal(e)
	}
	client := NewFeedClient()
	defer client.CloseIdleConnections()
	f := clipFetcher{client: client, remaining: maxArticleBytes}
	groups, e := resolvedArticleMedia(context.Background(), &f, data, "https://www.aljazeera.com/video/newsfeed/2026/10/9/navi-pillay-wins-nobel-peace-prize")
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, g := range groups {
		if g.Kind == "video" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("expected one video", count)
	}
	t.Log("second selected article: video resolved")
}
