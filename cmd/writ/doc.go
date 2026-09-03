package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
	"github.com/writtendev/writ/engine"
)

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringList) Set(val string) error {
	*s = append(*s, val)
	return nil
}

func runDoc(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		renderUsage(stderr, []string{"doc"}, docCmd)
		return 2
	}

	switch args[0] {
	case "-h", "-help", "--help":
		renderUsage(stdout, []string{"doc"}, docCmd)
		return 0
	case "create":
		return runDocCreate(ctx, defaultDir, args[1:], stdout, stderr)
	case "list":
		return runDocList(ctx, defaultDir, args[1:], stdout, stderr)
	case "show":
		return runDocShow(ctx, defaultDir, args[1:], stdout, stderr)
	case "edit":
		return runDocEdit(ctx, defaultDir, args[1:], stdout, stderr)
	case "link":
		return runDocLink(ctx, defaultDir, args[1:], stdout, stderr)
	case "section":
		return runDocSection(ctx, defaultDir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "writ doc: unknown command %q\n\n", args[0])
		renderUsage(stderr, []string{"doc"}, docCmd)
		return 2
	}
}

type docCreateOpts struct {
	dir      string
	title    string
	links    stringList
	labels   stringList
	jsonMode bool
}

func newDocCreateFlagSet(defaultDir string) (*flag.FlagSet, *docCreateOpts) {
	fs := flag.NewFlagSet("doc create", flag.ContinueOnError)
	opts := &docCreateOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.StringVar(&opts.title, "t", "", "Document title")
	fs.Var(&opts.links, "link", "Link in target:relation[:type] format (repeatable)")
	fs.Var(&opts.labels, "label", "Label to attach (repeatable)")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"doc", "create"}, docCreateCmd)
	}
	return fs, opts
}

func parseLinkFlag(s string) (writ.Link, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return writ.Link{}, fmt.Errorf("invalid link format %q (expected target:relation[:type])", s)
	}
	target := parts[0]
	relation := parts[1]
	targetType := ""
	if len(parts) >= 3 {
		targetType = parts[2]
	}
	return writ.Link{
		Target:     target,
		Relation:   relation,
		TargetType: targetType,
	}, nil
}

