package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/identity"
	"github.com/writtendev/writ/engine/state"
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
		renderUsage(stderr, []string{"review"}, reviewCmd)
		return 2
	}

	switch args[0] {
	case "-h", "-help", "--help":
		renderUsage(stdout, []string{"review"}, reviewCmd)
		return 0
	case "help":
		return runHelp(append([]string{"review"}, args[1:]...), stdout, stderr)
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
			renderUsage(stderr, []string{"review"}, reviewCmd)
			return 2
		}
	}

	switch args[0] {
	case "-h", "-help", "--help":
		renderUsage(stdout, []string{"review"}, reviewCmd)
		return 0
	case "help":
		return runHelp(append([]string{"review"}, args[1:]...), stdout, stderr)
	case "open":
		return runReviewOpen(ctx, targetDir, args[1:], stdout, stderr)
	case "comment":
		return runReviewComment(ctx, targetDir, args[1:], stdout, stderr)
	case "approve":
		return runReviewApprove(ctx, targetDir, args[1:], stdout, stderr)
	case "assign":
		return runReviewAssign(ctx, targetDir, args[1:], stdout, stderr)
	case "label":
		return runReviewLabel(ctx, targetDir, args[1:], stdout, stderr)
	case "link":
		return runReviewLink(ctx, targetDir, args[1:], stdout, stderr)
	case "status":
		return runReviewStatus(ctx, targetDir, args[1:], stdout, stderr)
	case "list":
		return runReviewList(ctx, targetDir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "writ review: unknown subcommand %q\n\n", args[0])
		renderUsage(stderr, []string{"review"}, reviewCmd)
		return 2
	}
}

type reviewOpenOpts struct {
	dir         string
	title       string
	description string
	base        string
	head        string
	draft       bool
}

func newReviewOpenFlagSet(defaultDir string) (*flag.FlagSet, *reviewOpenOpts) {
	fs := flag.NewFlagSet("review open", flag.ContinueOnError)
	opts := &reviewOpenOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&opts.title, "title", "", "Review title `<t>` (required)")
	fs.StringVar(&opts.description, "description", "", "Review description `<d>`")
	fs.StringVar(&opts.base, "base", "", "Base revision `<ref>` commit or ref")
	fs.StringVar(&opts.head, "head", "", "Head revision `<ref>` commit or ref")
	fs.BoolVar(&opts.draft, "draft", false, "Create review in draft state")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"review", "open"}, reviewOpenCmd)
	}
	return fs, opts
}

func runReviewOpen(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newReviewOpenFlagSet(defaultDir)
	fs.SetOutput(stderr)

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

	if opts.title == "" {
		fmt.Fprintln(stderr, "writ review open: -title is required")
		fs.Usage()
		return 2
	}

	if (opts.base != "" && opts.head == "") || (opts.base == "" && opts.head != "") {
		fmt.Fprintln(stderr, "writ review open: both -base and -head must be specified")
		fs.Usage()
		return 2
	}

	targetDir := opts.dir
	if targetDir == "" {
		targetDir = "."
	}

	var baseOID, headOID string
	if opts.base != "" && opts.head != "" {
		bOID, err := gitRevParse(ctx, targetDir, opts.base)
		if err != nil {
			return renderErr(stderr, err)
		}
		hOID, err := gitRevParse(ctx, targetDir, opts.head)
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
		Title:       opts.title,
		Description: opts.description,
		Base:        baseOID,
		Head:        headOID,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	status := "open"
	if opts.draft {
		status = "draft"
	}

	if err := store.Reviews.SetStatus(ctx, id, writ.ReviewStatus{Status: status}); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s (%s) %s\n", id, status, opts.title)
	return 0
}

type reviewCommentOpts struct {
	dir       string
	message   string
	replyTo   string
	resolve   bool
	unresolve bool
}

func newReviewCommentFlagSet(defaultDir string) (*flag.FlagSet, *reviewCommentOpts) {
	fs := flag.NewFlagSet("review comment", flag.ContinueOnError)
	opts := &reviewCommentOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&opts.message, "m", "", "Comment message text `<text>`")
	fs.StringVar(&opts.replyTo, "reply-to", "", "Comment ID `<comment-id>` to reply to")
	fs.BoolVar(&opts.resolve, "resolve", false, "Mark comment thread as resolved")
	fs.BoolVar(&opts.unresolve, "unresolve", false, "Mark comment thread as unresolved")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"review", "comment"}, reviewCommentCmd)
	}
	return fs, opts
}

