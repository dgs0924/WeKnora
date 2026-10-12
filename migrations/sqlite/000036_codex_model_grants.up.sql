CREATE TABLE codex_model_grants (
    model_id TEXT NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    owner_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    granted_by TEXT NOT NULL REFERENCES users(id),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (model_id, user_id)
);
CREATE INDEX idx_codex_model_grants_owner_user ON codex_model_grants(owner_user_id, user_id);
