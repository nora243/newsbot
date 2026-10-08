ALTER TABLE videos ADD COLUMN parent_id text REFERENCES videos(id);
ALTER TABLE videos ADD COLUMN draft_script jsonb;
CREATE INDEX videos_parent_idx ON videos(parent_id) WHERE parent_id IS NOT NULL;
