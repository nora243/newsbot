package newsbot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const maxFeedBytes = 4 << 20

func CanonicalURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return "", fmt.Errorf("URL không hợp lệ")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		if strings.HasPrefix(strings.ToLower(k), "utm_") || k == "fbclid" || k == "gclid" {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

func plainText(raw string) string {
	z := html.NewTokenizer(strings.NewReader(raw))
	var b strings.Builder
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.Join(strings.Fields(b.String()), " ")
		case html.StartTagToken:
			t, _ := z.TagName()
			if string(t) == "script" || string(t) == "style" {
				skip++
			}
			b.WriteByte(' ')
		case html.EndTagToken:
			t, _ := z.TagName()
			if (string(t) == "script" || string(t) == "style") && skip > 0 {
				skip--
			}
			b.WriteByte(' ')
		case html.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		}
	}
}

type feedDocument struct {
	XMLName xml.Name
	Channel struct {
		Items []struct {
			Title       string         `xml:"title"`
			Link        rssArticleLink `xml:"link"`
			Description string         `xml:"description"`
			Encoded     string         `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
			Date        string         `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
	Entries []struct {
		Title string `xml:"title"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
		Summary   string `xml:"summary"`
		Content   string `xml:"content"`
		Published string `xml:"published"`
		Updated   string `xml:"updated"`
	} `xml:"entry"`
}

func ParseFeed(r io.Reader, source Source) ([]Article, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFeedBytes {
		return nil, fmt.Errorf("RSS quá lớn")
	}
	var feed feedDocument
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, err
	}
	if feed.XMLName.Local != "rss" && feed.XMLName.Local != "feed" {
		return nil, fmt.Errorf("định dạng RSS/Atom không hợp lệ")
	}
	var out []Article
	add := func(title, link, content, date string) {
		canonical, err := CanonicalURL(link)
		if err != nil {
			return
		}
		title, content = plainText(title), plainText(content)
		if title == "" {
			return
		}
		if len([]rune(title)) > 500 {
			title = string([]rune(title)[:500])
		}
		if len([]rune(content)) > 12000 {
			content = string([]rune(content)[:12000])
		}
		h := sha256.Sum256([]byte(normalized(title + " " + content)))
		a := Article{SourceID: source.ID, SourceName: source.Name, URL: canonical, Title: title, Content: content, Hash: hex.EncodeToString(h[:])}
		trimmedDate := strings.TrimSpace(date)
		for _, layout := range []string{
			time.RFC3339,
			time.RFC1123Z,
			time.RFC1123,
			"Mon, 2 Jan 2006 15:04:05 MST",
			"Mon, 2 Jan 2006 15:04:05 -0700",
			"Mon, _2 Jan 2006 15:04:05 MST",
			"Mon, _2 Jan 2006 15:04:05 -0700",
			time.RFC822Z,
			time.RFC822,
			"2006-01-02 15:04:05",
		} {
			if t, err := time.Parse(layout, trimmedDate); err == nil {
				a.PublishedAt = &t
				break
			}
		}
		out = append(out, a)
	}
	for _, i := range feed.Channel.Items {
		content := i.Encoded
		if strings.TrimSpace(plainText(content)) == "" {
			content = i.Description
		}
		add(i.Title, string(i.Link), content, i.Date)
	}
	for _, e := range feed.Entries {
		link := ""
		for _, l := range e.Links {
			if l.Rel == "" || l.Rel == "alternate" {
				link = l.Href
				break
			}
		}
		content := e.Content
		if strings.TrimSpace(plainText(content)) == "" {
			content = e.Summary
		}
		date := e.Published
		if date == "" {
			date = e.Updated
		}
		add(e.Title, link, content, date)
	}
	return out, nil
}

func publicIP(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	// Shared address space, benchmarking and documentation ranges are not news hosts.
	for _, p := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32"} {
		if netip.MustParsePrefix(p).Contains(a) {
			return false
		}
	}
	return true
}

func NewFeedClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if port != "80" && port != "443" {
				return nil, fmt.Errorf("RSS chỉ cho phép cổng 80/443")
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !publicIP(ip) {
					return nil, fmt.Errorf("RSS không được truy cập mạng nội bộ")
				}
			}
			for _, ip := range ips {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
			}
			return nil, fmt.Errorf("không kết nối được nguồn RSS")
		},
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("quá nhiều redirect")
		}
		_, err := CanonicalURL(req.URL.String())
		return err
	}}
}

func FetchFeed(ctx context.Context, client *http.Client, source Source) ([]Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.RSSURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Newsbot/0.1 RSS reader")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("RSS HTTP %d", resp.StatusCode)
	}
	return ParseFeed(resp.Body, source)
}

// An Atom link may accompany the RSS link. It must not overwrite the article URL.
type rssArticleLink string

func (l *rssArticleLink) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var value string
	if err := d.DecodeElement(&value, &start); err != nil {
		return err
	}
	if start.Name.Space != "http://www.w3.org/2005/Atom" && strings.TrimSpace(value) != "" {
		*l = rssArticleLink(value)
	}
	return nil
}
