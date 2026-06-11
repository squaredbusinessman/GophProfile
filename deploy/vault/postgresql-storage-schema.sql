CREATE TABLE IF NOT EXISTS vault_kv_store (
    parent_path text COLLATE "C" NOT NULL,
    path text COLLATE "C",
    key text COLLATE "C",
    value bytea,
    CONSTRAINT vault_kv_store_pkey PRIMARY KEY (path, key)
);

CREATE INDEX IF NOT EXISTS vault_kv_store_parent_path_idx
    ON vault_kv_store (parent_path);

CREATE TABLE IF NOT EXISTS vault_ha_locks (
    ha_key text COLLATE "C" NOT NULL,
    ha_identity text COLLATE "C" NOT NULL,
    ha_value text COLLATE "C",
    valid_until timestamptz NOT NULL,
    CONSTRAINT vault_ha_locks_pkey PRIMARY KEY (ha_key)
);
