package writ_test

import (
	"context"
	"fmt"
	"log"

	"github.com/writtendev/writ/engine"
)

func ExampleOpen() {
	// Open a repository at the specified working tree or subdirectory path.
	store, err := writ.Open(".")
	if err != nil {
		log.Printf("open repo: %v", err)
		return
	}
	defer store.Close()

	// Access writer identity
	writer := store.Writer()
	fmt.Printf("Active writer: %s\n", writer.Name)
}

func ExampleReviews_Create() {
	ctx := context.Background()
	store, err := writ.Open(".")
	if err != nil {
		log.Printf("open repo: %v", err)
		return
	}
	defer store.Close()

	// Create a new code review
	reviewID, err := store.Reviews.Create(ctx, writ.NewReview{
		Title:       "Add OAuth2 authentication provider",
		Description: "Implements Google and GitHub OAuth2 flows",
		Base:        "main",
		Head:        "feature/oauth2",
	})
	if err != nil {
		log.Printf("create review: %v", err)
		return
	}

	fmt.Printf("Created review: %s\n", reviewID)
}

func ExampleQuery_Reviews() {
	store, err := writ.Open(".")
	if err != nil {
		log.Printf("open repo: %v", err)
		return
	}
	defer store.Close()

	// Query open reviews
	reviews, err := store.Query.Reviews(writ.ReviewFilter{
		Status:  []string{"open"},
		OrderBy: writ.OrderByUpdatedAtDesc,
	})
	if err != nil {
		log.Printf("query reviews: %v", err)
		return
	}

	for _, r := range reviews {
		fmt.Printf("Review %s: %s (status: %s)\n", r.ObjectID, r.Review.Title, r.Review.Status)
	}
}
