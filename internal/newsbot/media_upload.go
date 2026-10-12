package newsbot

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxMediaUpload = 100 << 20

func (a *API) uploadMedia(w http.ResponseWriter, r *http.Request) {
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(3 * time.Minute))
	_ = controller.SetWriteDeadline(time.Now().Add(4 * time.Minute))
	r.Body = http.MaxBytesReader(w, r.Body, maxMediaUpload+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		failure(w, 400, "Không đọc được tệp tải lên (tối đa 100 MB)")
		return
	}
	defer r.MultipartForm.RemoveAll()
	articleID, err := strconv.ParseInt(r.FormValue("article_id"), 10, 64)
	if err != nil {
		failure(w, 400, "Bài viết không hợp lệ")
		return
	}
	articles, err := a.Store.Articles(r.Context(), []int64{articleID})
	if err != nil {
		a.internal(w, err)
		return
	}
	if len(articles) != 1 {
		failure(w, 400, "Bài viết không tồn tại")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		failure(w, 400, "Hãy chọn ảnh hoặc video")
		return
	}
	defer file.Close()
	if header.Size > maxMediaUpload {
		failure(w, 413, "Tệp quá lớn (tối đa 100 MB)")
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	kind := "image"
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
	case ".mp4", ".mov", ".webm":
		kind = "video"
	default:
		failure(w, 400, "Chỉ hỗ trợ JPG, PNG, WebP, MP4, MOV và WebM")
		return
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		a.internal(w, err)
		return
	}
	id := hex.EncodeToString(token[:])
	dir, err := filepath.Abs(filepath.Join(a.Config.OutputDir, "media", id))
	if err != nil {
		a.internal(w, err)
		return
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		a.internal(w, err)
		return
	}
	saved := false
	defer func() {
		if !saved {
			os.RemoveAll(dir)
		}
	}()
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	if kind == "image" {
		data, e := io.ReadAll(io.LimitReader(file, (10<<20)+1))
		if e != nil || len(data) > 10<<20 {
			failure(w, 400, "Ảnh tối đa 10 MB")
			return
		}
		data, e = normalizePhoto(data)
		if e != nil {
			failure(w, 400, "Ảnh không hợp lệ: "+e.Error())
			return
		}
		err = os.WriteFile(filepath.Join(dir, "asset.jpg"), data, 0600)
	} else {
		input := filepath.Join(dir, "input"+ext)
		output, e := os.Create(input)
		if e != nil {
			a.internal(w, e)
			return
		}
		_, e = io.Copy(output, io.LimitReader(file, maxMediaUpload+1))
		closeErr := output.Close()
		if e != nil || closeErr != nil {
			a.internal(w, fmt.Errorf("không lưu được video"))
			return
		}
		format := "mov"
		if ext == ".webm" {
			format = "matroska"
		}
		_, err = command(ctx, dir, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-protocol_whitelist", "file", "-f", format, "-i", input, "-t", "90", "-map", "0:v:0", "-an", "-vf", "scale=1280:720:force_original_aspect_ratio=decrease:force_divisible_by=2,setsar=1", "-c:v", "libx264", "-threads", "2", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "asset.mp4")
		os.Remove(input)
		if err == nil {
			_, err = probe(ctx, dir, "asset.mp4")
		}
		if err != nil {
			failure(w, 400, "Video không hợp lệ hoặc không giải mã được")
			return
		}
	}
	if err != nil {
		a.internal(w, err)
		return
	}
	name := filepath.Base(header.Filename)
	if len([]rune(name)) > 200 {
		name = string([]rune(name)[:200])
	}
	_, err = a.Store.DB.Exec(ctx, "INSERT INTO media_assets(id,article_id,kind,candidates,uploaded,upload_name) VALUES($1,$2,$3,'[]',true,$4)", id, articleID, kind, name)
	if err != nil {
		a.internal(w, err)
		return
	}
	saved = true
	respond(w, 201, MediaAsset{ID: id, ArticleID: articleID, Kind: kind, SourceName: "Tải lên", ArticleTitle: name, Uploaded: true})
}
