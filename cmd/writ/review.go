package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/writtendev/writ/engine"
)

type stringSliceFlag []string

func (f *stringSliceFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringSliceFlag) Set(val string) error {
	*f = append(*f, val)
	return nil
}

func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var posArgs []string
	remaining := args
	for len(remaining) > 0 {
		if err := fs.Parse(remaining); err != nil {
			return nil, err
		}
		if len(fs.Args()) > 0 {
			posArgs = append(posArgs, fs.Args()[0])
			remaining = fs.Args()[1:]
		} else {
			break
		}
	}
	return posArgs, nil
}

func runReview(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printReviewUsage(stderr)
		return 2
	}

	switch args[0] {
	case "-h", "-help", "--help", "help":
		printReviewUsage(stdout)
		return 0
	}

	targetDir := defaultDir
	if args[0] == "-C" {
		if len(args) < 2 {
			fmt.Fprintln(stderr, "writ review: option -C requires an argument")
			return 2
		}
		targetDir = args[1]
		args = args[2:]
		if len(args) == 0 {
			printReviewUsage(stderr)
			return 2
		}
	}

	switch args[0] {
	case "-h", "-help", "--help", "help":
		printReviewUsage(stdout)
		return 0
	case "open":
		return runReviewOpen(ctx, targetDir, args[1:], stdout, stderr)
	case "comment":
		return runReviewComment(ctx, targetDir, args[1:], stdout, stderr)
	case "approve":
		return runReviewApprove(ctx, targetDir, args[1:], stdout, stderr)
	case "status":
		return runReviewStatus(ctx, targetDir, args[1:], stdout, stderr)
	case "list":
		return runReviewList(ctx, targetDir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "writ review: unknown subcommand %q\n\n", args[0])
		printReviewUsage(stderr)
		return 2
	}
}

func printReviewUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: writ review [-C <dir>] <subcommand> [arguments]

Manage code reviews.

Subcommands:
  open       Create a new code review
  comment    Add a comment to a review
  approve    Record a review verdict
  status     View or update review status
  list       List code reviews

