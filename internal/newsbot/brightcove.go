package newsbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

var brightcovePlayerPath = regexp.MustCompile(`^/([0-9]+)/([A-Za-z0-9_-]+)_default/index\.html$`)
var brightcoveVideoID = regexp.MustCompile(`^[0-9]+$`)

func brightcoveEmbed(raw string) (account, player, id string, ok bool) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host != "players.brightcove.net" || u.User != nil {
		return
	}
	m := brightcovePlayerPath.FindStringSubmatch(u.Path)
	if len(m) != 3 {
		return
	}
	id = u.Query().Get("videoId")
	if !brightcoveVideoID.MatchString(id) {
		return "", "", "", false
	}
	return m[1], m[2], id, true
}

// Only the VideoObject matching this article's headline is resolved, never recommendations.
func articleBrightcoveEmbeds(data []byte, page string) []string {
	if internationalPublisher(page) != "aljazeera" {
		return nil
	}
	root, e := html.Parse(bytes.NewReader(data))
	if e != nil {
		return nil
	}
	var headline string
	var scripts []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "h1" && headline == "" {
			var b strings.Builder
			_ = html.Render(&b, n)
			headline = normalized(plainText(b.String()))
		}
		if n.Type == html.ElementNode && n.Data == "script" && nodeAttr(n, "type") == "application/ld+json" && n.FirstChild != nil {
			scripts = append(scripts, n.FirstChild.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if headline == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	var visit func(any)
	visit = func(v any) {
		switch o := v.(type) {
		case []any:
			for _, c := range o {
				visit(c)
			}
		case map[string]any:
			name, _ := o["name"].(string)
			embed, _ := o["embedUrl"].(string)
			if o["@type"] == "VideoObject" && normalized(name) == headline {
				if _, _, _, ok := brightcoveEmbed(embed); ok && !seen[embed] {
					out = append(out, embed)
					seen[embed] = true
				}
			}
			for _, c := range o {
				visit(c)
			}
		}
	}
	for _, s := range scripts {
		var v any
		if json.Unmarshal([]byte(s), &v) == nil {
			visit(v)
		}
	}
	return out
}

type brightcoveSource struct {
	Src        string          `json:"src"`
	Height     int             `json:"height"`
	KeySystems json.RawMessage `json:"key_systems"`
}

func playableBrightcoveSources(data []byte, id string) ([]string, error) {
	var response struct {
		ID      string             `json:"id"`
		Sources []brightcoveSource `json:"sources"`
	}
	if e := json.Unmarshal(data, &response); e != nil {
		return nil, e
	}
	if response.ID != id {
		return nil, fmt.Errorf("playback trả về video khác bài đang chọn")
	}
	sort.SliceStable(response.Sources, func(i, j int) bool { return response.Sources[i].Height > response.Sources[j].Height })
	var out []string
	seen := map[string]bool{}
	for _, s := range response.Sources {
		if len(s.KeySystems) > 0 && string(s.KeySystems) != "null" && string(s.KeySystems) != "{}" {
			continue
		}
		u, e := url.Parse(s.Src)
		if e != nil || u.Scheme != "https" {
			continue
		}
		path := strings.ToLower(u.Path)
		if !strings.HasSuffix(path, ".mp4") && !strings.HasSuffix(path, ".m3u8") {
			continue
		}
		if _, e = CanonicalURL(s.Src); e != nil || seen[s.Src] {
			continue
		}
		seen[s.Src] = true
		out = append(out, s.Src)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("video không có MP4/HLS công khai không DRM")
	}
	return out, nil
}

func (f *clipFetcher) brightcove(ctx context.Context, embed string) ([]string, error) {
	account, player, id, ok := brightcoveEmbed(embed)
	if !ok {
		return nil, fmt.Errorf("trình phát Brightcove không hợp lệ")
	}
	data, _, e := f.get(ctx, fmt.Sprintf("https://players.brightcove.net/%s/%s_default/config.json", account, player), 1<<20)
	if e != nil {
		return nil, e
	}
	var config struct {
		Account string `json:"account_id"`
		Cloud   struct {
			Policy string `json:"policy_key"`
		} `json:"video_cloud"`
	}
	if e = json.Unmarshal(data, &config); e != nil {
		return nil, e
	}
	if config.Account != account || config.Cloud.Policy == "" || len(config.Cloud.Policy) > 2048 || strings.ContainsAny(config.Cloud.Policy, "\r\n") {
		return nil, fmt.Errorf("cấu hình playback không hợp lệ")
	}
	data, _, e = f.getAccept(ctx, fmt.Sprintf("https://edge.api.brightcove.com/playback/v1/accounts/%s/videos/%s", account, id), 2<<20, "application/json;pk="+config.Cloud.Policy)
	if e != nil {
		return nil, e
	}
	return playableBrightcoveSources(data, id)
}

func resolvedArticleMedia(ctx context.Context, f *clipFetcher, data []byte, page string) ([]MediaAsset, error) {
	groups := mediaGroups(data, page)
	var last error
	for _, embed := range articleBrightcoveEmbeds(data, page) {
		urls, e := f.brightcove(ctx, embed)
		if e != nil {
			last = e
			continue
		}
		groups = append(groups, MediaAsset{Kind: "video", Candidates: urls})
	}
	return groups, last
}
