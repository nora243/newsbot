package newsbot

import (
	"errors"
	"strings"
	"testing"
)

func TestScriptRefusalAndMalformedOutput(t *testing.T) {
	var refusal *InsufficientSourceError
	if err := (Script{Claims: []Claim{}, Scenes: []Scene{}}).Validate(nil); !errors.As(err, &refusal) || !refusal.Permanent() {
		t.Fatalf("expected permanent editorial refusal: %v", err)
	}
	if err := (Script{}).Validate(nil); err == nil || errors.As(err, &refusal) {
		t.Fatalf("missing fields must be malformed, not a refusal: %v", err)
	}
	if err := (Script{Title: "Test", Claims: []Claim{{ID: "c1"}}, Scenes: []Scene{{}}}).Validate(nil); err == nil || !strings.Contains(err.Error(), "1 dữ kiện, 1 cảnh") {
		t.Fatalf("missing diagnostic counts: %v", err)
	}
}

func TestFeedPrefersFullContent(t *testing.T) {
	feeds := []string{
		`<rss xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><item><title>Test</title><link>https://example.com/a</link><description>Short summary</description><content:encoded><![CDATA[<p>Full article content</p>]]></content:encoded></item></channel></rss>`,
		`<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>Test</title><link href="https://example.com/a"/><summary>Short summary</summary><content>Full article content</content></entry></feed>`,
	}
	for _, feed := range feeds {
		articles, err := ParseFeed(strings.NewReader(feed), Source{})
		if err != nil || len(articles) != 1 || articles[0].Content != "Full article content" {
			t.Fatalf("full content lost: %+v, %v", articles, err)
		}
	}
	feed := `<rss xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><item><title>Test</title><link>https://example.com/a</link><description>Summary fallback</description><content:encoded><![CDATA[<p></p>]]></content:encoded></item></channel></rss>`
	articles, err := ParseFeed(strings.NewReader(feed), Source{})
	if err != nil || len(articles) != 1 || articles[0].Content != "Summary fallback" {
		t.Fatalf("fallback lost: %+v, %v", articles, err)
	}
}
