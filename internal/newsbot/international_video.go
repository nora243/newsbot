package newsbot

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Match JSON-LD videos to the requested page, not recommendation video objects.
func matchingMetadataVideos(root *html.Node, page string) []string {
	if internationalPublisher(page) == "" {
		return nil
	}
	var out []string
	samePage := func(raw string) bool {
		a, e := url.Parse(page)
		b, f := url.Parse(raw)
		return e == nil && f == nil && strings.TrimPrefix(a.Hostname(), "www.") == strings.TrimPrefix(b.Hostname(), "www.") && a.Path == b.Path
	}
	var visit func(any)
	visit = func(v any) {
		switch o := v.(type) {
		case []any:
			for _, item := range o {
				visit(item)
			}
		case map[string]any:
			if o["@type"] == "VideoObject" {
				link, _ := o["url"].(string)
				switch main := o["mainEntityOfPage"].(type) {
				case string:
					link = main
				case map[string]any:
					link, _ = main["url"].(string)
					if link == "" {
						link, _ = main["@id"].(string)
					}
				}
				if samePage(link) {
					raw, _ := o["contentUrl"].(string)
					u, e := url.Parse(raw)
					if e == nil && (strings.HasSuffix(strings.ToLower(u.Path), ".mp4") || strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")) {
						if _, e = CanonicalURL(raw); e == nil {
							out = append(out, raw)
						}
					}
				}
			}
			for _, item := range o {
				visit(item)
			}
		}
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "script" && nodeAttr(n, "type") == "application/ld+json" && n.FirstChild != nil {
			var v any
			if json.Unmarshal([]byte(n.FirstChild.Data), &v) == nil {
				visit(v)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

func embeddedVideoWithoutFile(data []byte, page string) bool {
	if len(articleVideoURLs(data, page)) > 0 {
		return false
	}
	// These markers identify publisher players without pretending their embed URLs are media files.
	return bytes.Contains(data, []byte(`"VideoObject"`)) || bytes.Contains(data, []byte("FeaturedVideo")) || bytes.Contains(data, []byte("youtube.com/embed/")) || bytes.Contains(data, []byte("player.vimeo.com/video/"))
}
