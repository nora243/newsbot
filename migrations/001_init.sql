CREATE TABLE IF NOT EXISTS news_sources (
    id bigserial PRIMARY KEY,
    name text NOT NULL,
    rss_url text NOT NULL UNIQUE,
    language text NOT NULL DEFAULT 'vi',
    category text NOT NULL DEFAULT 'TECHNOLOGY',
    active boolean NOT NULL DEFAULT false,
    license_notes text NOT NULL DEFAULT 'Chưa xác nhận quyền sử dụng',
    allowed_images boolean NOT NULL DEFAULT false,
    allowed_video boolean NOT NULL DEFAULT false,
    attribution_required boolean NOT NULL DEFAULT true,
    last_fetched_at timestamptz,
    last_error text
);
CREATE TABLE IF NOT EXISTS news_articles (
    id bigserial PRIMARY KEY,
    source_id bigint NOT NULL REFERENCES news_sources(id),
    canonical_url text NOT NULL UNIQUE,
    title text NOT NULL,
    content text NOT NULL,
    content_hash text NOT NULL,
    published_at timestamptz,
    collected_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_id, content_hash)
);
CREATE INDEX IF NOT EXISTS articles_collected_idx ON news_articles (collected_at DESC);
CREATE TABLE IF NOT EXISTS videos (
    id text PRIMARY KEY,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','processing','pending_review','approved','rejected','failed')),
    script jsonb,
    fact_check jsonb,
    output_path text,
    duration_seconds double precision,
    last_error text,
    reviewed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS video_sources (
    video_id text NOT NULL REFERENCES videos(id),
    article_id bigint NOT NULL REFERENCES news_articles(id),
    used_for text NOT NULL DEFAULT 'FACT',
    PRIMARY KEY (video_id, article_id)
);
CREATE TABLE IF NOT EXISTS jobs (
    id bigserial PRIMARY KEY,
    video_id text NOT NULL UNIQUE REFERENCES videos(id),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','done','failed')),
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz,
    lease_token text,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS jobs_pending_idx ON jobs (available_at) WHERE status IN ('queued','running');
-- Bật từng nguồn sau khi kiểm tra điều khoản và ghi lại quyền sử dụng.
INSERT INTO news_sources (name,rss_url,language,category) VALUES
('VnExpress - Thế giới','https://vnexpress.net/rss/the-gioi.rss','vi','WORLD'),
('VnExpress - Kinh doanh','https://vnexpress.net/rss/kinh-doanh.rss','vi','BUSINESS'),
('BBC - Technology','https://feeds.bbci.co.uk/news/technology/rss.xml','en','TECHNOLOGY')
ON CONFLICT (rss_url) DO NOTHING;
