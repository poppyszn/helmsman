#!/bin/sh
# Puts a few sample emails into the dev Mailpit so the Mail page has something to show. Safe to run again:
# it does nothing when the inbox already has mail. Runs in a throwaway curl container (make seed-mail).
set -eu
M="${MAILPIT_URL:-http://mailpit:8025}"
total=$(curl -fsS "$M/api/v1/messages?limit=1" | sed -n 's/.*"total":\([0-9]*\).*/\1/p')
if [ "${total:-0}" -gt 0 ]; then echo "mailpit already has $total messages"; exit 0; fi

send() { curl -fsS -X POST "$M/api/v1/send" -H 'Content-Type: application/json' -d "$1" >/dev/null; }

send '{
  "From": {"Email": "orders@shop.example.com", "Name": "Demo Shop"},
  "To": [{"Email": "alice@example.com", "Name": "Alice"}],
  "Subject": "Your order #1042 is confirmed",
  "Text": "Hi Alice,\n\nThanks for your order #1042. Total: 54.00 USD.\nTrack it: https://shop.example.com/orders/1042\n",
  "HTML": "<div style=\"font-family:sans-serif\"><h2>Thanks for your order, Alice</h2><p>Order <b>#1042</b> is confirmed.</p><table cellpadding=\"6\"><tr><td>Linen shirt</td><td>2 x 20.00</td></tr><tr><td>Shipping</td><td>14.00</td></tr><tr><td><b>Total</b></td><td><b>54.00 USD</b></td></tr></table><p><a href=\"https://shop.example.com/orders/1042\">Track your order</a></p><img src=\"https://tracker.example.com/pixel.gif\" width=\"1\" height=\"1\"><script>document.title=\"should never run\"</script></div>",
  "Tags": ["orders"]
}'
send '{
  "From": {"Email": "noreply@shop.example.com", "Name": "Demo Shop"},
  "To": [{"Email": "bob@example.com"}],
  "Subject": "Reset your password",
  "Text": "Someone asked to reset the password for bob@example.com.\nUse this link within one hour: https://shop.example.com/reset?token=dev-token-123\n",
  "HTML": "<p>Someone asked to reset the password for <b>bob@example.com</b>.</p><p><a href=\"https://shop.example.com/reset?token=dev-token-123\">Choose a new password</a> (valid for one hour).</p>",
  "Tags": ["auth"]
}'
send '{
  "From": {"Email": "billing@shop.example.com", "Name": "Demo Billing"},
  "To": [{"Email": "carol@example.com", "Name": "Carol"}],
  "Cc": [{"Email": "accounts@example.com"}],
  "Subject": "Receipt for invoice INV-2026-0007",
  "Text": "Your receipt is attached.\n",
  "Attachments": [{"Filename": "receipt-INV-2026-0007.txt", "Content": "SW52b2ljZSBJTlYtMjAyNi0wMDA3ClRvdGFsOiAxMjAuMDAgVVNECg=="}],
  "Tags": ["billing"]
}'
echo "seeded 3 sample messages"
