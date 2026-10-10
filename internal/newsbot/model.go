package newsbot

import (
	"fmt"
	"strings"
	"time"
)

type Source struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	RSSURL        string     `json:"rss_url"`
	Language      string     `json:"language"`
	Category      string     `json:"category"`
	Active        bool       `json:"active"`
	LicenseNotes  string     `json:"license_notes"`
	LastError     *string    `json:"last_error"`
	LastFetchedAt *time.Time `json:"last_fetched_at"`
}

type Article struct {
	UsedForVideo bool       `json:"used_for_video"`
	ID           int64      `json:"id"`
	SourceID     int64      `json:"source_id"`
	SourceName   string     `json:"source_name"`
	URL          string     `json:"url"`
	Title        string     `json:"title"`
	Content      string     `json:"content"`
	PublishedAt  *time.Time `json:"published_at"`
	Hash         string     `json:"-"`
}

type Evidence struct {
	ArticleID int64  `json:"article_id"`
	Quote     string `json:"quote"`
}
type Claim struct {
	ID       string     `json:"id"`
	Text     string     `json:"text"`
	Evidence []Evidence `json:"evidence"`
}
type Scene struct {
	Narration string   `json:"narration"`
	Subtitle  string   `json:"subtitle"`
	ClaimIDs  []string `json:"claim_ids"`
}
type Script struct {
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Uncertainties []string `json:"uncertainties"`
	Claims        []Claim  `json:"claims"`
	Scenes        []Scene  `json:"scenes"`
}
type SceneCheck struct {
	SceneIndex int    `json:"scene_index"`
	Supported  bool   `json:"supported"`
	Reason     string `json:"reason"`
}
type FactCheck struct {
	Scenes []SceneCheck `json:"scenes"`
}

func normalized(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

// InsufficientSourceError represents an explicit editorial refusal, not a transport failure.
type InsufficientSourceError struct{}

func (*InsufficientSourceError) Error() string {
	return "AI không tạo kịch bản vì nguồn chưa đủ dữ kiện; hãy chọn thêm bài có nội dung liên quan hoặc nguồn có nội dung đầy đủ"
}
func (*InsufficientSourceError) Permanent() bool { return true }

// Validate checks provenance, not truth. Editorial review is still required.
func (s Script) Validate(articles []Article) error {
	if s.Claims != nil && s.Scenes != nil && len(s.Claims) == 0 && len(s.Scenes) == 0 {
		return &InsufficientSourceError{}
	}
	if strings.TrimSpace(s.Title) == "" || len([]rune(s.Title)) > 160 {
		return fmt.Errorf("tiêu đề kịch bản trống hoặc vượt 160 ký tự")
	}
	if len(s.Claims) == 0 || len(s.Scenes) < 2 || len(s.Scenes) > 8 {
		return fmt.Errorf("cấu trúc kịch bản không hợp lệ: nhận %d dữ kiện, %d cảnh; cần ít nhất 1 dữ kiện và 2–8 cảnh", len(s.Claims), len(s.Scenes))
	}
	inputs := map[int64]string{}
	for _, a := range articles {
		inputs[a.ID] = normalized(a.Title + " " + a.Content)
	}
	claims := map[string]bool{}
	for _, c := range s.Claims {
		if c.ID == "" || claims[c.ID] || c.Text == "" || len(c.Evidence) == 0 {
			return fmt.Errorf("dữ kiện không hợp lệ: %s", c.ID)
		}
		claims[c.ID] = true
		for _, e := range c.Evidence {
			body, ok := inputs[e.ArticleID]
			quote := normalized(e.Quote)
			if !ok || len([]rune(quote)) < 12 || !strings.Contains(body, quote) {
				return fmt.Errorf("bằng chứng không có trong nguồn cho %s", c.ID)
			}
		}
	}
	words := 0
	for _, scene := range s.Scenes {
		if strings.TrimSpace(scene.Narration) == "" || scene.Subtitle != scene.Narration || len(scene.ClaimIDs) == 0 {
			return fmt.Errorf("cảnh thiếu lời đọc, phụ đề khớp lời đọc hoặc dữ kiện")
		}
		words += len(strings.Fields(scene.Narration))
		for _, id := range scene.ClaimIDs {
			if !claims[id] {
				return fmt.Errorf("cảnh dẫn dữ kiện không tồn tại: %s", id)
			}
		}
	}
	if words < 260 || words > 480 {
		return fmt.Errorf("lời đọc phải có 260–480 từ, nhận %d", words)
	}
	return nil
}

func (f FactCheck) Validate(n int) error {
	if len(f.Scenes) != n {
		return fmt.Errorf("fact checker chưa kiểm tra đủ các cảnh")
	}
	seen := map[int]bool{}
	for _, c := range f.Scenes {
		if c.SceneIndex < 0 || c.SceneIndex >= n || seen[c.SceneIndex] || !c.Supported {
			return fmt.Errorf("cảnh %d chưa được nguồn hỗ trợ: %s", c.SceneIndex+1, c.Reason)
		}
		seen[c.SceneIndex] = true
	}
	return nil
}
