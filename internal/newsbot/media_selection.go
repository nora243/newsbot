package newsbot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type MediaAsset struct {
	ID           string   `json:"id"`
	ArticleID    int64    `json:"article_id"`
	Kind         string   `json:"kind"`
	SourceName   string   `json:"source_name"`
	ArticleTitle string   `json:"article_title"`
	Uploaded     bool     `json:"uploaded"`
	Candidates   []string `json:"-"`
}
type ResolvedMedia struct {
	ID, Kind, Path, SourceName string
	ArticleID                  int64
	Duration                   float64
}

var dwVideoSlug = regexp.MustCompile(`(?i)[a-z0-9]+_[a-z0-9_]+_01[a-z0-9]+`)

func articleVideoKey(u *url.URL) string {
	path := strings.ToLower(u.Path)
	if slug := dwVideoSlug.FindString(path); slug != "" {
		return "dw:" + strings.ToLower(slug)
	}
	clean := strings.TrimSuffix(path, "/master.m3u8")
	clean = strings.TrimSuffix(clean, ".mp4")
	clean = strings.TrimSuffix(clean, ".m3u8")
	clean = regexp.MustCompile(`[_-](?:1080p|720p|480p|[0-9]+x[0-9]+)$`).ReplaceAllString(clean, "")
	return u.Host + clean
}

