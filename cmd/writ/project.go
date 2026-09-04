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
	"github.com/writtendev/writ/spec"
)

func runProject(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		renderUsage(stderr, []string{"project"}, projectCmd)
		return 2
	}

	switch args[0] {
	case "-h", "-help", "--help":
		renderUsage(stdout, []string{"project"}, projectCmd)
		return 0
	case "create":
		return runProjectCreate(ctx, defaultDir, args[1:], stdout, stderr)
	case "list":
		return runProjectList(ctx, defaultDir, args[1:], stdout, stderr)
	case "show":
		return runProjectShow(ctx, defaultDir, args[1:], stdout, stderr)
	case "update":
		return runProjectUpdate(ctx, defaultDir, args[1:], stdout, stderr)
	case "status":
		return runProjectStatus(ctx, defaultDir, args[1:], stdout, stderr)
	case "add":
		return runProjectAdd(ctx, defaultDir, args[1:], stdout, stderr)
	case "remove":
		return runProjectRemove(ctx, defaultDir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "writ project: unknown command %q\n\n", args[0])
		renderUsage(stderr, []string{"project"}, projectCmd)
		return 2
	}
}

type projectCreateOpts struct {
	dir         string
	title       string
	description string
	jsonMode    bool
}

func newProjectCreateFlagSet(defaultDir string) (*flag.FlagSet, *projectCreateOpts) {
	fs := flag.NewFlagSet("project create", flag.ContinueOnError)
	opts := &projectCreateOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.StringVar(&opts.title, "t", "", "Project title")
	fs.StringVar(&opts.description, "description", "", "Project description")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"project", "create"}, projectCreateCmd)
	}
	return fs, opts
}

func runProjectCreate(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newProjectCreateFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	title := opts.title
	if title == "" && len(posArgs) > 0 {
		title = strings.Join(posArgs, " ")
		posArgs = nil
	}
	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ project create: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}
	if title == "" {
		fmt.Fprintln(stderr, "writ project create: project title is required (-t <title>)")
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

	projectID, err := store.Projects.Create(ctx, writ.NewProject{
		Title:       title,
		Description: opts.description,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		proj, err := store.Query.Project(projectID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindProjectCreate, wire.FromProjectResult(proj)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintln(stdout, projectID)
	return 0
}

type projectListOpts struct {
	dir       string
	statuses  stringList
	text      string
	limit     int
	sortOrder string
	jsonMode  bool
}

func newProjectListFlagSet(defaultDir string) (*flag.FlagSet, *projectListOpts) {
	fs := flag.NewFlagSet("project list", flag.ContinueOnError)
	opts := &projectListOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.Var(&opts.statuses, "status", "Filter by project status (repeatable)")
	fs.StringVar(&opts.text, "text", "", "Filter by text match in title or description")
	fs.IntVar(&opts.limit, "limit", 0, "Maximum number of projects to return")
	fs.StringVar(&opts.sortOrder, "sort", "", "Sort order (created_at_asc, created_at_desc, updated_at_asc, updated_at_desc, title_asc, title_desc)")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"project", "list"}, projectListCmd)
	}
	return fs, opts
}

