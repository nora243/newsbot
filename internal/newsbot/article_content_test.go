package newsbot

import (
	"os"
	"strings"
	"testing"
)

func TestExtractVnExpress(t *testing.T) {
	text := strings.Repeat("Dữ kiện thử nghiệm được nguồn công bố. ", 12)
	page := `<p class="Normal">Outside</p><article class="fck_detail"><script>Bad</script><figure><p class="Normal">Caption</p></figure><p class="Normal">` + text + `</p></article><p class="Normal">Related</p>`
	got, err := extractVnExpress(strings.NewReader(page))
	if err != nil || got != strings.TrimSpace(text) {
		t.Fatalf("wrong extraction: %q %v", got, err)
	}
	for _, page := range []string{`<p>Missing body</p>`, `<article class="fck_detail"><p class="Normal">Too short</p></article>`} {
		if _, err := extractVnExpress(strings.NewReader(page)); err == nil {
			t.Fatal("accepted missing/short body")
		}
	}
	for _, raw := range []string{"https://vnexpress.net/a.html", "https://www.vnexpress.net/a.html"} {
		if !vnexpressURL(raw) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"http://vnexpress.net/a.html", "https://vnexpress.net.evil.test/a.html", "https://vnexpress.net:8443/a.html", "https://user@vnexpress.net/a.html", "https://127.0.0.1/a.html"} {
		if vnexpressURL(raw) {
			t.Fatal(raw)
		}
	}
}

func TestVnExpressDownloadedPage(t *testing.T) {
	path := os.Getenv("NEWSBOT_ARTICLE_TEST_FILE")
	if path == "" {
		t.Skip("optional downloaded public page")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	content, err := extractVnExpress(f)
	if err != nil || len(strings.Fields(content)) < 70 {
		t.Fatalf("real page failed: %v", err)
	}
	t.Logf("extracted %d words", len(strings.Fields(content)))
}
