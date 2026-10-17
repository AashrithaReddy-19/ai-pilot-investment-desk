-- +goose Up
CREATE TABLE interviews (
    id               SMALLSERIAL PRIMARY KEY,
    interviewee_role TEXT NOT NULL,
    task             TEXT NOT NULL,
    minutes_per_task NUMERIC(6,1) CHECK (minutes_per_task IS NULL OR minutes_per_task >= 0),
    useful_result    TEXT,
    quote            TEXT,
    interviewed_on   DATE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS interviews;
