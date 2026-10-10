package newsbot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTitleLayout(t *testing.T) {
	title := "Những sai lầm phổ biến khi đo huyết áp tại nhà và nguy cơ sai lệch kết quả"
	size, text := titleLayout(title)
	if size < 64 {
		t.Fatalf("headline remains too small: %d", size)
	}
	if strings.ReplaceAll(text, `\N`, " ") != title {
		t.Fatalf("headline changed: %q", text)
	}
	if strings.Count(text, `\N`) < 1 || (strings.Count(text, `\N`)+1)*size > 220 {
		t.Fatalf("headline does not fit: %d %q", size, text)
	}
	long := strings.TrimSpace(strings.Repeat("Thông tin mới về sự kiện ", 6))
	size, text = titleLayout(long)
	if (strings.Count(text, `\N`)+1)*size > 220 {
		t.Fatal("long headline overlaps image")
	}
}

func TestTitleFrameIntegration(t *testing.T) {
	preview := os.Getenv("NEWSBOT_TITLE_PREVIEW")
	if preview == "" {
		t.Skip("set NEWSBOT_TITLE_PREVIEW with FFmpeg installed")
	}
	dir := t.TempDir()
	title := "Những sai lầm phổ biến khi đo huyết áp tại nhà và nguy cơ sai lệch kết quả"
	ass := assHeader + "Dialogue: 3,0:00:00.00,0:00:02.00,Brand,,0,0,0,,{\\pos(88,164)}So Híp{\\fs32} /\n" + fmt.Sprintf("Dialogue: 2,0:00:00.00,0:00:02.00,Title,,56,120,0,,%s\n", titleOverlay(title))
	if err := os.WriteFile(filepath.Join(dir, "subtitles.ass"), []byte(ass), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := command(context.Background(), dir, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c=0x101515:s=1080x1920", "-vf", videoOverlay(), "-frames:v", "1", "-threads", "1", preview); err != nil {
		t.Fatal(err)
	}
	size, text := titleLayout(title)
	t.Logf("title font size=%d, lines=%s", size, text)
}
