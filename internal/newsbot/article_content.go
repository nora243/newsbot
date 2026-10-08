package newsbot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

func vnexpressURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && (u.Hostname() == "vnexpress.net" || u.Hostname() == "www.vnexpress.net") && strings.HasSuffix(u.Path, ".html")
}

func hasClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, v := range strings.Fields(a.Val) {
				if v == class {
					return true
				}
			}
		}
	}
	return false
}

func extractVnExpress(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxFeedBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxFeedBytes {
		return "", fmt.Errorf("trang bài viết quá lớn")
	}
	root, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return "", err
	}
	var body *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if body != nil {
			return
		}
		if n.Type == html.ElementNode && hasClass(n, "fck_detail") {
			body = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)
	if body == nil {
		return "", fmt.Errorf("không tìm thấy nội dung bài VnExpress")
	}
	var paragraphs []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "figure" || n.Data == "aside") {
			return
		}
		if n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "Normal") {
			var b strings.Builder
			_ = html.Render(&b, n)
			if text := plainText(b.String()); text != "" {
				paragraphs = append(paragraphs, text)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(body)
	text := strings.Join(paragraphs, "\n")
	if len(strings.Fields(text)) < 70 {
		return "", fmt.Errorf("nội dung bài gốc vẫn quá ngắn")
	}
	if len([]rune(text)) > 12000 {
		text = string([]rune(text)[:12000])
	}
	return text, nil
}

// Fetch only selected VnExpress articles, using the same public-IP restrictions as RSS.
func fetchArticleContent(ctx context.Context, raw string) (string, error) {
	if !vnexpressURL(raw) {
		return "", fmt.Errorf("chưa hỗ trợ lấy nội dung đầy đủ từ nguồn này")
	}
	client := NewFeedClient()
	defer client.CloseIdleConnections()
	redirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !vnexpressURL(req.URL.String()) {
			return fmt.Errorf("bài viết chuyển hướng ngoài VnExpress")
		}
		return redirect(req, via)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Newsbot/0.1 article reader")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bài gốc HTTP %d", resp.StatusCode)
	}
	return extractVnExpress(resp.Body)
}