func runProjectList(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newProjectListFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ project list: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}

	for _, s := range opts.statuses {
		if !slices.Contains(spec.ProjectStatuses(), s) {
			fmt.Fprintf(stderr, "writ project list: invalid status %q (must be %s)\n", s, spec.FormatOptions(spec.ProjectStatuses()))
			fs.Usage()
			return 2
		}
	}

	if opts.limit < 0 {
		fmt.Fprintf(stderr, "writ project list: -limit must be non-negative, got %d\n", opts.limit)
		fs.Usage()
		return 2
	}

	var orderBy writ.OrderBy
	if opts.sortOrder != "" {
		var err error
		orderBy, err = parseOrderBy(opts.sortOrder)
		if err != nil {
			fmt.Fprintf(stderr, "writ project list: invalid sort order %q\n", opts.sortOrder)
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

	projects, err := store.Query.Projects(writ.ProjectFilter{
		Status:  opts.statuses,
		Text:    opts.text,
		Limit:   opts.limit,
		OrderBy: orderBy,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		if err := emitJSON(stdout, wire.KindProjectList, wire.FromProjectResults(projects)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	if len(projects) == 0 {
		return 0
	}

	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, p := range projects {
		shortID := p.ObjectID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		status := p.Project.Status
		if status == "" {
			status = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d issues\n", shortID, status, p.Project.Title, len(p.Project.Issues))
	}
	tw.Flush()
	return 0
}

type projectShowOpts struct {
	dir      string
	jsonMode bool
}

func newProjectShowFlagSet(defaultDir string) (*flag.FlagSet, *projectShowOpts) {
	fs := flag.NewFlagSet("project show", flag.ContinueOnError)
	opts := &projectShowOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"project", "show"}, projectShowCmd)
	}
	return fs, opts
}

func runProjectShow(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newProjectShowFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(posArgs) == 0 {
		fmt.Fprintln(stderr, "writ project show: project ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ project show: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
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

	projectID, err := resolveProjectID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	proj, err := store.Query.Project(projectID)
	if err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		if err := emitJSON(stdout, wire.KindProjectShow, wire.FromProjectResult(proj)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "# %s\n", proj.Project.Title)
	fmt.Fprintf(stdout, "ID: %s\n", proj.ObjectID)
	if proj.Project.Status != "" {
		fmt.Fprintf(stdout, "Status: %s\n", proj.Project.Status)
	}
	if proj.Project.Reason != "" {
		fmt.Fprintf(stdout, "Reason: %s\n", proj.Project.Reason)
	}
	if proj.Project.Description != "" {
		fmt.Fprintf(stdout, "\n%s\n", proj.Project.Description)
	}
	if len(proj.Project.Issues) > 0 {
		fmt.Fprintf(stdout, "\nIssues:\n")
		for _, iss := range proj.Project.Issues {
			fmt.Fprintf(stdout, "  %s\n", iss)
		}
	}

	return 0
}

type projectUpdateOpts struct {
	dir            string
	title          string
	description    string
	descriptionSet bool
	jsonMode       bool
}

func newProjectUpdateFlagSet(defaultDir string) (*flag.FlagSet, *projectUpdateOpts) {
	fs := flag.NewFlagSet("project update", flag.ContinueOnError)
	opts := &projectUpdateOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.StringVar(&opts.title, "t", "", "New title")
	fs.StringVar(&opts.description, "description", "", "New description")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"project", "update"}, projectUpdateCmd)
	}
	return fs, opts
}

func runProjectUpdate(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newProjectUpdateFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	fs.Visit(func(f *flag.Flag) {
		if f.Name == "description" {
			opts.descriptionSet = true
		}
	})

	if len(posArgs) == 0 {
		fmt.Fprintln(stderr, "writ project update: project ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ project update: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	if opts.title == "" && !opts.descriptionSet {
		fmt.Fprintln(stderr, "writ project update: at least one of -t or -description is required")
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

	projectID, err := resolveProjectID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	var edit writ.ProjectEdit
	if opts.title != "" {
		edit.Title = &opts.title
	}
	if opts.descriptionSet {
		edit.Description = &opts.description
	}

	if err := store.Projects.Update(ctx, projectID, edit); err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		proj, err := store.Query.Project(projectID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindProjectUpdate, wire.FromProjectResult(proj)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "%s (updated)\n", projectID)
	return 0
}

type projectStatusOpts struct {
	dir      string
	reason   string
	jsonMode bool
}

func newProjectStatusFlagSet(defaultDir string) (*flag.FlagSet, *projectStatusOpts) {
	fs := flag.NewFlagSet("project status", flag.ContinueOnError)
	opts := &projectStatusOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.StringVar(&opts.reason, "reason", "", "Reason for status change")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"project", "status"}, projectStatusCmd)
	}
	return fs, opts
}

func runProjectStatus(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newProjectStatusFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(posArgs) == 0 {
		fmt.Fprintln(stderr, "writ project status: project ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) == 1 {
		fmt.Fprintln(stderr, "writ project status: status is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 2 {
		fmt.Fprintf(stderr, "writ project status: unexpected arguments: %s\n", strings.Join(posArgs[2:], " "))
		fs.Usage()
		return 2
	}

	status := posArgs[1]
	if !slices.Contains(spec.ProjectStatuses(), status) {
		fmt.Fprintf(stderr, "writ project status: invalid status %q (must be %s)\n", status, spec.FormatOptions(spec.ProjectStatuses()))
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

	projectID, err := resolveProjectID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	if err := store.Projects.SetStatus(ctx, projectID, status, opts.reason); err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		proj, err := store.Query.Project(projectID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindProjectStatus, wire.FromProjectResult(proj)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "%s (status: %s)\n", projectID, status)
	return 0
}

type projectMembersOpts struct {
	dir      string
	jsonMode bool
}

func newProjectAddFlagSet(defaultDir string) (*flag.FlagSet, *projectMembersOpts) {
	fs := flag.NewFlagSet("project add", flag.ContinueOnError)
	opts := &projectMembersOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"project", "add"}, projectAddCmd)
	}
	return fs, opts
}

func newProjectRemoveFlagSet(defaultDir string) (*flag.FlagSet, *projectMembersOpts) {
	fs := flag.NewFlagSet("project remove", flag.ContinueOnError)
	opts := &projectMembersOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"project", "remove"}, projectRemoveCmd)
	}
	return fs, opts
}

func runProjectAdd(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	return runProjectMembers(ctx, defaultDir, args, stdout, stderr, "add", newProjectAddFlagSet)
}

func runProjectRemove(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	return runProjectMembers(ctx, defaultDir, args, stdout, stderr, "remove", newProjectRemoveFlagSet)
}

func runProjectMembers(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer, op string, newFlagSet func(string) (*flag.FlagSet, *projectMembersOpts)) int {
	fs, opts := newFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(posArgs) < 2 {
		fmt.Fprintf(stderr, "writ project %s: project ID and at least one issue reference are required\n", op)
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

	projectID, err := resolveProjectID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	for _, ref := range posArgs[1:] {
		var opErr error
		if op == "add" {
			opErr = store.Projects.AddIssue(ctx, projectID, ref)
		} else {
			opErr = store.Projects.RemoveIssue(ctx, projectID, ref)
		}
		if opErr != nil {
			return renderErr(stderr, opErr)
		}
	}

	if opts.jsonMode {
		proj, err := store.Query.Project(projectID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindProjectMembers, wire.FromProjectResult(proj)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "%s (%sed %d issue(s))\n", projectID, op, len(posArgs[1:]))
	return 0
}
