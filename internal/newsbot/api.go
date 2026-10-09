package newsbot

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

type API struct {
	Store  *Store
	Redis  *redis.Client
	Config Config
}

func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func failure(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("yêu cầu phải có đúng một JSON object")
	}
	return nil
}
func (a *API) Handler() (http.Handler, error) {
	if len(a.Config.AdminToken) < 24 {
		return nil, fmt.Errorf("ADMIN_TOKEN cần ít nhất 24 ký tự")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := a.Store.DB.Ping(ctx); err != nil {
			failure(w, 503, "database unavailable")
			return
		}
		respond(w, 200, map[string]string{"status": "ok"})
	})
	private := http.NewServeMux()
	private.HandleFunc("GET /api/overview", a.overview)
	private.HandleFunc("GET /api/sources", func(w http.ResponseWriter, r *http.Request) {
		s, err := a.Store.Sources(r.Context(), false)
		if err != nil {
			a.internal(w, err)
			return
		}
		respond(w, 200, s)
	})
	private.HandleFunc("POST /api/sources", a.addSource)
	private.HandleFunc("GET /api/sources/export", a.exportSources)
	private.HandleFunc("POST /api/sources/import", a.importSources)
	private.HandleFunc("PATCH /api/sources/{id}", a.updateSource)
	private.HandleFunc("POST /api/sources/{id}/collect", a.collectSource)
	private.HandleFunc("GET /api/articles", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var sourceIDs []int64
		rawSources := append(q["source_id"], q["source_ids"]...)
		for _, raw := range rawSources {
			for _, part := range strings.Split(raw, ",") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				sid, err := strconv.ParseInt(part, 10, 64)
				if err != nil || sid <= 0 {
					respond(w, 400, map[string]string{"error": "Nguồn không hợp lệ"})
					return
				}
				sourceIDs = append(sourceIDs, sid)
			}
		}
		offset := 0
		var err error
		if raw := q.Get("offset"); raw != "" {
			offset, err = strconv.Atoi(raw)
			if err != nil || offset < 0 {
				respond(w, 400, map[string]string{"error": "Vị trí trang không hợp lệ"})
				return
			}
		}
		query := strings.TrimSpace(q.Get("q"))
		if len([]rune(query)) > 300 {
			respond(w, 400, map[string]string{"error": "Từ khóa quá dài"})
			return
		}
		s, err := a.Store.SearchArticles(r.Context(), query, sourceIDs, q.Get("category"), offset)
		if err != nil {
			a.internal(w, err)
			return
		}
		respond(w, 200, s)
	})
	private.HandleFunc("DELETE /api/sources/{id}", a.deleteSource)
	private.HandleFunc("POST /api/media/discover", a.discoverMedia)
	private.HandleFunc("GET /api/media/{id}/file", a.mediaFile)
	private.HandleFunc("GET /api/media/{id}/preview", a.mediaPreview)
	private.HandleFunc("POST /api/videos", a.generate)
	private.HandleFunc("GET /api/videos", func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if len([]rune(query)) > 300 {
			failure(w, 400, "Từ khóa quá dài")
			return
		}
		s, err := a.Store.Videos(r.Context(), "", query, r.URL.Query().Get("status"))
		if err != nil {
			a.internal(w, err)
			return
		}
		respond(w, 200, s)
	})
	private.HandleFunc("GET /api/videos/{id}", a.video)
	private.HandleFunc("GET /api/videos/{id}/file", a.file)
	private.HandleFunc("POST /api/videos/{id}/review", a.review)
	private.HandleFunc("POST /api/videos/{id}/revisions", a.revision)
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expected := "Bearer " + a.Config.AdminToken
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
			failure(w, 401, "cần Bearer ADMIN_TOKEN")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		private.ServeHTTP(w, r)
	}))
	return mux, nil
}

func (a *API) overview(w http.ResponseWriter, r *http.Request) {
	var stats struct {
		Sources       int `json:"sources"`
		ActiveSources int `json:"active_sources"`
		Articles      int `json:"articles"`
		Videos        int `json:"videos"`
		PendingReview int `json:"pending_review"`
		Processing    int `json:"processing"`
		Failed        int `json:"failed"`
	}
	err := a.Store.DB.QueryRow(r.Context(), `SELECT
 (SELECT count(*) FROM news_sources WHERE deleted_at IS NULL), (SELECT count(*) FROM news_sources WHERE active AND deleted_at IS NULL), (SELECT count(*) FROM news_articles),
 count(*), count(*) FILTER(WHERE status='pending_review'), count(*) FILTER(WHERE status IN('queued','processing')), count(*) FILTER(WHERE status='failed') FROM videos`).Scan(&stats.Sources, &stats.ActiveSources, &stats.Articles, &stats.Videos, &stats.PendingReview, &stats.Processing, &stats.Failed)
	if err != nil {
		a.internal(w, err)
		return
	}
	text, _ := a.Config.provider(a.Config.textProvider())
	speech, _ := a.Config.provider(a.Config.speechProvider())
	respond(w, 200, map[string]any{"stats": stats, "ai": map[string]any{"provider": a.Config.textProvider(), "model": text.TextModel, "tts_provider": a.Config.speechProvider(), "tts_model": speech.TTSModel, "voice": speech.Voice, "ready": a.Config.ValidateAI() == nil}})
}