Run 'writ review <subcommand> -h' for more information on a subcommand.
`)
}

func runReviewOpen(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review open", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var dir string
	var title string
	var description string
	var base string
	var head string
	var draft bool

	fs.StringVar(&dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&title, "title", "", "Review title")
	fs.StringVar(&description, "description", "", "Review description")
	fs.StringVar(&base, "base", "", "Base revision commit or ref")
	fs.StringVar(&head, "head", "", "Head revision commit or ref")
	fs.BoolVar(&draft, "draft", false, "Create review in draft state")

	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: writ review open [-C <dir>] -title <t> [-description <d>] [-base <ref> -head <ref>] [-draft]

Create a new code review.

Flags:
  -C <dir>           Run as if writ was started in <dir>
  -title <t>         Review title (required)
  -description <d>   Review description
  -base <ref>        Base revision commit or ref
  -head <ref>        Head revision commit or ref
  -draft             Create review in draft state
`)
	}

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ review open: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}

	if title == "" {
		fmt.Fprintln(stderr, "writ review open: -title is required")
		fs.Usage()
		return 2
	}

	if (base != "" && head == "") || (base == "" && head != "") {
		fmt.Fprintln(stderr, "writ review open: both -base and -head must be specified")
		fs.Usage()
		return 2
	}

	targetDir := dir
	if targetDir == "" {
		targetDir = "."
	}

	var baseOID, headOID string
	if base != "" && head != "" {
		bOID, err := gitRevParse(ctx, targetDir, base)
		if err != nil {
			return renderErr(stderr, err)
		}
		hOID, err := gitRevParse(ctx, targetDir, head)
		if err != nil {
			return renderErr(stderr, err)
		}
		baseOID = bOID
		headOID = hOID
	}

	store, err := openStore(targetDir)
	if err != nil {
		return renderErr(stderr, err)
	}
	defer store.Close()

	id, err := store.Reviews.Create(ctx, writ.NewReview{
		Title:       title,
		Description: description,
		Base:        baseOID,
		Head:        headOID,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	status := "open"
	if draft {
		status = "draft"
	}

	if err := store.Reviews.SetStatus(ctx, id, writ.ReviewStatus{Status: status}); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s (%s) %s\n", id, status, title)
	return 0
}

func runReviewComment(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review comment", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var dir string
	var message string
	var replyTo string

	fs.StringVar(&dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&message, "m", "", "Comment message text")
	fs.StringVar(&replyTo, "reply-to", "", "Comment ID to reply to")

	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: writ review comment [-C <dir>] <id> -m <text> [-reply-to <comment-id>]

Add a comment to a review.

Flags:
  -C <dir>                Run as if writ was started in <dir>
  -m <text>               Comment message text (required)
  -reply-to <comment-id>  Comment ID to reply to
`)
	}

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) == 0 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "writ review comment: review ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ review comment: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	if message == "" {
		fmt.Fprintln(stderr, "writ review comment: -m is required")
		fs.Usage()
		return 2
	}

	targetDir := dir
	if targetDir == "" {
		targetDir = "."
	}

	store, err := openStore(targetDir)
	if err != nil {
		return renderErr(stderr, err)
	}
	defer store.Close()

	reviewID, err := resolveReviewID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	var replyToID string
	if replyTo != "" {
		comments, err := store.Query.Comments(writ.CommentFilter{
			SubjectType:    "review",
			SubjectID:      reviewID,
			IncludeDeleted: true,
		})
		if err != nil {
			return renderErr(stderr, err)
		}
		var matches []string
		for _, c := range comments {
			if strings.HasPrefix(c.ObjectID, replyTo) {
				matches = append(matches, c.ObjectID)
			}
		}
		if len(matches) == 1 {
			replyToID = matches[0]
		} else if len(matches) > 1 {
			return renderErr(stderr, fmt.Errorf("ambiguous comment ID prefix %q matches %d comments (%s)", replyTo, len(matches), strings.Join(matches, ", ")))
		} else {
			replyToID = replyTo
		}
	}

	commentID, err := store.Reviews.Comment(ctx, reviewID, writ.NewComment{
		Text:      message,
		InReplyTo: replyToID,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintln(stdout, commentID)
	return 0
}

func runReviewApprove(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review approve", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var dir string
	var verdict string
	var revision string
	var message string
	var subject string

	fs.StringVar(&dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&verdict, "verdict", "approve", "Review verdict: approve, request-changes, or none")
	fs.StringVar(&revision, "revision", "", "Revision commit ref or SHA (defaults to latest head)")
	fs.StringVar(&message, "m", "", "Review verdict message")
	fs.StringVar(&subject, "subject", "", "Subject identity (defaults to writer email or writer ID)")

	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: writ review approve [-C <dir>] <id> [-verdict approve|request-changes|none] [-revision <ref>] [-m <msg>] [-subject <s>]

Record a review verdict.

Flags:
  -C <dir>                                         Run as if writ was started in <dir>
  -verdict approve|request-changes|none            Verdict (default: approve)
  -revision <ref>                                  Revision commit ref or SHA (defaults to latest head)
  -m <msg>                                         Verdict message
  -subject <s>                                     Subject identity (defaults to writer email or writer ID)
`)
	}

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) == 0 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "writ review approve: review ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ review approve: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	switch verdict {
	case "approve", "request-changes", "none":
		// valid
	default:
		fmt.Fprintf(stderr, "writ review approve: invalid verdict %q (must be approve, request-changes, or none)\n", verdict)
		fs.Usage()
		return 2
	}

	targetDir := dir
	if targetDir == "" {
		targetDir = "."
	}

	var revOID string
	if revision != "" {
		rOID, err := gitRevParse(ctx, targetDir, revision)
		if err != nil {
			return renderErr(stderr, err)
		}
		revOID = rOID
	}

	store, err := openStore(targetDir)
	if err != nil {
		return renderErr(stderr, err)
	}
	defer store.Close()

	reviewID, err := resolveReviewID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	if subject == "" {
		writer := store.Writer()
		subject = writer.Email
		if subject == "" {
			subject = writer.ID
		}
	}

	if err := store.Reviews.Approve(ctx, reviewID, writ.Approval{
		Verdict:  verdict,
		Revision: revOID,
		Message:  message,
		Subject:  subject,
	}); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: recorded %s\n", reviewID, verdict)
	return 0
}

