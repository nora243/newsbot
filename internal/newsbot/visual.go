package newsbot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"

	_ "golang.org/x/image/webp"
	"image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Only publisher-selected social preview images are used, never unrelated stock imagery.
func previewImage(r io.Reader, pageURL string) (string, error) {
	root, err := html.Parse(r)
	if err != nil {
		return "", err
	}
	var og, twitter string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" {
			var key, value string
			for _, a := range n.Attr {
				if a.Key == "property" || a.Key == "name" {
					key = strings.ToLower(a.Val)
				}
				if a.Key == "content" {
					value = strings.TrimSpace(a.Val)
				}
			}
			if key == "og:image" && og == "" {
				og = value
			}
			if key == "twitter:image" && twitter == "" {
				twitter = value
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if og == "" {
		og = twitter
	}
	if og == "" {
		return "", fmt.Errorf("bài không có ảnh đại diện")
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(og)
	if err != nil {
		return "", err
	}
	return CanonicalURL(base.ResolveReference(ref).String())
}

// articleImages prefers editorial body images over social cards and excludes related-news widgets.
func articleImages(r io.Reader, pageURL string) ([]string, error) {
	if googleNewsArticle(pageURL) {
		return nil, fmt.Errorf("URL Google News chưa được phân giải về bài gốc")
	}
	data, err := io.ReadAll(io.LimitReader(r, maxArticleBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArticleBytes {
		return nil, fmt.Errorf("trang bài viết quá lớn")
	}
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}
	body := articleMediaBody(root)
	var result []string
	seen := map[string]bool{}
	add := func(raw string) bool {
		if strings.Contains(raw, "${formatId}") || strings.Contains(raw, "${formatid}") {
			raw = strings.ReplaceAll(raw, "${formatId}", "605")
			raw = strings.ReplaceAll(raw, "${formatid}", "605")
		}
		if strings.Contains(raw, "${") {
			return false
		}
		ref, e := url.Parse(strings.TrimSpace(raw))
		if e != nil {
			return false
		}
		resolved := base.ResolveReference(ref).String()
		canonical, e := CanonicalURL(resolved)
		if e != nil {
			return false
		}
		if !seen[canonical] {
			seen[canonical] = true
			result = append(result, resolved)
		}
		return true
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {

		if skipEditorialNode(n) {
			return
		}
		if n.Type == html.ElementNode && (n.Data == "img" || n.Data == "source" && n.Parent != nil && n.Parent.Data == "picture") {
			// Prefer high-resolution variants; lazy picture sources may be the only working URL.
			for _, a := range n.Attr {
				if (a.Key == "data-srcset" || a.Key == "srcset") && nodeAttr(n, "type") != "image/avif" {
					variants := imageSrcsetURLs(a.Val)
					for i := len(variants) - 1; i >= 0; i-- {
						add(variants[i])
					}
				}
			}
		}
		if n.Type == html.ElementNode && n.Data == "img" {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
			}
			added := false
			for _, key := range []string{"data-src", "data-original", "data-lazy-src", "data-url", "src"} {
				if attrs[key] != "" && add(attrs[key]) {
					added = true
					break
				}
			}
			if !added && attrs["data-id"] != "" && strings.Contains(base.Hostname(), "dw.com") {
				if add(fmt.Sprintf("https://static.dw.com/image/%s_605.jpg", attrs["data-id"])) {
					added = true
				}
			}
			if !added {
				for _, key := range []string{"data-srcset", "srcset"} {
					for _, variant := range imageSrcsetURLs(attrs[key]) {
						if add(variant) {
							added = true
							break
						}
					}
					if added {
						break
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	if body != nil {
		walk(body)
	}
	if preview, e := previewImage(bytes.NewReader(data), pageURL); e == nil {
		add(preview)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("không tìm thấy ảnh trong nội dung hoặc ảnh đại diện")
	}
	return result, nil
}

func loadArticlePhotos(ctx context.Context, article Article) ([][]byte, error) {
	client := NewFeedClient()
	defer client.CloseIdleConnections()
	return fetchArticlePhotos(ctx, client, article)
}

func fetchArticlePhotos(ctx context.Context, client *http.Client, article Article) ([][]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	fetch := func(raw string, limit int64) ([]byte, string, error) {
		if _, err := CanonicalURL(raw); err != nil {
			return nil, "", err
		}
		requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		// Preserve signed CDN query ordering; canonicalization is only used for validation.
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, "", err
		}
		if raw == article.URL {
			req.Header.Set("User-Agent", "Newsbot/0.1 article reader")
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil {
			return nil, "", err
		}
		if int64(len(data)) > limit {
			return nil, "", fmt.Errorf("dữ liệu quá lớn")
		}
		return data, resp.Request.URL.String(), nil
	}
	page, finalURL, err := fetch(article.URL, maxArticleBytes)
	if err != nil {
		return nil, fmt.Errorf("tải trang bài viết: %w", err)
	}
	urls, err := articleImages(bytes.NewReader(page), finalURL)
	if err != nil {
		return nil, err
	}
	previewURL, _ := previewImage(bytes.NewReader(page), finalURL)
	var photos [][]byte
	var lastErr error
	loaded := map[string]bool{}
	for i, raw := range urls {
		if i >= 12 || len(photos) >= 6 || ctx.Err() != nil {
			break
		}
		if len(photos) > 0 && raw == previewURL {
			continue
		}
		u, _ := url.Parse(raw)
		key := articleImageKey(u)
		if loaded[key] {
			continue
		}
		data, _, err := fetch(raw, 10<<20)
		if err == nil {
			data, err = normalizePhoto(data)
		}
		if err != nil {
			lastErr = err
			// Do not log signed URLs or query strings.
			slog.Warn("article image candidate failed", "article_id", article.ID, "candidate", i+1, "reason", photoErrorReason(err))
			continue
		}
		photos = append(photos, data)
		loaded[key] = true
	}
	if len(photos) == 0 {
		return nil, fmt.Errorf("không tải được ảnh hợp lệ từ %d địa chỉ: %s", len(urls), photoErrorReason(lastErr))
	}
	slog.Info("article images loaded", "article_id", article.ID, "images", len(photos))
	return photos, nil
}

func photoErrorReason(err error) string {
	if err == nil {
		return "hết thời gian tải ảnh"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err.Error()
	}
	return err.Error()
}

func normalizePhoto(data []byte) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("định dạng ảnh: %w", err)
	}
	if cfg.Width < 320 || cfg.Height < 180 || int64(cfg.Width)*int64(cfg.Height) > 24000000 {
		return nil, fmt.Errorf("kích thước ảnh không phù hợp: %dx%d", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("giải mã ảnh: %w", err)
	}
	var out bytes.Buffer
	if err = jpeg.Encode(&out, img, &jpeg.Options{Quality: 92}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func sceneArticle(scene Scene, script Script, articles []Article) (Article, bool) {
	for _, id := range scene.ClaimIDs {
		for _, claim := range script.Claims {
			if claim.ID != id {
				continue
			}
			for _, evidence := range claim.Evidence {
				for _, article := range articles {
					if article.ID == evidence.ArticleID {
						return article, true
					}
				}
			}
		}
	}
	return Article{}, false
}

// Prefer an explicitly identified article body over surrounding article cards.
func articleMediaBody(root *html.Node) *html.Node {
	var body *html.Node
	priority := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		score := 0
		if n.Type == html.ElementNode {
			if hasClass(n, "detail-content") && nodeAttr(n, "itemprop") == "articleBody" {
				score = 4
			}
			if n.Data == "article" {
				score = 1
			}
			if hasClass(n, "FITT_Article_main__body") || hasClass(n, "Article__Content") || hasClass(n, "wysiwyg--all-content") || nodeAttr(n, "data-testid") == "article-body" || nodeAttr(n, "data-testid") == "ArticleBody" {
				score = 3
			}
			if hasClass(n, "fck_detail") || hasClass(n, "article-body") || hasClass(n, "entry-content") {
				score = 2
			}
			for _, a := range n.Attr {
				if a.Key == "id" {
					if a.Val == "articleContent" {
						score = 3
					}
					if a.Val == "desktop-in-article" {
						score = 4
					}
				}
			}
		}
		if nodeAttr(n, "id") == "liveblog" {
			score = 5
		}
		if nodeAttr(n, "data-testid") == "prism-article-body" {
			score = 4
		}
		if skipEditorialNode(n) {
			return
		}
		if score > priority {
			body = n
			priority = score
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return body
}

var (
	dantriImagePath = regexp.MustCompile(`/20[0-9]{2}/[0-9]{2}/[0-9]{2}/.+$`)
	dwImagePath     = regexp.MustCompile(`^/image/([0-9]+)`)
	bbcImagePath    = regexp.MustCompile(`^/(?:news|ace)/(?:[a-z0-9_-]+/)?(?:[0-9]+|live)/(.*)$`)
)

func articleImageKey(u *url.URL) string {
	if u.Hostname() == "cdnphoto.dantri.com.vn" {
		if path := dantriImagePath.FindString(u.Path); path != "" {
			return u.Hostname() + path
		}
	}
	if u.Hostname() == "static.dw.com" {
		if m := dwImagePath.FindStringSubmatch(u.Path); len(m) > 1 {
			return u.Hostname() + "/image/" + m[1]
		}
	}
	if u.Hostname() == "ichef.bbci.co.uk" {
		if m := bbcImagePath.FindStringSubmatch(u.Path); len(m) > 1 {
			return u.Hostname() + "/" + strings.TrimSuffix(m[1], ".webp")
		}
	}
	return u.Host + u.Path
}

// CDN URLs may contain commas (CNN transforms); only descriptor boundaries separate variants.
func imageSrcsetURLs(raw string) []string {
	fields := strings.Fields(raw)
	var out []string
	for i := 0; i+1 < len(fields); i++ {
		descriptor := strings.TrimSuffix(fields[i+1], ",")
		if regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?[wx]$`).MatchString(descriptor) {
			out = append(out, strings.TrimSuffix(fields[i], ","))
			i++
		}
	}
	if len(out) == 0 && strings.TrimSpace(raw) != "" {
		out = append(out, strings.TrimSpace(raw))
	}
	return out
}
