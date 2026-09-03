package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
	"github.com/writtendev/writ/engine"
	"github.com/writtendev/writ/engine/state"
	"github.com/writtendev/writ/spec"
)

func runIssue(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		renderUsage(stderr, []string{"issue"}, issueCmd)
		return 2
	}

	switch args[0] {
	case "-h", "-help", "--help":
		renderUsage(stdout, []string{"issue"}, issueCmd)
		return 0
	case "help":
		return runHelp(append([]string{"issue"}, args[1:]...), stdout, stderr)
	}

	targetDir := defaultDir
	if args[0] == "-C" {
		if len(args) < 2 {
			fmt.Fprintln(stderr, "writ issue: option -C requires an argument")
			return 2
		}
		targetDir = args[1]
		args = args[2:]
		if len(args) == 0 {
			renderUsage(stderr, []string{"issue"}, issueCmd)
			return 2
		}
	}

	switch args[0] {
	case "-h", "-help", "--help":
		renderUsage(stdout, []string{"issue"}, issueCmd)
		return 0
	case "help":
		return runHelp(append([]string{"issue"}, args[1:]...), stdout, stderr)
	case "create":
		return runIssueCreate(ctx, targetDir, args[1:], stdout, stderr)
	case "status":
		return runIssueStatus(ctx, targetDir, args[1:], stdout, stderr)
	case "assign":
		return runIssueAssign(ctx, targetDir, args[1:], stdout, stderr)
	case "list":
		return runIssueList(ctx, targetDir, args[1:], stdout, stderr)
	case "link":
		return runIssueLink(ctx, targetDir, args[1:], stdout, stderr)
	case "label":
		return runIssueLabel(ctx, targetDir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "writ issue: unknown subcommand %q\n\n", args[0])
		renderUsage(stderr, []string{"issue"}, issueCmd)
		return 2
	}
}

type issueCreateOpts struct {
	dir         string
	title       string
	description string
	stateVal    string
	fixes       stringSliceFlag
	relates     stringSliceFlag
}

func newIssueCreateFlagSet(defaultDir string) (*flag.FlagSet, *issueCreateOpts) {
	fs := flag.NewFlagSet("issue create", flag.ContinueOnError)
	opts := &issueCreateOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&opts.title, "title", "", "Issue title `<t>` (required)")
	fs.StringVar(&opts.description, "description", "", "Issue description `<d>`")
	fs.StringVar(&opts.stateVal, "state", "", "Initial issue state `<state>` (open or closed)")
	fs.Var(&opts.fixes, "fixes", "Add a 'fixes' cross-reference link `<ref>` (repeatable)")
	fs.Var(&opts.relates, "relates", "Add a 'relates' cross-reference link `<ref>` (repeatable)")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"issue", "create"}, issueCreateCmd)
	}
	return fs, opts
}

func runIssueCreate(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newIssueCreateFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ issue create: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}

	if opts.title == "" {
		fmt.Fprintln(stderr, "writ issue create: -title is required")
		fs.Usage()
		return 2
	}

	if opts.stateVal != "" && !slices.Contains(spec.IssueStates(), opts.stateVal) {
		fmt.Fprintf(stderr, "writ issue create: invalid state %q (must be %s)\n", opts.stateVal, spec.FormatOptions(spec.IssueStates()))
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

	id, err := store.Issues.Create(ctx, writ.NewIssue{
		Title:       opts.title,
		Description: opts.description,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	if opts.stateVal != "" {
		if err := store.Issues.SetState(ctx, id, writ.IssueState{State: opts.stateVal}); err != nil {
			return renderErr(stderr, err)
		}
	}

	for _, fix := range opts.fixes {
		if err := store.Issues.Link(ctx, id, writ.Link{Target: fix, Relation: "fixes"}); err != nil {
			return renderErr(stderr, err)
		}
	}

	for _, rel := range opts.relates {
		if err := store.Issues.Link(ctx, id, writ.Link{Target: rel, Relation: "relates"}); err != nil {
			return renderErr(stderr, err)
		}
	}

	dispState := opts.stateVal
	if dispState == "" {
		dispState = "open"
	}

	fmt.Fprintf(stdout, "%s (%s) %s\n", id, dispState, opts.title)
	return 0
}

type issueStatusOpts struct {
	dir      string
	reason   string
	jsonMode bool
}

func newIssueStatusFlagSet(defaultDir string) (*flag.FlagSet, *issueStatusOpts) {
	fs := flag.NewFlagSet("issue status", flag.ContinueOnError)
	opts := &issueStatusOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&opts.reason, "reason", "", "Reason `<r>` for status change")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output result as JSON (view mode only)")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"issue", "status"}, issueStatusCmd)
	}
	return fs, opts
}

