-- Retain attribution/evidence of existing articles and videos when a source is removed.
ALTER TABLE news_sources ADD COLUMN deleted_at timestamptz;