func (a *API) collectSource(w http.ResponseWriter, r *http.Request) {
	sources, err := a.Store.Sources(r.Context(), false)
	if err != nil {
		a.internal(w, err)
		return
	}
	var source *Source
	for i := range sources {
		if fmt.Sprint(sources[i].ID) == r.PathValue("id") {
			source = &sources[i]
			break
		}
	}
	if source == nil {
		failure(w, 404, "không có nguồn")
		return
	}
	if !source.Active {
		failure(w, 409, "bật nguồn trước khi thu thập")
		return
	}
	articles, fetchErr := FetchFeed(r.Context(), NewFeedClient(), *source)
	added := 0
	if fetchErr == nil {
		for _, article := range articles {
			ok, err := a.Store.SaveArticle(r.Context(), article)
			if err != nil {
				fetchErr = err
				break
			}
			if ok {
				added++
			}
		}
	}
	var message *string
	if fetchErr != nil {
		m := fetchErr.Error()
		message = &m
	}
	if _, err := a.Store.DB.Exec(r.Context(), `UPDATE news_sources SET last_fetched_at=now(),last_error=$1 WHERE id=$2`, message, source.ID); err != nil {
		a.internal(w, err)
		return
	}
	if fetchErr != nil {
		failure(w, 502, "không thu thập được RSS; xem lỗi trong danh sách nguồn")
		return
	}
	respond(w, 200, map[string]int{"inserted": added, "received": len(articles)})
}

func (a *API) revision(w http.ResponseWriter, r *http.Request) {
	if err := a.Config.ValidateAI(); err != nil {
		failure(w, 503, err.Error())
		return
	}
	var input struct {
		Script   Script   `json:"script"`
		MediaIDs []string `json:"media_ids"`
	}
	if err := decode(w, r, &input); err != nil {
		failure(w, 400, err.Error())
		return
	}
	videos, err := a.Store.Videos(r.Context(), r.PathValue("id"))
	if err != nil {
		a.internal(w, err)
		return
	}
	if len(videos) == 0 {
		failure(w, 404, "không có video")
		return
	}
	if videos[0].Status == "queued" || videos[0].Status == "processing" {
		failure(w, 409, "chờ phiên bản hiện tại xử lý xong")
		return
	}
	articles, err := a.Store.JobArticles(r.Context(), videos[0].ID)
	if err != nil {
		a.internal(w, err)
		return
	}
	if err := input.Script.Validate(articles); err != nil {
		failure(w, 400, err.Error())
		return
	}
	if input.MediaIDs == nil {
		input.MediaIDs = videos[0].MediaIDs
	}
	ids := make([]int64, 0, len(articles))
	for _, article := range articles {
		ids = append(ids, article.ID)
	}
	if input.MediaIDs != nil {
		if err := a.Store.ValidateMedia(r.Context(), ids, input.MediaIDs); err != nil {
			failure(w, 400, err.Error())
			return
		}
	}
	id, err := a.Store.EnqueueRevision(r.Context(), videos[0].ID, articles, input.Script, input.MediaIDs)
	if err != nil {
		a.internal(w, err)
		return
	}
	a.wake(r)
	respond(w, 202, map[string]string{"id": id, "status": "queued", "parent_id": videos[0].ID})
}

