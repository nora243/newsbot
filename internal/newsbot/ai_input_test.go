package newsbot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateCategoryAndLocalSubtitles(t *testing.T) {
	articles, script := fixture()
	script.Category = "HEALTH"
	for i := range script.Scenes {
		script.Scenes[i].Subtitle = ""
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
			ResponseFormat struct {
				JSONSchema struct {
					Schema map[string]any `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var inputs []map[string]any
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &inputs); err != nil {
			t.Error(err)
			return
		}
		if len(inputs) != 1 || len(inputs[0]) != 3 || inputs[0]["content"] != articles[0].Content || inputs[0]["title"] != articles[0].Title {
			t.Error("grounding text lost or administrative metadata sent")
		}
		properties := request.ResponseFormat.JSONSchema.Schema["properties"].(map[string]any)
		if properties["category"] == nil {
			t.Error("generation missing speech category")
		}
		sceneProperties := properties["scenes"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
		if sceneProperties["subtitle"] != nil {
			t.Error("model still generates duplicate subtitles")
		}
		data, _ := json.Marshal(script)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(data)}}}})
	}))
	defer server.Close()
	result, err := NewAI(Config{OpenAI: ProviderConfig{APIKey: "test", BaseURL: server.URL}}).Generate(context.Background(), articles)
	if err != nil {
		t.Fatal(err)
	}
	if result.Category != "HEALTH" {
		t.Fatal("topic lost")
	}
	for _, scene := range result.Scenes {
		if scene.Subtitle != scene.Narration {
			t.Fatal("local subtitles do not match narration")
		}
	}
	checkInput := compactCheckScript(result)
	for _, scene := range checkInput["scenes"].([]map[string]any) {
		if scene["narration"] == nil || scene["claim_ids"] == nil || len(scene) != 2 {
			t.Fatal("checker input lost required facts or repeated subtitles")
		}
	}
}
