package newsbot

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Speaker interface {
	Speech(context.Context, string) ([]byte, error)
}
type Renderer struct {
	OutputDir string
	Speaker   Speaker
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
Style: Subtitle,Noto Sans,50,&H00FFFFFF,&H00FFFFFF,&H00131B29,&H90131B29,-1,0,0,0,100,100,0,0,1,3,0,2,100,100,380,1
Style: Title,Noto Sans,60,&H00FFFFFF,&H00FFFFFF,&H00131B29,&H00131B29,-1,0,0,0,100,100,0,0,1,0,0,8,90,90,350,1
Style: Brand,Noto Sans,34,&H00FFD078,&H00FFFFFF,&H00131B29,&H00131B29,-1,0,0,0,100,100,0,0,1,0,0,8,90,90,200,1

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
	total := 0.0
	for i, scene := range s.Scenes {
		audio, err := r.Speaker.Speech(ctx, scene.Narration)
		if err != nil {
			return "", 0, err
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
		for start := 0; start < len(words); start += 10 {
			end := start + 10
			if end > len(words) {
				end = len(words)
			}
			fmt.Fprintf(&ass, "Dialogue: 1,%s,%s,Subtitle,,0,0,0,,%s\n", assTime(total+d*float64(start)/float64(len(words))), assTime(total+d*float64(end)/float64(len(words))), assText(strings.Join(words[start:end], " ")))
		}
		total += d
	}
	if total < 20 || total > 90 {
		return "", 0, fmt.Errorf("video dài %.1fs; yêu cầu 20–90s", total)
	}
	end := assTime(total)
	fmt.Fprintf(&ass, "Dialogue: 0,0:00:00.00,%s,Title,,0,0,0,,%s\n", end, assText(s.Title))
	labels := []string{}
	seen := map[string]bool{}
	for _, a := range articles {
		if !seen[a.SourceName] {
			labels = append(labels, a.SourceName)
			seen[a.SourceName] = true
		}
	}
	fmt.Fprintf(&ass, "Dialogue: 0,0:00:00.00,%s,Brand,,0,0,0,,NEWSBOT • GIỌNG ĐỌC AI\n", end)
	fmt.Fprintf(&ass, "Dialogue: 0,0:00:00.00,%s,Subtitle,,0,0,180,,%s\n", end, assText("Nguồn: "+strings.Join(labels, ", ")))
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
	_, err = command(ctx, dir, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "color=c=0x101B2B:s=1080x1920:r=30", "-i", "audio.wav", "-vf", "drawbox=x=90:y=290:w=900:h=8:color=0x78D0FF:t=fill,ass=subtitles.ass", "-t", fmt.Sprintf("%.3f", total), "-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "192k", "-movflags", "+faststart", "video.mp4")
	if err != nil {
		return "", 0, err
	}
	return filepath.Join(dir, "video.mp4"), total, nil
}