func runReviewComment(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newReviewCommentFlagSet(defaultDir)
	fs.SetOutput(stderr)

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

	if opts.resolve && opts.unresolve {
		fmt.Fprintln(stderr, "writ review comment: cannot specify both -resolve and -unresolve")
		fs.Usage()
		return 2
	}

	if opts.message == "" && !opts.resolve && !opts.unresolve {
		fmt.Fprintln(stderr, "writ review comment: -m is required (or specify -resolve / -unresolve)")
		fs.Usage()
		return 2
	}

	targetDir := opts.dir
	if targetDir == "" {
		targetDir = "."
	}

	store, err := openStore(targetDir)
	if err != nil {
		return renderErr(stderr, err)
	}
	defer store.Close()

	reviewID, err := resolveReviewID(ctx, store, posArgs[0])
	var directCommentID string
	if err != nil {
		// Check if posArgs[0] is a comment ID prefix
		comments, cErr := store.Query.Comments(writ.CommentFilter{IncludeDeleted: true})
		if cErr == nil {
			var matches []writ.CommentResult
			for _, c := range comments {
				if strings.HasPrefix(c.ObjectID, posArgs[0]) {
					matches = append(matches, c)
				}
			}
			if len(matches) == 1 {
				reviewID = matches[0].Comment.Subject.ObjectID
				directCommentID = matches[0].ObjectID
				err = nil
			} else if len(matches) > 1 {
				return renderErr(stderr, fmt.Errorf("ambiguous ID prefix %q matches multiple comments", posArgs[0]))
			}
		}
	}
	if err != nil {
		return renderErr(stderr, err)
	}

	targetCommentLookup := opts.replyTo
	if targetCommentLookup == "" && directCommentID != "" {
		targetCommentLookup = directCommentID
	}

	var replyToID string
	var threadRootID string

	if targetCommentLookup != "" {
		comments, err := store.Query.Comments(writ.CommentFilter{
			SubjectType:    "review",
			SubjectID:      reviewID,
			IncludeDeleted: true,
		})
		if err != nil {
			return renderErr(stderr, err)
		}
		var matches []writ.CommentResult
		for _, c := range comments {
			if strings.HasPrefix(c.ObjectID, targetCommentLookup) {
				matches = append(matches, c)
			}
		}
		var matchedComment writ.CommentResult
		if len(matches) == 1 {
			matchedComment = matches[0]
			replyToID = matches[0].ObjectID
		} else if len(matches) > 1 {
			var matchIDs []string
			for _, m := range matches {
				matchIDs = append(matchIDs, m.ObjectID)
			}
			return renderErr(stderr, fmt.Errorf("ambiguous comment ID prefix %q matches %d comments (%s)", targetCommentLookup, len(matches), strings.Join(matchIDs, ", ")))
		} else {
			replyToID = targetCommentLookup
		}

		if matchedComment.ObjectID != "" {
			parentMap := make(map[string]string, len(comments))
			for _, c := range comments {
				if c.Comment.InReplyTo != "" {
					parentMap[c.ObjectID] = c.Comment.InReplyTo
				}
			}
			curr := matchedComment.ObjectID
			visited := make(map[string]bool, len(comments))
			for parentMap[curr] != "" && !visited[curr] {
				visited[curr] = true
				curr = parentMap[curr]
			}
			threadRootID = curr
		} else {
			threadRootID = replyToID
		}
	}

	var commentID string
	if opts.message != "" {
		cid, err := store.Reviews.Comment(ctx, reviewID, writ.NewComment{
			Text:      opts.message,
			InReplyTo: replyToID,
		})
		if err != nil {
			return renderErr(stderr, err)
		}
		commentID = cid
	}

	if opts.resolve || opts.unresolve {
		resolveTarget := threadRootID
		if resolveTarget == "" {
			if commentID != "" {
				resolveTarget = commentID
			} else {
				return renderErr(stderr, fmt.Errorf("writ review comment: comment or thread ID is required to resolve"))
			}
		}

		if opts.resolve {
			if err := store.Comments.Resolve(ctx, resolveTarget, writ.CommentResolve{Resolved: true}); err != nil {
				return renderErr(stderr, err)
			}
		} else {
			if err := store.Comments.Resolve(ctx, resolveTarget, writ.CommentResolve{Resolved: false}); err != nil {
				return renderErr(stderr, err)
			}
		}

		if commentID == "" {
			action := "resolved"
			if opts.unresolve {
				action = "unresolved"
			}
			fmt.Fprintf(stdout, "%s (%s)\n", resolveTarget, action)
			return 0
		}
	}

	fmt.Fprintln(stdout, commentID)
	return 0
}

type reviewApproveOpts struct {
	dir      string
	verdict  string
	revision string
	message  string
	subject  string
}