func runReviewStatus(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review status", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var dir string
	var reason string
	var mergeCommit string

	fs.StringVar(&dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&reason, "reason", "", "Reason for status transition")
	fs.StringVar(&mergeCommit, "merge-commit", "", "Merge commit ref or SHA (valid with status merged)")

	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: writ review status [-C <dir>] <id> [<state>] [-reason <r>] [-merge-commit <ref>]

View or update review status.

States:
  draft, open, closed, merged

Flags:
  -C <dir>              Run as if writ was started in <dir>
  -reason <r>           Reason for status change
  -merge-commit <ref>   Merge commit ref or SHA (valid when setting status to merged)
`)
	}

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) == 0 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "writ review status: review ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 2 {
		fmt.Fprintf(stderr, "writ review status: unexpected arguments: %s\n", strings.Join(posArgs[2:], " "))
		fs.Usage()
		return 2
	}

	var newState string
	if len(posArgs) == 1 {
		if mergeCommit != "" {
			fmt.Fprintln(stderr, "writ review status: -merge-commit is only valid when setting status to merged")
			fs.Usage()
			return 2
		}
		if reason != "" {
			fmt.Fprintln(stderr, "writ review status: -reason is only valid when setting status")
			fs.Usage()
			return 2
		}
	} else {
		newState = posArgs[1]
		switch newState {
		case "draft", "open", "closed", "merged":
			// valid
		default:
			fmt.Fprintf(stderr, "writ review status: invalid status %q (must be draft, open, closed, or merged)\n", newState)
			fs.Usage()
			return 2
		}

		if mergeCommit != "" && newState != "merged" {
			fmt.Fprintln(stderr, "writ review status: -merge-commit is only valid when setting status to merged")
			fs.Usage()
			return 2
		}
	}

	targetDir := dir
	if targetDir == "" {
		targetDir = "."
	}

	store, err := openStore(targetDir)
	if err != nil {
		return renderErr(stderr, err)
	}
	defer store.Close()

	reviewID, err := resolveReviewID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	// 1. Read / View status mode (len(posArgs) == 1)
	if len(posArgs) == 1 {
		res, err := store.Query.Review(reviewID)
		if err != nil {
			return renderErr(stderr, err)
		}

		status := res.Review.Status
		if status == "" {
			status = "-"
		}
		author := res.Author.Name
		if author == "" {
			author = res.Author.Email
		} else if res.Author.Email != "" {
			author = fmt.Sprintf("%s <%s>", res.Author.Name, res.Author.Email)
		}
		if author == "" {
			author = "-"
		}

		fmt.Fprintf(stdout, "Review:      %s\n", res.ObjectID)
		fmt.Fprintf(stdout, "Title:       %s\n", res.Review.Title)
		fmt.Fprintf(stdout, "Status:      %s\n", status)
		fmt.Fprintf(stdout, "Author:      %s\n", author)
		fmt.Fprintf(stdout, "Revisions:   %d\n", len(res.Review.Revisions))
		fmt.Fprintf(stdout, "Approvals:   %d\n", len(res.Review.Approvals))
		fmt.Fprintf(stdout, "CI Checks:   %d\n", len(res.Review.CIStatuses))
		if res.Review.MergeCommit != "" {
			fmt.Fprintf(stdout, "Merge commit: %s\n", res.Review.MergeCommit)
		}
		if res.Review.Reason != "" {
			fmt.Fprintf(stdout, "Reason:       %s\n", res.Review.Reason)
		}
		return 0
	}

	// 2. Update / Transition status mode (len(posArgs) == 2)
	// Check current status: refuse transition out of merged
	current, err := store.Query.Review(reviewID)
	if err != nil {
		return renderErr(stderr, err)
	}
	if current.Review.Status == "merged" {
		fmt.Fprintf(stderr, "writ review status: cannot transition review %s out of \"merged\" status\n", reviewID)
		return 1
	}

	var mergeCommitOID string
	if mergeCommit != "" {
		mOID, err := gitRevParse(ctx, targetDir, mergeCommit)
		if err != nil {
			return renderErr(stderr, err)
		}
		mergeCommitOID = mOID
	}

	if err := store.Reviews.SetStatus(ctx, reviewID, writ.ReviewStatus{
		Status:      newState,
		Reason:      reason,
		MergeCommit: mergeCommitOID,
	}); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: %s\n", reviewID, newState)
	return 0
}

func runReviewList(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review list", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var dir string
	var statuses stringSliceFlag
	var authors stringSliceFlag
	var text string
	var limit int
	var sortOrder string

	fs.StringVar(&dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.Var(&statuses, "status", "Filter by review status (repeatable: -status open -status draft)")
	fs.Var(&authors, "author", "Filter by author name or email (repeatable)")
	fs.StringVar(&text, "text", "", "Filter by title or description text query")
	fs.IntVar(&limit, "limit", 0, "Maximum number of reviews to return")
	fs.StringVar(&sortOrder, "sort", "", "Sort order: created_at_asc, created_at_desc, updated_at_asc, updated_at_desc, title_asc, title_desc")

	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: writ review list [-C <dir>] [-status <s>]... [-author <a>]... [-text <q>] [-limit N] [-sort <order>]

List code reviews.

Flags:
  -C <dir>         Run as if writ was started in <dir>
  -status <s>      Filter by review status (repeatable)
  -author <a>      Filter by author name or email (repeatable)
  -text <q>        Filter by text match in title or description
  -limit N         Maximum number of reviews to return
  -sort <order>    Sort order (created_at_asc, created_at_desc, updated_at_asc, updated_at_desc, title_asc, title_desc)
`)
	}

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ review list: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}

	if limit < 0 {
		fmt.Fprintf(stderr, "writ review list: -limit must be non-negative, got %d\n", limit)
		fs.Usage()
		return 2
	}

	var orderBy writ.OrderBy
	if sortOrder != "" {
		switch sortOrder {
		case "created_at_asc", "created-asc", "created_asc", "created":
			orderBy = writ.OrderByCreatedAtAsc
		case "created_at_desc", "created-desc", "created_desc":
			orderBy = writ.OrderByCreatedAtDesc
		case "updated_at_asc", "updated-asc", "updated_asc":
			orderBy = writ.OrderByUpdatedAtAsc
		case "updated_at_desc", "updated-desc", "updated_desc", "updated":
			orderBy = writ.OrderByUpdatedAtDesc
		case "title_asc", "title-asc", "title":
			orderBy = writ.OrderByTitleAsc
		case "title_desc", "title-desc":
			orderBy = writ.OrderByTitleDesc
		default:
			fmt.Fprintf(stderr, "writ review list: invalid sort order %q\n", sortOrder)
			fs.Usage()
			return 2
		}
	}

	targetDir := dir
	if targetDir == "" {
		targetDir = "."
	}

	store, err := openStore(targetDir)
	if err != nil {
		return renderErr(stderr, err)
	}
	defer store.Close()

	reviews, err := store.Query.Reviews(writ.ReviewFilter{
		Status:  statuses,
		Author:  authors,
		Text:    text,
		Limit:   limit,
		OrderBy: orderBy,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, r := range reviews {
		shortID := r.ObjectID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		status := r.Review.Status
		if status == "" {
			status = "-"
		}
		author := r.Author.Name
		if author == "" {
			author = r.Author.Email
		}
		if author == "" {
			author = "-"
		}
		updatedAt := r.UpdatedAt.Format("2006-01-02 15:04:05")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", shortID, status, r.Review.Title, author, updatedAt)
	}
	_ = tw.Flush()
	return 0
}
