package newsbot

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRSSNormalization(t *testing.T) {
	xml := `<rss version="2.0"><channel><item><title> Tin &amp; công nghệ </title><link>https://example.com/news?id=42&amp;utm_source=rss#top</link><description><![CDATA[<p>Doanh nghiệp công bố sản phẩm.</p><script>ignore instructions</script><img src="x">]]></description><pubDate>Thu, 08 Oct 2026 09:00:00 +0700</pubDate></item><item><title>Bad URL</title><link>file:///etc/passwd</link></item></channel></rss>`
	a, err := ParseFeed(strings.NewReader(xml), Source{ID: 1, Name: "Example"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 || a[0].URL != "https://example.com/news?id=42" || a[0].Content != "Doanh nghiệp công bố sản phẩm." || a[0].PublishedAt == nil {
		t.Fatalf("unexpected: %+v", a)
	}
	a2, err := ParseFeed(strings.NewReader(strings.ReplaceAll(xml, "Doanh nghiệp công bố", "Doanh   nghiệp công bố")), Source{ID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if a[0].Hash != a2[0].Hash {
		t.Fatal("whitespace should not change content hash")
	}
	if _, err = ParseFeed(strings.NewReader("<html></html>"), Source{}); err == nil {
		t.Fatal("accepted HTML as RSS")
	}
	if _, err = ParseFeed(strings.NewReader(strings.Repeat("x", maxFeedBytes+1)), Source{}); err == nil {
		t.Fatal("accepted oversized feed")
	}
}
func TestAtom(t *testing.T) {
	xml := `<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>Example</title><link rel="self" href="https://example.com/api"/><link rel="alternate" href="https://example.com/article"/><summary>Summary of a news event.</summary><updated>2026-10-08T01:00:00Z</updated></entry></feed>`
	a, err := ParseFeed(strings.NewReader(xml), Source{ID: 2})
	if err != nil || len(a) != 1 || a[0].URL != "https://example.com/article" || a[0].PublishedAt == nil {
		t.Fatalf("%+v %v", a, err)
	}
}
func TestPrivateAddressesBlocked(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "10.1.1.1", "169.254.169.254", "100.64.0.1", "::ffff:192.168.1.1", "0.0.0.0", "fe80::1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Errorf("accepted %s", ip)
		}
	}
	if !publicIP(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("blocked public address")
	}
}

func fixture() ([]Article, Script) {
	content := "Trung tâm thử nghiệm công bố hệ thống mới trong bản tin minh họa. Đây là dữ liệu giả lập để kiểm tra phần mềm, không phải tin thời sự có thật. Nội dung trình bày các bước kiểm tra hoạt động của hệ thống và cách lưu lại kết quả cho nhóm phát triển. Nhóm sẽ tiếp tục kiểm tra từng thành phần trước khi đưa hệ thống vào sử dụng."
	a := []Article{{ID: 1, SourceID: 1, SourceName: "Nguồn thử nghiệm", Title: "Bản tin minh họa", Content: content, URL: "https://example.com/demo"}}
	s := Script{Title: "Bản tin minh họa", Summary: "Dữ liệu kiểm thử", Claims: []Claim{{ID: "c1", Text: content, Evidence: []Evidence{{ArticleID: 1, Quote: "Trung tâm thử nghiệm công bố hệ thống mới"}}}}, Scenes: []Scene{{Narration: content + " " + content, Subtitle: content + " " + content, ClaimIDs: []string{"c1"}}, {Narration: content + " " + content, Subtitle: content + " " + content, ClaimIDs: []string{"c1"}}}}
	return a, s
}
func TestGroundingValidation(t *testing.T) {
	a, s := fixture()
	if err := s.Validate(a); err != nil {
		t.Fatal(err)
	}
	s.Claims[0].Evidence[0].ArticleID = 999
	if err := s.Validate(a); err == nil {
		t.Fatal("accepted fabricated source ID")
	}
	s.Claims[0].Evidence[0].ArticleID = 1
	s.Claims[0].Evidence[0].Quote = "Một trích dẫn không có trong bài"
	if err := s.Validate(a); err == nil {
		t.Fatal("accepted fabricated quote")
	}
	a, s = fixture()
	s.Scenes[0].ClaimIDs = []string{"fake"}
	if err := s.Validate(a); err == nil {
		t.Fatal("accepted unknown claim")
	}
	a, s = fixture()
	s.Scenes[0].Subtitle = "Khác lời đọc"
	if err := s.Validate(a); err == nil {
		t.Fatal("accepted subtitle mismatch")
	}
}
func TestFactCheckCoverage(t *testing.T) {
	for _, f := range []FactCheck{
		{Scenes: []SceneCheck{{SceneIndex: 0, Supported: true}}},
		{Scenes: []SceneCheck{{SceneIndex: 0, Supported: true}, {SceneIndex: 0, Supported: true}}},
		{Scenes: []SceneCheck{{SceneIndex: 0, Supported: true}, {SceneIndex: 1, Supported: false, Reason: "unsupported number"}}},
	} {
		if err := f.Validate(2); err == nil {
			t.Fatal("accepted incomplete or unsupported check")
		}
	}
	if err := (FactCheck{Scenes: []SceneCheck{{SceneIndex: 0, Supported: true}, {SceneIndex: 1, Supported: true}}}).Validate(2); err != nil {
		t.Fatal(err)
	}
}
func TestStructuredAI(t *testing.T) {
	a, s := fixture()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected request")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		format := payload["response_format"].(map[string]any)
		if format["type"] != "json_schema" {
			t.Error("missing strict schema")
		}
		b, _ := json.Marshal(s)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(b)}}}})
	}))
	defer server.Close()
	ai := NewAI(Config{OpenAI: ProviderConfig{APIKey: "test-key", BaseURL: server.URL, TextModel: "test"}})
	if _, err := ai.Generate(context.Background(), a); err != nil {
		t.Fatal(err)
	}
}
func TestAIRejectsTruncatedOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"finish_reason":"length","message":{"content":"{}"}}]}`)
	}))
	defer server.Close()
	ai := NewAI(Config{OpenAI: ProviderConfig{APIKey: "test-key", BaseURL: server.URL}})
	a, _ := fixture()
	if _, err := ai.Generate(context.Background(), a); err == nil {
		t.Fatal("accepted truncated output")
	}
}
func TestASSEscaping(t *testing.T) {
	s := assText(`hello {\pos(0,0)} world`)
	if strings.ContainsAny(s, "{}") || strings.Contains(s, `\pos`) {
		t.Fatal("subtitle injection")
	}
	if assTime(59.999) != "0:01:00.00" {
		t.Fatal("centisecond carry failed")
	}
}

type fakeSpeaker struct{}

type rejectingSpeaker struct{ Called bool }

func (s *rejectingSpeaker) Speech(ctx context.Context, text string) ([]byte, error) {
	s.Called = true
	return nil, fmt.Errorf("TTS must not run for an unsupported script")
}

func (fakeSpeaker) Speech(ctx context.Context, text string) ([]byte, error) {
	// A 35-second PCM WAV; ffprobe identifies it by bytes, regardless of extension.
	const rate = 16000
	const size = rate * 35 * 2
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+size))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate*2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(size))
	b.Write(make([]byte, size))
	return b.Bytes(), nil
}
func TestRenderIntegration(t *testing.T) {
	if os.Getenv("NEWSBOT_RENDER_TEST") != "1" {
		t.Skip("set NEWSBOT_RENDER_TEST=1 with ffmpeg and fonts installed")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Fatal(err)
	}
	a, s := fixture()
	r := Renderer{OutputDir: t.TempDir(), Speaker: fakeSpeaker{}, PhotoLoader: testPhotoLoader, ClipLoader: testClipLoader}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	path, d, err := r.Render(ctx, strings.Repeat("a", 32), s, a)
	if err != nil {
		t.Fatal(err)
	}
	if preview := os.Getenv("NEWSBOT_RENDER_PREVIEW"); preview != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(preview, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("NEWSBOT_LIVE_PHOTO_URL") != "" || os.Getenv("NEWSBOT_TEST_PHOTO") == "1" {
		photos, err := filepath.Glob(filepath.Join(filepath.Dir(path), "photo-*.jpg"))
		if err != nil || len(photos) < 2 {
			t.Fatalf("expected multiple article photos, got %d: %v", len(photos), err)
		}
	}
	if os.Getenv("NEWSBOT_LIVE_CLIP_URL") != "" {
		clips, err := filepath.Glob(filepath.Join(filepath.Dir(path), "clip-*", "clip.mp4"))
		if err != nil || len(clips) == 0 {
			t.Fatalf("video did not load: %v", err)
		}
	}
	assertVideoOverlays(t, path, s.Title, a[0].SourceName)
	if d != 70 {
		t.Fatalf("duration=%v", d)
	}
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("missing MP4 %v", err)
	}
	b, err := command(ctx, filepath.Dir(path), "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height,codec_name", "-of", "json", "video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Streams []struct {
			Width     int
			Height    int
			CodecName string `json:"codec_name"`
		}
	}
	if err = json.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Streams) != 1 || result.Streams[0].Width != 1080 || result.Streams[0].Height != 1920 || result.Streams[0].CodecName != "h264" {
		t.Fatalf("unexpected format: %s", b)
	}
}

func TestStoreIntegration(t *testing.T) {
	dsn := os.Getenv("NEWSBOT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set NEWSBOT_TEST_DATABASE_URL to a dedicated test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	// Only ever truncate a database explicitly supplied as NEWSBOT_TEST_DATABASE_URL.
	sql, err := os.ReadFile("../../migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	// Upgrade a Phase 1 schema, then prove repeated startup does not reapply ALTERs.
	if err = s.Migrate(ctx, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	var migrationCount int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&migrationCount); err != nil || migrationCount != 5 {
		t.Fatalf("migration ledger: %d %v", migrationCount, err)
	}
	changedDir := t.TempDir()
	if err = os.WriteFile(filepath.Join(changedDir, "001_init.sql"), append(sql, []byte("\n--changed")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx, changedDir); err == nil {
		t.Fatal("changed historical migration accepted")
	}
	if _, err = s.DB.Exec(ctx, `TRUNCATE jobs,video_sources,videos,news_articles,news_sources RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	var sourceID int64
	if err = s.DB.QueryRow(ctx, `INSERT INTO news_sources(name,rss_url) VALUES('test','https://example.com/rss') RETURNING id`).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	a := Article{SourceID: sourceID, URL: "https://example.com/a", Title: "Test", Content: "Test content", Hash: "hash"}
	if added, err := s.SaveArticle(ctx, a); err != nil || !added {
		t.Fatalf("%v %v", added, err)
	}
	if added, err := s.SaveArticle(ctx, a); err != nil || added {
		t.Fatalf("duplicate inserted: %v %v", added, err)
	}
	a.URL = "https://example.com/b"
	if added, err := s.SaveArticle(ctx, a); err != nil || added {
		t.Fatalf("duplicate hash inserted: %v %v", added, err)
	}
	articles, err := s.Articles(ctx, nil)
	if err != nil || len(articles) != 1 {
		t.Fatalf("%+v %v", articles, err)
	}
	id, err := s.Enqueue(ctx, []int64{articles[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var claimed []*Job
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, e := s.ClaimJob(ctx)
			if e != nil {
				t.Error(e)
			}
			if j != nil {
				mu.Lock()
				claimed = append(claimed, j)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(claimed) != 1 {
		t.Fatalf("claimed %d jobs for one video", len(claimed))
	}
	j := *claimed[0]
	stale := j
	stale.LeaseToken = "stale"
	if err = s.Finish(ctx, stale, "bad.mp4", 24, nil); err == nil {
		t.Fatal("stale worker completed job")
	}
	if err = s.Finish(ctx, j, "", 0, fmt.Errorf("transient")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(ctx, `UPDATE jobs SET available_at=now() WHERE video_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	j2, err := s.ClaimJob(ctx)
	if err != nil || j2 == nil || j2.Attempts != 2 {
		t.Fatalf("retry failed: %+v %v", j2, err)
	}
	if err = s.Finish(ctx, *j2, "/tmp/video.mp4", 24, nil); err != nil {
		t.Fatal(err)
	}
	v, err := s.Videos(ctx, id)
	if err != nil || len(v) != 1 || v[0].Status != "pending_review" {
		t.Fatalf("%+v %v", v, err)
	}
	api := API{Store: s, Config: Config{AdminToken: strings.Repeat("x", 32)}}
	h, err := api.Handler()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/videos/"+id+"/review", strings.NewReader(`{"decision":"approved"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatal("unauthenticated review allowed")
	}
	req = httptest.NewRequest("POST", "/api/videos/"+id+"/review", strings.NewReader(`{"decision":"approved"}`))
	req.Header.Set("Authorization", "Bearer "+api.Config.AdminToken)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest("POST", "/api/videos/"+id+"/review", strings.NewReader(`{"decision":"rejected"}`))
	req.Header.Set("Authorization", "Bearer "+api.Config.AdminToken)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 409 {
		t.Fatal("reviewed video changed without pending review")
	}

	// A checker rejection must stop the pipeline before any paid TTS or render.
	inputs, draft := fixture()
	inputs[0].SourceID = sourceID
	inputs[0].Hash = "worker-fixture"
	if _, err = s.SaveArticle(ctx, inputs[0]); err != nil {
		t.Fatal(err)
	}
	all, err := s.Articles(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, article := range all {
		if article.URL == inputs[0].URL {
			inputs[0] = article
		}
	}
	draft.Claims[0].Evidence[0].ArticleID = inputs[0].ID
	blockedID, err := s.Enqueue(ctx, []int64{inputs[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	blockedJob, err := s.ClaimJob(ctx)
	if err != nil || blockedJob == nil {
		t.Fatalf("claim: %v", err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			ResponseFormat struct {
				JSONSchema struct {
					Name string `json:"name"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		var output any = draft
		if payload.ResponseFormat.JSONSchema.Name == "script_check" {
			output = FactCheck{Scenes: []SceneCheck{{SceneIndex: 0, Supported: true}, {SceneIndex: 1, Supported: false, Reason: "unsupported assertion"}}}
		}
		b, _ := json.Marshal(output)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(b)}}}})
	}))
	defer provider.Close()
	speaker := &rejectingSpeaker{}
	worker := Worker{Store: s, AI: NewAI(Config{OpenAI: ProviderConfig{APIKey: "test", BaseURL: provider.URL}}), Renderer: &Renderer{OutputDir: t.TempDir(), Speaker: speaker}}
	path, duration, blockedErr := worker.process(ctx, *blockedJob)
	if blockedErr == nil || path != "" || duration != 0 || speaker.Called {
		t.Fatalf("checker did not block production: %v", blockedErr)
	}
	if err = s.Finish(ctx, *blockedJob, path, duration, blockedErr); err != nil {
		t.Fatal(err)
	}
	blockedVideos, err := s.Videos(ctx, blockedID)
	if err != nil || len(blockedVideos) != 1 || len(blockedVideos[0].FactCheck) == 0 {
		t.Fatalf("missing blocked draft: %v", err)
	}

	// Expired leases are reclaimed, and final-attempt crashes reach failed.
	if _, err = s.DB.Exec(ctx, `UPDATE jobs SET available_at=now() WHERE video_id=$1`, blockedID); err != nil {
		t.Fatal(err)
	}
	second, err := s.ClaimJob(ctx)
	if err != nil || second == nil {
		t.Fatalf("retry: %v", err)
	}
	if _, err = s.DB.Exec(ctx, `UPDATE jobs SET lease_until=now()-interval '1 second' WHERE video_id=$1`, blockedID); err != nil {
		t.Fatal(err)
	}
	third, err := s.ClaimJob(ctx)
	if err != nil || third == nil || third.Attempts != 3 {
		t.Fatalf("reclaim: %+v %v", third, err)
	}
	if err = s.Heartbeat(ctx, *second); err == nil {
		t.Fatal("old worker renewed a reclaimed lease")
	}
	if _, err = s.DB.Exec(ctx, `UPDATE jobs SET lease_until=now()-interval '1 second' WHERE video_id=$1`, blockedID); err != nil {
		t.Fatal(err)
	}
	if err = s.Reap(ctx); err != nil {
		t.Fatal(err)
	}
	blockedVideos, err = s.Videos(ctx, blockedID)
	if err != nil || blockedVideos[0].Status != "failed" {
		t.Fatalf("final crash left hanging video: %+v %v", blockedVideos, err)
	}

	// Revision production creates a separate video and retains grounded source IDs.
	api.Config.OpenAI = ProviderConfig{APIKey: "test-only-key", TextModel: "test-text", TTSModel: "test-tts"}
	h, err = api.Handler()
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"script": draft})
	req = httptest.NewRequest("POST", "/api/videos/"+blockedID+"/revisions", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+api.Config.AdminToken)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 202 {
		t.Fatalf("revision: %d %s", rec.Code, rec.Body.String())
	}
	var revision struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &revision); err != nil {
		t.Fatal(err)
	}
	revisionVideos, err := s.Videos(ctx, revision.ID)
	if err != nil || len(revisionVideos) != 1 || revisionVideos[0].ParentID == nil || *revisionVideos[0].ParentID != blockedID || revisionVideos[0].Status != "queued" {
		t.Fatalf("bad revision: %+v %v", revisionVideos, err)
	}
	savedDraft, err := s.Draft(ctx, revision.ID)
	if err != nil || savedDraft == nil || savedDraft.Title != draft.Title {
		t.Fatalf("draft not retained: %v", err)
	}
	parent, err := s.Videos(ctx, blockedID)
	if err != nil || parent[0].Status != "failed" {
		t.Fatal("revision changed original video")
	}
	draft.Claims[0].Evidence[0].Quote = "fabricated evidence that is not in the source"
	payload, _ = json.Marshal(map[string]any{"script": draft})
	req = httptest.NewRequest("POST", "/api/videos/"+blockedID+"/revisions", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+api.Config.AdminToken)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatal("ungrounded revision accepted")
	}
	req = httptest.NewRequest("GET", "/api/overview", nil)
	req.Header.Set("Authorization", "Bearer "+api.Config.AdminToken)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "test-only-key") || strings.Contains(rec.Body.String(), api.Config.AdminToken) {
		t.Fatalf("overview leaks secret or failed: %d", rec.Code)
	}
	permanentJob, err := s.ClaimJob(ctx)
	if err != nil || permanentJob == nil {
		t.Fatalf("claim revision: %v", err)
	}
	permanentErr := fmt.Errorf("script generate: %w", &ProviderHTTPError{Provider: "google", HTTPStatus: 400, Message: "invalid request"})
	if err = s.Finish(ctx, *permanentJob, "", 0, permanentErr); err != nil {
		t.Fatal(err)
	}
	permanentVideos, err := s.Videos(ctx, permanentJob.VideoID)
	if err != nil || permanentVideos[0].Status != "failed" || permanentJob.Attempts != 1 {
		t.Fatalf("400 was queued for retry: %+v %v", permanentVideos, err)
	}
	_, err = s.Enqueue(ctx, []int64{inputs[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	transientJob, err := s.ClaimJob(ctx)
	if err != nil || transientJob == nil {
		t.Fatalf("claim transient: %v", err)
	}
	if err = s.Finish(ctx, *transientJob, "", 0, &ProviderHTTPError{Provider: "google", HTTPStatus: 429, Message: "rate limited"}); err != nil {
		t.Fatal(err)
	}
	transientVideos, err := s.Videos(ctx, transientJob.VideoID)
	if err != nil || transientVideos[0].Status != "queued" {
		t.Fatalf("429 did not retry: %+v %v", transientVideos, err)
	}
	var delaySeconds float64
	if err = s.DB.QueryRow(ctx, `SELECT extract(epoch FROM available_at-now())::float8 FROM jobs WHERE id=$1`, transientJob.ID).Scan(&delaySeconds); err != nil || delaySeconds < 110 || delaySeconds > 121 {
		t.Fatalf("retry was not delayed by two minutes: %v seconds, %v", delaySeconds, err)
	}
	_, err = s.Enqueue(ctx, []int64{inputs[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	refusalJob, err := s.ClaimJob(ctx)
	if err != nil || refusalJob == nil {
		t.Fatalf("claim refusal: %v", err)
	}
	if err = s.Finish(ctx, *refusalJob, "", 0, fmt.Errorf("script validate: %w", &InsufficientSourceError{})); err != nil {
		t.Fatal(err)
	}
	refusedVideos, err := s.Videos(ctx, refusalJob.VideoID)
	if err != nil || refusedVideos[0].Status != "failed" || refusalJob.Attempts != 1 {
		t.Fatalf("editorial refusal retried: %+v %v", refusedVideos, err)
	}
}

func assertVideoOverlays(t *testing.T, videoPath, title string, sources ...string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(videoPath), "subtitles.ass"))
	if err != nil {
		t.Fatal(err)
	}
	overlays := string(data)
	forbidden := append([]string{"Nguồn:", "TIN NÓNG", "DIỄN BIẾN", "ẢNH TỪ BÀI VIẾT", "VIDEO TỪ BÀI VIẾT"}, sources...)
	for _, text := range forbidden {
		if text != "" && strings.Contains(overlays, text) {
			t.Errorf("unexpected overlay %q", text)
		}
	}
	expected := titleOverlay(title)
	if !strings.Contains(overlays, expected) {
		t.Error("missing enlarged title")
	}
}
