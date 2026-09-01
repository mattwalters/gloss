package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

type flagSpec struct {
	Name       string // "title", "status"
	Arg        string // "<t>", "" for bool flags
	Usage      string
	Values     []string // closed enum, for completion: approve|request-changes|none
	Repeatable bool
}

type command struct {
	Name, Short string
	UsageLine   string // first line, verbatim
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
		versionCmd,
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
		{Name: "C"},
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
		{Name: "C"},
		{Name: "title"},
		{Name: "description"},
		{Name: "state", Values: []string{"open", "closed"}},
		{Name: "fixes", Repeatable: true},
		{Name: "relates", Repeatable: true},
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
		{Name: "C"},
		{Name: "reason"},
		{Name: "json"},
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
		{Name: "C"},
		{Name: "add", Repeatable: true},
		{Name: "remove", Repeatable: true},
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
		{Name: "C"},
		{Name: "state", Values: []string{"open", "closed"}, Repeatable: true},
		{Name: "assignee", Repeatable: true},
		{Name: "label", Repeatable: true},
		{Name: "author", Repeatable: true},
		{Name: "text"},
		{Name: "limit"},
		{Name: "sort", Values: []string{"created_at_asc", "created_at_desc", "updated_at_asc", "updated_at_desc", "title_asc", "title_desc"}},
		{Name: "json"},
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
		{Name: "C"},
		{Name: "target"},
		{Name: "relation", Values: []string{"fixes", "relates", "none"}},
		{Name: "target-type"},
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
		{Name: "C"},
		{Name: "title"},
		{Name: "description"},
		{Name: "base"},
		{Name: "head"},
		{Name: "draft"},
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
		{Name: "C"},
		{Name: "m"},
		{Name: "reply-to"},
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
		{Name: "C"},
		{Name: "verdict", Values: []string{"approve", "request-changes", "none"}},
		{Name: "revision"},
		{Name: "m"},
		{Name: "subject"},
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
		{Name: "C"},
		{Name: "reason"},
		{Name: "merge-commit"},
		{Name: "json"},
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
		{Name: "C"},
		{Name: "status", Values: []string{"draft", "open", "closed", "merged"}, Repeatable: true},
		{Name: "author", Repeatable: true},
		{Name: "text"},
		{Name: "limit"},
		{Name: "sort", Values: []string{"created_at_asc", "created_at_desc", "updated_at_asc", "updated_at_desc", "title_asc", "title_desc"}},
		{Name: "json"},
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
		{Name: "C"},
		{Name: "status"},
		{Name: "json"},
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

var versionCmd = &command{
	Name:      "version",
	Short:     "Print the writ version",
	UsageLine: "Usage: writ version",
	Long:      "Print the version of the writ binary.",
	Examples: []string{
		"writ version",
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

var flagSetConstructors map[string]func() *flag.FlagSet

func init() {
	flagSetConstructors = map[string]func() *flag.FlagSet{
		"init":           func() *flag.FlagSet { fs, _ := newInitFlagSet(""); return fs },
		"issue create":   func() *flag.FlagSet { fs, _ := newIssueCreateFlagSet(""); return fs },
		"issue status":   func() *flag.FlagSet { fs, _ := newIssueStatusFlagSet(""); return fs },
		"issue assign":   func() *flag.FlagSet { fs, _ := newIssueAssignFlagSet(""); return fs },
		"issue list":     func() *flag.FlagSet { fs, _ := newIssueListFlagSet(""); return fs },
		"issue link":     func() *flag.FlagSet { fs, _ := newIssueLinkFlagSet(""); return fs },
		"review open":    func() *flag.FlagSet { fs, _ := newReviewOpenFlagSet(""); return fs },
		"review comment": func() *flag.FlagSet { fs, _ := newReviewCommentFlagSet(""); return fs },
		"review approve": func() *flag.FlagSet { fs, _ := newReviewApproveFlagSet(""); return fs },
		"review status":  func() *flag.FlagSet { fs, _ := newReviewStatusFlagSet(""); return fs },
		"review list":    func() *flag.FlagSet { fs, _ := newReviewListFlagSet(""); return fs },
		"sync":           func() *flag.FlagSet { fs, _ := newSyncFlagSet(""); return fs },
	}
}

func commandFlags(path []string, c *command) []flagSpec {
	cmdPath := strings.Join(path, " ")
	ctor, ok := flagSetConstructors[cmdPath]
	if !ok {
		return c.Flags
	}
	fs := ctor()
	flags := make([]flagSpec, len(c.Flags))
	for i, f := range c.Flags {
		flagObj := fs.Lookup(f.Name)
		arg, usage := "", ""
		if flagObj != nil {
			arg, usage = flag.UnquoteUsage(flagObj)
		}
		flags[i] = flagSpec{
			Name:       f.Name,
			Arg:        arg,
			Usage:      usage,
			Values:     f.Values,
			Repeatable: f.Repeatable,
		}
	}
	return flags
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

	flags := commandFlags(path, c)
	if len(flags) > 0 {
		fmt.Fprintln(w, "Flags:")
		maxFlagLen := 0
		type flagFormat struct {
			display string
			usage   string
		}
		var formatted []flagFormat
		for _, f := range flags {
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