func mediaGroups(data []byte, page string) []MediaAsset {
	var out []MediaAsset
	photos, _ := articleImages(bytes.NewReader(data), page)
	imageGroups := map[string]int{}
	for _, raw := range photos {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		key := articleImageKey(u)
		if i, ok := imageGroups[key]; ok {
			out[i].Candidates = append(out[i].Candidates, raw)
			continue
		}
		imageGroups[key] = len(out)
		out = append(out, MediaAsset{Kind: "image", Candidates: []string{raw}})
	}
	videoGroups := map[string]int{}
	for _, raw := range articleVideoURLs(data, page) {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		path := strings.ToLower(u.Path)
		if !strings.HasSuffix(path, ".mp4") && !strings.HasSuffix(path, ".m3u8") {
			continue
		}
		key := articleVideoKey(u)
		if i, ok := videoGroups[key]; ok {
			out[i].Candidates = append(out[i].Candidates, raw)
			continue
		}
		videoGroups[key] = len(out)
		out = append(out, MediaAsset{Kind: "video", Candidates: []string{raw}})
	}
	for i := range out {
		seen := map[string]bool{}
		var dedup []string
		for _, c := range out[i].Candidates {
			if !seen[c] {
				seen[c] = true
				dedup = append(dedup, c)
			}
		}
		if out[i].Kind == "image" {
			sort.SliceStable(dedup, func(a, b int) bool {
				aJpg := strings.HasSuffix(strings.ToLower(dedup[a]), ".jpg") || strings.HasSuffix(strings.ToLower(dedup[a]), ".jpeg")
				bJpg := strings.HasSuffix(strings.ToLower(dedup[b]), ".jpg") || strings.HasSuffix(strings.ToLower(dedup[b]), ".jpeg")
				if aJpg != bJpg {
					return aJpg
				}
				return false
			})
		}
		out[i].Candidates = dedup
	}
	return out
}
func (s *Store) Asset(ctx context.Context, id string) (MediaAsset, error) {
	var m MediaAsset
	var raw []byte
	err := s.DB.QueryRow(ctx, `SELECT m.id,m.article_id,m.kind,m.candidates,m.uploaded,CASE WHEN m.uploaded THEN 'Tải lên' ELSE s.name END,CASE WHEN m.uploaded THEN m.upload_name ELSE a.title END FROM media_assets m JOIN news_articles a ON a.id=m.article_id JOIN news_sources s ON s.id=a.source_id WHERE m.id=$1`, id).Scan(&m.ID, &m.ArticleID, &m.Kind, &raw, &m.Uploaded, &m.SourceName, &m.ArticleTitle)
	if err == nil {
		err = json.Unmarshal(raw, &m.Candidates)
	}
	return m, err
}
func (s *Store) ValidateMedia(ctx context.Context, articles []int64, ids []string) error {
	if len(ids) > 24 {
		return fmt.Errorf("chọn tối đa 24 ảnh/video")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return fmt.Errorf("ảnh/video được chọn bị trùng")
		}
		seen[id] = true
		var valid bool
		err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM media_assets WHERE id=$1 AND article_id=ANY($2::bigint[]))`, id, articles).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("ảnh/video không thuộc các bài đã chọn")
		}
	}
	return nil
}
func (a *API) discoverMedia(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ArticleIDs []int64 `json:"article_ids"`
	}
	if err := decode(w, r, &input); err != nil {
		failure(w, 400, err.Error())
		return
	}
	if len(input.ArticleIDs) < 1 || len(input.ArticleIDs) > 6 {
		failure(w, 400, "chọn 1–6 bài")
		return
	}
	articles, err := a.Store.Articles(r.Context(), input.ArticleIDs)
	if err != nil {
		a.internal(w, err)
		return
	}
	if len(articles) != len(input.ArticleIDs) {
		failure(w, 400, "bài viết không hợp lệ hoặc bị trùng")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	client := NewFeedClient()
	defer client.CloseIdleConnections()
	assets := []MediaAsset{}
	warnings := []string{}
	for _, article := range articles {
		if googleNewsArticle(article.URL) {
			warnings = append(warnings, fmt.Sprintf("%s: nguồn cung cấp URL Google News, chưa có URL bài Reuters trực tiếp nên không thể quét media chính xác.", article.Title))
			continue
		}
		f := clipFetcher{client: client, remaining: maxArticleBytes}
		data, base, e := f.get(ctx, article.URL, maxArticleBytes)
		if e != nil {
			warnings = append(warnings, fmt.Sprintf("%s: không đọc được trang bài viết; chỉ hiển thị media đã lưu.", article.Title))
		} else {
			groups, resolveErr := resolvedArticleMedia(ctx, &f, data, base)
			if resolveErr != nil {
				warnings = append(warnings, fmt.Sprintf("%s: không đọc được playback video (%v).", article.Title, resolveErr))
			}
			hasVideo := false
			for _, g := range groups {
				if g.Kind == "video" {
					hasVideo = true
				}
			}
			if !hasVideo && embeddedVideoWithoutFile(data, base) {
				warnings = append(warnings, fmt.Sprintf("%s: có trình phát video nhưng chưa tìm thấy URL MP4/HLS công khai; chưa thể chọn video này.", article.Title))
			}
			currentIDs := map[string]bool{}
			for _, m := range groups {
				u, _ := url.Parse(m.Candidates[0])
				key := m.Candidates[0]
				if m.Kind == "image" {
					key = articleImageKey(u)
				} else if m.Kind == "video" {
					key = articleVideoKey(u)
				}
				hash := sha256.Sum256(fmt.Appendf(nil, "%d:%s:%s", article.ID, m.Kind, key))
				m.ID = hex.EncodeToString(hash[:16])
				currentIDs[m.ID] = true
				raw, _ := json.Marshal(m.Candidates)
				if _, err = a.Store.DB.Exec(ctx, `INSERT INTO media_assets(id,article_id,kind,candidates) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET candidates=$4`, m.ID, article.ID, m.Kind, raw); err != nil {
					a.internal(w, err)
					return
				}
			}
			if len(currentIDs) > 0 {
				var keepIDs []string
				for id := range currentIDs {
					keepIDs = append(keepIDs, id)
				}
				if _, err = a.Store.DB.Exec(ctx, `DELETE FROM media_assets WHERE article_id=$1 AND NOT uploaded AND NOT (id=ANY($2))`, article.ID, keepIDs); err != nil {
					a.internal(w, err)
					return
				}
			}
		}
		rows, e := a.Store.DB.Query(ctx, `SELECT id,kind,uploaded,upload_name FROM media_assets WHERE article_id=$1 ORDER BY created_at,id`, article.ID)
		if e != nil {
			a.internal(w, e)
			return
		}
		count := 0
		for rows.Next() {
			m := MediaAsset{ArticleID: article.ID, SourceName: article.SourceName, ArticleTitle: article.Title}
			if e = rows.Scan(&m.ID, &m.Kind, &m.Uploaded, &m.ArticleTitle); e != nil {
				break
			}
			if m.Uploaded {
				m.SourceName = "Tải lên"
			} else {
				m.ArticleTitle = article.Title
			}
			assets = append(assets, m)
			count++
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			a.internal(w, e)
			return
		}
		if count == 0 {
			warnings = append(warnings, article.Title+": chưa tìm thấy ảnh/video hỗ trợ.")
		}
	}
	respond(w, 200, map[string]any{"assets": assets, "warnings": warnings})
}

// Cache immutable assets in the shared video volume, so preview and render use the same bytes.
var mediaLocks [64]sync.Mutex

func materializeMedia(ctx context.Context, base string, m MediaAsset) (ResolvedMedia, error) {
	result := ResolvedMedia{ID: m.ID, Kind: m.Kind, ArticleID: m.ArticleID, SourceName: m.SourceName}
	if len(m.ID) != 32 || strings.Trim(m.ID, "0123456789abcdef") != "" {
		return result, fmt.Errorf("media ID không hợp lệ")
	}
	hash := sha256.Sum256([]byte(m.ID))
	mu := &mediaLocks[int(hash[0])%len(mediaLocks)]
	mu.Lock()
	defer mu.Unlock()
	dir, err := filepath.Abs(filepath.Join(base, "media", m.ID))
	if err != nil {
		return result, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return result, err
	}
	ext := ".jpg"
	if m.Kind == "video" {
		ext = ".mp4"
	}
	result.Path = filepath.Join(dir, "asset"+ext)
	if _, err = os.Stat(result.Path); err == nil {
		if m.Kind == "video" {
			result.Duration, err = probe(ctx, dir, result.Path)
		}
		return result, err
	}
	if m.Uploaded {
		return result, fmt.Errorf("tệp tải lên không còn trên máy chủ")
	}
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	client := NewFeedClient()
	defer client.CloseIdleConnections()
	f := clipFetcher{client: client, remaining: 120 << 20}
	if m.Kind == "video" {
		clip, e := downloadClipURLs(ctx, &f, m.Candidates, dir, m.ArticleID)
		if e != nil {
			return result, e
		}
		if clip.Name == "" {
			return result, fmt.Errorf("không tải được video")
		}
		result.Duration = clip.Duration
		err = os.Rename(clip.Name, result.Path)
	} else {
		var data []byte
		err = fmt.Errorf("không tải được ảnh")
		for _, raw := range m.Candidates {
			data, _, err = f.get(ctx, raw, 10<<20)
			if err == nil {
				data, err = normalizePhoto(data)
			}
			if err == nil {
				break
			}
		}
		if err == nil {
			tmp, e := os.CreateTemp(dir, "image-")
			if e != nil {
				return result, e
			}
			name := tmp.Name()
			defer os.Remove(name)
			_, err = tmp.Write(data)
			closeErr := tmp.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(name, result.Path)
			}
		}
	}
	return result, err
}
func (a *API) mediaFile(w http.ResponseWriter, r *http.Request) {
	m, err := a.Store.Asset(r.Context(), r.PathValue("id"))
	if err != nil {
		failure(w, 404, "không có media")
		return
	}
	resolved, err := materializeMedia(r.Context(), a.Config.OutputDir, m)
	if err != nil {
		failure(w, 502, "Không tải được ảnh/video này. Hãy thử lại hoặc bỏ chọn.")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, resolved.Path)
}
func (s *Store) ResolveVideoMedia(ctx context.Context, id, dir string) ([]ResolvedMedia, error) {
	var ids []string
	if err := s.DB.QueryRow(ctx, `SELECT media_ids FROM videos WHERE id=$1`, id).Scan(&ids); err != nil {
		return nil, err
	}
	if ids == nil {
		return nil, nil
	}
	out := make([]ResolvedMedia, 0, len(ids))
	for _, id := range ids {
		m, err := s.Asset(ctx, id)
		if err != nil {
			return nil, err
		}
		resolved, err := materializeMedia(ctx, dir, m)
		if err != nil {
			return nil, fmt.Errorf("media đã chọn %s không tải được; hãy chọn lại", id)
		}
		out = append(out, resolved)
	}
	return out, nil
}
