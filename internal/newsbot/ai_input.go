package newsbot

// Keep complete article text and IDs for grounding; omit administrative metadata.
func compactArticles(articles []Article) []map[string]any {
	out := make([]map[string]any, 0, len(articles))
	for _, article := range articles {
		out = append(out, map[string]any{"id": article.ID, "title": article.Title, "content": article.Content})
	}
	return out
}
func compactCheckScript(script Script) map[string]any {
	scenes := make([]map[string]any, 0, len(script.Scenes))
	for _, scene := range script.Scenes {
		scenes = append(scenes, map[string]any{"narration": scene.Narration, "claim_ids": scene.ClaimIDs})
	}
	return map[string]any{"title": script.Title, "summary": script.Summary, "claims": script.Claims, "scenes": scenes}
}
