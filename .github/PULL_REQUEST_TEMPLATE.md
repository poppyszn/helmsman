## What changed and why

<!-- What a user or operator will notice, and the reason. Link the issue or roadmap item if there is one. -->

## How it was checked

- [ ] `make test` and `make lint` pass
- [ ] `make e2e` passes for what I touched (or: what I could not run, and why)
- [ ] Tests added: unit, API integration (who may call it and who may not), and a Playwright test for anything a person clicks

## Rules that do not bend

- [ ] Every new route is in the route table with a permission and scope
- [ ] Anything that changes state is audited, without secrets or log text
- [ ] Migrations only add (and use the next free number); the agent protocol only gains optional fields
- [ ] No secrets in the diff, logs or API answers
- [ ] Production stays explicit: nothing grants a wildcard environment or seeding to production

## Docs

- [ ] I updated the docs that describe this change (name them): 
- [ ] `make docs-check` passes
- [ ] The roadmap is updated if this ships or changes an item

## Notes for the reviewer

<!-- Anything you want a second pair of eyes on: a permission choice, a migration, a trade-off. -->
