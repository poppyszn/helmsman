# Notifications

Helmsman can tell people when something needs them, by **email** or **webhook**, so nobody has to watch the portal. An admin sets it up under **Notifications** in the sidebar.

## What it announces

| Event | When |
|---|---|
| A migration or seed failed | A run started from Helmsman failed, or could not be started |
| A production run is waiting for approval | A second person with `action.approve` is needed |
| A migration or seed succeeded | Off by default |
| A deployment failed or succeeded | A finished run of an app's deploy workflow (the one named in the catalog) |
| An environment went down, or recovered | Two health checks in a row failed or its container stopped, and the same again for coming back |
| A container is crash looping | Repeated exits in a short span |
| An agent went offline, or came back | The agent's connection was silent for a minute |

Each channel picks the events it wants and whether it covers all environments, only production, or only non-production. Events that belong to an agent or a whole repository, with no single environment, go to channels set to all environments.

A change is announced only after Helmsman has seen it on two looks 30 seconds apart, so a blip tells nobody. A flapping environment is held back: the same event for the same environment is not sent again to a channel within ten minutes. When Helmsman starts, or the first time it sees an environment or a deploy workflow, it only records the state, so restarting or deploying Helmsman itself never causes a burst of messages.

Notifications never contain log text or secrets: a title, a short message, the environment, a few facts (for example the commit and who started it) and a link back to the portal.

## Email

Email channels need a mail server. On the **Notifications** page, fill in **Mail server (SMTP)**: the host, the port, the connection type (STARTTLS on 587, TLS on 465, or none), a username and password if the server asks for a login, and the address to send from. Use **Send test email** to check it. The password is stored encrypted with `IDP_ENCRYPTION_KEY` and is never shown again.

Then add a channel of type Email with up to 20 plain addresses. **Send test** on a channel sends a sample message through it.

Mail is plain text. If a password is set, the connection must be encrypted (STARTTLS or TLS); Go's SMTP client refuses to send a password over a plain connection.

## Webhooks

A webhook channel receives an HTTP `POST` with a JSON body:

```json
{
  "event": "env.down",
  "time": "2026-11-12T10:15:00Z",
  "app": "billing-api",
  "env": "prod",
  "tier": "prod",
  "title": "billing-api / prod is down",
  "message": "Two health checks in a row failed, or its container is not running.",
  "url": "https://portal.example.com/apps/billing-api",
  "details": { "environment": "billing-api / prod" }
}
```

Headers: `X-Helmsman-Event`, `X-Helmsman-Timestamp` (Unix seconds) and, when the channel has a secret, `X-Helmsman-Signature: sha256=<hex>`, the HMAC-SHA256 of `timestamp + "." + body` with the secret. Check it, and reject timestamps older than a few minutes. A secret is generated for you when you add the channel and shown once; paste your own to keep an existing receiver's.

Answer with any 2xx. A redirect counts as a failure: Helmsman does not follow redirects, so a signed body is never forwarded somewhere you did not choose.

A receiver that speaks Slack, Teams or Discord needs a small adapter in between (a function that turns this JSON into their message format), or a service that offers a generic incoming webhook.

### Which addresses are allowed

The address must be http or https, without a user and password in it, and not a cloud metadata or link-local address (169.254.x.x and similar). This is checked when you save and again when the connection is made, so a host name that points there is refused too. Private network addresses are allowed, since a webhook often points inside the company.

## Delivery

Every event is stored first, then sent by a background worker, so a slow or down receiver never delays Helmsman. A send that fails is retried after 30 seconds, 2 minutes and 10 minutes, then marked failed. **Recent deliveries** on the page shows each one with its status and the last error (never the receiver's reply). Deliveries are kept for 30 days.

## Who can set it up

Admins only (`admin.access`). Creating, changing, testing and deleting channels and the mail server are all in the audit log, without any secret.

## Try it locally

In the dev stack, add a webhook channel that points at any listener you run, or use the Playwright test as an example (`e2e/tests/dev/12-notifications.spec.ts`), which starts a receiver and checks the signature.
