// Package writ provides the public Go API for Writ: a git-native collaborative
// SDLC engine for code reviews, issues, and discussions.
//
// The API is domain-shaped, never git-shaped — callers interact with high-level
// operations and queries without dealing with git commit SHAs, refspecs, or
// internal transport machinery unless explicitly requested.
//
// All client layers — CLI, downstream TUIs/viewers, GitHub bridges, and hosted services — build
// on this single interface.
//
// Basic usage:
//
//	store, err := writ.Open("/path/to/repo")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer store.Close()
//
//	// Create a code review
//	reviewID, err := store.Reviews.Create(ctx, writ.NewReview{
//	    Title: "Add OAuth2 authentication provider",
//	    Base:  "main",
//	    Head:  "feature/oauth2",
//	})
//
//	// Query reviews
//	reviews, err := store.Query.Reviews(writ.ReviewFilter{
//	    Status: []string{"open"},
//	})
//
//	// Synchronize with git remote
//	syncResult, err := store.Sync(ctx, "origin")
package writ
