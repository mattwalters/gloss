package main

import (
	"flag"
	"sort"
	"strings"
	"testing"
)

func TestCommands_Drift(t *testing.T) {
	constructors := map[string]func() *flag.FlagSet{
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

	var collectLeaves func(path []string, cmd *command) []struct {
		path string
		cmd  *command
	}

	collectLeaves = func(path []string, cmd *command) []struct {
		path string
		cmd  *command
	} {
		var res []struct {
			path string
			cmd  *command
		}
		if len(cmd.Subs) > 0 {
			for _, sub := range cmd.Subs {
				res = append(res, collectLeaves(append(path, sub.Name), sub)...)
			}
		} else {
			res = append(res, struct {
				path string
				cmd  *command
			}{
				path: strings.Join(path, " "),
				cmd:  cmd,
			})
		}
		return res
	}

	leaves := collectLeaves(nil, rootCommand)

	for _, leaf := range leaves {
		cmdPath := leaf.path
		cmd := leaf.cmd

		// completion, help, and version don't have flag sets
		if cmdPath == "completion" || cmdPath == "help" || cmdPath == "version" {
			continue
		}

		ctor, ok := constructors[cmdPath]
		if !ok {
			t.Errorf("missing FlagSet constructor for command %q", cmdPath)
			continue
		}

		fs := ctor()
		var fsFlags []string
		fs.VisitAll(func(f *flag.Flag) {
			fsFlags = append(fsFlags, f.Name)
		})
		sort.Strings(fsFlags)

		var tableFlags []string
		for _, f := range cmd.Flags {
			tableFlags = append(tableFlags, f.Name)
		}
		sort.Strings(tableFlags)

		fsJoined := strings.Join(fsFlags, ",")
		tableJoined := strings.Join(tableFlags, ",")

		if fsJoined != tableJoined {
			t.Errorf("command %q flag drift: FlagSet has [%s] but table has [%s]", cmdPath, fsJoined, tableJoined)
		}
	}
}

func TestCommands_ExamplesAndMetadata(t *testing.T) {
	var walk func(path []string, cmd *command)
	walk = func(path []string, cmd *command) {
		cmdPath := strings.Join(path, " ")
		if len(cmd.Subs) > 0 {
			for _, sub := range cmd.Subs {
				walk(append(path, sub.Name), sub)
			}
		} else {
			if len(cmd.Examples) == 0 {
				t.Errorf("leaf command %q has no examples", cmdPath)
			}
			for _, ex := range cmd.Examples {
				if !strings.HasPrefix(ex, "writ ") {
					t.Errorf("command %q example %q does not start with 'writ '", cmdPath, ex)
				}
			}
			if cmd.Short == "" {
				t.Errorf("leaf command %q has empty Short description", cmdPath)
			}
			if !strings.HasPrefix(cmd.UsageLine, "Usage: writ ") {
				t.Errorf("leaf command %q UsageLine %q does not start with 'Usage: writ '", cmdPath, cmd.UsageLine)
			}
		}
	}

	for _, sub := range rootCommand.Subs {
		walk([]string{sub.Name}, sub)
	}
}
