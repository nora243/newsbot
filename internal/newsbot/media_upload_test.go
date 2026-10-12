package newsbot

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMediaUploadIntegration(t *testing.T) {
	dsn := os.Getenv("NEWSBOT_SEARCH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated database required")
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
 INSERT INTO news_sources(name,rss_url) VALUES('Example','https://example.com/rss');
 INSERT INTO news_articles(source_id,canonical_url,title,content,content_hash) VALUES(1,'https://example.com/one','One','content','one'),(1,'https://example.com/two','Two','content','two');`)
	if err != nil {
		t.Fatal(err)
	}
	api := API{Store: s, Config: Config{OutputDir: t.TempDir()}}
	upload := func(name string, data []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		writer.WriteField("article_id", "1")
		part, _ := writer.CreateFormFile("file", name)
		part.Write(data)
		writer.Close()
		req := httptest.NewRequest("POST", "/api/media/upload", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		api.uploadMedia(w, req)
		return w
	}
	var data bytes.Buffer
	png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 640, 360)))
	w := upload("photo.png", data.Bytes())
	if w.Code != 201 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	var asset MediaAsset
	if err = json.Unmarshal(w.Body.Bytes(), &asset); err != nil {
		t.Fatal(err)
	}
	stored, err := s.Asset(ctx, asset.ID)
	if err != nil || !stored.Uploaded || stored.SourceName != "Tải lên" || stored.ArticleTitle != "photo.png" {
		t.Fatalf("stored: %+v %v", stored, err)
	}
	resolved, err := materializeMedia(ctx, api.Config.OutputDir, stored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(resolved.Path); err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateMedia(ctx, []int64{1}, []string{asset.ID}); err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateMedia(ctx, []int64{2}, []string{asset.ID}); err == nil {
		t.Fatal("accepted upload from unselected article")
	}
	if w = upload("fake.png", []byte("not an image")); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w = upload("file.html", []byte("<html>")); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if os.Getenv("NEWSBOT_RENDER_TEST") == "1" {
		dir := t.TempDir()
		_, err = command(ctx, dir, "ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=blue:s=640x360:d=1", "-c:v", "libx264", "-threads", "2", "test.mp4")
		if err != nil {
			t.Fatal(err)
		}
		video, err := os.ReadFile(filepath.Join(dir, "test.mp4"))
		if err != nil {
			t.Fatal(err)
		}
		w = upload("video.mp4", video)
		if w.Code != 201 {
			t.Fatalf("video: %d %s", w.Code, w.Body.String())
		}
		json.Unmarshal(w.Body.Bytes(), &asset)
		stored, err = s.Asset(ctx, asset.ID)
		if err != nil {
			t.Fatal(err)
		}
		resolved, err = materializeMedia(ctx, api.Config.OutputDir, stored)
		if err != nil || resolved.Duration <= 0 {
			t.Fatalf("video resolution: %+v %v", resolved, err)
		}
		if w = upload("fake.mp4", []byte("not a video")); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
}
