-- +goose Up
-- What a succeeded job produced, such as a webhook's answer or the files it wrote.
alter table jobs add column result jsonb;

-- +goose Down
alter table jobs drop column result;
