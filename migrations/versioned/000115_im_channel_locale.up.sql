-- Kept after the fork's already-released 000110 LDAP migration so databases
-- that recorded version 110 still receive the upstream locale column.
ALTER TABLE im_channels
    ADD COLUMN IF NOT EXISTS locale VARCHAR(16) NOT NULL DEFAULT '';

COMMENT ON COLUMN im_channels.locale IS
    'Fixed locale for responses on this IM channel; empty uses the deployment default';