func (a *API) wake(r *http.Request) {
	if a.Redis == nil {
		return
	}
	if err := a.Redis.LPush(r.Context(), "newsbot:wake", "1").Err(); err != nil {
		slog.Warn("redis wake failed; worker will poll", "error", err)
		return
	}
	_ = a.Redis.LTrim(r.Context(), "newsbot:wake", 0, 99).Err()
}
func (a *API) internal(w http.ResponseWriter, err error) {
	slog.Error("api error", "error", err)
	failure(w, 500, "lỗi nội bộ; xem log dịch vụ")
}
func (a *API) addSource(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name         string `json:"name"`
		RSSURL       string `json:"rss_url"`
		Language     string `json:"language"`
		Category     string `json:"category"`
		LicenseNotes string `json:"license_notes"`
	}
	if err := decode(w, r, &input); err != nil {
		failure(w, 400, err.Error())
		return
	}
	u, err := CanonicalURL(input.RSSURL)
	if err != nil || strings.TrimSpace(input.Name) == "" || len(input.Name) > 200 {
		failure(w, 400, "name hoặc rss_url không hợp lệ")
		return
	}
	if input.Language == "" {
		input.Language = "vi"
	}
	if input.Category == "" {
		input.Category = "TECHNOLOGY"
	}
	if input.LicenseNotes == "" {
		input.LicenseNotes = "Chưa xác nhận quyền sử dụng"
	}
	var id int64
	err = a.Store.DB.QueryRow(r.Context(), `INSERT INTO news_sources(name,rss_url,language,category,license_notes) VALUES($1,$2,$3,$4,$5) ON CONFLICT(rss_url) DO UPDATE SET name=EXCLUDED.name,language=EXCLUDED.language,category=EXCLUDED.category,license_notes=EXCLUDED.license_notes,deleted_at=NULL,active=false WHERE news_sources.deleted_at IS NOT NULL RETURNING id`, input.Name, u, input.Language, input.Category, input.LicenseNotes).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		failure(w, 409, "nguồn RSS đã tồn tại")
		return
	}
	if err != nil {
		a.internal(w, err)
		return
	}
	respond(w, 201, map[string]any{"id": id, "active": false})
}
func (a *API) updateSource(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Active       bool   `json:"active"`
		LicenseNotes string `json:"license_notes"`
	}
	if err := decode(w, r, &input); err != nil {
		failure(w, 400, err.Error())
		return
	}
	if input.Active && len(strings.TrimSpace(input.LicenseNotes)) < 10 {
		failure(w, 400, "ghi license_notes khi bật nguồn")
		return
	}
	result, err := a.Store.DB.Exec(r.Context(), `UPDATE news_sources SET active=$1,license_notes=$2 WHERE id::text=$3 AND deleted_at IS NULL`, input.Active, input.LicenseNotes, r.PathValue("id"))
	if err != nil {
		a.internal(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		failure(w, 404, "không có nguồn")
		return
	}
	respond(w, 200, map[string]bool{"active": input.Active})
}
func (a *API) generate(w http.ResponseWriter, r *http.Request) {
	if err := a.Config.ValidateAI(); err != nil {
		failure(w, 503, err.Error())
		return
	}
	var input struct {
		ArticleIDs []int64  `json:"article_ids"`
		MediaIDs   []string `json:"media_ids"`
	}
	if err := decode(w, r, &input); err != nil {
		failure(w, 400, err.Error())
		return
	}
	id, err := a.Store.Enqueue(r.Context(), input.ArticleIDs, input.MediaIDs)
	if err != nil {
		failure(w, 400, "chọn 1–6 ID bài viết có thật và không trùng")
		return
	}
	a.wake(r)
	respond(w, 202, map[string]string{"id": id, "status": "queued"})
}
func (a *API) video(w http.ResponseWriter, r *http.Request) {
	v, err := a.Store.Videos(r.Context(), r.PathValue("id"))
	if err != nil {
		a.internal(w, err)
		return
	}
	if len(v) == 0 {
		failure(w, 404, "không có video")
		return
	}
	v[0].Sources, err = a.Store.JobArticles(r.Context(), v[0].ID)
	if err != nil {
		a.internal(w, err)
		return
	}
	respond(w, 200, v[0])
}
func (a *API) review(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Decision string `json:"decision"`
	}
	if err := decode(w, r, &input); err != nil {
		failure(w, 400, err.Error())
		return
	}
	if input.Decision != "approved" && input.Decision != "rejected" {
		failure(w, 400, "decision phải là approved hoặc rejected")
		return
	}
	result, err := a.Store.DB.Exec(r.Context(), `UPDATE videos SET status=$1,reviewed_at=now() WHERE id=$2 AND status='pending_review'`, input.Decision, r.PathValue("id"))
	if err != nil {
		a.internal(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		failure(w, 409, "chỉ duyệt video pending_review")
		return
	}
	respond(w, 200, map[string]string{"status": input.Decision})
}
func (a *API) file(w http.ResponseWriter, r *http.Request) {
	var path *string
	err := a.Store.DB.QueryRow(r.Context(), `SELECT output_path FROM videos WHERE id=$1 AND status IN('pending_review','approved','rejected')`, r.PathValue("id")).Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && path == nil {
		failure(w, 404, "video chưa sẵn sàng")
		return
	}
	if err != nil {
		a.internal(w, err)
		return
	}
	base, err := filepath.Abs(a.Config.OutputDir)
	if err != nil {
		a.internal(w, err)
		return
	}
	real, err := filepath.EvalSymlinks(*path)
	if err != nil {
		failure(w, 404, "không tìm thấy file video")
		return
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		a.internal(w, err)
		return
	}
	rel, err := filepath.Rel(base, real)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		failure(w, 403, "file ngoài thư mục video")
		return
	}
	f, err := os.Open(real)
	if err != nil {
		failure(w, 404, "không tìm thấy file video")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		a.internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", `attachment; filename="newsbot.mp4"`)
	http.ServeContent(w, r, "video.mp4", info.ModTime(), f)
}

func HTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second}
}

func (a *API) deleteSource(w http.ResponseWriter, r *http.Request) {
	result, err := a.Store.DB.Exec(r.Context(), `UPDATE news_sources SET deleted_at=now(),active=false WHERE id::text=$1 AND deleted_at IS NULL`, r.PathValue("id"))
	if err != nil {
		a.internal(w, err)
		return
	}
	if result.RowsAffected() == 0 {
		failure(w, 404, "không có nguồn")
		return
	}
	respond(w, 200, map[string]bool{"deleted": true})
}
