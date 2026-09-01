package main

import (
	"fmt"
	"io"
	"strings"
)

type flagSpec struct {
	Name       string   // "title", "status"
	Arg        string   // "<t>", "" for bool flags
	Usage      string
	Values     []string // closed enum, for completion: approve|request-changes|none
	Repeatable bool
}

type command struct {
	Name, Short string
	UsageLine   string   // first line, verbatim
	Long        string
	Examples    []string // at least one per verb (DoD)
	ExitCodes   []string // only sync populates this today
	Flags       []flagSpec
	Subs        []*command
}

var rootCommand = &command{
	Name:      "writ",
	Short:     "Collaborative SDLC layer in git",
	UsageLine: "Usage: writ [-C <dir>] <command> [arguments]",
	Long:      "",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
	},
	Subs: []*command{
		initCmd,
		issueCmd,
		reviewCmd,
		syncCmd,
		completionCmd,
		helpCmd,
	},
}

var initCmd = &command{
	Name:      "init",
	Short:     "Initialize writ configuration (writer ID and remote fetch refspecs)",
	UsageLine: "Usage: writ init [-C <dir>] [remote...]",
	Long: "Initialize writ repository configuration by resolving or minting a writer ID,\n" +
		"verifying SSH signing key configuration, and adding fetch refspecs for git remotes.",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
	},
	Examples: []string{
		"writ init",
		"writ init origin",
	},
}

var issueCmd = &command{
	Name:      "issue",
	Short:     "Manage issues (create, status, assign, list, link)",
	UsageLine: "Usage: writ issue [-C <dir>] <subcommand> [arguments]",
	Long:      "Manage issues.",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
	},
	Subs: []*command{
		issueCreateCmd,
		issueStatusCmd,
		issueAssignCmd,
		issueListCmd,
		issueLinkCmd,
	},
}

var issueCreateCmd = &command{
	Name:      "create",
	Short:     "Create a new issue",
	UsageLine: "Usage: writ issue create [-C <dir>] -title <t> [-description <d>] [-state open|closed] [-fixes <ref>]... [-relates <ref>]...",
	Long:      "Create a new issue.",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
		{
			Name:  "title",
			Arg:   "<t>",
			Usage: "Issue title (required)",
		},
		{
			Name:  "description",
			Arg:   "<d>",
			Usage: "Issue description",
		},
		{
			Name:   "state",
			Arg:    "<state>",
			Usage:  "Initial issue state (open or closed)",
			Values: []string{"open", "closed"},
		},
		{
			Name:       "fixes",
			Arg:        "<ref>",
			Usage:      "Add a 'fixes' cross-reference link (repeatable)",
			Repeatable: true,
		},
		{
			Name:       "relates",
			Arg:        "<ref>",
			Usage:      "Add a 'relates' cross-reference link (repeatable)",
			Repeatable: true,
		},
	},
	Examples: []string{
		`writ issue create -title "Fix memory leak"`,
		`writ issue create -title "Bug in parser" -fixes 01J8ABC`,
	},
}

var issueStatusCmd = &command{
	Name:      "status",
	Short:     "View or update issue status",
	UsageLine: "Usage: writ issue status [-C <dir>] <id> [<state>] [-reason <r>] [--json]",
	Long:      "View or update issue status.\n\nStates:\n  open, closed",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
		{
			Name:  "reason",
			Arg:   "<r>",
			Usage: "Reason for status change",
		},
		{
			Name:  "json",
			Usage: "Output result as JSON (view mode only)",
		},
	},
	Examples: []string{
		"writ issue status 01J8ABC",
		`writ issue status 01J8ABC closed -reason "resolved in #42"`,
		"writ issue status 01J8ABC --json",
	},
}

var issueAssignCmd = &command{
	Name:      "assign",
	Short:     "Add or remove issue assignees",
	UsageLine: "Usage: writ issue assign [-C <dir>] <id> [-add <a>]... [-remove <a>]...",
	Long:      "Add or remove issue assignees.",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
		{
			Name:       "add",
			Arg:        "<a>",
			Usage:      "Add assignee email or ID (repeatable)",
			Repeatable: true,
		},
		{
			Name:       "remove",
			Arg:        "<a>",
			Usage:      "Remove assignee email or ID (repeatable)",
			Repeatable: true,
		},
	},
	Examples: []string{
		"writ issue assign 01J8ABC -add alice@example.com",
		"writ issue assign 01J8ABC -remove bob@example.com",
	},
}

