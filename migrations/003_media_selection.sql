CREATE TABLE media_assets (
    id text PRIMARY KEY,
    article_id bigint NOT NULL REFERENCES news_articles(id),
    kind text NOT NULL CHECK (kind IN ('image','video')),
    candidates jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX media_assets_article_idx ON media_assets(article_id);
-- NULL: legacy automatic media; []: explicitly selected text-only video.
ALTER TABLE videos ADD COLUMN media_ids jsonb;
