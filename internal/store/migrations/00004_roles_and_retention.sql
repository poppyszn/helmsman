-- +goose Up
-- M4: least-privilege runtime role and retention.
--
-- The server runs as helmsman_app (or a login role that is a member of it). That role can read
-- and write every table, but on audit_events it can only INSERT and SELECT, so the append-only
-- rule holds at the permission level on top of the triggers. Migrations keep running as the
-- table owner. deploy/db/init-app-role.sh gives helmsman_app a login and password.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'helmsman_app') THEN
        CREATE ROLE helmsman_app NOLOGIN;
    END IF;
END
$$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA public TO helmsman_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO helmsman_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO helmsman_app;
REVOKE UPDATE, DELETE, TRUNCATE ON audit_events FROM helmsman_app;
REVOKE ALL ON goose_db_version FROM helmsman_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO helmsman_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO helmsman_app;

-- Audit retention (1 year) cannot be a plain DELETE because the trigger forbids it. This function
-- is the only way rows leave the table: it refuses to keep less than 90 days, records that it ran,
-- and is executable by the owner only (never by helmsman_app). Run it from cron as the owner.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION audit_events_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' AND current_setting('helmsman.audit_purge', true) = 'on' THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'audit_events is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION audit_purge(keep interval DEFAULT interval '365 days') RETURNS bigint AS $$
DECLARE n bigint;
BEGIN
    IF keep < interval '90 days' THEN
        RAISE EXCEPTION 'audit_purge refuses to keep less than 90 days';
    END IF;
    PERFORM set_config('helmsman.audit_purge', 'on', true);
    DELETE FROM audit_events WHERE at < now() - keep;
    GET DIAGNOSTICS n = ROW_COUNT;
    PERFORM set_config('helmsman.audit_purge', 'off', true);
    INSERT INTO audit_events (actor_type, actor_id, action, target, details, result)
    VALUES ('system', 'maintenance', 'audit.purge', NULL, jsonb_build_object('deleted', n, 'kept', keep::text), 'ok');
    RETURN n;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION audit_purge(interval) FROM PUBLIC;

-- +goose Down
DROP FUNCTION audit_purge(interval);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION audit_events_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
