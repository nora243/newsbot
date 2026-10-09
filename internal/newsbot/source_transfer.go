package newsbot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type SourceSettings struct {
	Name                string `json:"name"`
	RSSURL              string `json:"rss_url"`
	Language            string `json:"language"`
	Category            string `json:"category"`
	Active              bool   `json:"active"`
	LicenseNotes        string `json:"license_notes"`
	AllowedImages       bool   `json:"allowed_images"`
	AllowedVideo        bool   `json:"allowed_video"`
	AttributionRequired bool   `json:"attribution_required"`
}
type SourceTransfer struct {
	Format  string           `json:"format"`
	Version int              `json:"version"`
	Sources []SourceSettings `json:"sources"`
}

func (f *SourceTransfer) validate() error {
	if f.Format != "newsbot-sources" || f.Version != 1 {
		return fmt.Errorf("định dạng hoặc phiên bản file nguồn không được hỗ trợ")
	}
	if f.Sources == nil {
		return fmt.Errorf("file thiếu danh sách sources")
	}
	if len(f.Sources) > 1000 {
		return fmt.Errorf("chỉ hỗ trợ tối đa 1000 nguồn mỗi file")
	}
	seen := map[string]bool{}
	for i := range f.Sources {
		s := &f.Sources[i]
		u, err := CanonicalURL(s.RSSURL)
		if err != nil {
			return fmt.Errorf("nguồn %d: URL RSS không hợp lệ", i+1)
		}
		s.RSSURL = u
		s.Name = strings.TrimSpace(s.Name)
		if s.Name == "" || len([]rune(s.Name)) > 300 || s.Language == "" || len(s.Language) > 30 || s.Category == "" || len(s.Category) > 100 || len([]rune(s.LicenseNotes)) > 10000 {
			return fmt.Errorf("nguồn %d: cấu hình không hợp lệ", i+1)
		}
		if s.Active && strings.TrimSpace(s.LicenseNotes) == "" {
			return fmt.Errorf("nguồn %d: cần ghi chú quyền sử dụng khi bật", i+1)
		}
		if seen[u] {
			return fmt.Errorf("URL RSS trùng trong file: %s", u)
		}
		seen[u] = true
	}
	return nil
}
func (a *API) exportSources(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.DB.Query(r.Context(), `SELECT name,rss_url,language,category,active,license_notes,allowed_images,allowed_video,attribution_required FROM news_sources WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		a.internal(w, err)
		return
	}
	defer rows.Close()
	file := SourceTransfer{Format: "newsbot-sources", Version: 1, Sources: []SourceSettings{}}
	for rows.Next() {
		var s SourceSettings
		if err = rows.Scan(&s.Name, &s.RSSURL, &s.Language, &s.Category, &s.Active, &s.LicenseNotes, &s.AllowedImages, &s.AllowedVideo, &s.AttributionRequired); err != nil {
			a.internal(w, err)
			return
		}
		file.Sources = append(file.Sources, s)
	}
	if err = rows.Err(); err != nil {
		a.internal(w, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="newsbot-sources.json"`)
	respond(w, 200, file)
}
func (a *API) importSources(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	var file SourceTransfer
	if err := d.Decode(&file); err != nil {
		failure(w, 400, "file JSON không hợp lệ hoặc vượt 1 MiB")
		return
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		failure(w, 400, "file phải chứa đúng một JSON object")
		return
	}
	if err := file.validate(); err != nil {
		failure(w, 400, err.Error())
		return
	}
	tx, err := a.Store.DB.Begin(r.Context())
	if err != nil {
		a.internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	for _, s := range file.Sources {
		_, err = tx.Exec(r.Context(), `INSERT INTO news_sources(name,rss_url,language,category,active,license_notes,allowed_images,allowed_video,attribution_required) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(rss_url) DO UPDATE SET name=EXCLUDED.name,language=EXCLUDED.language,category=EXCLUDED.category,active=EXCLUDED.active,license_notes=EXCLUDED.license_notes,allowed_images=EXCLUDED.allowed_images,allowed_video=EXCLUDED.allowed_video,attribution_required=EXCLUDED.attribution_required,deleted_at=NULL`, s.Name, s.RSSURL, s.Language, s.Category, s.Active, s.LicenseNotes, s.AllowedImages, s.AllowedVideo, s.AttributionRequired)
		if err != nil {
			a.internal(w, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.internal(w, err)
		return
	}
	respond(w, 200, map[string]int{"imported": len(file.Sources)})
}