func runIssueStatus(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newIssueStatusFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) == 0 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "writ issue status: issue ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 2 {
		fmt.Fprintf(stderr, "writ issue status: unexpected arguments: %s\n", strings.Join(posArgs[2:], " "))
		fs.Usage()
		return 2
	}

	var newState string
	if len(posArgs) == 1 {
		if opts.reason != "" {
			fmt.Fprintln(stderr, "writ issue status: -reason is only valid when setting status")
			fs.Usage()
			return 2
		}
	} else {
		if opts.jsonMode {
			fmt.Fprintln(stderr, "writ issue status: --json is only valid when viewing status")
			fs.Usage()
			return 2
		}

		newState = posArgs[1]
		if !slices.Contains(spec.IssueStates(), newState) {
			fmt.Fprintf(stderr, "writ issue status: invalid status %q (must be %s)\n", newState, spec.FormatOptions(spec.IssueStates()))
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

	issueID, err := resolveIssueID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	// 1. Read / View status mode (len(posArgs) == 1)
	if len(posArgs) == 1 {
		res, err := store.Query.Issue(issueID)
		if err != nil {
			return renderErr(stderr, err)
		}

		if opts.jsonMode {
			wireIssue := wire.FromIssueResult(res)
			if err := emitJSON(stdout, wire.KindIssueStatus, wireIssue); err != nil {
				fmt.Fprintf(stderr, "writ issue status: marshal json: %v\n", err)
				return 1
			}
			return 0
		}

		stateVal := res.Issue.State
		if stateVal == "" {
			stateVal = "open"
		}
		author := authorDisplay(res.Author.Name, res.Author.Email)

		var assignees string
		if len(res.Issue.Assignees) > 0 {
			assignees = strings.Join(res.Issue.Assignees, ", ")
		} else {
			assignees = "-"
		}

		var labels string
		if len(res.Issue.Labels) > 0 {
			labels = strings.Join(res.Issue.Labels, ", ")
		} else {
			labels = "-"
		}

		fmt.Fprintf(stdout, "Issue:       %s\n", res.ObjectID)
		fmt.Fprintf(stdout, "Title:       %s\n", res.Issue.Title)
		fmt.Fprintf(stdout, "State:       %s\n", stateVal)
		if res.Issue.Reason != "" {
			fmt.Fprintf(stdout, "Reason:      %s\n", res.Issue.Reason)
		}
		fmt.Fprintf(stdout, "Author:      %s\n", author)
		fmt.Fprintf(stdout, "Assignees:   %s\n", assignees)
		fmt.Fprintf(stdout, "Labels:      %s\n", labels)

		if len(res.Issue.Links) > 0 {
			fmt.Fprintln(stdout, "Links:")
			for _, link := range res.Issue.Links {
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
	if err := store.Issues.SetState(ctx, issueID, writ.IssueState{
		State:  newState,
		Reason: opts.reason,
	}); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: %s\n", issueID, newState)
	return 0
}

type issueAssignOpts struct {
	dir    string
	add    stringSliceFlag
	remove stringSliceFlag
}

func newIssueAssignFlagSet(defaultDir string) (*flag.FlagSet, *issueAssignOpts) {
	fs := flag.NewFlagSet("issue assign", flag.ContinueOnError)
	opts := &issueAssignOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.Var(&opts.add, "add", "Add assignee `<a>`, a scheme:value person identifier (repeatable)")
	fs.Var(&opts.remove, "remove", "Remove assignee `<a>`, a scheme:value person identifier (repeatable)")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"issue", "assign"}, issueAssignCmd)
	}
	return fs, opts
}

func runIssueAssign(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newIssueAssignFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) == 0 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "writ issue assign: issue ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ issue assign: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	if len(opts.add) == 0 && len(opts.remove) == 0 {
		fmt.Fprintln(stderr, "writ issue assign: at least one -add or -remove is required")
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

	issueID, err := resolveIssueID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	if err := store.Issues.Assign(ctx, issueID, opts.add, opts.remove); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: updated assignees\n", issueID)
	return 0
}

type issueListOpts struct {
	dir       string
	states    stringSliceFlag
	assignees stringSliceFlag
	labels    stringSliceFlag
	authors   stringSliceFlag
	text      string
	limit     int
	sortOrder string
	jsonMode  bool
}

func newIssueListFlagSet(defaultDir string) (*flag.FlagSet, *issueListOpts) {
	fs := flag.NewFlagSet("issue list", flag.ContinueOnError)
	opts := &issueListOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.Var(&opts.states, "state", "Filter by issue state `<s>` (repeatable)")
	fs.Var(&opts.assignees, "assignee", "Filter by assignee `<a>`, a scheme:value person identifier (repeatable)")
	fs.Var(&opts.labels, "label", "Filter by label `<l>` (repeatable)")
	fs.Var(&opts.authors, "author", "Filter by author `<a>` name or email (repeatable)")
	fs.StringVar(&opts.text, "text", "", "Filter by text `<q>` match in title or description")
	fs.IntVar(&opts.limit, "limit", 0, "Maximum number `N` of issues to return")
	fs.StringVar(&opts.sortOrder, "sort", "", "Sort order `<order>` (created_at_asc, created_at_desc, updated_at_asc, updated_at_desc, title_asc, title_desc)")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output result as JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"issue", "list"}, issueListCmd)
	}
	return fs, opts
}