func runDocCreate(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newDocCreateFlagSet(defaultDir)
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
		fmt.Fprintf(stderr, "writ doc create: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}
	if title == "" {
		fmt.Fprintln(stderr, "writ doc create: document title is required (-t <title>)")
		fs.Usage()
		return 2
	}

	var links []writ.Link
	for _, lStr := range opts.links {
		l, err := parseLinkFlag(lStr)
		if err != nil {
			fmt.Fprintf(stderr, "writ doc create: %v\n", err)
			return 2
		}
		links = append(links, l)
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

	docID, err := store.Documents.Create(ctx, writ.NewDocument{
		Title:  title,
		Labels: opts.labels,
		Links:  links,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		doc, err := store.Query.Document(docID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindDocCreate, wire.FromDocumentResult(doc)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintln(stdout, docID)
	return 0
}

type docListOpts struct {
	dir      string
	labels   stringList
	jsonMode bool
}

func newDocListFlagSet(defaultDir string) (*flag.FlagSet, *docListOpts) {
	fs := flag.NewFlagSet("doc list", flag.ContinueOnError)
	opts := &docListOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.Var(&opts.labels, "label", "Filter by label (repeatable)")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"doc", "list"}, docListCmd)
	}
	return fs, opts
}

func runDocList(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newDocListFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ doc list: unexpected arguments: %s\n", strings.Join(posArgs, " "))
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

	docs, err := store.Query.Documents(writ.DocumentFilter{
		Labels: opts.labels,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		if err := emitJSON(stdout, wire.KindDocList, wire.FromDocumentResults(docs)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	if len(docs) == 0 {
		return 0
	}

	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, d := range docs {
		shortID := d.ObjectID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		labelStr := ""
		if len(d.Document.Labels) > 0 {
			labelStr = "[" + strings.Join(d.Document.Labels, ", ") + "]"
		}
		secCount := fmt.Sprintf("%d sections", len(d.Sections))
		if len(d.Sections) == 1 {
			secCount = "1 section"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", shortID, d.Document.Title, secCount, labelStr)
	}
	tw.Flush()
	return 0
}

type docShowOpts struct {
	dir      string
	jsonMode bool
}

func newDocShowFlagSet(defaultDir string) (*flag.FlagSet, *docShowOpts) {
	fs := flag.NewFlagSet("doc show", flag.ContinueOnError)
	opts := &docShowOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"doc", "show"}, docShowCmd)
	}
	return fs, opts
}

func runDocShow(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newDocShowFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(posArgs) == 0 {
		fmt.Fprintln(stderr, "writ doc show: document ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ doc show: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
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

	docID, err := resolveDocumentID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	doc, err := store.Query.Document(docID)
	if err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		if err := emitJSON(stdout, wire.KindDocShow, wire.FromDocumentResult(doc)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "# %s\n", doc.Document.Title)
	fmt.Fprintf(stdout, "ID: %s\n", doc.ObjectID)
	if len(doc.Document.Labels) > 0 {
		fmt.Fprintf(stdout, "Labels: %s\n", strings.Join(doc.Document.Labels, ", "))
	}
	if len(doc.Document.Links) > 0 {
		var linkStrs []string
		for _, l := range doc.Document.Links {
			linkStrs = append(linkStrs, fmt.Sprintf("%s (%s)", l.Target, l.Relation))
		}
		fmt.Fprintf(stdout, "Links: %s\n", strings.Join(linkStrs, ", "))
	}
	fmt.Fprintln(stdout)

	for i, sec := range doc.Sections {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		secTitle := sec.Section.Title
		if secTitle == "" {
			secTitle = fmt.Sprintf("Section %s", sec.ObjectID[:8])
		}
		fmt.Fprintf(stdout, "## %s\n\n", secTitle)

		if sec.Section.IsConflicted() {
			bodies := sec.Section.ConflictBodies()
			fmt.Fprintln(stdout, "<<<<<<<")
			for j, b := range bodies {
				if j > 0 {
					fmt.Fprintln(stdout, "=======")
				}
				fmt.Fprintln(stdout, b)
			}
			fmt.Fprintln(stdout, ">>>>>>>")
		} else {
			body := sec.Section.SettledBody()
			if body != "" {
				fmt.Fprintln(stdout, body)
			}
		}
	}

	return 0
}

type docEditOpts struct {
	dir          string
	title        string
	addLabels    stringList
	removeLabels stringList
	jsonMode     bool
}

func newDocEditFlagSet(defaultDir string) (*flag.FlagSet, *docEditOpts) {
	fs := flag.NewFlagSet("doc edit", flag.ContinueOnError)
	opts := &docEditOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.StringVar(&opts.title, "t", "", "New title")
	fs.Var(&opts.addLabels, "label", "Add label (repeatable)")
	fs.Var(&opts.removeLabels, "remove-label", "Remove label (repeatable)")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"doc", "edit"}, docEditCmd)
	}
	return fs, opts
}

func runDocEdit(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newDocEditFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(posArgs) == 0 {
		fmt.Fprintln(stderr, "writ doc edit: document ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ doc edit: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	if opts.title == "" && len(opts.addLabels) == 0 && len(opts.removeLabels) == 0 {
		fmt.Fprintln(stderr, "writ doc edit: at least one edit flag (-t, -label, -remove-label) is required")
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

	docID, err := resolveDocumentID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	var titlePtr *string
	if opts.title != "" {
		titlePtr = &opts.title
	}

	var labelEdit *writ.DocumentLabelEdit
	if len(opts.addLabels) > 0 || len(opts.removeLabels) > 0 {
		labelEdit = &writ.DocumentLabelEdit{
			Add:    opts.addLabels,
			Remove: opts.removeLabels,
		}
	}

	if err := store.Documents.Update(ctx, docID, writ.DocumentEdit{
		Title:  titlePtr,
		Labels: labelEdit,
	}); err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		doc, err := store.Query.Document(docID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindDocEdit, wire.FromDocumentResult(doc)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "%s (updated)\n", docID)
	return 0
}

type docLinkOpts struct {
	dir        string
	target     string
	relation   string
	targetType string
	jsonMode   bool
}

func newDocLinkFlagSet(defaultDir string) (*flag.FlagSet, *docLinkOpts) {
	fs := flag.NewFlagSet("doc link", flag.ContinueOnError)
	opts := &docLinkOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.StringVar(&opts.target, "target", "", "Target entity identifier")
	fs.StringVar(&opts.relation, "relation", "", "Relationship predicate")
	fs.StringVar(&opts.targetType, "target-type", "", "Optional target type")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"doc", "link"}, docLinkCmd)
	}
	return fs, opts
}

func runDocLink(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newDocLinkFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(posArgs) == 0 {
		fmt.Fprintln(stderr, "writ doc link: document ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ doc link: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}
	if opts.target == "" || opts.relation == "" {
		fmt.Fprintln(stderr, "writ doc link: --target and --relation are required")
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

	docID, err := resolveDocumentID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	if err := store.Documents.Link(ctx, docID, writ.Link{
		Target:     opts.target,
		Relation:   opts.relation,
		TargetType: opts.targetType,
	}); err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		doc, err := store.Query.Document(docID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindDocLink, wire.FromDocumentResult(doc)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "%s linked to %s (%s)\n", docID, opts.target, opts.relation)
	return 0
}

func runDocSection(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		renderUsage(stderr, []string{"doc", "section"}, docSectionCmd)
		return 2
	}

	switch args[0] {
	case "-h", "-help", "--help":
		renderUsage(stdout, []string{"doc", "section"}, docSectionCmd)
		return 0
	case "add":
		return runDocSectionAdd(ctx, defaultDir, args[1:], stdout, stderr)
	case "edit":
		return runDocSectionEdit(ctx, defaultDir, args[1:], stdout, stderr)
	case "move":
		return runDocSectionMove(ctx, defaultDir, args[1:], stdout, stderr)
	case "delete":
		return runDocSectionDelete(ctx, defaultDir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "writ doc section: unknown subcommand %q\n\n", args[0])
		renderUsage(stderr, []string{"doc", "section"}, docSectionCmd)
		return 2
	}
}

type docSectionAddOpts struct {
	dir      string
	title    string
	message  string
	file     string
	after    string
	before   string
	position string
	jsonMode bool
}

func newDocSectionAddFlagSet(defaultDir string) (*flag.FlagSet, *docSectionAddOpts) {
	fs := flag.NewFlagSet("doc section add", flag.ContinueOnError)
	opts := &docSectionAddOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.StringVar(&opts.title, "t", "", "Section title")
	fs.StringVar(&opts.message, "m", "", "Section body content")
	fs.StringVar(&opts.file, "F", "", "Read section body from file ('-' for stdin)")
	fs.StringVar(&opts.after, "after", "", "Insert section after specified section ID")
	fs.StringVar(&opts.before, "before", "", "Insert section before specified section ID")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"doc", "section", "add"}, docSectionAddCmd)
	}
	return fs, opts
}

