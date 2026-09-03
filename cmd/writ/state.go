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
	"github.com/writtendev/writ/spec"
)

func runState(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		renderUsage(stderr, []string{"state"}, stateCmd)
		return 2
	}

	switch args[0] {
	case "-h", "-help", "--help":
		renderUsage(stdout, []string{"state"}, stateCmd)
		return 0
	case "list":
		return runStateList(ctx, defaultDir, args[1:], stdout, stderr)
	case "create":
		return runStateCreate(ctx, defaultDir, args[1:], stdout, stderr)
	case "update":
		return runStateUpdate(ctx, defaultDir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "writ state: unknown command %q\n\n", args[0])
		renderUsage(stderr, []string{"state"}, stateCmd)
		return 2
	}
}

type stateListOpts struct {
	dir      string
	jsonMode bool
}

func newStateListFlagSet(defaultDir string) (*flag.FlagSet, *stateListOpts) {
	fs := flag.NewFlagSet("state list", flag.ContinueOnError)
	opts := &stateListOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	return fs, opts
}

func runStateList(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newStateListFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ state list: unexpected arguments: %s\n", strings.Join(posArgs, " "))
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

	states, err := store.Query.WorkflowStates(writ.WorkflowStateFilter{})
	if err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		wireSummaries := wire.FromWorkflowStateResultSummaries(states)
		if err := emitJSON(stdout, wire.KindStateList, wireSummaries); err != nil {
			fmt.Fprintf(stderr, "writ state list: marshal json: %v\n", err)
			return 1
		}
		return 0
	}

	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, st := range states {
		shortID := st.ObjectID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		color := st.WorkflowState.Color
		if color == "" {
			color = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", shortID, st.WorkflowState.Name, st.WorkflowState.Type, st.WorkflowState.Position, color)
	}
	_ = tw.Flush()
	return 0
}

type stateCreateOpts struct {
	dir         string
	name        string
	stateType   string
	position    string
	color       string
	description string
}

func newStateCreateFlagSet(defaultDir string) (*flag.FlagSet, *stateCreateOpts) {
	fs := flag.NewFlagSet("state create", flag.ContinueOnError)
	opts := &stateCreateOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.StringVar(&opts.name, "name", "", "State display name")
	fs.StringVar(&opts.stateType, "type", "", "State type ("+strings.Join(spec.WorkflowStateTypes(), ", ")+")")
	fs.StringVar(&opts.position, "position", "", "Fractional order position")
	fs.StringVar(&opts.color, "color", "", "Hex color client hint")
	fs.StringVar(&opts.description, "description", "", "State description")
	return fs, opts
}

func runStateCreate(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newStateCreateFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ state create: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}

	if opts.name == "" {
		fmt.Fprintln(stderr, "writ state create: -name is required")
		fs.Usage()
		return 2
	}

	if opts.stateType == "" {
		fmt.Fprintln(stderr, "writ state create: -type is required")
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

	id, err := store.WorkflowStates.Create(ctx, writ.NewWorkflowState{
		Name:        opts.name,
		Type:        opts.stateType,
		Position:    opts.position,
		Color:       opts.color,
		Description: opts.description,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s (%s) %s\n", id, opts.stateType, opts.name)
	return 0
}

type stateUpdateOpts struct {
	dir            string
	name           string
	nameSet        bool
	stateType      string
	position       string
	color          string
	colorSet       bool
	description    string
	descriptionSet bool
}

func newStateUpdateFlagSet(defaultDir string) (*flag.FlagSet, *stateUpdateOpts) {
	fs := flag.NewFlagSet("state update", flag.ContinueOnError)
	opts := &stateUpdateOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.Func("name", "State display name", func(s string) error {
		opts.name = s
		opts.nameSet = true
		return nil
	})
	fs.StringVar(&opts.stateType, "type", "", "State type ("+strings.Join(spec.WorkflowStateTypes(), ", ")+")")
	fs.StringVar(&opts.position, "position", "", "Fractional order position")
	fs.StringVar(&opts.color, "color", "", "Hex color client hint")
	fs.StringVar(&opts.description, "description", "", "State description")
	return fs, opts
}

func runStateUpdate(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newStateUpdateFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "color":
			opts.colorSet = true
		case "description":
			opts.descriptionSet = true
		}
	})

	if len(posArgs) == 0 {
		fmt.Fprintln(stderr, "writ state update: state ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ state update: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	hasField := opts.nameSet || opts.stateType != "" || opts.position != "" || opts.colorSet || opts.descriptionSet
	if !hasField {
		fmt.Fprintln(stderr, "writ state update: at least one update flag is required")
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

	stateID, err := resolveStateID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	var edit writ.WorkflowStateEdit
	if opts.nameSet {
		edit.Name = &opts.name
	}
	if opts.stateType != "" {
		edit.Type = &opts.stateType
	}
	if opts.position != "" {
		edit.Position = &opts.position
	}
	if opts.colorSet {
		edit.Color = &opts.color
	}
	if opts.descriptionSet {
		edit.Description = &opts.description
	}

	if err := store.WorkflowStates.Update(ctx, stateID, edit); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: updated\n", stateID)
	return 0
}

func resolveStateID(ctx context.Context, store *writ.Store, target string) (string, error) {
	states, err := store.Query.WorkflowStates(writ.WorkflowStateFilter{})
	if err != nil {
		return "", err
	}
	if len(states) == 0 {
		return "", fmt.Errorf("no workflow states defined")
	}
	var matches []writ.WorkflowStateResult
	for _, s := range states {
		if s.ObjectID == target || strings.HasPrefix(s.ObjectID, target) || strings.EqualFold(s.WorkflowState.Name, target) {
			matches = append(matches, s)
		}
	}
	if len(matches) == 1 {
		return matches[0].ObjectID, nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous state reference %q matches %d states", target, len(matches))
	}

	// If no match by ID or Name, check for aliases and semantic type matches
	if strings.EqualFold(target, "closed") {
		for _, s := range states {
			if s.WorkflowState.Type == "completed" {
				return s.ObjectID, nil
			}
		}
	}
	if strings.EqualFold(target, "open") {
		for _, s := range states {
			if s.WorkflowState.Type == "unstarted" {
				return s.ObjectID, nil
			}
		}
		for _, s := range states {
			if s.WorkflowState.Type == "backlog" {
				return s.ObjectID, nil
			}
		}
	}
	for _, s := range states {
		if strings.EqualFold(s.WorkflowState.Type, target) {
			return s.ObjectID, nil
		}
	}

	return "", fmt.Errorf("state %q not found", target)
}
