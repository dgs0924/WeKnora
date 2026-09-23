ALTER TABLE users ADD COLUMN IF NOT EXISTS ldap_subject TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_ldap_subject ON users (ldap_subject) WHERE ldap_subject IS NOT NULL;
