-- +goose Up
-- Keys are looked up by the hash of what the caller sends. The prefix is only
-- there so people can tell their keys apart, so it no longer has to be unique.
alter table api_keys drop constraint api_keys_prefix_key;
alter table api_keys add constraint api_keys_hash_key unique (hash);
create index api_keys_team_idx on api_keys (team_id, id);

-- +goose Down
drop index api_keys_team_idx;
alter table api_keys drop constraint api_keys_hash_key;
alter table api_keys add constraint api_keys_prefix_key unique (prefix);