func newReviewApproveFlagSet(defaultDir string) (*flag.FlagSet, *reviewApproveOpts) {
	fs := flag.NewFlagSet("review approve", flag.ContinueOnError)
	opts := &reviewApproveOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&opts.verdict, "verdict", "approve", "Verdict `approve|request-changes|none` (default: approve)")
	fs.StringVar(&opts.revision, "revision", "", "Revision commit ref or SHA `<ref>` (defaults to latest head)")
	fs.StringVar(&opts.message, "m", "", "Verdict message `<msg>`")
	fs.StringVar(&opts.subject, "subject", "", "Subject person identifier `<s>`, scheme:value (defaults to writ.personId, else email:<user.email>)")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"review", "approve"}, reviewApproveCmd)
	}
	return fs, opts
}

func runReviewApprove(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newReviewApproveFlagSet(defaultDir)
	fs.SetOutput(stderr)

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

	switch opts.verdict {
	case "approve", "request-changes", "none":
		// valid
	default:
		fmt.Fprintf(stderr, "writ review approve: invalid verdict %q (must be approve, request-changes, or none)\n", opts.verdict)
		fs.Usage()
		return 2
	}

	targetDir := opts.dir
	if targetDir == "" {
		targetDir = "."
	}

	var revOID string
	if opts.revision != "" {
		rOID, err := gitRevParse(ctx, targetDir, opts.revision)
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

	// Normalize before the guards: the engine normalizes the subject on the way
	// into the op, so a whitespace-only -subject would pass a raw != "" check,
	// skip the writer fallback, and record a silently anonymous approval.
	subject := state.NormalizePerson(opts.subject)
	if subject == "" {
		// The fallback is the writer's own person identifier — writ.personId,
		// or email:<user.email>. There is no fallback past that: a writer-id
		// carries no scheme, so substituting one would write a bare identifier,
		// which is not a person identifier at all.
		subject = store.Writer().PersonID
		if subject == "" {
			return renderErr(stderr, fmt.Errorf("writ: no approval subject: pass -subject, or configure %s (for example %q) or user.email", identity.PersonIDKey, "user:alice"))
		}
	}

	if err := store.Reviews.Approve(ctx, reviewID, writ.Approval{
		Verdict:  opts.verdict,
		Revision: revOID,
		Message:  opts.message,
		Subject:  subject,
	}); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: recorded %s\n", reviewID, opts.verdict)
	return 0
}

type reviewAssignOpts struct {
	dir    string
	add    stringSliceFlag
	remove stringSliceFlag
}

func newReviewAssignFlagSet(defaultDir string) (*flag.FlagSet, *reviewAssignOpts) {
	fs := flag.NewFlagSet("review assign", flag.ContinueOnError)
	opts := &reviewAssignOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.Var(&opts.add, "add", "Add assignee `<a>` email or ID (repeatable)")
	fs.Var(&opts.remove, "remove", "Remove assignee `<a>` email or ID (repeatable)")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"review", "assign"}, reviewAssignCmd)
	}
	return fs, opts
}

func runReviewAssign(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newReviewAssignFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) == 0 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "writ review assign: review ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ review assign: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	if len(opts.add) == 0 && len(opts.remove) == 0 {
		fmt.Fprintln(stderr, "writ review assign: at least one -add or -remove is required")
		fs.Usage()
		return 2
	}

	targetDir := opts.dir
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

	if err := store.Reviews.Assign(ctx, reviewID, opts.add, opts.remove); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: updated assignees\n", reviewID)
	return 0
}

type reviewLabelOpts struct {
	dir    string
	add    stringSliceFlag
	remove stringSliceFlag
}

func newReviewLabelFlagSet(defaultDir string) (*flag.FlagSet, *reviewLabelOpts) {
	fs := flag.NewFlagSet("review label", flag.ContinueOnError)
	opts := &reviewLabelOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.Var(&opts.add, "add", "Add label `<l>` (repeatable)")
	fs.Var(&opts.remove, "remove", "Remove label `<l>` (repeatable)")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"review", "label"}, reviewLabelCmd)
	}
	return fs, opts
}

func runReviewLabel(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newReviewLabelFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) == 0 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "writ review label: review ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ review label: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	if len(opts.add) == 0 && len(opts.remove) == 0 {
		fmt.Fprintln(stderr, "writ review label: at least one -add or -remove is required")
		fs.Usage()
		return 2
	}

	targetDir := opts.dir
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

	if err := store.Reviews.Label(ctx, reviewID, opts.add, opts.remove); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: updated labels\n", reviewID)
	return 0
}

