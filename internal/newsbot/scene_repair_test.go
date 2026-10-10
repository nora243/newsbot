package newsbot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

func repairFixture() ([]Article, Script, FactCheck, sceneRepair) {
	articles, script := fixture()
	check := FactCheck{Scenes: []SceneCheck{{SceneIndex: 0, Supported: true}, {SceneIndex: 1, Supported: false, Reason: "unsupported assertion"}}}
	narration := strings.ReplaceAll(script.Scenes[1].Narration, "Trung tâm thử nghiệm công bố hệ thống mới", "Hệ thống mới được công bố tại trung tâm thử nghiệm")
	patch := sceneRepair{SceneIndex: 1, Narration: narration, Claims: []repairedClaim{{Text: script.Claims[0].Text, Evidence: script.Claims[0].Evidence}}}
	return articles, script, check, patch
}

func TestSceneRepairPreservesPassedScenes(t *testing.T) {
	articles, script, check, patch := repairFixture()
	before, _ := json.Marshal(script)
	repaired, err := applySceneRepairs(script, check, []sceneRepair{patch}, articles)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(repaired.Scenes[0], script.Scenes[0]) || !reflect.DeepEqual(repaired.Claims[0], script.Claims[0]) {
		t.Fatal("passed scene or shared evidence changed")
	}
	if repaired.Scenes[1].Narration != patch.Narration || repaired.Scenes[1].Subtitle != patch.Narration {
		t.Fatal("replacement or subtitle missing")
	}
	if repaired.Title != script.Title || repaired.Summary != script.Summary || !reflect.DeepEqual(repaired.Uncertainties, script.Uncertainties) {
		t.Fatal("script metadata changed")
	}
	after, _ := json.Marshal(script)
	if string(before) != string(after) {
		t.Fatal("original mutated")
	}
	for _, invalid := range [][]sceneRepair{nil, {{SceneIndex: 0}}, {patch, patch}} {
		if _, err := applySceneRepairs(script, check, invalid, articles); err == nil {
			t.Fatal("accepted incomplete, duplicate or approved-scene edits")
		}
	}
	patch.Claims = []repairedClaim{{Text: "fabricated", Evidence: []Evidence{{ArticleID: 999, Quote: "fabricated source quote"}}}}
	if _, err := applySceneRepairs(script, check, []sceneRepair{patch}, articles); err == nil {
		t.Fatal("accepted fabricated evidence")
	}
	for _, invalid := range []FactCheck{{}, {Scenes: []SceneCheck{{SceneIndex: 0}, {SceneIndex: 0}}}, {Scenes: []SceneCheck{{SceneIndex: 0, Supported: true}, {SceneIndex: 1, Supported: true}}}} {
		if _, err := failedSceneIndices(invalid, 2); err == nil {
			t.Fatal("accepted invalid or fully supported check")
		}
	}
}

func TestAIRepairsOnlyFailedScenes(t *testing.T) {
	articles, script, check, patch := repairFixture()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		var input struct {
			Indices []int     `json:"failed_scene_indices"`
			Check   FactCheck `json:"fact_check"`
		}
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &input); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(input.Indices, []int{1}) || !reflect.DeepEqual(input.Check, check) {
			t.Error("missing failed indices or reasons")
		}
		data, _ := json.Marshal(map[string]any{"scenes": []sceneRepair{patch}})
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(data)}}}})
	}))
	defer server.Close()
	ai := NewAI(Config{OpenAI: ProviderConfig{APIKey: "test", BaseURL: server.URL}})
	repaired, err := ai.RepairScenes(context.Background(), articles, script, check)
	if err != nil || !reflect.DeepEqual(repaired.Scenes[0], script.Scenes[0]) || repaired.Scenes[1].Narration != patch.Narration {
		t.Fatalf("repair failed: %v", err)
	}
}

