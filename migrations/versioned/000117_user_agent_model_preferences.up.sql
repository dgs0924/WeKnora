CREATE TABLE IF NOT EXISTS user_agent_model_preferences (
    user_id VARCHAR(36) NOT NULL,
    tenant_id BIGINT NOT NULL,
    agent_id VARCHAR(64) NOT NULL,
    model_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, tenant_id, agent_id)
);
CREATE INDEX IF NOT EXISTS idx_user_agent_model_preferences_tenant_user
    ON user_agent_model_preferences (tenant_id, user_id);