func runIssueList(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newIssueListFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ issue list: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}

	if opts.limit < 0 {
		fmt.Fprintf(stderr, "writ issue list: -limit must be non-negative, got %d\n", opts.limit)
		fs.Usage()
		return 2
	}

	var orderBy writ.OrderBy
	if opts.sortOrder != "" {
		var err error
		orderBy, err = parseOrderBy(opts.sortOrder)
		if err != nil {
			fmt.Fprintf(stderr, "writ issue list: invalid sort order %q\n", opts.sortOrder)
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

	issues, err := store.Query.Issues(writ.IssueFilter{
		State:    opts.states,
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
		wireSummaries := wire.FromIssueResultSummaries(issues)
		if err := emitJSON(stdout, wire.KindIssueList, wireSummaries); err != nil {
			fmt.Fprintf(stderr, "writ issue list: marshal json: %v\n", err)
			return 1
		}
		return 0
	}

	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, iss := range issues {
		shortID := iss.ObjectID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		stateVal := iss.Issue.State
		if stateVal == "" {
			stateVal = "open"
		}
		assigneesStr := "-"
		if len(iss.Issue.Assignees) > 0 {
			assigneesStr = strings.Join(iss.Issue.Assignees, ", ")
		}
		updatedAt := iss.UpdatedAt.Format("2006-01-02 15:04:05")
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", shortID, stateVal, iss.Issue.Title, assigneesStr, updatedAt)
	}
	_ = tw.Flush()
	return 0
}

type issueLinkOpts struct {
	dir        string
	target     string
	relation   string
	targetType string
}

func newIssueLinkFlagSet(defaultDir string) (*flag.FlagSet, *issueLinkOpts) {
	fs := flag.NewFlagSet("issue link", flag.ContinueOnError)
	opts := &issueLinkOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&opts.target, "target", "", "Target reference `<ref>` (required, e.g. <repo-id>#<object-id> or <object-id>)")
	fs.StringVar(&opts.relation, "relation", "", "Link relation `<rel>`: fixes, relates, or none (required)")
	fs.StringVar(&opts.targetType, "target-type", "", "Target object type `<t>`")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"issue", "link"}, issueLinkCmd)
	}
	return fs, opts
}

func runIssueLink(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newIssueLinkFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) == 0 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "writ issue link: issue ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ issue link: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	if opts.target == "" {
		fmt.Fprintln(stderr, "writ issue link: -target is required")
		fs.Usage()
		return 2
	}

	if opts.relation == "" {
		fmt.Fprintln(stderr, "writ issue link: -relation is required")
		fs.Usage()
		return 2
	}

	if !slices.Contains(spec.LinkRelations(), opts.relation) {
		fmt.Fprintf(stderr, "writ issue link: invalid relation %q (must be %s)\n", opts.relation, spec.FormatOptions(spec.LinkRelations()))
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

	issueID, err := resolveIssueID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	if err := store.Issues.Link(ctx, issueID, writ.Link{
		Target:     opts.target,
		Relation:   opts.relation,
		TargetType: opts.targetType,
	}); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: link %s -> %s\n", issueID, opts.relation, opts.target)
	return 0
}

type issueLabelOpts struct {
	dir      string
	add      stringSliceFlag
	remove   stringSliceFlag
	jsonMode bool
}

func newIssueLabelFlagSet(defaultDir string) (*flag.FlagSet, *issueLabelOpts) {
	fs := flag.NewFlagSet("issue label", flag.ContinueOnError)
	opts := &issueLabelOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.Var(&opts.add, "add", "Add label `<l>` (repeatable)")
	fs.Var(&opts.remove, "remove", "Remove label `<l>` (repeatable)")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output result as JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"issue", "label"}, issueLabelCmd)
	}
	return fs, opts
}

func runIssueLabel(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newIssueLabelFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) == 0 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "writ issue label: issue ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ issue label: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
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

	issueID, err := resolveIssueID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	if len(opts.add) == 0 && len(opts.remove) == 0 {
		res, err := store.Query.Issue(issueID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if opts.jsonMode {
			if err := emitJSON(stdout, wire.KindIssueLabel, wire.FromIssueLabels(issueID, res.Issue.Labels)); err != nil {
				fmt.Fprintf(stderr, "writ issue label: marshal json: %v\n", err)
				return 1
			}
			return 0
		}
		for _, l := range res.Issue.Labels {
			fmt.Fprintln(stdout, l)
		}
		return 0
	}

	if err := store.Issues.Label(ctx, issueID, opts.add, opts.remove); err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		res, err := store.Query.Issue(issueID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindIssueLabel, wire.FromIssueLabels(issueID, res.Issue.Labels)); err != nil {
			fmt.Fprintf(stderr, "writ issue label: marshal json: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintf(stdout, "%s: updated labels\n", issueID)
	return 0
}
