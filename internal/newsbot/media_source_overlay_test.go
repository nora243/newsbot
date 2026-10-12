package newsbot

import (
	"strings"
	"testing"
)

func TestMediaSourceOverlay(t *testing.T) {
	if mediaSourceOverlay(0, 10, "") != "" {
		t.Fatal("empty media source displayed")
	}
	line := mediaSourceOverlay(35, 70, "Nguồn thử nghiệm")
	if !strings.Contains(line, "0:00:35.00,0:01:10.00") || !strings.Contains(line, "Nguồn: Nguồn thử nghiệm") {
		t.Fatal("source label lost timing or source name", line)
	}
	escaped := mediaSourceOverlay(0, 1, `BBC {\pos(0,0)}`)
	if strings.Contains(escaped, `BBC {`) {
		t.Fatal("source name allowed ASS injection")
	}
	if (Renderer{}).ShowMediaSource {
		t.Fatal("source overlay should default to off")
	}
}
