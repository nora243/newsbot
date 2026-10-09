package newsbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type articleClip struct {
	Name     string
	Duration float64
}

func articleVideoURLs(data []byte, page string) []string {
	if googleNewsArticle(page) {
		return nil
	}
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	base, err := url.Parse(page)
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inVideo bool) {
		if skipEditorialNode(n) {
			return
		}
		inVideo = inVideo || n.Type == html.ElementNode && n.Data == "video"
		// Thanh Niên/VTV store the actual file on the editorial VideoStream block.
		if n.Type == html.ElementNode && nodeAttr(n, "type") == "VideoStream" && (base.Hostname() == "thanhnien.vn" || base.Hostname() == "vtv.vn") {
			ref := strings.TrimSpace(nodeAttr(n, "data-vid"))
			if strings.HasPrefix(ref, "thanhnien.mediacdn.vn/") {
				ref = "https://" + ref
			}
			if raw, e := resolveMedia(page, ref); ref != "" && e == nil {
				u, _ := url.Parse(raw)
				if (strings.HasSuffix(strings.ToLower(u.Path), ".mp4") || strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")) && !seen[raw] {
					seen[raw] = true
					out = append(out, raw)
				}
			}
		}
		if inVideo && n.Type == html.ElementNode && (n.Data == "video" || n.Data == "source") {
			for _, a := range n.Attr {
				if a.Key == "src" || a.Key == "data-src" {
					ref, e := url.Parse(a.Val)
					if e != nil {
						continue
					}
					raw := base.ResolveReference(ref).String()
					if _, e = CanonicalURL(raw); e == nil && !seen[raw] {
						seen[raw] = true
						out = append(out, raw)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inVideo)
		}
	}
	body := articleMediaBody(root)
	if body == nil {
		body = root
	}
	walk(body, false)
	// VTV video pages place the main player outside the (sometimes empty) article body.
	// Match its article ID instead of scanning all recommendation players.
	if base.Hostname() == "vtv.vn" && strings.HasPrefix(base.Path, "/video/") {
		var findMain func(*html.Node)
		findMain = func(n *html.Node) {
			id := nodeAttr(n, "data-videofocus-id")
			if id != "" && regexp.MustCompile(`^[0-9]+$`).MatchString(id) && strings.HasSuffix(base.Path, "-"+id+".htm") && nodeAttr(n, "type") == "VideoStream" {
				walk(n, false)
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				findMain(c)
			}
		}
		findMain(root)
	}
	for _, raw := range matchingMetadataVideos(root, page) {
		if !seen[raw] {
			seen[raw] = true
			out = append(out, raw)
		}
	}
	// Dân trí identifies embedded videos by ID; the playable URL lives in JSON-LD.
	if base.Hostname() == "dantri.com.vn" || base.Hostname() == "www.dantri.com.vn" {
		ids := map[string]bool{}
		var collect func(*html.Node)
		collect = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "video" {
				for _, a := range n.Attr {
					if a.Key == "data-video-id" && regexp.MustCompile(`^[0-9]+$`).MatchString(a.Val) {
						ids[a.Val] = true
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				collect(c)
			}
		}
		collect(body)
		var visitJSON func(any)
		visitJSON = func(value any) {
			switch v := value.(type) {
			case []any:
				for _, child := range v {
					visitJSON(child)
				}
			case map[string]any:
				if v["@type"] == "VideoObject" {
					link, _ := v["url"].(string)
					media, _ := v["contentUrl"].(string)
					u, e := url.Parse(link)
					if e == nil && (u.Hostname() == "dantri.com.vn" || u.Hostname() == "www.dantri.com.vn") {
						for id := range ids {
							if strings.HasSuffix(u.Path, "-"+id+".htm") {
								raw, e := resolveMedia(page, media)
								if media != "" && e == nil && !seen[raw] {
									seen[raw] = true
									out = append([]string{raw}, out...)
								}
							}
						}
					}
				}
				for _, child := range v {
					visitJSON(child)
				}
			}
		}
		var metadata func(*html.Node)
		metadata = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "script" {
				for _, a := range n.Attr {
					if a.Key == "type" && a.Val == "application/ld+json" && n.FirstChild != nil {
						var value any
						if json.Unmarshal([]byte(n.FirstChild.Data), &value) == nil {
							visitJSON(value)
						}
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				metadata(c)
			}
		}
		metadata(root)
	}
	return out
}

// Fetch every playlist, key and segment through the restricted HTTP client. FFmpeg
// only receives a generated local playlist and cannot follow remote media URLs.
type clipFetcher struct {
	client    *http.Client
	remaining int64
}

func (f *clipFetcher) get(ctx context.Context, raw string, limit int64) ([]byte, string, error) {
	return f.getAccept(ctx, raw, limit, "")
}

func (f *clipFetcher) getAccept(ctx context.Context, raw string, limit int64, accept string) ([]byte, string, error) {
	if _, err := CanonicalURL(raw); err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("video HTTP %d", resp.StatusCode)
	}
	if limit > f.remaining {
		limit = f.remaining
	}
	if limit <= 0 {
		return nil, "", fmt.Errorf("video vượt giới hạn tải")
	}
	if resp.ContentLength > 0 && resp.ContentLength > limit {
		return nil, "", fmt.Errorf("video vượt giới hạn tải")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(b)) > limit {
		return nil, "", fmt.Errorf("video vượt giới hạn tải")
	}
	f.remaining -= int64(len(b))
	return b, resp.Request.URL.String(), nil
}
func resolveMedia(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(strings.TrimSpace(ref))
	if err != nil {
		return "", err
	}
	raw := b.ResolveReference(u).String()
	_, err = CanonicalURL(raw)
	return raw, err
}

var hlsAttr = regexp.MustCompile(`([A-Z0-9-]+)=("[^"]*"|[^,]*)`)

func hlsAttributes(line string) map[string]string {
	out := map[string]string{}
	for _, m := range hlsAttr.FindAllStringSubmatch(line, -1) {
		out[m[1]] = strings.Trim(m[2], `"`)
	}
	return out
}
func (f *clipFetcher) playlist(ctx context.Context, raw, dir string, depth int) (string, error) {
	if depth > 3 {
		return "", fmt.Errorf("HLS lồng quá sâu")
	}
	data, base, err := f.get(ctx, raw, 1<<20)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(data)), "#EXTM3U") {
		return "", fmt.Errorf("playlist HLS không hợp lệ")
	}
	lines := strings.Split(string(data), "\n")
	best := ""
	bestBandwidth := 0
	pending := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			a := hlsAttributes(line)
			pending, _ = strconv.Atoi(a["BANDWIDTH"])
			if pending == 0 {
				pending = 1
			}
			continue
		}
		if pending > 0 && line != "" && !strings.HasPrefix(line, "#") {
			if pending > bestBandwidth {
				best = line
				bestBandwidth = pending
			}
			pending = 0
		}
	}
	if best != "" {
		next, e := resolveMedia(base, best)
		if e != nil {
			return "", e
		}
		return f.playlist(ctx, next, dir, depth+1)
	}
	var out strings.Builder
	out.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:60\n")
	duration := 0.0
	segmentDuration := 0.0
	segments := 0
	keys := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			seq, e := strconv.ParseUint(strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"), 10, 64)
			if e != nil {
				return "", e
			}
			fmt.Fprintf(&out, "#EXT-X-MEDIA-SEQUENCE:%d\n", seq)
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			a := hlsAttributes(line)
			if a["METHOD"] == "NONE" {
				out.WriteString("#EXT-X-KEY:METHOD=NONE\n")
				continue
			}
			if a["METHOD"] != "AES-128" || a["KEYFORMAT"] != "" && a["KEYFORMAT"] != "identity" {
				return "", fmt.Errorf("kiểu mã hóa HLS chưa hỗ trợ")
			}
			keyURL, e := resolveMedia(base, a["URI"])
			if e != nil {
				return "", e
			}
			key, _, e := f.get(ctx, keyURL, 16)
			if e != nil {
				return "", e
			}
			if len(key) != 16 {
				return "", fmt.Errorf("khóa HLS không hợp lệ")
			}
			name := fmt.Sprintf("key-%d.key", keys)
			keys++
			if keys > 32 {
				return "", fmt.Errorf("quá nhiều khóa HLS")
			}
			if e = os.WriteFile(filepath.Join(dir, name), key, 0600); e != nil {
				return "", e
			}
			fmt.Fprintf(&out, "#EXT-X-KEY:METHOD=AES-128,URI=\"%s\"", name)
			if iv := a["IV"]; iv != "" {
				if !regexp.MustCompile(`^0[xX][0-9a-fA-F]{32}$`).MatchString(iv) {
					return "", fmt.Errorf("IV không hợp lệ")
				}
				fmt.Fprintf(&out, ",IV=%s", iv)
			}
			out.WriteByte('\n')
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE"), strings.HasPrefix(line, "#EXT-X-MAP"):
			return "", fmt.Errorf("HLS byte-range/fMP4 chưa hỗ trợ")
		case strings.HasPrefix(line, "#EXTINF:"):
			value := strings.SplitN(strings.TrimPrefix(line, "#EXTINF:"), ",", 2)[0]
			segmentDuration, err = strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(segmentDuration) || math.IsInf(segmentDuration, 0) || segmentDuration <= 0 || segmentDuration > 60 {
				return "", fmt.Errorf("thời lượng đoạn HLS không hợp lệ")
			}
		case line == "#EXT-X-DISCONTINUITY":
			out.WriteString(line)
			out.WriteByte('\n')
		case line != "" && !strings.HasPrefix(line, "#"):
			if segmentDuration == 0 {
				return "", fmt.Errorf("HLS thiếu thời lượng")
			}
			u, e := resolveMedia(base, line)
			if e != nil {
				return "", e
			}
			b, _, e := f.get(ctx, u, 20<<20)
			if e != nil {
				return "", e
			}
			name := fmt.Sprintf("segment-%03d.ts", segments)
			if e = os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
				return "", e
			}
			fmt.Fprintf(&out, "#EXTINF:%.6f,\n%s\n", segmentDuration, name)
			segments++
			duration += segmentDuration
			segmentDuration = 0
		}
		if duration >= 90 || segments >= 90 {
			break
		}
	}
	if segments == 0 {
		return "", fmt.Errorf("HLS không có đoạn video")
	}
	out.WriteString("#EXT-X-ENDLIST\n")
	path := filepath.Join(dir, "local.m3u8")
	return path, os.WriteFile(path, []byte(out.String()), 0600)
}

