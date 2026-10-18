-- +goose Up
ALTER TABLE uploads ADD COLUMN storage_provider TEXT NOT NULL DEFAULT 'postgres'
    CHECK (storage_provider IN ('postgres', 's3'));
ALTER TABLE uploads ADD COLUMN object_key TEXT;
ALTER TABLE uploads ADD COLUMN uploaded_at TIMESTAMPTZ;
ALTER TABLE uploads ALTER COLUMN content DROP NOT NULL;
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_status_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_status_check
    CHECK (status IN ('pending_upload', 'received', 'processing', 'parsed', 'failed', 'confirmed'));
ALTER TABLE uploads ADD CONSTRAINT uploads_storage_reference_check
    CHECK ((storage_provider = 'postgres' AND content IS NOT NULL AND object_key IS NULL)
        OR (storage_provider = 's3' AND object_key IS NOT NULL));
CREATE UNIQUE INDEX idx_uploads_s3_object_key ON uploads (object_key) WHERE object_key IS NOT NULL;

-- +goose Down
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM uploads WHERE storage_provider = 's3') THEN
        RAISE EXCEPTION 'cannot roll back S3 upload storage while S3-backed uploads exist';
    END IF;
END $$;
DROP INDEX IF EXISTS idx_uploads_s3_object_key;
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_storage_reference_check;
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_status_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_status_check
    CHECK (status IN ('received', 'processing', 'parsed', 'failed', 'confirmed'));
ALTER TABLE uploads ALTER COLUMN content SET NOT NULL;
ALTER TABLE uploads DROP COLUMN IF EXISTS uploaded_at;
ALTER TABLE uploads DROP COLUMN IF EXISTS object_key;
ALTER TABLE uploads DROP COLUMN IF EXISTS storage_provider;
