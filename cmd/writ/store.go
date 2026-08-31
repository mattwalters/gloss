package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/identity"
)

type notFoundError struct {
	id string
}

func (e notFoundError) Error() string {
	return fmt.Sprintf("no review with id %s", e.id)
}

func (e notFoundError) Unwrap() error {
	return writ.ErrNotFound
}

func openStore(dir string) (*writ.Store, error) {
	if dir == "" {
		dir = "."
	}
	return writ.Open(dir)
}

func renderErr(w io.Writer, err error) int {
	if err == nil {
		return 0
	}

	var cfgErr *identity.ConfigError
	if errors.As(err, &cfgErr) {
		fmt.Fprintf(w, "writ: %v\n", cfgErr)
		return 1
	}

	if errors.Is(err, writ.ErrNoIdentity) {
		fmt.Fprintln(w, "writ: no writer identity configured (run 'writ init' to configure)")
		return 1
	}

	if errors.Is(err, writ.ErrNoSigningKey) {
		fmt.Fprintln(w, "writ: no signing key configured (run 'writ init' to configure)")
		return 1
	}

	if errors.Is(err, writ.ErrNotFound) {
		var nf notFoundError
		if errors.As(err, &nf) {
			fmt.Fprintf(w, "writ: %s\n", nf.Error())
			return 1
		}
	}

	msg := err.Error()
	if !strings.HasPrefix(msg, "writ: ") {
		msg = "writ: " + msg
	}
	fmt.Fprintln(w, msg)
	return 1
}

func resolveReviewID(ctx context.Context, store *writ.Store, prefix string) (string, error) {
	if prefix == "" {
		return "", fmt.Errorf("review ID required")
	}

	reviews, err := store.Query.Reviews(writ.ReviewFilter{})
	if err != nil {
		return "", err
	}

	var matches []string
	for _, r := range reviews {
		if strings.HasPrefix(r.ObjectID, prefix) {
			matches = append(matches, r.ObjectID)
		}
	}

	if len(matches) == 0 {
		return "", notFoundError{id: prefix}
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous review ID prefix %q matches %d reviews (%s)", prefix, len(matches), strings.Join(matches, ", "))
	}

	return matches[0], nil
}

func gitRevParse(ctx context.Context, dir, ref string) (string, error) {
	if dir == "" {
		dir = "."
	}
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", ref+"^{commit}")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback to plain rev-parse if ^{commit} failed (e.g. if ref is already an OID)
		cmdPlain := exec.CommandContext(ctx, "git", "rev-parse", "--verify", ref)
		cmdPlain.Dir = dir
		outPlain, errPlain := cmdPlain.CombinedOutput()
		if errPlain != nil {
			return "", fmt.Errorf("resolve ref %q: %v (%s)", ref, err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(outPlain)), nil
	}
	return strings.TrimSpace(string(out)), nil
}
