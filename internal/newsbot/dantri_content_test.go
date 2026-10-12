package newsbot

import (
	"os"
	"strings"
	"testing"
)

func TestExtractDanTri(t *testing.T) {
	text := strings.Repeat("Nội dung dữ kiện thử nghiệm trong bài báo. ", 12)
	for _, container := range []string{`id="desktop-in-article" data-slot="content"`, `class="singular-content"`, `class="e-magazine__body dnews__body"`} {
		page := `<p>Outside</p><div ` + container + `><h2>Heading</h2><p>` + text + `</p><figure><p>Caption</p></figure><div data-slot="ads-in-content"><p>Advertising</p></div><div class="related-news"><p>Related</p></div><script>Bad</script></div><p>Footer</p>`
		got, err := extractDanTri(strings.NewReader(page))
		if err != nil || got != "Heading\n"+strings.TrimSpace(text) {
			t.Fatalf("wrong extraction: %q %v", got, err)
		}
	}
	for _, page := range []string{`<p>missing body</p>`, `<div id="desktop-in-article"><p>Short</p></div>`} {
		if _, err := extractDanTri(strings.NewReader(page)); err == nil {
			t.Fatal("accepted missing/short article")
		}
	}
	for _, raw := range []string{"https://dantri.com.vn/suc-khoe/example-123.htm", "https://www.dantri.com.vn/suc-khoe/example-123.htm"} {
		if !dantriURL(raw) || !isFullContentSupported(raw) {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"http://dantri.com.vn/suc-khoe/a.htm", "https://dantri.com.vn.evil.test/suc-khoe/a.htm", "https://dantri.com.vn:8443/suc-khoe/a.htm", "https://user@dantri.com.vn/suc-khoe/a.htm", "https://dantri.com.vn/suc-khoe.htm"} {
		if dantriURL(raw) {
			t.Fatal(raw)
		}
	}
}

func TestDanTriDownloadedPage(t *testing.T) {
	path := os.Getenv("NEWSBOT_DANTRI_TEST_FILE")
	if path == "" {
		t.Skip("optional downloaded public article")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	content, err := extractDanTri(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{"354.957", "UK Biobank", "13 năm", "5 tách"} {
		if !strings.Contains(content, fact) {
			t.Fatalf("missing article detail: %s", fact)
		}
	}
	t.Logf("extracted %d words", len(strings.Fields(content)))
}

func TestDanTriDNewsDownloadedPage(t *testing.T) {
	path := os.Getenv("NEWSBOT_DANTRI_DNEWS_TEST_FILE")
	if path == "" {
		t.Skip("optional downloaded DNews article")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	content, err := extractDanTri(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{"10 phút", "Phụ sản Hà Nội", "khai sinh"} {
		if !strings.Contains(content, fact) {
			t.Fatalf("missing article detail: %s", fact)
		}
	}
	t.Logf("extracted %d words", len(strings.Fields(content)))
}
