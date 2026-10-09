package newsbot

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Article HTML can include large hydration bundles (CNN); RSS keeps its smaller limit.
const maxArticleBytes = 12 << 20

func internationalPublisher(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return ""
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	switch host {
	case "bbc.co.uk", "bbc.com":
		if strings.HasPrefix(u.Path, "/news/") {
			return "bbc"
		}
	case "theguardian.com":
		if strings.Count(u.Path, "/") >= 4 {
			return "guardian"
		}
	case "cnn.com":
		if strings.HasSuffix(u.Path, ".html") || strings.HasPrefix(u.Path, "/videos/") {
			return "cnn"
		}
	case "abcnews.com", "abcnews.go.com":
		if strings.Contains(u.Path, "/story") || strings.Contains(u.Path, "/wireStory/") || strings.Contains(u.Path, "/video/") {
			return "abc"
		}
	case "aljazeera.com":
		if strings.Count(u.Path, "/") >= 4 {
			return "aljazeera"
		}
	case "reuters.com":
		if strings.Count(u.Path, "/") >= 3 {
			return "reuters"
		}
	case "nytimes.com":
		if strings.HasSuffix(u.Path, ".html") {
			return "nyt"
		}
	}
	return ""
}

func nodeAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func skipEditorialNode(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch n.Data {
	case "script", "style", "aside", "nav", "header":
		return true
	}
	for _, c := range []string{"related", "box-tinlienquan", "related-content", "article__related-content", "advertisement", "submeta", "Article__Footer", "more-on", "related-articles"} {
		if hasClass(n, c) {
			return true
		}
	}
	if nodeAttr(n, "type") == "RelatedOneNews" || nodeAttr(n, "type") == "RelatedNewsBox" {
		return true
	}
	switch nodeAttr(n, "data-block") {
	case "topicList", "links", "relatedLinks", "byline", "metadata":
		return true
	}
	return nodeAttr(n, "data-component") == "links-block" || nodeAttr(n, "data-testid") == "prism-collection" || nodeAttr(n, "data-testid") == "prism-ad"
}

func extractInternational(r io.Reader, page string) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxArticleBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxArticleBytes {
		return "", fmt.Errorf("trang bài viết quá lớn")
	}
	root, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return "", err
	}
	publisher := internationalPublisher(page)
	var body *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if body != nil {
			return
		}
		if n.Type == html.ElementNode && ((publisher == "bbc" && n.Data == "article") || (publisher == "guardian" && (hasClass(n, "article-body-viewer-selector") || nodeAttr(n, "id") == "liveblog-body")) || (publisher == "cnn" && hasClass(n, "article__content")) || (publisher == "nyt" && nodeAttr(n, "name") == "articleBody") || (publisher == "abc" && (nodeAttr(n, "data-testid") == "prism-article-body" || hasClass(n, "Article__Content"))) || (publisher == "aljazeera" && hasClass(n, "wysiwyg--all-content")) || (publisher == "reuters" && (nodeAttr(n, "data-testid") == "article-body" || nodeAttr(n, "data-testid") == "ArticleBody"))) {
			body = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(root)
	if body == nil {
		return "", fmt.Errorf("không tìm thấy nội dung công khai của bài %s", publisher)
	}
	var paragraphs []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if skipEditorialNode(n) || n.Type == html.ElementNode && (n.Data == "figure" || n.Data == "figcaption" || hasClass(n, "article__caption")) {
			return
		}
		if n.Type == html.ElementNode && n.Data == "p" {
			var b strings.Builder
			_ = html.Render(&b, n)
			if t := plainText(b.String()); t != "" {
				paragraphs = append(paragraphs, t)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(body)
	text := strings.Join(paragraphs, "\n")
	if len(strings.Fields(text)) < 70 && publisher == "aljazeera" {
		u, _ := url.Parse(page)
		if strings.HasPrefix(u.Path, "/video/") {
			var summary string
			var findSummary func(*html.Node)
			findSummary = func(n *html.Node) {
				if summary != "" || skipEditorialNode(n) {
					return
				}
				if n.Type == html.ElementNode && n.Data == "p" && hasClass(n, "article__subhead") {
					var b strings.Builder
					_ = html.Render(&b, n)
					summary = plainText(b.String())
					return
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					findSummary(c)
				}
			}
			findSummary(root)
			if len(strings.Fields(summary)) >= 12 {
				return clipText(summary, 12000), nil
			}
		}
	}
	if len(strings.Fields(text)) < 70 {
		return "", fmt.Errorf("nội dung công khai quá ngắn; nguồn có thể yêu cầu đăng nhập hoặc chỉ có video")
	}
	if len([]rune(text)) > 12000 {
		text = string([]rune(text)[:12000])
	}
	return text, nil
}

func googleNewsArticle(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Hostname() == "news.google.com" && (strings.HasPrefix(u.Path, "/rss/articles/") || strings.HasPrefix(u.Path, "/articles/"))
}
