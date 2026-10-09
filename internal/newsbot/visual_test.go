package newsbot

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testPhotoLoader(context.Context, Article) ([][]byte, error) {
	if raw := os.Getenv("NEWSBOT_LIVE_PHOTO_URL"); raw != "" {
		return loadArticlePhotos(context.Background(), Article{ID: 59, URL: raw})
	}
	if os.Getenv("NEWSBOT_TEST_PHOTO") != "1" {
		return nil, fmt.Errorf("no photo")
	}
	img := image.NewRGBA(image.Rect(0, 0, 960, 720))
	for y := 0; y < 720; y++ {
		for x := 0; x < 960; x++ {
			img.Set(x, y, color.RGBA{uint8(40 + x/8), uint8(60 + y/5), 100, 255})
		}
	}
	var b bytes.Buffer
	err := png.Encode(&b, img)
	return [][]byte{b.Bytes(), b.Bytes()}, err
}
func TestPreviewImage(t *testing.T) {
	tests := []struct{ html, want string }{
		{`<meta content="/photo.jpg" property="og:image">`, "https://example.com/photo.jpg"},
		{`<meta name="twitter:image" content="../image.png">`, "https://example.com/image.png"},
		{`<meta name="twitter:image" content="/twitter.png"><meta property="og:image" content="//cdn.example.com/og.jpg">`, "https://cdn.example.com/og.jpg"},
	}
	for _, tt := range tests {
		got, err := previewImage(strings.NewReader(tt.html), "https://example.com/news/story")
		if err != nil || got != tt.want {
			t.Fatalf("got %q, %v; want %q", got, err, tt.want)
		}
	}
	for _, raw := range []string{`<p>No image</p>`, `<meta property="og:image" content="file:///etc/passwd">`, `<meta property="og:image" content="https://user:pass@example.com/a.jpg">`} {
		if _, err := previewImage(strings.NewReader(raw), "https://example.com"); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
func TestSceneArticle(t *testing.T) {
	articles := []Article{{ID: 1}, {ID: 2}}
	script := Script{Claims: []Claim{{ID: "second", Evidence: []Evidence{{ArticleID: 2}}}}}
	a, ok := sceneArticle(Scene{ClaimIDs: []string{"second"}}, script, articles)
	if !ok || a.ID != 2 {
		t.Fatal("wrong photo provenance")
	}
	if _, ok := sceneArticle(Scene{ClaimIDs: []string{"missing"}}, script, articles); ok {
		t.Fatal("unrelated image fallback")
	}
}

func TestArticleBodyImages(t *testing.T) {
	page := `<img src="/logo.jpg"><article><div class="fck_detail"><figure><picture><source data-srcset="/first.jpg?dpr=1 1x, /first.jpg?dpr=2 2x"><img src="data:image/gif;base64,AAAA" data-src="/first.jpg?dpr=1"></picture></figure><figure><img data-original="/second.jpg"></figure><aside><img src="/related.jpg"></aside></div></article><meta property="og:image" content="/cover.jpg">`
	urls, err := articleImages(strings.NewReader(page), "https://example.com/story")
	want := []string{"https://example.com/first.jpg?dpr=2", "https://example.com/first.jpg?dpr=1", "https://example.com/second.jpg", "https://example.com/cover.jpg"}
	if err != nil || !reflect.DeepEqual(urls, want) {
		t.Fatalf("got %v %v", urls, err)
	}
}

func TestPhotoDownloadFallsThroughAndDeduplicates(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 400, 300))); err != nil {
		t.Fatal(err)
	}
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if r.URL.Path == "/article" {
			fmt.Fprint(w, `<article><picture><source srcset="/one?size=1 1x, /one?size=2 2x"><img data-src="/one?size=1"></picture><img src="/two"><img src="/broken"></article>`)
			return
		}
		if r.URL.Path == "/broken" || r.URL.RawQuery == "size=2" {
			w.WriteHeader(403)
			return
		}
		w.Write(b.Bytes())
	}))
	defer server.Close()
	photos, err := fetchArticlePhotos(context.Background(), server.Client(), Article{URL: server.URL + "/article"})
	if err != nil || len(photos) != 2 {
		t.Fatalf("photos=%d err=%v paths=%v", len(photos), err, paths)
	}
	for _, data := range photos {
		if _, format, err := image.Decode(bytes.NewReader(data)); err != nil || format != "jpeg" {
			t.Fatalf("normalization failed %s %v", format, err)
		}
	}
}

func TestDantriArticleImages(t *testing.T) {
	page := `<article><img src="https://example.com/related.jpg"></article><article id="articleContent"><img src="https://example.com/author.jpg"><div id="desktop-in-article"><figure><img data-src="https://cdnphoto.dantri.com.vn/a=/thumb_w/1020/2026/10/05/plane.jpg" data-srcset="https://cdnphoto.dantri.com.vn/b=/thumb_w/1360/2026/10/05/plane.jpg 2x"></figure><figure><img data-original="https://cdnphoto.dantri.com.vn/c=/2026/10/05/airport.jpg"></figure></div></article>`
	urls, err := articleImages(strings.NewReader(page), "https://dantri.com.vn/story.htm")
	if err != nil || len(urls) != 3 {
		t.Fatalf("%v %v", urls, err)
	}
	a, _ := url.Parse(urls[0])
	b, _ := url.Parse(urls[1])
	c, _ := url.Parse(urls[2])
	if articleImageKey(a) != articleImageKey(b) || articleImageKey(a) == articleImageKey(c) {
		t.Fatal("wrong image identity")
	}
	for _, raw := range urls {
		if strings.Contains(raw, "related") {
			t.Fatal("unrelated article image")
		}
	}
}

