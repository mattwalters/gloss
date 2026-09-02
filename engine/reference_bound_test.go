package writ_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	writ "github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/codec"
)

// TestLinkTargetLengthBoundIsEnforcedOnTheProducerPath answers the question
// WRIT-136 asked about the reference bound: does writ's own producer stop
// itself writing an op the schema would reject?
//
// It does, and not because anything here guards it. codec.BuildCommit — the
// sole commit constructor — validates every op body against its vocabulary
// schema (WRIT-129, PR #100), and review-ops.schema.json's link_body.target
// $refs identifiers.schema.json#/$defs/reference, whose maxLength is 513. So
// the bound reaches the write path through the schema the fixtures already
// pin, with no second copy of the number in Go. That is the outcome to
// prefer: a reference-length constant in engine/ would be a rule stated
// twice, free to drift from the one the corpus enforces.
//
// requireCommitOID next door is the case that does earn a domain-side guard,
// and the difference is instructive. "main" is a value a caller types on
// purpose, and the fix — resolve the ref first — is only obvious if the error
// names the field. A 514-code-point reference is not something anyone types;
// it is a program handing over a string it never bounded, and a schema
// violation is a truthful answer to it.
//
// The test is written against behaviour rather than against that reasoning:
// it goes through the public API, checks both sides of the bound, and checks
// that the refusal left nothing behind.
func TestLinkTargetLengthBoundIsEnforcedOnTheProducerPath(t *testing.T) {
	repoDir, _ := setupConfiguredRepo(t)
	s, err := writ.Open(repoDir, writ.WithSigner(dummySigner()))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	reviewID, err := s.Reviews.Create(ctx, writ.NewReview{Title: "Reference bound"})
	if err != nil {
		t.Fatalf("Create review failed: %v", err)
	}

	// 513 code points and 1026 bytes. A producer counting octets would refuse
	// this one, so accepting it is what pins the unit on the write path the
	// way spec/testdata/references/valid/at-limit-multibyte.json pins it in
	// the corpus.
	atLimit := strings.Repeat("é", 513)
	if got := len([]rune(atLimit)); got != 513 || len(atLimit) != 1026 {
		t.Fatalf("test setup: atLimit is %d code points / %d bytes, want 513 / 1026", got, len(atLimit))
	}
	if err := s.Reviews.Link(ctx, reviewID, writ.Link{
		Target:     atLimit,
		TargetType: "issue",
		Relation:   "fixes",
	}); err != nil {
		t.Fatalf("a 513-code-point target is inside the bound and must be accepted: %v", err)
	}

	// 514 code points, one over.
	overLimit := strings.Repeat("a", 514)
	err = s.Reviews.Link(ctx, reviewID, writ.Link{
		Target:     overLimit,
		TargetType: "issue",
		Relation:   "fixes",
	})
	if err == nil {
		t.Fatal("a 514-code-point target is over the bound and must be refused")
	}
	var reject *codec.RejectError
	if !errors.As(err, &reject) {
		t.Errorf("refusal should be a codec reject, got %T: %v", err, err)
	} else if reject.Reason != codec.RejectSchemaViolation {
		t.Errorf("reject reason = %q, want %q", reject.Reason, codec.RejectSchemaViolation)
	}

	// The refusal must be total. An op log that recorded the over-long link
	// and then reported an error would have written the very thing the bound
	// exists to keep out of the repository.
	res, err := s.Query.Review(reviewID)
	if err != nil {
		t.Fatalf("Query.Review failed: %v", err)
	}
	if len(res.Review.Links) != 1 {
		t.Fatalf("expected the one accepted link, got %d: %+v", len(res.Review.Links), res.Review.Links)
	}
	if res.Review.Links[0].Target != atLimit {
		t.Errorf("surviving link target is not the accepted one: %d code points",
			len([]rune(res.Review.Links[0].Target)))
	}
}
