DROP INDEX IF EXISTS idx_users_ldap_subject;
ALTER TABLE users DROP COLUMN IF EXISTS ldap_subject;
