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
)

func runLabel(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		renderUsage(stderr, []string{"label"}, labelCmd)
		return 2
	}

	switch args[0] {
	case "-h", "-help", "--help":
		renderUsage(stdout, []string{"label"}, labelCmd)
		return 0
	case "list":
		return runLabelList(ctx, defaultDir, args[1:], stdout, stderr)
	case "create":
		return runLabelCreate(ctx, defaultDir, args[1:], stdout, stderr)
	case "edit", "update":
		return runLabelEdit(ctx, defaultDir, args[1:], stdout, stderr)
	case "migrate":
		return runLabelMigrate(ctx, defaultDir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "writ label: unknown command %q\n\n", args[0])
		renderUsage(stderr, []string{"label"}, labelCmd)
		return 2
	}
}

type labelListOpts struct {
	dir      string
	jsonMode bool
}

func newLabelListFlagSet(defaultDir string) (*flag.FlagSet, *labelListOpts) {
	fs := flag.NewFlagSet("label list", flag.ContinueOnError)
	opts := &labelListOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	return fs, opts
}

func runLabelList(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newLabelListFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ label list: unexpected arguments: %s\n", strings.Join(posArgs, " "))
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

	labels, err := store.Query.Labels(writ.LabelFilter{})
	if err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		wireSummaries := wire.FromLabelResultSummaries(labels)
		if err := emitJSON(stdout, wire.KindLabelList, wireSummaries); err != nil {
			fmt.Fprintf(stderr, "writ label list: marshal json: %v\n", err)
			return 1
		}
		return 0
	}

	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	for _, l := range labels {
		shortID := l.ObjectID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		color := l.Label.Color
		if color == "" {
			color = "-"
		}
		desc := l.Label.Description
		if desc == "" {
			desc = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", shortID, l.Label.Name, color, desc)
	}
	_ = tw.Flush()
	return 0
}

type labelCreateOpts struct {
	dir         string
	name        string
	color       string
	description string
}

func newLabelCreateFlagSet(defaultDir string) (*flag.FlagSet, *labelCreateOpts) {
	fs := flag.NewFlagSet("label create", flag.ContinueOnError)
	opts := &labelCreateOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.StringVar(&opts.name, "name", "", "Label display name")
	fs.StringVar(&opts.color, "color", "", "Hex color client hint")
	fs.StringVar(&opts.description, "description", "", "Label description")
	return fs, opts
}

func runLabelCreate(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newLabelCreateFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ label create: unexpected arguments: %s\n", strings.Join(posArgs, " "))
		fs.Usage()
		return 2
	}

	if opts.name == "" {
		fmt.Fprintln(stderr, "writ label create: -name is required")
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

	id, err := store.Labels.Create(ctx, writ.NewLabel{
		Name:        opts.name,
		Color:       opts.color,
		Description: opts.description,
	})
	if err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s %s\n", id, opts.name)
	return 0
}

type labelEditOpts struct {
	dir            string
	name           string
	nameSet        bool
	color          string
	colorSet       bool
	description    string
	descriptionSet bool
}

func newLabelEditFlagSet(defaultDir string) (*flag.FlagSet, *labelEditOpts) {
	fs := flag.NewFlagSet("label edit", flag.ContinueOnError)
	opts := &labelEditOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.Func("name", "Label display name", func(s string) error {
		opts.name = s
		opts.nameSet = true
		return nil
	})
	fs.StringVar(&opts.color, "color", "", "Hex color client hint")
	fs.StringVar(&opts.description, "description", "", "Label description")
	return fs, opts
}

