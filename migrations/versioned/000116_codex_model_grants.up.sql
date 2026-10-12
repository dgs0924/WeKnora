CREATE TABLE codex_model_grants (
    model_id VARCHAR(64) NOT NULL REFERENCES models(id) ON DELETE CASCADE,
    user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    owner_user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    granted_by VARCHAR(36) NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (model_id, user_id)
);
CREATE INDEX idx_codex_model_grants_owner_user ON codex_model_grants(owner_user_id, user_id);
