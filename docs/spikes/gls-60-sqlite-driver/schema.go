// Package sqlitedriver holds a throwaway benchmark comparing mattn/go-sqlite3
// (cgo) against modernc.org/sqlite (pure Go) on a schema shaped like Gloss's
// projection: reviews with an indexed set of comments, bulk-loaded the way a
// from-scratch refold would, then read back by the index a live query would
// use.
package sqlitedriver

const schema = `
CREATE TABLE reviews (
	id     TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	base   TEXT NOT NULL,
	head   TEXT NOT NULL
);
CREATE TABLE comments (
	id         TEXT PRIMARY KEY,
	review_id  TEXT NOT NULL,
	author     TEXT NOT NULL,
	body       TEXT NOT NULL,
	blob_hash  TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	resolved   INTEGER NOT NULL
);
CREATE INDEX idx_comments_review_id ON comments(review_id);
`

// Sizes chosen to sit in the neighborhood of an imported PR history for an
// active repo: a few thousand reviews, tens of thousands of comments.
const (
	numReviews        = 5000
	commentsPerReview = 20
	numComments       = numReviews * commentsPerReview
)
