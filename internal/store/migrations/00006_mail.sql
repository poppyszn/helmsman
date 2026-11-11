-- +goose Up
-- Mail viewer: an admin can connect a Mailpit (the dev mail catcher) from the Integrations page, and people
-- with the new mail.view permission read its messages inside Helmsman. Additive: one more integration kind
-- and one more permission on the roles that already work on development environments.
ALTER TABLE integrations DROP CONSTRAINT integrations_kind_check;
ALTER TABLE integrations ADD CONSTRAINT integrations_kind_check CHECK (kind IN ('loki', 'mailpit'));

UPDATE roles SET permissions = array_append(permissions, 'mail.view')
 WHERE name IN ('developer', 'operator', 'admin') AND NOT ('mail.view' = ANY (permissions));

-- +goose Down
UPDATE roles SET permissions = array_remove(permissions, 'mail.view');
DELETE FROM integrations WHERE kind = 'mailpit';
ALTER TABLE integrations DROP CONSTRAINT integrations_kind_check;
ALTER TABLE integrations ADD CONSTRAINT integrations_kind_check CHECK (kind IN ('loki'));
