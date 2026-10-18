-- +goose Up

-- Phase A document validation accepts the formats supported by the gateway.
-- Parsing remains an explicit processing step; a stored original is never reported as parsed
-- until the worker receives a valid engine response.
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_format_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_format_check
    CHECK (format IN ('csv', 'pdf', 'xlsx', 'xls', 'docx'));

-- +goose Down
ALTER TABLE uploads DROP CONSTRAINT IF EXISTS uploads_format_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_format_check
    CHECK (format IN ('csv', 'xlsx'));
