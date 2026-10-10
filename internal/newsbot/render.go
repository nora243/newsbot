package newsbot

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
)

func formatArticleTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	vnTime := t.In(time.FixedZone("ICT", 7*3600))
	if vnTime.Hour() == 0 && vnTime.Minute() == 0 && vnTime.Second() == 0 {
		return vnTime.Format("02/01/2006")
	}
	return vnTime.Format("15:04 02/01/2006")
}

type Speaker interface {
	Speech(context.Context, string) ([]byte, error)
}
type Renderer struct {
	SelectedMedia []ResolvedMedia
	OutputDir     string
	Speaker       Speaker
	PhotoLoader   func(context.Context, Article) ([][]byte, error)
	ClipLoader    func(context.Context, Article, string) (articleClip, error)
}

func command(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	b, err := c.CombinedOutput()
	if err != nil {
		if len(b) > 2000 {
			b = b[len(b)-2000:]
		}
		return nil, fmt.Errorf("%s: %w: %s", name, err, b)
	}
	return b, nil
}
func probe(ctx context.Context, dir, path string) (float64, error) {
	b, err := command(ctx, dir, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", path)
	if err != nil {
		return 0, err
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if err != nil || math.IsNaN(d) || math.IsInf(d, 0) || d <= 0 {
		return 0, fmt.Errorf("audio duration không hợp lệ")
	}
	return d, nil
}
func assTime(seconds float64) string {
	cs := int(math.Round(seconds * 100))
	return fmt.Sprintf("%d:%02d:%02d.%02d", cs/360000, (cs/6000)%60, (cs/100)%60, cs%100)
}
func assText(s string) string {
	s = strings.NewReplacer("\\", "／", "{", "(", "}", ")", "\n", " ", "\r", " ").Replace(s)
	words := strings.Fields(s)
	var lines []string
	line := ""
	for _, w := range words {
		if line != "" && utf8.RuneCountInString(line+" "+w) > 32 {
			lines = append(lines, line)
			line = w
		} else {
			if line != "" {
				line += " "
			}
			line += w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\\N")
}

const assHeader = `[Script Info]
ScriptType: v4.00+
PlayResX: 1080
PlayResY: 1920
WrapStyle: 2

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Subtitle,Noto Sans,54,&H00FFFFFF,&H00FFFFFF,&H00151010,&H00151010,-1,0,0,0,100,100,0,0,1,3,0,5,90,210,0,1
Style: Title,Noto Sans,58,&H00FFFFFF,&H00FFFFFF,&H00151010,&H00151010,-1,0,0,0,100,100,0,0,1,0,0,7,72,204,0,1
Style: Brand,Noto Sans,70,&H00151010,&H00FFFFFF,&H00151010,&H00151010,-1,0,0,0,100,100,-2,0,1,0,0,7,72,180,0,1
Style: Meta,Noto Sans,26,&H00B8B9B4,&H00FFFFFF,&H00151010,&H00151010,0,0,0,0,100,100,1,0,1,0,0,7,72,210,0,1
Style: Accent,Noto Sans,30,&H0063F5D8,&H00FFFFFF,&H00151010,&H00151010,-1,0,0,0,100,100,2,0,1,0,0,7,72,210,0,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`

func (r *Renderer) Render(ctx context.Context, id string, s Script, articles []Article) (string, float64, error) {
	if len(id) != 32 || strings.ContainsAny(id, "/\\.") {
		return "", 0, fmt.Errorf("video ID không hợp lệ")
	}
	base, err := filepath.Abs(r.OutputDir)
	if err != nil {
		return "", 0, err
	}
	// Each lease renders into an isolated directory; stale workers cannot overwrite the winner.
	dir, err := os.MkdirTemp(base, id+"-")
	if err != nil {
		return "", 0, err
	}
	var ass strings.Builder
	ass.WriteString(assHeader)
	var concat strings.Builder
	var visuals strings.Builder
	photos := map[int64][]string{}
	photoUses := map[int64]int{}
	clips := map[int64]articleClip{}
	clipOffsets := map[int64]float64{}
	clipLoader := r.ClipLoader
	if clipLoader == nil {
		clipLoader = loadArticleClip
	}
	loader := r.PhotoLoader
	if loader == nil {
		loader = loadArticlePhotos
	}
	total := 0.0
	for i, scene := range s.Scenes {
		audio, err := r.Speaker.Speech(ctx, scene.Narration)
		if err != nil {
			return "", 0, fmt.Errorf("TTS cảnh %02d: %w", i+1, err)
		}
		// ffprobe/FFmpeg detect the container from bytes (OpenAI MP3 or Google WAV).
		name := fmt.Sprintf("scene-%02d.audio", i)
		if err = os.WriteFile(filepath.Join(dir, name), audio, 0600); err != nil {
			return "", 0, err
		}
		d, err := probe(ctx, dir, name)
		if err != nil {
			return "", 0, err
		}
		if d > 45 {
			return "", 0, fmt.Errorf("một cảnh dài quá 45 giây")
		}
		fmt.Fprintf(&concat, "file '%s'\n", name)
		// TTS per scene gives exact scene boundaries. Chunks within each scene use estimated word timing.
		words := strings.Fields(scene.Subtitle)
		for start := 0; start < len(words); start += 7 {
			end := start + 7
			if end > len(words) {
				end = len(words)
			}
			fmt.Fprintf(&ass, "Dialogue: 1,%s,%s,Subtitle,,0,0,0,,%s\n", assTime(total+d*float64(start)/float64(len(words))), assTime(total+d*float64(end)/float64(len(words))), `{\pos(468,1360)\fad(70,70)}`+assText(strings.Join(words[start:end], " ")))
		}

		article, found := sceneArticle(scene, s, articles)
		photo := ""
		clip := articleClip{}
		if found && r.SelectedMedia == nil {
			var cached bool
			clip, cached = clips[article.ID]
			if !cached {
				var clipErr error
				clip, clipErr = clipLoader(ctx, article, dir)
				if clipErr != nil {
					slog.Warn("article video unavailable; trying images", "article_id", article.ID, "reason", photoErrorReason(clipErr))
				}
				clips[article.ID] = clip
			}
		}
		if found && clip.Name == "" && r.SelectedMedia == nil {
			cachedPhotos, cached := photos[article.ID]
			if !cached {
				images, photoErr := loader(ctx, article)
				if photoErr == nil {
					for j, data := range images {
						name := fmt.Sprintf("photo-%d-%02d.jpg", article.ID, j)
						if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
							return "", 0, err
						}
						cachedPhotos = append(cachedPhotos, name)
					}
				} else {
					slog.Warn("article photo unavailable; using news card", "article_id", article.ID, "reason", photoErrorReason(photoErr))
				}
				photos[article.ID] = cachedPhotos
			}
			if len(cachedPhotos) > 0 {
				photo = cachedPhotos[photoUses[article.ID]%len(cachedPhotos)]
				photoUses[article.ID]++
			}
		}
		sceneEnd := assTime(total + d)
		if photo == "" && clip.Name == "" && len(r.SelectedMedia) == 0 {
			text := scene.Narration
			for _, claim := range s.Claims {
				if len(scene.ClaimIDs) > 0 && claim.ID == scene.ClaimIDs[0] {
					text = claim.Text
					break
				}
			}
			text = clipText(text, 155)
			fmt.Fprintf(&ass, "Dialogue: 1,%s,%s,Accent,,0,0,0,,{\\pos(100,620)\\fs36}TIN ĐÁNG CHÚ Ý\n", assTime(total), sceneEnd)
			fmt.Fprintf(&ass, "Dialogue: 1,%s,%s,Title,,0,0,0,,{\\pos(100,710)\\fs48}%s\n", assTime(total), sceneEnd, assText(text))
		}
		if len(r.SelectedMedia) == 0 {
			visualName := fmt.Sprintf("visual-%02d.mp4", i)
			if err := renderVisual(ctx, dir, visualName, photo, clip, d, clipOffsets[article.ID]); err != nil {
				return "", 0, err
			}
			clipOffsets[article.ID] += d
			fmt.Fprintf(&visuals, "file '%s'\nduration %.6f\n", visualName, d)
		}

		total += d
	}
	if total <= 60 || total > 180 {
		return "", 0, fmt.Errorf("video dài %.1fs; yêu cầu dài hơn 60 giây và tối đa 180 giây; hãy bổ sung lời đọc có nguồn nếu video quá ngắn", total)
	}
	// Every explicitly selected asset appears once, in selection order, over the full narration.
	for i, m := range r.SelectedMedia {
		duration := total / float64(len(r.SelectedMedia))
		photo := ""
		clip := articleClip{}
		if m.Kind == "video" {
			clip = articleClip{Name: m.Path, Duration: m.Duration}
		} else {
			photo = m.Path
		}
		name := fmt.Sprintf("selected-%02d.mp4", i)
		if err := renderVisual(ctx, dir, name, photo, clip, duration, 0); err != nil {
			return "", 0, err
		}
		fmt.Fprintf(&visuals, "file '%s'\nduration %.6f\n", name, duration)
	}
	end := assTime(total)
	// ASS animates a clipped vector bar continuously without frame-dependent drawbox expressions.
	fmt.Fprintf(&ass, "Dialogue: 4,0:00:00.00,%s,Accent,,0,0,0,,{\\an7\\pos(72,1572)\\bord0\\shad0\\1c&H63F5D8&\\clip(72,1572,72,1576)\\t(0,%d,\\clip(72,1572,876,1576))\\p1}m 0 0 l 804 0 804 4 0 4\n", end, int(total*1000))

	fmt.Fprintf(&ass, "Dialogue: 3,0:00:00.00,%s,Brand,,0,0,0,,{\\pos(88,164)}So Híp{\\fs32} /\n", end)
	fmt.Fprintf(&ass, "Dialogue: 2,0:00:00.00,%s,Title,,56,120,0,,%s\n", end, titleOverlay(s.Title))
	footerMeta := "SO HÍP / TIN TỨC • GIỌNG ĐỌC AI"
	fmt.Fprintf(&ass, "Dialogue: 2,0:00:00.00,%s,Meta,,0,0,0,,{\\pos(72,1595)}%s\n", end, assText(footerMeta))
	if err = os.WriteFile(filepath.Join(dir, "visuals.txt"), []byte(visuals.String()), 0600); err != nil {
		return "", 0, err
	}
	if err = os.WriteFile(filepath.Join(dir, "subtitles.ass"), []byte(ass.String()), 0600); err != nil {
		return "", 0, err
	}
	if err = os.WriteFile(filepath.Join(dir, "audio.txt"), []byte(concat.String()), 0600); err != nil {
		return "", 0, err
	}
	_, err = command(ctx, dir, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "concat", "-safe", "1", "-i", "audio.txt", "-c:a", "pcm_s16le", "audio.wav")
	if err != nil {
		return "", 0, err
	}
	_, err = command(ctx, dir, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "concat", "-safe", "1", "-i", "visuals.txt", "-i", "audio.wav", "-vf", videoOverlay(), "-r", "30", "-threads", "2", "-t", fmt.Sprintf("%.3f", total), "-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "192k", "-movflags", "+faststart", "video.mp4")
	if err != nil {
		return "", 0, err
	}
	return filepath.Join(dir, "video.mp4"), total, nil
}

// Fit the headline to the full 904-pixel media width and the space below the brand.
func titleLayout(title string) (int, string) {
	data, err := os.ReadFile("/usr/share/fonts/truetype/noto/NotoSans-Bold.ttf")
	if err != nil {
		data = gobold.TTF
	}
	parsed, err := opentype.Parse(data)
	if err != nil {
		parsed, _ = opentype.Parse(gobold.TTF)
	}
	// libass sizes fonts by ascent + descent rather than the font's em square.
	reference, _ := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 100, DPI: 72})
	metrics := reference.Metrics()
	ratio := float64(metrics.Ascent+metrics.Descent) / 64 / 100
	reference.Close()
	clean := strings.NewReplacer("\\", "／", "{", "(", "}", ")").Replace(title)
	words := strings.Fields(clean)
	for size := 88; size >= 28; size -= 2 {
		face, _ := opentype.NewFace(parsed, &opentype.FaceOptions{Size: float64(size) / ratio, DPI: 72})
		var lines []string
		line := ""
		for _, word := range words {
			candidate := word
			if line != "" {
				candidate = line + " " + word
			}
			if line != "" && font.MeasureString(face, candidate).Ceil() > 904 {
				lines = append(lines, line)
				line = word
			} else {
				line = candidate
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
		fits := len(lines)*size <= 220
		for _, line := range lines {
			if font.MeasureString(face, line).Ceil() > 904 {
				fits = false
			}
		}
		face.Close()
		if fits || size == 28 {
			return size, strings.Join(lines, `\N`)
		}
	}
	return 28, clean
}

func titleOverlay(title string) string {
	size, text := titleLayout(title)
	return fmt.Sprintf(`{\pos(56,280)\fs%d}%s`, size, text)
}
func clipText(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	r := []rune(text)
	text = string(r[:limit-1])
	if i := strings.LastIndex(text, " "); i > 0 {
		text = text[:i]
	}
	return text + "…"
}
func videoOverlay() string {
	return "drawbox=x=56:y=156:w=330:h=106:c=0xD8F563:t=fill,drawbox=x=56:y=510:w=904:h=8:c=0xFF693B:t=fill,drawbox=x=56:y=1290:w=844:h=170:c=0x1F302C:t=fill,drawbox=x=56:y=1290:w=6:h=170:c=0xD8F563:t=fill,drawbox=x=72:y=1572:w=804:h=4:c=0x34433E:t=fill,ass=subtitles.ass"
}

func renderVisual(ctx context.Context, dir, visualName, photo string, clip articleClip, d, offset float64) error {
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-filter_complex_threads", "1", "-f", "lavfi", "-i", "color=c=0x101515:s=1080x1920:r=30"}
	filter := "[0:v]drawgrid=w=90:h=90:t=1:c=0x24302D,drawbox=x=56:y=520:w=904:h=670:c=0x1F302C:t=fill[v]"
	if photo != "" {
		args = append(args, "-loop", "1", "-i", photo)
		filter = "[1:v]scale=1808:1340:force_original_aspect_ratio=increase,crop=1808:1340,zoompan=z='min(1+on*0.00012,1.08)':x='iw/2-iw/zoom/2':y='ih/2-ih/zoom/2':d=1:s=904x670:fps=30[p];[0:v][p]overlay=56:520[v]"
	}
	if clip.Name != "" {
		offset = math.Mod(offset, clip.Duration)
		args = append(args, "-stream_loop", "-1", "-ss", fmt.Sprintf("%.3f", offset), "-protocol_whitelist", "file", "-i", clip.Name)
		filter = "[1:v]setpts=PTS-STARTPTS,fps=30,scale=904:608:force_original_aspect_ratio=decrease:force_divisible_by=2,pad=904:670:(ow-iw)/2:(608-ih)/2:color=0x101515,setsar=1[p];[0:v][p]overlay=56:520[v]"
	}
	args = append(args, "-filter_complex", filter, "-map", "[v]", "-t", fmt.Sprintf("%.6f", d), "-an", "-c:v", "libx264", "-threads", "2", "-preset", "veryfast", "-crf", "21", "-pix_fmt", "yuv420p", visualName)
	if _, err := command(ctx, dir, "ffmpeg", args...); err != nil {
		return err
	}

	return nil
}
