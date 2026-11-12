package main

import (
	"fmt"
	"io"

	"github.com/poppyszn/helmsman/internal/catalog"
)

// catalogCheck is "server catalog-check <file>": it parses and validates a catalog file and prints every problem.
// Agents are created on the Agents page, so a file cannot be checked against them here and any agent id is accepted.
// It exists so a repository that holds the catalog can fail a pull request before a bad file reaches the server.
func catalogCheck(args []string, out, errOut io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(errOut, "usage: server catalog-check <apps.yaml>")
		return 2
	}
	c, _, err := catalog.LoadFile(args[0])
	if err != nil {
		fmt.Fprintln(errOut, "catalog:", err)
		return 1
	}
	if errs := catalog.Validate(c, func(string) bool { return true }); len(errs) > 0 {
		fmt.Fprintln(errOut, "catalog:", catalog.JoinErrors(errs))
		return 1
	}
	fmt.Fprintf(out, "catalog ok: %d apps\n", len(c.Apps))
	return 0
}
