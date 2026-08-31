package fixtures

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// treeNode is either file content (string) with an optional file mode or
// a subdirectory (map[string]*treeNode), built up from a flat map of
// slash-separated paths before being written out as nested git tree objects.
type treeNode struct {
	content  string
	mode     filemode.FileMode
	isDir    bool
	children map[string]*treeNode
}

// buildTree writes files as a (possibly nested) git tree with default regular
// file mode (100644) and returns its hash. files maps a slash-separated path
// to full file content — each commit specifies its complete tree, not a diff
// against the parent.
func buildTree(store storer.EncodedObjectStorer, files map[string]string) (plumbing.Hash, error) {
	return buildTreeWithModes(store, files, nil)
}

// buildTreeWithModes writes files as a git tree using custom file modes for
// paths specified in modes (defaulting to 100644 filemode.Regular if omitted
// or zero).
func buildTreeWithModes(store storer.EncodedObjectStorer, files map[string]string, modes map[string]filemode.FileMode) (plumbing.Hash, error) {
	root := &treeNode{isDir: true, children: map[string]*treeNode{}}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		var mode filemode.FileMode
		if modes != nil {
			mode = modes[p]
		}
		if mode == 0 {
			mode = filemode.Regular
		}
		if err := insertPath(root, strings.Split(p, "/"), files[p], mode); err != nil {
			return plumbing.ZeroHash, fmt.Errorf("fixtures: file path %q: %w", p, err)
		}
	}
	return writeTreeNode(store, root)
}

func insertPath(node *treeNode, parts []string, content string, mode filemode.FileMode) error {
	name := parts[0]
	if name == "" {
		return fmt.Errorf("empty path segment")
	}
	if len(parts) == 1 {
		// This check and the symmetric one below make collision detection
		// independent of insertion order, even though buildTree's sorted
		// insertion currently means a leaf/directory collision on the same
		// name is always caught by the other branch first (a leaf path is
		// always a lexicographic prefix of, and so always sorts before,
		// any deeper path through a directory of the same name). Kept
		// order-independent on purpose so this doesn't silently break if
		// that sort ever changes.
		if existing, ok := node.children[name]; ok && existing.isDir {
			return fmt.Errorf("collides with directory %q", name)
		}
		node.children[name] = &treeNode{content: content, mode: mode}
		return nil
	}
	child, ok := node.children[name]
	if !ok {
		child = &treeNode{isDir: true, children: map[string]*treeNode{}}
		node.children[name] = child
	} else if !child.isDir {
		return fmt.Errorf("collides with file %q", name)
	}
	return insertPath(child, parts[1:], content, mode)
}

// writeTreeNode recursively writes node's children as blobs/subtrees and
// returns the resulting tree object's hash. Entries are sorted the way
// git requires: byte order by name, except directory names sort as if
// suffixed with "/" — which only differs from a plain name sort when one
// entry is a strict prefix of another (e.g. "foo" vs "foo.txt" vs a
// directory "foo").
func writeTreeNode(store storer.EncodedObjectStorer, node *treeNode) (plumbing.Hash, error) {
	names := make([]string, 0, len(node.children))
	for name := range node.children {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return treeSortKey(node.children[names[i]], names[i]) < treeSortKey(node.children[names[j]], names[j])
	})

	tree := &object.Tree{}
	for _, name := range names {
		child := node.children[name]
		if child.isDir {
			hash, err := writeTreeNode(store, child)
			if err != nil {
				return plumbing.ZeroHash, err
			}
			tree.Entries = append(tree.Entries, object.TreeEntry{Name: name, Mode: filemode.Dir, Hash: hash})
			continue
		}
		blobHash, err := writeBlob(store, child.content)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		mode := child.mode
		if mode == 0 {
			mode = filemode.Regular
		}
		tree.Entries = append(tree.Entries, object.TreeEntry{Name: name, Mode: mode, Hash: blobHash})
	}

	obj := store.NewEncodedObject()
	obj.SetType(plumbing.TreeObject)
	if err := tree.Encode(obj); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("fixtures: encode tree: %w", err)
	}
	return store.SetEncodedObject(obj)
}

func treeSortKey(n *treeNode, name string) string {
	if n.isDir {
		return name + "/"
	}
	return name
}

func writeBlob(store storer.EncodedObjectStorer, content string) (plumbing.Hash, error) {
	obj := store.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("fixtures: open blob writer: %w", err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("fixtures: write blob: %w", err)
	}
	if err := w.Close(); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("fixtures: close blob writer: %w", err)
	}
	return store.SetEncodedObject(obj)
}
