CREATE TABLE IF NOT EXISTS user_agent_model_preferences (
    user_id TEXT NOT NULL,
    tenant_id INTEGER NOT NULL,
    agent_id TEXT NOT NULL,
    model_id TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, tenant_id, agent_id)
);
CREATE INDEX IF NOT EXISTS idx_user_agent_model_preferences_tenant_user
    ON user_agent_model_preferences (tenant_id, user_id);