func runLabelEdit(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newLabelEditFlagSet(defaultDir)
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
		fmt.Fprintln(stderr, "writ label edit: label ID or name is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ label edit: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	hasField := opts.nameSet || opts.colorSet || opts.descriptionSet
	if !hasField {
		fmt.Fprintln(stderr, "writ label edit: at least one update flag is required")
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

	labelID, err := resolveLabelID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	var edit writ.LabelEdit
	if opts.nameSet {
		edit.Name = &opts.name
	}
	if opts.colorSet {
		edit.Color = &opts.color
	}
	if opts.descriptionSet {
		edit.Description = &opts.description
	}

	if err := store.Labels.Update(ctx, labelID, edit); err != nil {
		return renderErr(stderr, err)
	}

	fmt.Fprintf(stdout, "%s: updated label\n", labelID)
	return 0
}

type labelMigrateOpts struct {
	dir      string
	jsonMode bool
}

func newLabelMigrateFlagSet(defaultDir string) (*flag.FlagSet, *labelMigrateOpts) {
	fs := flag.NewFlagSet("label migrate", flag.ContinueOnError)
	opts := &labelMigrateOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in <dir>")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output machine-readable JSON")
	return fs, opts
}

func isCanonicalHexID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < 32; i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func isQualifiedID(s string) bool {
	parts := strings.Split(s, "#")
	return len(parts) == 2 && isCanonicalHexID(parts[1])
}

func isLegacyBareLabel(s string) bool {
	return !isCanonicalHexID(s) && !isQualifiedID(s)
}

func runLabelMigrate(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newLabelMigrateFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) > 0 {
		fmt.Fprintf(stderr, "writ label migrate: unexpected arguments: %s\n", strings.Join(posArgs, " "))
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

	existingLabels, err := store.Query.Labels(writ.LabelFilter{})
	if err != nil {
		return renderErr(stderr, err)
	}

	existingByName := make(map[string]string)
	existingByID := make(map[string]bool)
	for _, l := range existingLabels {
		existingByName[strings.ToLower(l.Label.Name)] = l.ObjectID
		existingByID[l.ObjectID] = true
	}

	issues, err := store.Query.Issues(writ.IssueFilter{})
	if err != nil {
		return renderErr(stderr, err)
	}

	reviews, err := store.Query.Reviews(writ.ReviewFilter{})
	if err != nil {
		return renderErr(stderr, err)
	}

	// 1. Collect distinct bare strings across all issues and reviews
	distinctBare := make(map[string]string) // lower -> canonical case
	for _, iss := range issues {
		for _, lbl := range iss.Issue.Labels {
			if isLegacyBareLabel(lbl) && !existingByID[lbl] {
				low := strings.ToLower(lbl)
				if _, ok := distinctBare[low]; !ok {
					distinctBare[low] = lbl
				}
			}
		}
	}
	for _, rev := range reviews {
		for _, lbl := range rev.Review.Labels {
			if isLegacyBareLabel(lbl) && !existingByID[lbl] {
				low := strings.ToLower(lbl)
				if _, ok := distinctBare[low]; !ok {
					distinctBare[low] = lbl
				}
			}
		}
	}

	// 2. Create missing label objects
	labelsCreated := 0
	for low, rawName := range distinctBare {
		if _, ok := existingByName[low]; !ok {
			id, err := store.Labels.Create(ctx, writ.NewLabel{Name: rawName})
			if err != nil {
				return renderErr(stderr, fmt.Errorf("writ label migrate: create label %q: %w", rawName, err))
			}
			existingByName[low] = id
			existingByID[id] = true
			labelsCreated++
		}
	}

	// 3. Migrate issues
	migratedIssues := 0
	for _, iss := range issues {
		var toAdd []string
		var toRemove []string
		for _, lbl := range iss.Issue.Labels {
			if isLegacyBareLabel(lbl) && !existingByID[lbl] {
				targetID := existingByName[strings.ToLower(lbl)]
				if targetID != "" {
					toRemove = append(toRemove, lbl)
					toAdd = append(toAdd, targetID)
				}
			}
		}
		if len(toRemove) > 0 {
			if err := store.Issues.Label(ctx, iss.ObjectID, toAdd, toRemove); err != nil {
				return renderErr(stderr, fmt.Errorf("writ label migrate: update issue %s: %w", iss.ObjectID, err))
			}
			migratedIssues++
		}
	}

	// 4. Migrate reviews
	migratedReviews := 0
	for _, rev := range reviews {
		var toAdd []string
		var toRemove []string
		for _, lbl := range rev.Review.Labels {
			if isLegacyBareLabel(lbl) && !existingByID[lbl] {
				targetID := existingByName[strings.ToLower(lbl)]
				if targetID != "" {
					toRemove = append(toRemove, lbl)
					toAdd = append(toAdd, targetID)
				}
			}
		}
		if len(toRemove) > 0 {
			if err := store.Reviews.Label(ctx, rev.ObjectID, toAdd, toRemove); err != nil {
				return renderErr(stderr, fmt.Errorf("writ label migrate: update review %s: %w", rev.ObjectID, err))
			}
			migratedReviews++
		}
	}

	if opts.jsonMode {
		payload := map[string]any{
			"labels_created":   labelsCreated,
			"issues_migrated":  migratedIssues,
			"reviews_migrated": migratedReviews,
		}
		if err := emitJSON(stdout, "label.migrate", payload); err != nil {
			fmt.Fprintf(stderr, "writ label migrate: marshal json: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintf(stdout, "Migrated %d legacy label(s) across %d issue(s) and %d review(s)\n",
		labelsCreated, migratedIssues, migratedReviews)
	return 0
}

func resolveLabelID(ctx context.Context, store *writ.Store, target string) (string, error) {
	labels, err := store.Query.Labels(writ.LabelFilter{})
	if err != nil {
		return "", err
	}
	var matches []writ.LabelResult
	for _, l := range labels {
		if l.ObjectID == target || strings.HasPrefix(l.ObjectID, target) || strings.EqualFold(l.Label.Name, target) {
			matches = append(matches, l)
		}
	}
	if len(matches) == 1 {
		return matches[0].ObjectID, nil
	}
	if len(matches) > 1 {
		for _, m := range matches {
			if m.ObjectID == target || m.Label.Name == target {
				return m.ObjectID, nil
			}
		}
		return "", fmt.Errorf("ambiguous label reference %q matches %d labels", target, len(matches))
	}
	if isCanonicalHexID(target) {
		return target, nil
	}
	return "", fmt.Errorf("label %q not found", target)
}

func resolveLabelReference(ctx context.Context, store *writ.Store, target string) (string, error) {
	if isQualifiedID(target) {
		return target, nil
	}
	return resolveLabelID(ctx, store, target)
}

func resolveLabelsForModification(ctx context.Context, store *writ.Store, currentLabels, add, remove []string) ([]string, []string, error) {
	var resolvedAdd []string
	var resolvedRemove []string

	for _, a := range add {
		id, err := resolveLabelReference(ctx, store, a)
		if err != nil {
			return nil, nil, err
		}
		resolvedAdd = append(resolvedAdd, id)
	}

	for _, r := range remove {
		var removedAny bool
		// If r resolves to a known label ID, remove that ID
		if id, err := resolveLabelID(ctx, store, r); err == nil {
			resolvedRemove = append(resolvedRemove, id)
			removedAny = true
		}
		// Also if current object carries r verbatim (e.g. legacy string), remove that too
		foundVerbatim := false
		for _, cur := range currentLabels {
			if cur == r {
				foundVerbatim = true
				break
			}
		}
		if foundVerbatim {
			if len(resolvedRemove) == 0 || resolvedRemove[len(resolvedRemove)-1] != r {
				resolvedRemove = append(resolvedRemove, r)
			}
			removedAny = true
		} else if isCanonicalHexID(r) || isQualifiedID(r) {
			if len(resolvedRemove) == 0 || resolvedRemove[len(resolvedRemove)-1] != r {
				resolvedRemove = append(resolvedRemove, r)
			}
			removedAny = true
		}

		if !removedAny {
			return nil, nil, fmt.Errorf("label %q not found", r)
		}
	}

	return resolvedAdd, resolvedRemove, nil
}

