package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogCheck(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	var out, errOut bytes.Buffer
	if rc := catalogCheck([]string{write("ok.yaml", "version: 1\napps: []\n")}, &out, &errOut); rc != 0 || !strings.Contains(out.String(), "catalog ok") {
		t.Fatalf("empty catalog: rc=%d out=%q err=%q", rc, out.String(), errOut.String())
	}
	errOut.Reset()
	if rc := catalogCheck([]string{write("bad.yaml", "version: 1\napps: []\nnope: 1\n")}, &out, &errOut); rc != 1 || errOut.Len() == 0 {
		t.Fatalf("unknown field must fail: rc=%d err=%q", rc, errOut.String())
	}
	if rc := catalogCheck(nil, &out, &errOut); rc != 2 {
		t.Fatalf("no argument: rc=%d", rc)
	}
}