func loadArticleClip(ctx context.Context, article Article, dir string) (articleClip, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	client := NewFeedClient()
	defer client.CloseIdleConnections()
	f := clipFetcher{client: client, remaining: 120 << 20}
	page, base, err := f.get(ctx, article.URL, maxArticleBytes)
	if err != nil {
		return articleClip{}, err
	}
	groups, _ := resolvedArticleMedia(ctx, &f, page, base)
	var urls []string
	for _, g := range groups {
		if g.Kind == "video" {
			urls = append(urls, g.Candidates...)
		}
	}
	if len(urls) == 0 {
		return articleClip{}, nil
	}
	return downloadClipURLs(ctx, &f, urls, dir, article.ID)
}

func downloadClipURLs(ctx context.Context, f *clipFetcher, urls []string, dir string, articleID int64) (articleClip, error) {
	var last error
	for i, raw := range urls {
		if i >= 3 {
			break
		}
		sub, err := os.MkdirTemp(dir, "clip-")
		if err != nil {
			return articleClip{}, err
		}
		u, _ := url.Parse(raw)
		var input string
		if strings.HasSuffix(strings.ToLower(u.Path), ".m3u8") {
			input, err = f.playlist(ctx, raw, sub, 0)
		} else if strings.HasSuffix(strings.ToLower(u.Path), ".mp4") {
			var data []byte
			data, _, err = f.get(ctx, raw, 100<<20)
			input = filepath.Join(sub, "input.mp4")
			if err == nil {
				err = os.WriteFile(input, data, 0600)
			}
		} else {
			err = fmt.Errorf("định dạng video chưa hỗ trợ")
		}
		if err != nil {
			last = err
			continue
		}
		args := []string{"-hide_banner", "-loglevel", "error", "-y", "-protocol_whitelist", "file,crypto"}
		if strings.HasSuffix(input, ".m3u8") {
			args = append(args, "-f", "hls", "-allowed_extensions", "ALL")
		} else {
			args = append(args, "-f", "mov")
		}
		args = append(args, "-i", input, "-t", "90", "-map", "0:v:0", "-an", "-vf", "scale=1280:720:force_original_aspect_ratio=decrease:force_divisible_by=2,setsar=1", "-c:v", "libx264", "-threads", "2", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p", "clip.mp4")
		_, err = command(ctx, sub, "ffmpeg", args...)
		if err != nil {
			last = fmt.Errorf("không giải mã được clip nguồn")
			continue
		}
		duration, e := probe(ctx, sub, "clip.mp4")
		if e != nil {
			last = e
			continue
		}
		slog.Info("article video loaded", "article_id", articleID, "duration", duration)
		return articleClip{Name: filepath.Join(sub, "clip.mp4"), Duration: duration}, nil
	}
	return articleClip{}, last
}