type reviewLinkOpts struct {
	dir        string
	target     string
	relation   string
	targetType string
}

func newReviewLinkFlagSet(defaultDir string) (*flag.FlagSet, *reviewLinkOpts) {
	fs := flag.NewFlagSet("review link", flag.ContinueOnError)
	opts := &reviewLinkOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&opts.target, "target", "", "Target reference `<ref>` (required, e.g. <repo-id>#<object-id> or <object-id>)")
	fs.StringVar(&opts.relation, "relation", "", "Link relation `<rel>`: fixes, relates, or none (required)")
	fs.StringVar(&opts.targetType, "target-type", "", "Target object type `<t>`")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"review", "link"}, reviewLinkCmd)
	}
	return fs, opts
}

func runReviewLink(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newReviewLinkFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) == 0 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "writ review link: review ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ review link: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	if opts.target == "" {
		fmt.Fprintln(stderr, "writ review link: -target is required")
		fs.Usage()
		return 2
	}

	if opts.relation == "" {
		fmt.Fprintln(stderr, "writ review link: -relation is required")
		fs.Usage()
		return 2
	}

	switch opts.relation {
	case "fixes", "relates", "none":
		// valid
	default:
		fmt.Fprintf(stderr, "writ review link: invalid relation %q (must be fixes, relates, or none)\n", opts.relation)
		fs.Usage()
		return 2
	}

	targetDir := opts.dir
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

	if err := store.Reviews.Link(ctx, reviewID, writ.Link{
		Target:     opts.target,
		Relation:   opts.relation,
		TargetType: opts.targetType,
	}); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: link %s -> %s\n", reviewID, opts.relation, opts.target)
	return 0
}

type reviewStatusOpts struct {
	dir         string
	reason      string
	mergeCommit string
	jsonMode    bool
}

func newReviewStatusFlagSet(defaultDir string) (*flag.FlagSet, *reviewStatusOpts) {
	fs := flag.NewFlagSet("review status", flag.ContinueOnError)
	opts := &reviewStatusOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&opts.reason, "reason", "", "Reason `<r>` for status change")
	fs.StringVar(&opts.mergeCommit, "merge-commit", "", "Merge commit ref or SHA `<ref>` (valid when setting status to merged)")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output result as JSON (view mode only)")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"review", "status"}, reviewStatusCmd)
	}
	return fs, opts
}