func TestDWImageAndVideoDeduplication(t *testing.T) {
	page := `<article class="sk6xmai">
		<picture>
			<source srcset="https://static.dw.com/image/79079894_803.webp 800w, https://static.dw.com/image/79079894_605.webp 600w" type="image/webp">
			<source srcset="https://static.dw.com/image/79079894_803.jpg 800w, https://static.dw.com/image/79079894_605.jpg 600w" type="image/jpeg">
			<img src="https://static.dw.com/image/79079894_6.jpg" alt="test">
		</picture>
		<video>
			<source src="https://hlsvod.dw.com/i/vps/webvideos/ENG/2026/DWVG/DWVGENG261008_UkraineNatoIns_01SMW_,AVC_480x270,.mp4.csmil/master.m3u8">
			<source src="https://tvdownloaddw-a.akamaihd.net/vps/webvideos/ENG/2026/DWVG/DWVGENG261008_UkraineNatoIns_01SMW_AVC_1920x1080.mp4">
		</video>
	</article>`
	groups := mediaGroups([]byte(page), "https://www.dw.com/story")
	if len(groups) != 2 {
		t.Fatalf("expected 2 media groups (1 image, 1 video), got %d: %+v", len(groups), groups)
	}
	if groups[0].Kind != "image" || len(groups[0].Candidates) != 5 {
		t.Fatalf("expected 1 image with 5 candidates, got %+v", groups[0])
	}
	if groups[1].Kind != "video" || len(groups[1].Candidates) != 2 {
		t.Fatalf("expected 1 video with 2 candidates, got %+v", groups[1])
	}
}

func TestDWArticleWithBodyImages(t *testing.T) {
	page := `<article class="sk6xmai">
		<picture>
			<source srcset="https://static.dw.com/image/79079894_803.webp 800w, https://static.dw.com/image/79079894_605.webp 600w" type="image/webp">
			<source srcset="https://static.dw.com/image/79079894_803.jpg 800w, https://static.dw.com/image/79079894_605.jpg 600w" type="image/jpeg">
			<img src="https://static.dw.com/image/79079894_6.jpg" alt="hero">
		</picture>
		<p>Paragraph 1</p>
		<figure class="placeholder-image master_landscape big">
			<img data-format="MASTER_LANDSCAPE" data-id="79574315" data-url="https://static.dw.com/image/79574315_${formatId}.jpg" data-aspect-ratio="16/9" alt="second">
			<figcaption class="img-caption">The capital Kyiv again came under attack</figcaption>
		</figure>
		<p>Paragraph 2</p>
		<figure class="placeholder-image master_landscape big">
			<img data-format="MASTER_LANDSCAPE" data-id="79606899" data-url="https://static.dw.com/image/79606899_${formatId}.jpg" data-aspect-ratio="16/9" alt="third">
			<figcaption class="img-caption">A destroyed gas station</figcaption>
		</figure>
		<video>
			<source src="https://hlsvod.dw.com/i/vps/webvideos/ENG/2026/DWVG/DWVGENG261008_UkraineNatoIns_01SMW_,AVC_480x270,.mp4.csmil/master.m3u8">
		</video>
	</article>`
	groups := mediaGroups([]byte(page), "https://www.dw.com/en/story/a-79606735")
	var imgCount, vidCount int
	for _, g := range groups {
		if g.Kind == "image" {
			imgCount++
		} else if g.Kind == "video" {
			vidCount++
		}
	}
	if imgCount != 3 {
		t.Fatalf("expected 3 images, got %d: %+v", imgCount, groups)
	}
	if vidCount != 1 {
		t.Fatalf("expected 1 video, got %d: %+v", vidCount, groups)
	}
}

func TestFormatArticleTime(t *testing.T) {
	if formatArticleTime(nil) != "" {
		t.Fatal("expected empty for nil")
	}
	zero := time.Time{}
	if formatArticleTime(&zero) != "" {
		t.Fatal("expected empty for zero")
	}
	utcTime := time.Date(2026, 10, 9, 3, 7, 0, 0, time.UTC)
	if got := formatArticleTime(&utcTime); got != "10:07 09/10/2026" {
		t.Fatalf("expected 10:07 09/10/2026, got %s", got)
	}
	midnightUTC := time.Date(2026, 10, 8, 17, 0, 0, 0, time.UTC)
	if got := formatArticleTime(&midnightUTC); got != "09/10/2026" {
		t.Fatalf("expected 09/10/2026, got %s", got)
	}
}
