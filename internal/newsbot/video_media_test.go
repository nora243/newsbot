package newsbot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testClipLoader(ctx context.Context, a Article, dir string) (articleClip, error) {
	if raw := os.Getenv("NEWSBOT_LIVE_CLIP_URL"); raw != "" {
		a.URL = raw
		return loadArticleClip(ctx, a, dir)
	}
	return articleClip{}, nil
}
func TestArticleVideoURLs(t *testing.T) {
	html := `<aside><video src="/ad.mp4"></video></aside><video src="/master.m3u8?a=1&amp;b=2"><source src="/fallback.mp4"></video><iframe src="/other"></iframe><video src="file:///etc/passwd"></video>`
	urls := articleVideoURLs([]byte(html), "https://example.com/article")
	if len(urls) != 2 || urls[0] != "https://example.com/master.m3u8?a=1&b=2" || urls[1] != "https://example.com/fallback.mp4" {
		t.Fatal(urls)
	}
}
func TestLocalHLSPlaylist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=10\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=20\nmedia.m3u8\n")
		case "/media.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\"\n#EXT-X-MEDIA-SEQUENCE:1\n#EXTINF:5,\nsegment.ts\n#EXT-X-ENDLIST\n")
		case "/key":
			w.Write(make([]byte, 16))
		case "/segment.ts":
			fmt.Fprint(w, "test segment")
		default:
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	f := clipFetcher{client: server.Client(), remaining: 1 << 20}
	dir := t.TempDir()
	path, err := f.playlist(context.Background(), server.URL+"/master.m3u8", dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "http") || !strings.Contains(string(b), "segment-000.ts") || !strings.Contains(string(b), "MEDIA-SEQUENCE:1") {
		t.Fatalf("unsafe/wrong playlist %s", b)
	}
	for _, name := range []string{"key-0.key", "segment-000.ts"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
}
func TestHLSRejectsUnsupportedAndOversize(t *testing.T) {
	for _, body := range []string{"#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"key\"", "#EXTM3U\n#EXTINF:NaN,\nsegment.ts", "#EXTM3U\n#EXTINF:5,\nfile:///etc/passwd", "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\""} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		f := clipFetcher{client: server.Client(), remaining: 1 << 20}
		_, err := f.playlist(context.Background(), server.URL, t.TempDir(), 0)
		server.Close()
		if err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestDantriEmbeddedVideoMetadata(t *testing.T) {
	page := `<article id="articleContent"><video data-src="/2026/10/01/clip.mov" data-video-id="203331"></video></article><script type="application/ld+json">{"@type":"NewsArticle","video":{"@type":"VideoObject","url":"https://dantri.com.vn/video/story-203331.htm","contentUrl":"https://vcdn.dantri.com.vn/vod/clip/playlist.m3u8"},"related":{"@type":"VideoObject","url":"https://dantri.com.vn/video/other-999.htm","contentUrl":"https://vcdn.dantri.com.vn/other.m3u8"}}</script>`
	urls := articleVideoURLs([]byte(page), "https://dantri.com.vn/story.htm")
	if len(urls) != 2 || urls[0] != "https://vcdn.dantri.com.vn/vod/clip/playlist.m3u8" {
		t.Fatal(urls)
	}
	for _, raw := range urls {
		if strings.Contains(raw, "other") {
			t.Fatal("unrelated video")
		}
	}
}

func TestDantriMediaIntegration(t *testing.T) {
	raw := os.Getenv("NEWSBOT_DANTRI_TEST_URL")
	if raw == "" {
		t.Skip("optional live Dân trí test")
	}
	ctx := context.Background()
	a := Article{ID: 10031, URL: raw}
	photos, err := loadArticlePhotos(ctx, a)
	if err != nil || len(photos) != 2 {
		t.Fatalf("images=%d err=%v", len(photos), err)
	}
	if preview := os.Getenv("NEWSBOT_MEDIA_PREVIEW_DIR"); preview != "" {
		for i, data := range photos {
			if err := os.WriteFile(filepath.Join(preview, fmt.Sprintf("dantri-image-%d.jpg", i+1)), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	clip, err := loadArticleClip(ctx, a, t.TempDir())
	if err != nil || clip.Name == "" || clip.Duration < 50 {
		t.Fatalf("clip=%+v err=%v", clip, err)
	}
	t.Logf("downloaded %d distinct images and %.1fs video", len(photos), clip.Duration)
}