func readBodyInput(msg, file string) (string, error) {
	if msg != "" && file != "" {
		return "", fmt.Errorf("cannot specify both message (-m) and file (-F)")
	}
	if msg != "" {
		return msg, nil
	}
	if file != "" {
		if file == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return "", fmt.Errorf("read stdin: %w", err)
			}
			return string(data), nil
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", file, err)
		}
		return string(data), nil
	}
	return "", nil
}

func runDocSectionAdd(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newDocSectionAddFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(posArgs) == 0 {
		fmt.Fprintln(stderr, "writ doc section add: document ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ doc section add: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	body, err := readBodyInput(opts.message, opts.file)
	if err != nil {
		fmt.Fprintf(stderr, "writ doc section add: %v\n", err)
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

	docID, err := resolveDocumentID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	var afterID, beforeID string
	if opts.after != "" {
		resolved, err := resolveSectionID(ctx, store, opts.after)
		if err != nil {
			return renderErr(stderr, err)
		}
		afterID = resolved
	}
	if opts.before != "" {
		resolved, err := resolveSectionID(ctx, store, opts.before)
		if err != nil {
			return renderErr(stderr, err)
		}
		beforeID = resolved
	}

	secID, err := store.Documents.AddSection(ctx, docID, writ.NewSection{
		Title:    opts.title,
		Body:     body,
		Position: opts.position,
		After:    afterID,
		Before:   beforeID,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		sec, err := store.Query.Section(secID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindDocSection, wire.FromSectionResult(sec)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintln(stdout, secID)
	return 0
}

type docSectionEditOpts struct {
	dir      string
	message  string
	file     string
	jsonMode bool
}

func newDocSectionEditFlagSet(defaultDir string) (*flag.FlagSet, *docSectionEditOpts) {
	fs := flag.NewFlagSet("doc section edit", flag.ContinueOnError)
	opts := &docSectionEditOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.StringVar(&opts.message, "m", "", "New section body")
	fs.StringVar(&opts.file, "F", "", "Read new section body from file ('-' for stdin)")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"doc", "section", "edit"}, docSectionEditCmd)
	}
	return fs, opts
}

func runDocSectionEdit(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newDocSectionEditFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(posArgs) == 0 {
		fmt.Fprintln(stderr, "writ doc section edit: section ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ doc section edit: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	body, err := readBodyInput(opts.message, opts.file)
	if err != nil {
		fmt.Fprintf(stderr, "writ doc section edit: %v\n", err)
		return 2
	}
	if opts.message == "" && opts.file == "" {
		fmt.Fprintln(stderr, "writ doc section edit: -m or -F is required")
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

	secID, err := resolveSectionID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	if err := store.Documents.EditSection(ctx, secID, body); err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		sec, err := store.Query.Section(secID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindDocSection, wire.FromSectionResult(sec)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "%s (edited)\n", secID)
	return 0
}

type docSectionMoveOpts struct {
	dir      string
	after    string
	before   string
	jsonMode bool
}

func newDocSectionMoveFlagSet(defaultDir string) (*flag.FlagSet, *docSectionMoveOpts) {
	fs := flag.NewFlagSet("doc section move", flag.ContinueOnError)
	opts := &docSectionMoveOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.StringVar(&opts.after, "after", "", "Move section after specified section ID")
	fs.StringVar(&opts.before, "before", "", "Move section before specified section ID")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"doc", "section", "move"}, docSectionMoveCmd)
	}
	return fs, opts
}

func runDocSectionMove(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newDocSectionMoveFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(posArgs) == 0 {
		fmt.Fprintln(stderr, "writ doc section move: section ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ doc section move: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}
	if opts.after == "" && opts.before == "" {
		fmt.Fprintln(stderr, "writ doc section move: at least one of --after or --before is required")
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

	secID, err := resolveSectionID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	var afterID, beforeID string
	if opts.after != "" {
		resolved, err := resolveSectionID(ctx, store, opts.after)
		if err != nil {
			return renderErr(stderr, err)
		}
		afterID = resolved
	}
	if opts.before != "" {
		resolved, err := resolveSectionID(ctx, store, opts.before)
		if err != nil {
			return renderErr(stderr, err)
		}
		beforeID = resolved
	}

	if err := store.Documents.MoveSection(ctx, secID, afterID, beforeID); err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		sec, err := store.Query.Section(secID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindDocSection, wire.FromSectionResult(sec)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "%s (moved)\n", secID)
	return 0
}

type docSectionDeleteOpts struct {
	dir      string
	jsonMode bool
}

func newDocSectionDeleteFlagSet(defaultDir string) (*flag.FlagSet, *docSectionDeleteOpts) {
	fs := flag.NewFlagSet("doc section delete", flag.ContinueOnError)
	opts := &docSectionDeleteOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"doc", "section", "delete"}, docSectionDeleteCmd)
	}
	return fs, opts
}

func runDocSectionDelete(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newDocSectionDeleteFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if len(posArgs) == 0 {
		fmt.Fprintln(stderr, "writ doc section delete: section ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ doc section delete: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
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

	secID, err := resolveSectionID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	if err := store.Documents.DeleteSection(ctx, secID); err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		data := map[string]any{
			"object_id": secID,
			"deleted":   true,
		}
		if err := emitJSON(stdout, wire.KindDocSection, data); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "%s (deleted)\n", secID)
	return 0
}
