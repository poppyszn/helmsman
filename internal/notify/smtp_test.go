package notify

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/poppyszn/helmsman/internal/store"
)

func TestSMTPSettingsAndSending(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; run via make test")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.Pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `INSERT INTO users (github_id, login, name) VALUES (1, 'alice', 'Alice')`); err != nil {
		t.Fatal(err)
	}
	var uid int64
	if err := st.Pool.QueryRow(ctx, `SELECT id FROM users WHERE login = 'alice'`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	svc := New(st, bytes.Repeat([]byte{7}, 32), "https://portal.example.com", slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.TestSMTP(ctx, "a@example.com"); err == nil || !strings.Contains(err.Error(), "no mail server") {
		t.Fatalf("sending before setup says so: %v", err)
	}
	addr, got := fakeSMTP(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	pass := "hunter2"
	if err := svc.SaveSMTP(ctx, store.User{ID: uid}, SMTPInput{Host: host, Port: port, Security: "none", From: "Helmsman <helmsman@example.com>", Password: &pass}); err != nil {
		t.Fatal(err)
	}
	status, _ := svc.SMTP(ctx)
	if !status.Configured || !status.HasPassword || status.Host != host || status.UpdatedBy != "alice" {
		t.Fatalf("status: %+v", status)
	}
	var raw string
	if err := st.Pool.QueryRow(ctx, `SELECT encode(secret_enc, 'escape') FROM integrations WHERE kind = 'smtp'`).Scan(&raw); err != nil || strings.Contains(raw, pass) {
		t.Fatalf("the password must be stored encrypted: %v", err)
	}
	if err := svc.TestSMTP(ctx, "dev@example.com\r\nBcc: evil@example.com"); err == nil {
		t.Fatal("a recipient with a line break must be refused")
	}
	if err := svc.TestSMTP(ctx, "dev@example.com"); err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case m := <-got:
		if !strings.Contains(m, "RCPT TO:<dev@example.com>") || !strings.Contains(m, "Subject: [Helmsman] Test email") || !strings.Contains(m, "From: \"Helmsman\" <helmsman@example.com>") {
			t.Fatalf("message:\n%s", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no message arrived")
	}
	// Keeping the saved password when none is submitted.
	if err := svc.SaveSMTP(ctx, store.User{ID: uid}, SMTPInput{Host: host, Port: port, Security: "none", From: "helmsman@example.com"}); err != nil {
		t.Fatal(err)
	}
	if st2, _ := svc.SMTP(ctx); !st2.HasPassword {
		t.Fatal("a nil password keeps the stored one")
	}
	if err := svc.RemoveSMTP(ctx); err != nil {
		t.Fatal(err)
	}
	if st3, _ := svc.SMTP(ctx); st3.Configured {
		t.Fatal("removed")
	}
}
