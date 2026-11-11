-- +goose Up
-- M3: GitHub Actions runs. Additive only; 00001 and 00002 are untouched.
ALTER TABLE action_runs
    ADD COLUMN repo          TEXT,
    ADD COLUMN workflow      TEXT,
    ADD COLUMN ref           TEXT,
    ADD COLUMN warning       TEXT,
    ADD COLUMN jobs          JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN rejected_by   BIGINT REFERENCES users(id),
    ADD COLUMN decided_at    TIMESTAMPTZ,
    ADD COLUMN dispatched_at TIMESTAMPTZ,
    ADD COLUMN conclusion    TEXT,
    ADD COLUMN cancel_requested BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN rerun_of      UUID REFERENCES action_runs(id),
    ADD COLUMN next_poll_at  TIMESTAMPTZ NOT NULL DEFAULT now();

-- One active run per environment, enforced by the database so two servers or two
-- clicks cannot both start a migration.
CREATE UNIQUE INDEX action_runs_one_active_per_env ON action_runs(app_env_id)
    WHERE status IN ('pending_approval','dispatching','queued','in_progress','waiting');
CREATE INDEX action_runs_active_idx ON action_runs(next_poll_at)
    WHERE status IN ('dispatching','queued','in_progress','waiting');

-- Job logs, captured when a run finishes. The service caps each run at 5 MB.
CREATE TABLE action_run_logs (
    run_id    UUID NOT NULL REFERENCES action_runs(id) ON DELETE CASCADE,
    job_id    BIGINT NOT NULL,
    job_name  TEXT NOT NULL,
    content   TEXT NOT NULL,
    truncated BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (run_id, job_id)
);

-- +goose Down
DROP TABLE action_run_logs;
DROP INDEX action_runs_active_idx, action_runs_one_active_per_env;
ALTER TABLE action_runs
    DROP COLUMN repo, DROP COLUMN workflow, DROP COLUMN ref, DROP COLUMN warning, DROP COLUMN jobs,
    DROP COLUMN rejected_by, DROP COLUMN decided_at, DROP COLUMN dispatched_at, DROP COLUMN conclusion,
    DROP COLUMN cancel_requested, DROP COLUMN rerun_of, DROP COLUMN next_poll_at;
