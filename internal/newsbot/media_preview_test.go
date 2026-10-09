package newsbot

import (
	"context"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestMediaImagePreviewBounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "asset.jpg")
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, 3200, 2400)), nil); e != nil {
		t.Fatal(e)
	}
	f.Close()
	original, e := os.Stat(path)
	if e != nil {
		t.Fatal(e)
	}
	preview, e := makeMediaPreview(context.Background(), ResolvedMedia{Path: path, Kind: "image"})
	if e != nil {
		t.Fatal(e)
	}
	f, e = os.Open(preview)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	cfg, e := jpeg.DecodeConfig(f)
	if e != nil || cfg.Width > 640 || cfg.Height > 360 || cfg.Width*3 != cfg.Height*4 {
		t.Fatalf("preview dimensions %+v %v", cfg, e)
	}
	info, e := os.Stat(path)
	if e != nil || info.Size() != original.Size() || info.ModTime() != original.ModTime() {
		t.Fatal("render asset was modified")
	}
	before, e := os.Stat(preview)
	if e != nil {
		t.Fatal(e)
	}
	again, e := makeMediaPreview(context.Background(), ResolvedMedia{Path: path, Kind: "image"})
	if e != nil || again != preview {
		t.Fatal(again, e)
	}
	after, e := os.Stat(preview)
	if e != nil || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("cache not reused")
	}
}

func TestMediaVideoPreview(t *testing.T) {
	if os.Getenv("NEWSBOT_RENDER_TEST") != "1" {
		t.Skip("optional ffmpeg preview test")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "asset.mp4")
	if _, e := command(context.Background(), dir, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c=blue:s=720x1280:r=1", "-t", "1", "-threads", "1", path); e != nil {
		t.Fatal(e)
	}
	preview, e := makeMediaPreview(context.Background(), ResolvedMedia{Path: path, Kind: "video"})
	if e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(preview)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	cfg, e := jpeg.DecodeConfig(f)
	if e != nil || cfg.Width > 640 || cfg.Height > 360 {
		t.Fatalf("video poster %+v %v", cfg, e)
	}
}
