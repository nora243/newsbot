package newsbot

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPostingCaption(t *testing.T) {
	for _, s := range []Script{{Title: "Bản tin mới"}, {Title: "Lãi suất ngân hàng tăng"}, {Title: "Trump nói về Flydubai tại Việt Nam", Summary: "AI và công nghệ trí tuệ nhân tạo"}} {
		caption := postingCaption(s)
		parts := strings.Split(caption, "\n\n")
		if len(parts) != 2 || parts[0] != s.Title {
			t.Fatal(caption)
		}
		tags := strings.Fields(parts[1])
		if len(tags) < 3 || len(tags) > 5 {
			t.Fatal(tags)
		}
		seen := map[string]bool{}
		for _, tag := range tags {
			if !strings.HasPrefix(tag, "#") || seen[tag] {
				t.Fatal(tags)
			}
			seen[tag] = true
		}
	}
	if !strings.Contains(postingCaption(Script{Title: "Lãi suất ngân hàng"}), "#KinhTe") {
		t.Fatal("missing relevant hashtag")
	}
}
func TestMediaSelectionIntegration(t *testing.T) {
	dsn := os.Getenv("NEWSBOT_SEARCH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated media test database required")
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
 INSERT INTO news_sources(name,rss_url)VALUES('Example','https://example.com/rss');
 INSERT INTO news_articles(source_id,canonical_url,title,content,content_hash)VALUES(1,'https://example.com/one','One','content','one'),(1,'https://example.com/two','Two','content','two');
 INSERT INTO media_assets(id,article_id,kind,candidates)VALUES('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,'image','["https://example.com/image.jpg"]'),('bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',2,'video','["https://example.com/video.mp4"]')`)
	if err != nil {
		t.Fatal(err)
	}
	good := strings.Repeat("a", 32)
	bad := strings.Repeat("b", 32)
	if _, err = s.Enqueue(ctx, []int64{1}, []string{bad}); err == nil {
		t.Fatal("accepted media from another article")
	}
	if _, err = s.Enqueue(ctx, []int64{1}, []string{good, good}); err == nil {
		t.Fatal("accepted duplicate media")
	}
	id, err := s.Enqueue(ctx, []int64{1}, []string{good})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Videos(ctx, id)
	if err != nil || len(rows) != 1 || len(rows[0].MediaIDs) != 1 || rows[0].MediaIDs[0] != good {
		t.Fatalf("selection not stored: %+v %v", rows, err)
	}
	id, err = s.Enqueue(ctx, []int64{1}, []string{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s.Videos(ctx, id)
	if err != nil || rows[0].MediaIDs == nil {
		t.Fatal("explicit empty selection became automatic")
	}
	id, err = s.Enqueue(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s.Videos(ctx, id)
	if err != nil || rows[0].MediaIDs != nil {
		t.Fatal("legacy selection changed")
	}
	articles, draft := fixture()
	articles[0].ID = 1
	for i := range draft.Claims {
		for j := range draft.Claims[i].Evidence {
			draft.Claims[i].Evidence[j].ArticleID = 1
		}
	}
	// Use fixture content to validate revision evidence, without involving AI.
	revision, err := s.EnqueueRevision(ctx, id, articles, draft, []string{good})
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s.Videos(ctx, revision)
	if err != nil || rows[0].MediaIDs[0] != good || rows[0].ParentID == nil {
		t.Fatal("revision lost media", err)
	}
}
func TestSelectedMediaRenderIntegration(t *testing.T) {
	if os.Getenv("NEWSBOT_RENDER_TEST") != "1" {
		t.Skip("FFmpeg required")
	}
	dir := t.TempDir()
	photo := filepath.Join(dir, "input.png")
	file, err := os.Create(photo)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(file, image.NewRGBA(image.Rect(0, 0, 640, 480))); err != nil {
		t.Fatal(err)
	}
	file.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err = command(ctx, dir, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30", "-t", "2", "-c:v", "libx264", "-threads", "2", "input.mp4"); err != nil {
		t.Fatal(err)
	}
	a, script := fixture()
	r := Renderer{OutputDir: dir, Speaker: fakeSpeaker{}, SelectedMedia: []ResolvedMedia{{ID: "one", Kind: "image", Path: photo, SourceName: "Selected image"}, {ID: "two", Kind: "video", Path: filepath.Join(dir, "input.mp4"), Duration: 2, SourceName: "Selected video"}}, PhotoLoader: func(context.Context, Article) ([][]byte, error) { t.Fatal("automatic images called"); return nil, nil }, ClipLoader: func(context.Context, Article, string) (articleClip, error) {
		t.Fatal("automatic video called")
		return articleClip{}, nil
	}}
	path, d, err := r.Render(ctx, strings.Repeat("c", 32), script, a)
	if err != nil || d != 70 {
		t.Fatalf("render %s %v %v", path, d, err)
	}
	assertVideoOverlays(t, path, script.Title, a[0].SourceName, "Selected image", "Selected video")
	list, err := os.ReadFile(filepath.Join(filepath.Dir(path), "visuals.txt"))
	if err != nil || strings.Count(string(list), "file '") != 2 || !strings.Contains(string(list), "selected-01") {
		t.Fatal("selected montage missing", err)
	}
	var result map[string]any
	data, err := command(ctx, dir, "ffprobe", "-v", "error", "-show_entries", "stream=codec_type", "-of", "json", path)
	if err != nil || json.Unmarshal(data, &result) != nil {
		t.Fatal("invalid MP4", err)
	}
	r.SelectedMedia = []ResolvedMedia{}
	if _, _, err = r.Render(ctx, strings.Repeat("d", 32), script, a); err != nil {
		t.Fatal("text-only render failed", err)
	}
}
