-- +goose Up
ALTER TABLE plugins ADD COLUMN source VARCHAR(255) NOT NULL DEFAULT 'wordpress.org';
ALTER TABLE plugins DROP CONSTRAINT plugins_slug_key;
ALTER TABLE plugins ADD CONSTRAINT plugins_slug_source_key UNIQUE (slug, source);

ALTER TABLE themes ADD COLUMN source VARCHAR(255) NOT NULL DEFAULT 'wordpress.org';
ALTER TABLE themes DROP CONSTRAINT themes_slug_key;
ALTER TABLE themes ADD CONSTRAINT themes_slug_source_key UNIQUE (slug, source);

-- +goose Down
ALTER TABLE plugins DROP CONSTRAINT plugins_slug_source_key;
ALTER TABLE plugins ADD CONSTRAINT plugins_slug_key UNIQUE (slug);
ALTER TABLE plugins DROP COLUMN source;

ALTER TABLE themes DROP CONSTRAINT themes_slug_source_key;
ALTER TABLE themes ADD CONSTRAINT themes_slug_key UNIQUE (slug);
ALTER TABLE themes DROP COLUMN source;