var issueListCmd = &command{
	Name:      "list",
	Short:     "List issues",
	UsageLine: "Usage: writ issue list [-C <dir>] [-state <s>]... [-assignee <a>]... [-label <l>]... [-author <a>]... [-text <q>] [-limit N] [-sort <order>] [--json]",
	Long:      "List issues.",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
		{
			Name:       "state",
			Arg:        "<s>",
			Usage:      "Filter by issue state (repeatable)",
			Values:     []string{"open", "closed"},
			Repeatable: true,
		},
		{
			Name:       "assignee",
			Arg:        "<a>",
			Usage:      "Filter by assignee name or email (repeatable)",
			Repeatable: true,
		},
		{
			Name:       "label",
			Arg:        "<l>",
			Usage:      "Filter by label (repeatable)",
			Repeatable: true,
		},
		{
			Name:       "author",
			Arg:        "<a>",
			Usage:      "Filter by author name or email (repeatable)",
			Repeatable: true,
		},
		{
			Name:  "text",
			Arg:   "<q>",
			Usage: "Filter by text match in title or description",
		},
		{
			Name:  "limit",
			Arg:   "N",
			Usage: "Maximum number of issues to return",
		},
		{
			Name:   "sort",
			Arg:    "<order>",
			Usage:  "Sort order (created_at_asc, created_at_desc, updated_at_asc, updated_at_desc, title_asc, title_desc)",
			Values: []string{"created_at_asc", "created_at_desc", "updated_at_asc", "updated_at_desc", "title_asc", "title_desc"},
		},
		{
			Name:  "json",
			Usage: "Output result as JSON",
		},
	},
	Examples: []string{
		"writ issue list",
		"writ issue list -state open",
		"writ issue list -assignee alice@example.com --json",
	},
}

var issueLinkCmd = &command{
	Name:      "link",
	Short:     "Manage issue cross-reference links",
	UsageLine: "Usage: writ issue link [-C <dir>] <id> -target <ref> -relation fixes|relates|none [-target-type <t>]",
	Long:      "Manage issue cross-reference links.",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
		{
			Name:  "target",
			Arg:   "<ref>",
			Usage: "Target reference (required, e.g. <repo-id>#<object-id> or <object-id>)",
		},
		{
			Name:   "relation",
			Arg:    "<rel>",
			Usage:  "Link relation: fixes, relates, or none (required)",
			Values: []string{"fixes", "relates", "none"},
		},
		{
			Name:  "target-type",
			Arg:   "<t>",
			Usage: "Target object type",
		},
	},
	Examples: []string{
		"writ issue link 01J8ABC -target 01J8DEF -relation fixes",
		"writ issue link 01J8ABC -target other-repo#01J8DEF -relation relates",
	},
}

var reviewCmd = &command{
	Name:      "review",
	Short:     "Manage code reviews (open, comment, approve, status, list)",
	UsageLine: "Usage: writ review [-C <dir>] <subcommand> [arguments]",
	Long:      "Manage code reviews.",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
	},
	Subs: []*command{
		reviewOpenCmd,
		reviewCommentCmd,
		reviewApproveCmd,
		reviewStatusCmd,
		reviewListCmd,
	},
}

var reviewOpenCmd = &command{
	Name:      "open",
	Short:     "Create a new code review",
	UsageLine: "Usage: writ review open [-C <dir>] -title <t> [-description <d>] [-base <ref> -head <ref>] [-draft]",
	Long:      "Create a new code review.",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
		{
			Name:  "title",
			Arg:   "<t>",
			Usage: "Review title (required)",
		},
		{
			Name:  "description",
			Arg:   "<d>",
			Usage: "Review description",
		},
		{
			Name:  "base",
			Arg:   "<ref>",
			Usage: "Base revision commit or ref",
		},
		{
			Name:  "head",
			Arg:   "<ref>",
			Usage: "Head revision commit or ref",
		},
		{
			Name:  "draft",
			Usage: "Create review in draft state",
		},
	},
	Examples: []string{
		`writ review open -title "Add feature X"`,
		`writ review open -title "Add feature X" -base main -head feature-x`,
		`writ review open -title "WIP: feature" -draft`,
	},
}

