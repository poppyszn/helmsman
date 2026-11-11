# Mail viewer

The **Mail** item in the sidebar shows the emails your development environments send, read from a [Mailpit](https://mailpit.axllent.org) you already run. Developers read sign-up and password-reset mails inside Helmsman instead of opening Mailpit separately.

It is read-only: Helmsman lists and opens messages and downloads attachments. It never deletes, sends or releases mail.

## Connect Mailpit

1. Your apps already send their mail to Mailpit (SMTP on port 1025 in a default install). Nothing changes there.
2. In Helmsman, open **Integrations**, find **Mail (Mailpit)**, and enter Mailpit's **web address**, the one that serves its UI and API (port 8025 by default), as the Helmsman server reaches it. If Mailpit runs in the same Docker network, that is `http://mailpit:8025`.
3. Choose **Test connection**, then **Save and turn on**. **Mail** appears in the sidebar for everyone who may read it. There is no restart, and you can turn it off or remove it on the same page.

A Mailpit on another host has to be reachable from the Helmsman server. Mailpit's own UI has no login by default, so do not publish it as it is. Give it one and put TLS in front of it:

```bash
# on the Mailpit host: a username and password for its web UI and API
MP_UI_AUTH=helmsman:a-long-random-password
```

Put Mailpit behind your reverse proxy with HTTPS (a hostname such as `mail.dev.example.com`), then enter that address with the username and password in Helmsman. The password is stored encrypted and is never shown again. SMTP port 1025 stays on the private network.

To start with Mailpit already connected, set `IDP_MAILPIT_URL` (and `IDP_MAILPIT_USER`, `IDP_MAILPIT_PASSWORD`) in the server's environment. An admin can still change it on the Integrations page.

## Who can read it

- The permission is `mail.view`. The `developer`, `operator` and `admin` roles have it, `viewer` and `approver` do not.
- Mail is not tied to one app. A shared Mailpit holds every app's mail, so the permission counts only when it is bound on **every app and every environment** (the same rule as the admin pages). A developer bound to a single app does not see Mail.
- Site admins always can.

## What you see

- A list of messages, newest first, refreshed every few seconds, with search. The search box takes Mailpit's syntax: plain words, `to:alice@example.com`, `from:`, `subject:`.
- The message: sender, recipients, date, the HTML as designed, the plain text, and attachments.

## How it stays safe

Development mail often carries real-looking links, tokens and sometimes real addresses, and an email's HTML is untrusted content. So:

- The HTML is shown in a **sandboxed frame** with no scripts, no access to Helmsman, and a policy that blocks every network load. Remote images and tracking pixels do not load. Links open in a new tab.
- **Attachments are always downloads**, never rendered in the page.
- Opening a message and downloading an attachment are **audited** (`mail.view`, `mail.attachment`) with the message id and never its content.
- Connecting, changing and removing Mailpit are audited too, without the password.
- Mailpit's address must be http or https, without credentials in the URL, and not a cloud metadata address.

## Try it locally

`make up` starts a Mailpit, connects it, and sends three sample emails (an order with HTML, a password reset, a receipt with an attachment). Sign in as `alice` or `bob` and open **Mail**. `carol` is a viewer and does not see it. `make seed-mail` sends the samples again when the inbox is empty. Mailpit's own UI is on http://localhost:8025 and its SMTP port on 1025, so you can point a local app at it.
