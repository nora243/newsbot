package newsbot

import (
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/image/draw"
)

// Limit preview work independently of rendering; many cards can become visible together.
var mediaPreviewSlots = make(chan struct{}, 2)

func makeMediaPreview(ctx context.Context, asset ResolvedMedia) (string, error) {
	path := filepath.Join(filepath.Dir(asset.Path), "preview.jpg")
	if _, e := os.Stat(path); e == nil {
		return path, nil
	}
	f, e := os.CreateTemp(filepath.Dir(asset.Path), "preview-*.jpg")
	if e != nil {
		return "", e
	}
	temp := f.Name()
	defer os.Remove(temp)
	if asset.Kind == "video" {
		if e = f.Close(); e != nil {
			return "", e
		}
		_, e = command(ctx, filepath.Dir(asset.Path), "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-threads", "1", "-i", asset.Path, "-frames:v", "1", "-vf", "scale=640:360:force_original_aspect_ratio=decrease", "-q:v", "4", temp)
	} else {
		var source *os.File
		source, e = os.Open(asset.Path)
		if e == nil {
			var img image.Image
			img, _, e = image.Decode(source)
			source.Close()
			if e == nil {
				b := img.Bounds()
				w, h := b.Dx(), b.Dy()
				if w <= 0 || h <= 0 {
					e = fmt.Errorf("ảnh không hợp lệ")
				} else {
					ratio := min(1.0, min(640.0/float64(w), 360.0/float64(h)))
					dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(w)*ratio)), max(1, int(float64(h)*ratio))))
					draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
					e = jpeg.Encode(f, dst, &jpeg.Options{Quality: 80})
				}
			}
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
	}
	if e != nil {
		return "", e
	}
	if e = os.Rename(temp, path); e != nil {
		return "", e
	}
	return path, nil
}

func (a *API) mediaPreview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	select {
	case mediaPreviewSlots <- struct{}{}:
		defer func() { <-mediaPreviewSlots }()
	case <-ctx.Done():
		return
	}
	m, e := a.Store.Asset(ctx, r.PathValue("id"))
	if e != nil {
		failure(w, 404, "không có media")
		return
	}
	asset, e := materializeMedia(ctx, a.Config.OutputDir, m)
	if e == nil {
		asset.Path, e = makeMediaPreview(ctx, asset)
	}
	if e != nil {
		failure(w, 502, "Không tải được ảnh xem trước")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, asset.Path)
}
