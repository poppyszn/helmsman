-- Dev seed. Idempotent. Users are keyed on github_id; fake-github serves the same ids,
-- and the first login fills in the profile. Bindings reference users by github_id.
INSERT INTO agents (id, name) VALUES ('dev-agent', 'Local dev agent') ON CONFLICT DO NOTHING;

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
