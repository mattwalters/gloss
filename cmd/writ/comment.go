package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
	"github.com/writtendev/writ/engine"
)

func runComment(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		renderUsage(stderr, []string{"comment"}, commentCmd)
		return 2
	}

	switch args[0] {
	case "-h", "-help", "--help":
		renderUsage(stdout, []string{"comment"}, commentCmd)
		return 0
	case "help":
		return runHelp(append([]string{"comment"}, args[1:]...), stdout, stderr)
	}

	targetDir := defaultDir
	if args[0] == "-C" {
		if len(args) < 2 {
			fmt.Fprintln(stderr, "writ comment: option -C requires an argument")
			return 2
		}
		targetDir = args[1]
		args = args[2:]
		if len(args) == 0 {
			renderUsage(stderr, []string{"comment"}, commentCmd)
			return 2
		}
	}

	switch args[0] {
	case "-h", "-help", "--help":
		renderUsage(stdout, []string{"comment"}, commentCmd)
		return 0
	case "help":
		return runHelp(append([]string{"comment"}, args[1:]...), stdout, stderr)
	case "edit":
		return runCommentEdit(ctx, targetDir, args[1:], stdout, stderr)
	case "delete":
		return runCommentDelete(ctx, targetDir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "writ comment: unknown subcommand %q\n\n", args[0])
		renderUsage(stderr, []string{"comment"}, commentCmd)
		return 2
	}
}

type commentEditOpts struct {
	dir      string
	message  string
	jsonMode bool
}

func newCommentEditFlagSet(defaultDir string) (*flag.FlagSet, *commentEditOpts) {
	fs := flag.NewFlagSet("comment edit", flag.ContinueOnError)
	opts := &commentEditOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.StringVar(&opts.message, "m", "", "Comment message `<msg>`")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output result as JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"comment", "edit"}, commentEditCmd)
	}
	return fs, opts
}

func runCommentEdit(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newCommentEditFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) == 0 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "writ comment edit: comment ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ comment edit: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
		fs.Usage()
		return 2
	}

	if opts.message == "" {
		fmt.Fprintln(stderr, "writ comment edit: -m is required")
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

	commentID, err := resolveCommentID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	if err := store.Comments.Edit(ctx, commentID, opts.message); err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		res, err := fetchCommentResult(ctx, store, commentID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindCommentEdit, wire.FromCommentResult(res)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "%s (edited)\n", commentID)
	return 0
}

type commentDeleteOpts struct {
	dir      string
	jsonMode bool
}

func newCommentDeleteFlagSet(defaultDir string) (*flag.FlagSet, *commentDeleteOpts) {
	fs := flag.NewFlagSet("comment delete", flag.ContinueOnError)
	opts := &commentDeleteOpts{}
	fs.StringVar(&opts.dir, "C", defaultDir, "Run as if writ was started in `<dir>`")
	fs.BoolVar(&opts.jsonMode, "json", false, "Output result as JSON")
	fs.Usage = func() {
		renderUsage(fs.Output(), []string{"comment", "delete"}, commentDeleteCmd)
	}
	return fs, opts
}

func runCommentDelete(ctx context.Context, defaultDir string, args []string, stdout, stderr io.Writer) int {
	fs, opts := newCommentDeleteFlagSet(defaultDir)
	fs.SetOutput(stderr)

	posArgs, err := parseArgs(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if len(posArgs) == 0 || posArgs[0] == "" {
		fmt.Fprintln(stderr, "writ comment delete: comment ID is required")
		fs.Usage()
		return 2
	}
	if len(posArgs) > 1 {
		fmt.Fprintf(stderr, "writ comment delete: unexpected arguments: %s\n", strings.Join(posArgs[1:], " "))
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

	commentID, err := resolveCommentID(ctx, store, posArgs[0])
	if err != nil {
		return renderErr(stderr, err)
	}

	if err := store.Comments.Delete(ctx, commentID); err != nil {
		return renderErr(stderr, err)
	}

	if opts.jsonMode {
		res, err := fetchCommentResult(ctx, store, commentID)
		if err != nil {
			return renderErr(stderr, err)
		}
		if err := emitJSON(stdout, wire.KindCommentDelete, wire.FromCommentResult(res)); err != nil {
			return renderErr(stderr, err)
		}
		return 0
	}

	fmt.Fprintf(stdout, "%s (deleted)\n", commentID)
	return 0
}

func fetchCommentResult(ctx context.Context, store *writ.Store, commentID string) (writ.CommentResult, error) {
	comments, err := store.Query.Comments(writ.CommentFilter{IncludeDeleted: true})
	if err != nil {
		return writ.CommentResult{}, err
	}
	for _, c := range comments {
		if c.ObjectID == commentID {
			return c, nil
		}
	}
	return writ.CommentResult{}, notFoundError{kind: "comment", id: commentID}
}