func runReviewStatus(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newReviewStatusFlagSet(defaultDir)
	fs.SetOutput(stderr)

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
		if opts.mergeCommit != "" {
			fmt.Fprintln(stderr, "writ review status: -merge-commit is only valid when setting status to merged")
			fs.Usage()
			return 2
		}
		if opts.reason != "" {
			fmt.Fprintln(stderr, "writ review status: -reason is only valid when setting status")
			fs.Usage()
			return 2
		}
	} else {
		if opts.jsonMode {
			fmt.Fprintln(stderr, "writ review status: --json is only valid when viewing status")
			fs.Usage()
			return 2
		}

		newState = posArgs[1]
		switch newState {
		case "draft", "open", "closed", "merged":
			// valid
		default:
			fmt.Fprintf(stderr, "writ review status: invalid status %q (must be draft, open, closed, or merged)\n", newState)
			fs.Usage()
			return 2
		}

		if opts.mergeCommit != "" && newState != "merged" {
			fmt.Fprintln(stderr, "writ review status: -merge-commit is only valid when setting status to merged")
			fs.Usage()
			return 2
		}
	}

	targetDir := opts.dir
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

		if opts.jsonMode {
			wireReview := wire.FromReviewResult(res)
			if err := emitJSON(stdout, wire.KindReviewStatus, wireReview); err != nil {
				fmt.Fprintf(stderr, "writ review status: marshal json: %v\n", err)
				return 1
			}
			return 0
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

		var assignees string
		if len(res.Review.Assignees) > 0 {
			assignees = strings.Join(res.Review.Assignees, ", ")
		} else {
			assignees = "-"
		}

		var labels string
		if len(res.Review.Labels) > 0 {
			labels = strings.Join(res.Review.Labels, ", ")
		} else {
			labels = "-"
		}

		fmt.Fprintf(stdout, "Review:      %s\n", res.ObjectID)
		fmt.Fprintf(stdout, "Title:       %s\n", res.Review.Title)
		fmt.Fprintf(stdout, "Status:      %s\n", status)
		fmt.Fprintf(stdout, "Author:      %s\n", author)
		fmt.Fprintf(stdout, "Assignees:   %s\n", assignees)
		fmt.Fprintf(stdout, "Labels:      %s\n", labels)
		fmt.Fprintf(stdout, "Revisions:   %d\n", len(res.Review.Revisions))
		fmt.Fprintf(stdout, "Approvals:   %d\n", len(res.Review.Approvals))
		fmt.Fprintf(stdout, "CI Checks:   %d\n", len(res.Review.CIStatuses))
		if res.Review.MergeCommit != "" {
			fmt.Fprintf(stdout, "Merge commit: %s\n", res.Review.MergeCommit)
		}
		if res.Review.Reason != "" {
			fmt.Fprintf(stdout, "Reason:       %s\n", res.Review.Reason)
		}

		if len(res.Review.Links) > 0 {
			fmt.Fprintln(stdout, "Links:")
			for _, link := range res.Review.Links {
				scope, slug, _, err := resolveIssueRef(ctx, store, link.Target)
				var outcome string
				if err != nil || scope == "unresolved" {
					outcome = "unresolved"
				} else if scope == "cross-repo" {
					if slug != "" {
						outcome = fmt.Sprintf("cross-repo %s", slug)
					} else {
						des, _, _ := state.ParseReference(link.Target)
						outcome = fmt.Sprintf("cross-repo %s", des)
					}
				} else {
					outcome = "local"
				}
				fmt.Fprintf(stdout, "  %s %s (%s)\n", link.Relation, link.Target, outcome)
			}
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
	if opts.mergeCommit != "" {
		mOID, err := gitRevParse(ctx, targetDir, opts.mergeCommit)
		if err != nil {
			return renderErr(stderr, err)
		}
		mergeCommitOID = mOID
	}

	if err := store.Reviews.SetStatus(ctx, reviewID, writ.ReviewStatus{
		Status:      newState,
		Reason:      opts.reason,
		MergeCommit: mergeCommitOID,
	}); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: %s\n", reviewID, newState)
	return 0
}

type reviewListOpts struct {
	dir       string
	statuses  stringSliceFlag
	assignees stringSliceFlag
	labels    stringSliceFlag
	authors   stringSliceFlag
	text      string
	limit     int
	sortOrder string
	jsonMode  bool
}

func newReviewListFlagSet(defaultDir string) (*flag.FlagSet, *reviewListOpts) {
	fs := flag.NewFlagSet("review list", flag.ContinueOnError)
	opts := &reviewListOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.Var(&opts.statuses, "status", "Filter by review status `<s>` (repeatable)")
	fs.Var(&opts.assignees, "assignee", "Filter by assignee `<a>` name or email (repeatable)")
	fs.Var(&opts.labels, "label", "Filter by label `<l>` (repeatable)")
	fs.Var(&opts.authors, "author", "Filter by author `<a>` name or email (repeatable)")
	fs.StringVar(&opts.text, "text", "", "Filter by text `<q>` match in title or description")
	fs.IntVar(&opts.limit, "limit", 0, "Maximum number `N` of reviews to return")
	fs.StringVar(&opts.sortOrder, "sort", "", "Sort order `<order>` (created_at_asc, created_at_desc, updated_at_asc, updated_at_desc, title_asc, title_desc)")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output result as JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"review", "list"}, reviewListCmd)
	}
	return fs, opts
}

func runReviewList(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newReviewListFlagSet(defaultDir)
	fs.SetOutput(stderr)

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

	if opts.limit < 0 {
		fmt.Fprintf(stderr, "writ review list: -limit must be non-negative, got %d\n", opts.limit)
		fs.Usage()
		return 2
	}

	var orderBy writ.OrderBy
	if opts.sortOrder != "" {
		var err error
		orderBy, err = parseOrderBy(opts.sortOrder)
		if err != nil {
			fmt.Fprintf(stderr, "writ review list: invalid sort order %q\n", opts.sortOrder)
			fs.Usage()
			return 2
		}
	}

	targetDir := opts.dir
	if targetDir == "" {
		targetDir = "."
	}

	store, err := openStore(targetDir)
	if err != nil {
		return renderErr(stderr, err)
	}
	defer store.Close()

	reviews, err := store.Query.Reviews(writ.ReviewFilter{
		Status:   opts.statuses,
		Assignee: opts.assignees,
		Label:    opts.labels,
		Author:   opts.authors,
		Text:     opts.text,
		Limit:    opts.limit,
		OrderBy:  orderBy,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		wireSummaries := wire.FromReviewResultSummaries(reviews)
		if err := emitJSON(stdout, wire.KindReviewList, wireSummaries); err != nil {
			fmt.Fprintf(stderr, "writ review list: marshal json: %v\n", err)
			return 1
		}
		return 0
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
