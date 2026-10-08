package newsbot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"mime"
	"regexp"
	"strconv"
	"strings"
)

var googleModelName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func googlePath(model string) (string, error) {
	if !googleModelName.MatchString(model) {
		return "", fmt.Errorf("tên Google model không hợp lệ")
	}
	return "/models/" + model + ":generateContent", nil
}

type googlePart struct {
	Text       string `json:"text"`
	Thought    bool   `json:"thought"`
	InlineData *struct {
		MIMEType string `json:"mimeType"`
		Data     string `json:"data"`
	} `json:"inlineData"`
}

func googleParts(response []byte) ([]googlePart, error) {
	var envelope struct {
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		Candidates []struct {
			FinishReason string `json:"finishReason"`
			Content      struct {
				Parts []googlePart `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		return nil, err
	}
	if envelope.PromptFeedback.BlockReason != "" || len(envelope.Candidates) != 1 || envelope.Candidates[0].FinishReason != "STOP" {
		return nil, fmt.Errorf("Google từ chối hoặc trả nội dung chưa hoàn chỉnh")
	}
	return envelope.Candidates[0].Content.Parts, nil
}

func (a *AI) googleStructured(ctx context.Context, p ProviderConfig, system string, input, schema, out any) error {
	path, err := googlePath(p.TextModel)
	if err != nil {
		return err
	}
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	response, err := a.post(ctx, "google", p, path, map[string]any{
		"systemInstruction": map[string]any{"parts": []map[string]string{{"text": system}}},
		"contents":          []map[string]any{{"role": "user", "parts": []map[string]string{{"text": string(data)}}}},
		// REST TextResponseFormat uses an enum, unlike the SDK's MIME string helpers.
		"generationConfig": map[string]any{"responseFormat": map[string]any{"text": map[string]any{"mimeType": "APPLICATION_JSON", "schema": schema}}},
	}, 2<<20)
	if err != nil {
		return err
	}
	parts, err := googleParts(response)
	if err != nil {
		return err
	}
	var text strings.Builder
	for _, part := range parts {
		if !part.Thought {
			text.WriteString(part.Text)
		}
	}
	return decodeAIJSON(text.String(), out)
}

func (a *AI) googleSpeech(ctx context.Context, p ProviderConfig, text string) ([]byte, error) {
	path, err := googlePath(p.TTSModel)
	if err != nil {
		return nil, err
	}
	voiceConfig := map[string]any{"voice": p.Voice}
	// Earlier preview models use the prebuiltVoiceConfig field and return PCM.
	if strings.HasPrefix(p.TTSModel, "gemini-2.5-") || strings.HasPrefix(p.TTSModel, "gemini-3.1-") {
		voiceConfig = map[string]any{"prebuiltVoiceConfig": map[string]string{"voiceName": p.Voice}}
	}
	response, err := a.post(ctx, "google", p, path, map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": []map[string]string{{"text": text}}}},
		"generationConfig": map[string]any{
			"responseModalities": []string{"AUDIO"},
			"speechConfig":       map[string]any{"voiceConfig": voiceConfig},
		},
	}, 32<<20)
	if err != nil {
		return nil, err
	}
	parts, err := googleParts(response)
	if err != nil {
		return nil, err
	}
	var audio []byte
	for _, part := range parts {
		if part.Thought || part.InlineData == nil {
			continue
		}
		if audio != nil {
			return nil, fmt.Errorf("Google trả nhiều audio parts; cần một audio hoàn chỉnh")
		}
		decoded, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
		if err != nil {
			return nil, fmt.Errorf("Google audio base64 không hợp lệ")
		}
		audio, err = googleAudio(decoded, part.InlineData.MIMEType)
		if err != nil {
			return nil, err
		}
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("Google không trả audio")
	}
	return audio, nil
}

func googleAudio(data []byte, contentType string) ([]byte, error) {
	if len(data) == 0 || len(data) > 20<<20 {
		return nil, fmt.Errorf("Google audio trống hoặc quá lớn")
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("Google audio MIME không hợp lệ")
	}
	switch strings.ToLower(mediaType) {
	case "audio/wav", "audio/x-wav", "audio/wave":
		if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
			return nil, fmt.Errorf("Google WAV không hợp lệ")
		}
		return data, nil
	case "audio/l16", "audio/pcm":
		rate := 24000
		if v := params["rate"]; v != "" {
			rate, err = strconv.Atoi(v)
			if err != nil || rate < 8000 || rate > 96000 {
				return nil, fmt.Errorf("Google PCM sample rate không hợp lệ")
			}
		}
		if c := params["codec"]; c != "" && strings.ToLower(c) != "pcm" {
			return nil, fmt.Errorf("Google PCM codec không được hỗ trợ")
		}
		if c := params["channels"]; c != "" && c != "1" {
			return nil, fmt.Errorf("Google PCM cần mono")
		}
		if len(data)%2 != 0 {
			return nil, fmt.Errorf("Google PCM 16-bit không hợp lệ")
		}
		// Gemini PCM is signed 16-bit little-endian mono; wrap it for ffprobe/FFmpeg.
		var wav bytes.Buffer
		wav.WriteString("RIFF")
		_ = binary.Write(&wav, binary.LittleEndian, uint32(36+len(data)))
		wav.WriteString("WAVEfmt ")
		_ = binary.Write(&wav, binary.LittleEndian, uint32(16))
		for _, v := range []uint16{1, 1} {
			_ = binary.Write(&wav, binary.LittleEndian, v)
		}
		_ = binary.Write(&wav, binary.LittleEndian, uint32(rate))
		_ = binary.Write(&wav, binary.LittleEndian, uint32(rate*2))
		for _, v := range []uint16{2, 16} {
			_ = binary.Write(&wav, binary.LittleEndian, v)
		}
		wav.WriteString("data")
		_ = binary.Write(&wav, binary.LittleEndian, uint32(len(data)))
		wav.Write(data)
		return wav.Bytes(), nil
	default:
		return nil, fmt.Errorf("Google audio format không được hỗ trợ: %s", mediaType)
	}
}
