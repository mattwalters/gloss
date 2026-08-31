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
	"github.com/writtendev/writ/engine/state"
)

func runIssue(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printIssueUsage(stderr)
		return 2
	}

	switch args[0] {
	case "-h", "-help", "--help", "help":
		printIssueUsage(stdout)
		return 0
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
			printIssueUsage(stderr)
			return 2
		}
	}

	switch args[0] {
	case "-h", "-help", "--help", "help":
		printIssueUsage(stdout)
		return 0
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
	default:
		fmt.Fprintf(stderr, "writ issue: unknown subcommand %q\n\n", args[0])
		printIssueUsage(stderr)
		return 2
	}
}

func printIssueUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: writ issue [-C <dir>] <subcommand> [arguments]

Manage issues.

Subcommands:
  create    Create a new issue
  status    View or update issue status
  assign    Add or remove issue assignees
  list      List issues
  link      Manage issue cross-reference links

Run 'writ issue <subcommand> -h' for more information on a subcommand.
`)
}

func runIssueCreate(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("issue create", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var dir string
	var title string
	var description string
	var stateVal string
	var fixes stringSliceFlag
	var relates stringSliceFlag

	fs.StringVar(&dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&title, "title", "", "Issue title")
	fs.StringVar(&description, "description", "", "Issue description")
	fs.StringVar(&stateVal, "state", "", "Initial issue state (open or closed)")
	fs.Var(&fixes, "fixes", "Add a 'fixes' cross-reference link (repeatable)")
	fs.Var(&relates, "relates", "Add a 'relates' cross-reference link (repeatable)")

	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: writ issue create [-C <dir>] -title <t> [-description <d>] [-state open|closed] [-fixes <ref>]... [-relates <ref>]...

Create a new issue.

Flags:
  -C <dir>           Run as if writ was started in <dir>
  -title <t>         Issue title (required)
  -description <d>   Issue description
  -state <state>     Initial issue state (open or closed)
  -fixes <ref>       Add a 'fixes' cross-reference link (repeatable)
  -relates <ref>     Add a 'relates' cross-reference link (repeatable)
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
		fmt.Fprintf(stderr, "writ issue create: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}

	if title == "" {
		fmt.Fprintln(stderr, "writ issue create: -title is required")
		fs.Usage()
		return 2
	}

	if stateVal != "" && stateVal != "open" && stateVal != "closed" {
		fmt.Fprintf(stderr, "writ issue create: invalid state %q (must be open or closed)\n", stateVal)
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

	id, err := store.Issues.Create(ctx, writ.NewIssue{
		Title:       title,
		Description: description,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	if stateVal != "" {
		if err := store.Issues.SetState(ctx, id, writ.IssueState{State: stateVal}); err != nil {
			return renderErr(stderr, err)
		}
	}

	for _, fix := range fixes {
		if err := store.Issues.Link(ctx, id, writ.Link{Target: fix, Relation: "fixes"}); err != nil {
			return renderErr(stderr, err)
		}
	}

	for _, rel := range relates {
		if err := store.Issues.Link(ctx, id, writ.Link{Target: rel, Relation: "relates"}); err != nil {
			return renderErr(stderr, err)
		}
	}

	dispState := stateVal
	if dispState == "" {
		dispState = "open"
	}

	fmt.Fprintf(stdout, "%s (%s) %s\n", id, dispState, title)
	return 0
}

func runIssueStatus(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("issue status", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var dir string
	var reason string

	fs.StringVar(&dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&reason, "reason", "", "Reason for status change")

	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: writ issue status [-C <dir>] <id> [<state>] [-reason <r>]

View or update issue status.

States:
  open, closed

Flags:
  -C <dir>      Run as if writ was started in <dir>
  -reason <r>   Reason for status change
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
		if reason != "" {
			fmt.Fprintln(stderr, "writ issue status: -reason is only valid when setting status")
			fs.Usage()
			return 2
		}
	} else {
		newState = posArgs[1]
		if newState != "open" && newState != "closed" {
			fmt.Fprintf(stderr, "writ issue status: invalid status %q (must be open or closed)\n", newState)
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

		stateVal := res.Issue.State
		if stateVal == "" {
			stateVal = "open"
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
		Reason: reason,
	}); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: %s\n", issueID, newState)
	return 0
}

func runIssueAssign(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("issue assign", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var dir string
	var add stringSliceFlag
	var remove stringSliceFlag

	fs.StringVar(&dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.Var(&add, "add", "Add assignee email or ID (repeatable)")
	fs.Var(&remove, "remove", "Remove assignee email or ID (repeatable)")

	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: writ issue assign [-C <dir>] <id> [-add <a>]... [-remove <a>]...

Add or remove issue assignees.

Flags:
  -C <dir>       Run as if writ was started in <dir>
  -add <a>       Add assignee email or ID (repeatable)
  -remove <a>    Remove assignee email or ID (repeatable)
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
		fmt.Fprintln(stderr, "writ issue assign: issue ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ issue assign: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	if len(add) == 0 && len(remove) == 0 {
		fmt.Fprintln(stderr, "writ issue assign: at least one -add or -remove is required")
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

	issueID, err := resolveIssueID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	if err := store.Issues.Assign(ctx, issueID, add, remove); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: updated assignees\n", issueID)
	return 0
}

func runIssueList(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("issue list", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var dir string
	var states stringSliceFlag
	var assignees stringSliceFlag
	var labels stringSliceFlag
	var authors stringSliceFlag
	var text string
	var limit int
	var sortOrder string

	fs.StringVar(&dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.Var(&states, "state", "Filter by issue state (repeatable: -state open -state closed)")
	fs.Var(&assignees, "assignee", "Filter by assignee name or email (repeatable)")
	fs.Var(&labels, "label", "Filter by label (repeatable)")
	fs.Var(&authors, "author", "Filter by author name or email (repeatable)")
	fs.StringVar(&text, "text", "", "Filter by text match in title or description")
	fs.IntVar(&limit, "limit", 0, "Maximum number of issues to return")
	fs.StringVar(&sortOrder, "sort", "", "Sort order: created_at_asc, created_at_desc, updated_at_asc, updated_at_desc, title_asc, title_desc")

	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: writ issue list [-C <dir>] [-state <s>]... [-assignee <a>]... [-label <l>]... [-author <a>]... [-text <q>] [-limit N] [-sort <order>]

List issues.

Flags:
  -C <dir>         Run as if writ was started in <dir>
  -state <s>       Filter by issue state (repeatable)
  -assignee <a>    Filter by assignee name or email (repeatable)
  -label <l>       Filter by label (repeatable)
  -author <a>      Filter by author name or email (repeatable)
  -text <q>        Filter by text match in title or description
  -limit N         Maximum number of issues to return
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
		fmt.Fprintf(stderr, "writ issue list: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}

	if limit < 0 {
		fmt.Fprintf(stderr, "writ issue list: -limit must be non-negative, got %d\n", limit)
		fs.Usage()
		return 2
	}

	var orderBy writ.OrderBy
	if sortOrder != "" {
		var err error
		orderBy, err = parseOrderBy(sortOrder)
		if err != nil {
			fmt.Fprintf(stderr, "writ issue list: invalid sort order %q\n", sortOrder)
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

	issues, err := store.Query.Issues(writ.IssueFilter{
		State:    states,
		Assignee: assignees,
		Label:    labels,
		Author:   authors,
		Text:     text,
		Limit:    limit,
		OrderBy:  orderBy,
	})
	if err != nil {
		return renderErr(stderr, err)
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

func runIssueLink(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("issue link", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var dir string
	var target string
	var relation string
	var targetType string

	fs.StringVar(&dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&target, "target", "", "Target reference (e.g. <repo-id>#<object-id> or <object-id>)")
	fs.StringVar(&relation, "relation", "", "Link relation: fixes, relates, or none")
	fs.StringVar(&targetType, "target-type", "", "Target object type")

	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: writ issue link [-C <dir>] <id> -target <ref> -relation fixes|relates|none [-target-type <t>]

Manage issue cross-reference links.

Flags:
  -C <dir>           Run as if writ was started in <dir>
  -target <ref>      Target reference (required, e.g. <repo-id>#<object-id> or <object-id>)
  -relation <rel>    Link relation: fixes, relates, or none (required)
  -target-type <t>   Target object type
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
		fmt.Fprintln(stderr, "writ issue link: issue ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ issue link: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	if target == "" {
		fmt.Fprintln(stderr, "writ issue link: -target is required")
		fs.Usage()
		return 2
	}

	if relation == "" {
		fmt.Fprintln(stderr, "writ issue link: -relation is required")
		fs.Usage()
		return 2
	}

	switch relation {
	case "fixes", "relates", "none":
		// valid
	default:
		fmt.Fprintf(stderr, "writ issue link: invalid relation %q (must be fixes, relates, or none)\n", relation)
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

	issueID, err := resolveIssueID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	if err := store.Issues.Link(ctx, issueID, writ.Link{
		Target:     target,
		Relation:   relation,
		TargetType: targetType,
	}); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: link %s -> %s\n", issueID, relation, target)
	return 0
}