var reviewCommentCmd = &command{
	Name:      "comment",
	Short:     "Add a comment to a review",
	UsageLine: "Usage: writ review comment [-C <dir>] <id> -m <text> [-reply-to <comment-id>]",
	Long:      "Add a comment to a review.",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
		{
			Name:  "m",
			Arg:   "<text>",
			Usage: "Comment message text (required)",
		},
		{
			Name:  "reply-to",
			Arg:   "<comment-id>",
			Usage: "Comment ID to reply to",
		},
	},
	Examples: []string{
		`writ review comment 01J8ABC -m "Looks good to me"`,
		`writ review comment 01J8ABC -m "Addressed feedback" -reply-to 01J8DEF`,
	},
}

var reviewApproveCmd = &command{
	Name:      "approve",
	Short:     "Record a review verdict",
	UsageLine: "Usage: writ review approve [-C <dir>] <id> [-verdict approve|request-changes|none] [-revision <ref>] [-m <msg>] [-subject <s>]",
	Long:      "Record a review verdict.",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
		{
			Name:   "verdict",
			Arg:    "approve|request-changes|none",
			Usage:  "Verdict (default: approve)",
			Values: []string{"approve", "request-changes", "none"},
		},
		{
			Name:  "revision",
			Arg:   "<ref>",
			Usage: "Revision commit ref or SHA (defaults to latest head)",
		},
		{
			Name:  "m",
			Arg:   "<msg>",
			Usage: "Verdict message",
		},
		{
			Name:  "subject",
			Arg:   "<s>",
			Usage: "Subject identity (defaults to writer email or writer ID)",
		},
	},
	Examples: []string{
		"writ review approve 01J8ABC",
		`writ review approve 01J8ABC -verdict request-changes -m "Please fix tests"`,
	},
}

var reviewStatusCmd = &command{
	Name:      "status",
	Short:     "View or update review status",
	UsageLine: "Usage: writ review status [-C <dir>] <id> [<state>] [-reason <r>] [-merge-commit <ref>] [--json]",
	Long:      "View or update review status.\n\nStates:\n  draft, open, closed, merged",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
		{
			Name:  "reason",
			Arg:   "<r>",
			Usage: "Reason for status change",
		},
		{
			Name:  "merge-commit",
			Arg:   "<ref>",
			Usage: "Merge commit ref or SHA (valid when setting status to merged)",
		},
		{
			Name:  "json",
			Usage: "Output result as JSON (view mode only)",
		},
	},
	Examples: []string{
		"writ review status 01J8ABC",
		`writ review status 01J8ABC closed -reason "superseded"`,
		"writ review status 01J8ABC merged -merge-commit main",
		"writ review status 01J8ABC --json",
	},
}

var reviewListCmd = &command{
	Name:      "list",
	Short:     "List code reviews",
	UsageLine: "Usage: writ review list [-C <dir>] [-status <s>]... [-author <a>]... [-text <q>] [-limit N] [-sort <order>] [--json]",
	Long:      "List code reviews.",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
		{
			Name:       "status",
			Arg:        "<s>",
			Usage:      "Filter by review status (repeatable)",
			Values:     []string{"draft", "open", "closed", "merged"},
			Repeatable: true,
		},
		{
			Name:       "author",
			Arg:        "<a>",
			Usage:      "Filter by author name or email (repeatable)",
			Repeatable: true,
		},
		{
			Name:  "text",
			Arg:   "<q>",
			Usage: "Filter by text match in title or description",
		},
		{
			Name:  "limit",
			Arg:   "N",
			Usage: "Maximum number of reviews to return",
		},
		{
			Name:   "sort",
			Arg:    "<order>",
			Usage:  "Sort order (created_at_asc, created_at_desc, updated_at_asc, updated_at_desc, title_asc, title_desc)",
			Values: []string{"created_at_asc", "created_at_desc", "updated_at_asc", "updated_at_desc", "title_asc", "title_desc"},
		},
		{
			Name:  "json",
			Usage: "Output result as JSON",
		},
	},
	Examples: []string{
		"writ review list",
		"writ review list -status open",
		"writ review list -status open -status draft --json",
	},
}

