-- Dev seed. Idempotent. Users are keyed on github_id; fake-github serves the same ids,
-- and the first login fills in the profile. Bindings reference users by github_id.
-- The join token below is the fixed dev token set on the agent container in compose.dev.yml.
-- Re-running this re-arms it, which also recovers an agent whose credential was lost.
INSERT INTO agents (id, name, join_token_hash, join_expires_at)
VALUES ('dev-agent', 'Local dev agent', sha256('dev-join-token-not-for-production'::bytea), now() + interval '10 years')
ON CONFLICT (id) DO UPDATE SET join_token_hash = EXCLUDED.join_token_hash, join_expires_at = EXCLUDED.join_expires_at;

INSERT INTO users (github_id, login, name) VALUES
  (1001, 'alice', 'Alice Admin'), (1002, 'bob', 'Bob Developer'),
  (1003, 'carol', 'Carol Viewer'), (1005, 'erin', 'Erin Approver')
ON CONFLICT (github_id) DO NOTHING;

-- bob: developer on every nonprod env (env * never reaches prod)
INSERT INTO role_bindings (subject_type, subject, role_id, app_id, env)
SELECT 'user', u.id::text, r.id, '*', '*' FROM users u, roles r WHERE u.github_id=1002 AND r.name='developer'
ON CONFLICT DO NOTHING;
-- carol: viewer on demo-app staging only
INSERT INTO role_bindings (subject_type, subject, role_id, app_id, env)
SELECT 'user', u.id::text, r.id, 'demo-app', 'staging' FROM users u, roles r WHERE u.github_id=1003 AND r.name='viewer'
ON CONFLICT DO NOTHING;
-- erin's team: approver everywhere nonprod
INSERT INTO role_bindings (subject_type, subject, role_id, app_id, env)
SELECT 'team', 'acme-org/leads', r.id, '*', '*' FROM roles r WHERE r.name='approver'
ON CONFLICT DO NOTHING;

-- Prod stand-in (demo-app/prod): bob can request migrations, erin's team approves them.
-- Wildcard env bindings never reach prod, so these are explicit.
INSERT INTO role_bindings (subject_type, subject, role_id, app_id, env)
SELECT 'user', u.id::text, r.id, 'demo-app', 'prod' FROM users u, roles r WHERE u.github_id=1002 AND r.name='operator'
ON CONFLICT DO NOTHING;
INSERT INTO role_bindings (subject_type, subject, role_id, app_id, env)
SELECT 'team', 'acme-org/leads', r.id, 'demo-app', 'prod' FROM roles r WHERE r.name='approver'
ON CONFLICT DO NOTHING;
