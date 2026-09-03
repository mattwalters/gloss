package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/engine/dag"
	"github.com/writtendev/writ/engine/identity"
)

func TestDoc_CLI_Commands(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed with %d; stderr: %s", code, stderr.String())
	}

	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	// 1. writ doc create
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"doc", "create", "-C", env.repoDir,
		"-t", "RFC: Document Object",
		"--label", "design",
		"--label", "spec",
		"--link", "issue-1:plan:issue",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc create failed with %d; stderr: %s", code, stderr.String())
	}
	docID := strings.TrimSpace(stdout.String())
	if docID == "" {
		t.Fatalf("doc create returned empty ID")
	}

	// 2. writ doc list
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"doc", "list", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc list failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "RFC: Document Object") {
		t.Fatalf("doc list missing created doc: %s", stdout.String())
	}

	// 3. writ doc list --json
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"doc", "list", "-C", env.repoDir, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc list --json failed with %d; stderr: %s", code, stderr.String())
	}
	var listEnv wire.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &listEnv); err != nil {
		t.Fatalf("unmarshal list json: %v", err)
	}
	if listEnv.Kind != wire.KindDocList {
		t.Fatalf("expected kind %s, got %s", wire.KindDocList, listEnv.Kind)
	}

	// 4. writ doc edit
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"doc", "edit", "-C", env.repoDir, docID,
		"-t", "RFC: Document Object (v2)",
		"--label", "approved",
		"--remove-label", "design",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc edit failed with %d; stderr: %s", code, stderr.String())
	}

	// 5. writ doc link
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"doc", "link", "-C", env.repoDir, docID,
		"--target", "issue-2",
		"--relation", "implements",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc link failed with %d; stderr: %s", code, stderr.String())
	}

	// 6. writ doc section add (section 1)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"doc", "section", "add", "-C", env.repoDir, docID,
		"-t", "Motivation",
		"-m", "We need documents in git.",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc section add 1 failed with %d; stderr: %s", code, stderr.String())
	}
	sec1ID := strings.TrimSpace(stdout.String())

	// 7. writ doc section add (section 2)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"doc", "section", "add", "-C", env.repoDir, docID,
		"-t", "Design",
		"-m", "Document details.",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc section add 2 failed with %d; stderr: %s", code, stderr.String())
	}
	sec2ID := strings.TrimSpace(stdout.String())

	// 8. writ doc section move (move section 2 before section 1)
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"doc", "section", "move", "-C", env.repoDir, sec2ID,
		"--before", sec1ID,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc section move failed with %d; stderr: %s", code, stderr.String())
	}

	// 9. writ doc section edit: test updating title only via -t
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"doc", "section", "edit", "-C", env.repoDir, sec1ID,
		"-t", "Motivation & Background",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc section edit -t failed with %d; stderr: %s", code, stderr.String())
	}

	// Also test editing both -t and -m
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"doc", "section", "edit", "-C", env.repoDir, sec1ID,
		"-t", "Motivation",
		"-m", "Updated motivation text.",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc section edit -t -m failed with %d; stderr: %s", code, stderr.String())
	}

	// 10. writ doc show
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"doc", "show", "-C", env.repoDir, docID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc show failed with %d; stderr: %s", code, stderr.String())
	}
	showOut := stdout.String()
	if !strings.Contains(showOut, "RFC: Document Object (v2)") {
		t.Fatalf("doc show missing updated title: %s", showOut)
	}
	if !strings.Contains(showOut, "Updated motivation text.") {
		t.Fatalf("doc show missing updated section body: %s", showOut)
	}
	// Section 2 should appear before section 1 in the output
	posSec2 := strings.Index(showOut, "Design")
	posSec1 := strings.Index(showOut, "Motivation")
	if posSec2 == -1 || posSec1 == -1 || posSec2 > posSec1 {
		t.Fatalf("expected Design before Motivation, got indices %d and %d in:\n%s", posSec2, posSec1, showOut)
	}

	// 11. writ doc show --json
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"doc", "show", "-C", env.repoDir, docID, "--json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc show --json failed with %d; stderr: %s", code, stderr.String())
	}
	var showEnv wire.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &showEnv); err != nil {
		t.Fatalf("unmarshal show json: %v", err)
	}
	if showEnv.Kind != wire.KindDocShow {
		t.Fatalf("expected kind %s, got %s", wire.KindDocShow, showEnv.Kind)
	}

	// 12. writ doc section delete
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"doc", "section", "delete", "-C", env.repoDir, sec2ID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc section delete failed with %d; stderr: %s", code, stderr.String())
	}

	// Verify deleted section is gone from doc show
	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"doc", "show", "-C", env.repoDir, docID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc show after delete failed with %d; stderr: %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "## Design") {
		t.Fatalf("deleted section still present in doc show: %s", stdout.String())
	}
}

func TestDocShow_UntitledShortSectionID(t *testing.T) {
	env := setupTestCLIEnv(t)
	setupSigningKey(t, env.repoDir)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"init", "-C", env.repoDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("writ init failed with %d; stderr: %s", code, stderr.String())
	}
	commitFile(t, env.repoDir, "README.md", "# Hello", "initial commit")

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{
		"doc", "create", "-C", env.repoDir,
		"-t", "Doc With Short Section ID",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc create failed with %d; stderr: %s", code, stderr.String())
	}
	docID := strings.TrimSpace(stdout.String())

	ident, err := identity.Load(context.Background(), env.repoDir)
	if err != nil {
		t.Fatalf("identity.Load: %v", err)
	}
	dagStore, err := dag.Open(env.repoDir, ident)
	if err != nil {
		t.Fatalf("dag.Open: %v", err)
	}
	bodyBytes, _ := json.Marshal(map[string]any{
		"document_id": docID,
		"position":    "a1",
		"body":        "Short object ID section body.",
	})
	secEnv := codec.Envelope{
		ObjectID:   "sec-1",
		ObjectType: "section",
		OpType:     "create",
		OpVersion:  1,
		Body:       bodyBytes,
	}
	if _, err := dagStore.Append(context.Background(), secEnv, nil); err != nil {
		t.Fatalf("dagStore.Append: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	code = run(context.Background(), []string{"doc", "show", "-C", env.repoDir, docID}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("doc show failed with %d; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "## Section sec-1") {
		t.Fatalf("expected '## Section sec-1' in output, got:\n%s", stdout.String())
	}
}