var syncCmd = &command{
	Name:      "sync",
	Short:     "Synchronize operations with git remotes",
	UsageLine: "Usage: writ sync [-C <dir>] [--status] [--json] [remote...]",
	Long: "Synchronize collaborative SDLC operations with one or more git remotes.\n\n" +
		"Fetch remote operations, push local operations, and refresh the local projection cache.\n" +
		"With no remote specified, defaults to 'origin' or the sole configured remote.",
	Flags: []flagSpec{
		{
			Name:  "C",
			Arg:   "<dir>",
			Usage: "Run as if writ was started in <dir>",
		},
		{
			Name:  "status",
			Usage: "Report unpushed ops count without network transport",
		},
		{
			Name:  "json",
			Usage: "Output result as JSON",
		},
	},
	ExitCodes: []string{
		"0  Success",
		"1  Transport or unclassified git failure",
		"2  Usage error (bad flag, no resolvable default remote)",
		"3  Unknown or unconfigured remote",
		"4  Rejected non-fast-forward update",
		"5  Not a git repository / store cannot be opened",
		"6  Authentication or credentials failure",
		"7  Network or remote unreachable",
	},
	Examples: []string{
		"writ sync",
		"writ sync origin",
		"writ sync --status",
		"writ sync --json",
	},
}

var completionCmd = &command{
	Name:      "completion",
	Short:     "Generate shell completion scripts",
	UsageLine: "Usage: writ completion <shell>",
	Long:      "Generate shell completion scripts for bash, zsh, or fish.\n\nSupported shells: bash, zsh, fish.",
	Examples: []string{
		"writ completion bash > /etc/bash_completion.d/writ",
		`writ completion zsh > "${fpath[1]}/_writ"`,
		"writ completion fish > ~/.config/fish/completions/writ.fish",
	},
}

var helpCmd = &command{
	Name:      "help",
	Short:     "Show help for commands",
	UsageLine: "Usage: writ help [<command> [<subcommand>]]",
	Long:      "Show detailed help and examples for a command or subcommand.",
	Examples: []string{
		"writ help",
		"writ help review",
		"writ help review open",
	},
}


func findCommandByPath(path []string) (*command, []string, error) {
	if len(path) == 0 {
		return rootCommand, nil, nil
	}

	curr := rootCommand
	var matchedPath []string

	for i, segment := range path {
		var found *command
		for _, sub := range curr.Subs {
			if sub.Name == segment {
				found = sub
				break
			}
		}
		if found == nil {
			if i == 0 {
				return nil, nil, fmt.Errorf("unknown command %q", segment)
			}
			return nil, nil, fmt.Errorf("unknown subcommand %q for \"writ %s\"", segment, strings.Join(matchedPath, " "))
		}
		curr = found
		matchedPath = append(matchedPath, segment)
	}

	return curr, matchedPath, nil
}

func renderUsage(w io.Writer, path []string, c *command) {
	fmt.Fprintln(w, c.UsageLine)
	fmt.Fprintln(w)
	if c.Long != "" {
		fmt.Fprintln(w, c.Long)
		fmt.Fprintln(w)
	}

	if len(c.Subs) > 0 {
		sectionName := "Commands:"
		if len(path) > 0 {
			sectionName = "Subcommands:"
		}
		fmt.Fprintln(w, sectionName)
		maxLen := 0
		for _, sub := range c.Subs {
			if len(sub.Name) > maxLen {
				maxLen = len(sub.Name)
			}
		}
		for _, sub := range c.Subs {
			padding := strings.Repeat(" ", maxLen-len(sub.Name)+2)
			fmt.Fprintf(w, "  %s%s%s\n", sub.Name, padding, sub.Short)
		}
		fmt.Fprintln(w)

		if len(path) == 0 {
			fmt.Fprintln(w, "Plumbing:")
			fmt.Fprintln(w, "  Every read verb supports --json for machine-readable output.")
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Run 'writ <command> -h' for more information on a command.")
		} else {
			cmdPath := strings.Join(path, " ")
			fmt.Fprintf(w, "Run 'writ %s <subcommand> -h' for more information on a subcommand.\n", cmdPath)
		}
		return
	}

	if len(c.Flags) > 0 {
		fmt.Fprintln(w, "Flags:")
		maxFlagLen := 0
		type flagFormat struct {
			display string
			usage   string
		}
		var formatted []flagFormat
		for _, f := range c.Flags {
			disp := "-" + f.Name
			if f.Arg != "" {
				disp += " " + f.Arg
			}
			if len(disp) > maxFlagLen {
				maxFlagLen = len(disp)
			}
			formatted = append(formatted, flagFormat{display: disp, usage: f.Usage})
		}
		for _, ff := range formatted {
			padding := strings.Repeat(" ", maxFlagLen-len(ff.display)+3)
			fmt.Fprintf(w, "  %s%s%s\n", ff.display, padding, ff.usage)
		}
	}

	if len(c.ExitCodes) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Exit codes:")
		for _, ec := range c.ExitCodes {
			fmt.Fprintf(w, "  %s\n", ec)
		}
	}
}