func TestSceneRepairIntegration(t *testing.T) {
	dsn := os.Getenv("NEWSBOT_REPAIR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated repair test database required")
	}
	ctx := context.Background()
	store, err := NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB.Close()
	if err = store.Migrate(ctx, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `TRUNCATE jobs,video_sources,videos,media_assets,news_articles,news_sources RESTART IDENTITY CASCADE; INSERT INTO news_sources(name,rss_url) VALUES('Test','https://example.com/rss')`); err != nil {
		t.Fatal(err)
	}
	articles, script, check, patch := repairFixture()
	articles[0].Hash = "repair"
	if _, err = store.SaveArticle(ctx, articles[0]); err != nil {
		t.Fatal(err)
	}
	articles, err = store.Articles(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := store.Enqueue(ctx, []int64{articles[0].ID}, []string{})
	if err != nil {
		t.Fatal(err)
	}
	rawScript, _ := json.Marshal(script)
	rawCheck, _ := json.Marshal(check)
	if _, err = store.DB.Exec(ctx, `UPDATE videos SET status='failed',script=$2,fact_check=$3 WHERE id=$1;`, parent, rawScript, rawCheck); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.Exec(ctx, `UPDATE jobs SET status='failed' WHERE video_id=$1`, parent); err != nil {
		t.Fatal(err)
	}
	pass := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ResponseFormat struct {
				JSONSchema struct {
					Name string `json:"name"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		json.NewDecoder(r.Body).Decode(&request)
		var output any
		switch request.ResponseFormat.JSONSchema.Name {
		case "scene_repair":
			output = map[string]any{"scenes": []sceneRepair{patch}}
		case "script_check":
			output = FactCheck{Scenes: []SceneCheck{{SceneIndex: 0, Supported: true}, {SceneIndex: 1, Supported: pass, Reason: "check replacement"}}}
		default:
			t.Errorf("unexpected AI operation: %s", request.ResponseFormat.JSONSchema.Name)
		}
		data, _ := json.Marshal(output)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": string(data)}}}})
	}))
	defer server.Close()
	config := Config{AdminToken: strings.Repeat("r", 32), OpenAI: ProviderConfig{APIKey: "test", BaseURL: server.URL, TextModel: "test", TTSModel: "test", Voice: "test"}}
	handler, err := (&API{Store: store, Config: config}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	for _, supported := range []bool{false, true} {
		pass = supported
		req := httptest.NewRequest("POST", "/api/videos/"+parent+"/revisions", strings.NewReader(`{"repair_failed_scenes":true}`))
		req.Header.Set("Authorization", "Bearer "+config.AdminToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != 202 {
			t.Fatalf("repair API: %d %s", rec.Code, rec.Body.String())
		}
		var response struct {
			ID string `json:"id"`
		}
		json.Unmarshal(rec.Body.Bytes(), &response)
		videos, err := store.Videos(ctx, response.ID)
		if err != nil || len(videos) != 1 || videos[0].ParentID == nil || *videos[0].ParentID != parent || videos[0].MediaIDs == nil || len(videos[0].MediaIDs) != 0 {
			t.Fatal("revision lost lineage or explicit media selection", err)
		}
		job, err := store.ClaimJob(ctx)
		if err != nil || job == nil || job.VideoID != response.ID {
			t.Fatal("repair not queued", err)
		}
		speaker := &rejectingSpeaker{}
		worker := Worker{Store: store, AI: NewAI(config), Renderer: &Renderer{OutputDir: t.TempDir(), Speaker: speaker}}
		_, _, processErr := worker.process(ctx, *job)
		if processErr == nil || speaker.Called != supported {
			t.Fatalf("fact check did not gate TTS: passed=%v called=%v err=%v", supported, speaker.Called, processErr)
		}
		videos, err = store.Videos(ctx, response.ID)
		if err != nil {
			t.Fatal(err)
		}
		var saved Script
		json.Unmarshal(videos[0].Script, &saved)
		if !reflect.DeepEqual(saved.Scenes[0], script.Scenes[0]) || saved.Scenes[1].Narration != patch.Narration {
			t.Fatal("worker changed passed scene or did not repair failed scene")
		}
		if _, err = store.DB.Exec(ctx, `UPDATE jobs SET status='failed' WHERE video_id=$1`, response.ID); err != nil {
			t.Fatal(err)
		}
	}
	// The parent is untouched.
	videos, err := store.Videos(ctx, parent)
	if err != nil || string(videos[0].Script) == "" {
		t.Fatal(err)
	}
	var unchanged Script
	json.Unmarshal(videos[0].Script, &unchanged)
	if !reflect.DeepEqual(unchanged, script) {
		t.Fatal("parent modified")
	}
}
