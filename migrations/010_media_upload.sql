ALTER TABLE media_assets ADD COLUMN uploaded boolean NOT NULL DEFAULT false;
ALTER TABLE media_assets ADD COLUMN upload_name text NOT NULL DEFAULT '';
