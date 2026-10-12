-- Kept after the fork's already-released 000030 LDAP migration so databases
-- that recorded version 30 still receive the upstream locale column.
ALTER TABLE im_channels ADD COLUMN locale VARCHAR(16) NOT NULL DEFAULT '';
