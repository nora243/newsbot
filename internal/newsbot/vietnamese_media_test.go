package newsbot

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestVietnameseEditorialMedia(t *testing.T) {
	for _, host := range []string{"thanhnien.vn", "vtv.vn"} {
		t.Run(host, func(t *testing.T) {
			page := "https://" + host + "/news.htm"
			body := `<img src="https://example.com/logo.jpg"><div class="detail-content afcbc-body" itemprop="articleBody"><img src="https://example.com/story.jpg"><div type="VideoStream" data-vid="thanhnien.mediacdn.vn/story.mp4"></div><div type="RelatedOneNews"><img src="https://example.com/related.jpg"><div type="VideoStream" data-vid="https://example.com/related.mp4"></div></div></div><div type="VideoStream" data-vid="https://example.com/sidebar.mp4"></div>`
			images, err := articleImages(strings.NewReader(body), page)
			if err != nil || len(images) != 1 || images[0] != "https://example.com/story.jpg" {
				t.Fatalf("images=%v error=%v", images, err)
			}
			videos := articleVideoURLs([]byte(body), page)
			if len(videos) != 1 || videos[0] != "https://thanhnien.mediacdn.vn/story.mp4" {
				t.Fatalf("videos=%v", videos)
			}
		})
	}
}

func TestVTVMainPlayerOutsideBody(t *testing.T) {
	body := `<div type="VideoStream" data-videoFocus-id="108699410" data-vid="https://cdn-videos.vtv.vn/main.mp4"></div><div class="detail-content" itemprop="articleBody"></div><div type="VideoStream" data-videoFocus-id="108700170" data-vid="https://cdn-videos.vtv.vn/related.mp4"></div>`
	videos := articleVideoURLs([]byte(body), "https://vtv.vn/video/example-108699410.htm")
	if len(videos) != 1 || videos[0] != "https://cdn-videos.vtv.vn/main.mp4" {
		t.Fatalf("videos=%v", videos)
	}
}

func TestVietnameseLiveMedia(t *testing.T) {
	page := os.Getenv("NEWSBOT_VI_MEDIA_URL")
	if page == "" {
		t.Skip("optional live article/media decoding test")
	}
	a := Article{ID: 1, URL: page}
	photos, err := loadArticlePhotos(context.Background(), a)
	if err != nil || len(photos) == 0 {
		t.Fatalf("photos=%d error=%v", len(photos), err)
	}
	if os.Getenv("NEWSBOT_VI_MEDIA_VIDEO") == "1" {
		clip, err := loadArticleClip(context.Background(), a, t.TempDir())
		if err != nil || clip.Name == "" || clip.Duration <= 0 {
			t.Fatalf("clip=%+v error=%v", clip, err)
		}
		t.Logf("decoded %d photos and %.2fs video", len(photos), clip.Duration)
	} else {
		t.Logf("decoded %d photos", len(photos))
	}
}
