DROP INDEX IF EXISTS idx_users_ldap_subject;
ALTER TABLE users DROP COLUMN ldap_subject;
