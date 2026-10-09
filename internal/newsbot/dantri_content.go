package newsbot

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

func dantriURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && (u.Hostname() == "dantri.com.vn" || u.Hostname() == "www.dantri.com.vn") && strings.Count(u.Path, "/") >= 2 && strings.HasSuffix(u.Path, ".htm")
}
func nodeAttribute(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func extractDanTri(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxArticleBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxArticleBytes {
		return "", fmt.Errorf("trang bài Dân trí quá lớn")
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
		if n.Type == html.ElementNode && (nodeAttribute(n, "id") == "desktop-in-article" || hasClass(n, "singular-content")) {
			body = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)
	if body == nil {
		return "", fmt.Errorf("không tìm thấy nội dung chính bài Dân trí")
	}
	var paragraphs []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "figure", "aside", "nav", "iframe", "form":
				return
			}
			if strings.Contains(nodeAttribute(n, "data-slot"), "ads") || nodeAttribute(n, "data-module") != "" || hasClass(n, "related-news") || hasClass(n, "related-box") || hasClass(n, "advertisement") {
				return
			}
			if n.Data == "p" || n.Data == "h2" || n.Data == "h3" || n.Data == "li" {
				var b strings.Builder
				_ = html.Render(&b, n)
				if text := plainText(b.String()); text != "" {
					paragraphs = append(paragraphs, text)
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(body)
	text := strings.Join(paragraphs, "\n")
	if len(strings.Fields(text)) < 70 {
		return "", fmt.Errorf("nội dung bài Dân trí vẫn quá ngắn")
	}
	if len([]rune(text)) > 12000 {
		text = string([]rune(text)[:12000])
	}
	return text, nil
}
