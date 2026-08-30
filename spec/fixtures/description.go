// Package fixtures generates the conformance-corpus git repositories from
// declarative descriptions. See README.md in this directory for why the
// corpus is built this way rather than committed as bundles or tarballs.
package fixtures

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Description is the declarative, checked-in definition of one fixture
// repo: a set of refs, each built from one or more history generations.
// A ref with more than one generation models a force-push: every
// generation but the last represents history that was once at the ref's
// tip and later got pushed over.
type Description struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Refs        []RefDesc `yaml:"refs"`
}

// RefDesc describes one ref and everything ever pushed to it, oldest
// generation first. The final generation's tip is the ref's value in the
// generated repo.
type RefDesc struct {
	Name    string       `yaml:"name"`
	History []Generation `yaml:"history"`
}

// Generation is one contiguous commit chain, rooted (its first commit has
// no parent) rather than built on a previous generation — force-pushed
// history in real git need not share ancestry with what it replaces, and
// keeping generations independent keeps both the format and the generator
// simple. If a fixture needs a shared-ancestry force-push, model the
// shared commits as a literal prefix repeated in both generations; content
// addressing collapses them back onto the same objects, which is exactly
// what real git does when two histories share a base.
//
// A generation with no KeptAs is a true force-push casualty: its commits
// are written to the object store (so they're inspectable by SHA, and so
// determinism covers them) but no ref points at them in the generated
// repo, matching how an overwritten branch tip actually behaves in git
// until GC. Set KeptAs to also expose the generation under an auxiliary
// ref, e.g. for fixtures that need to assert against the pre-rewrite
// state directly (the orphaned-anchors family).
type Generation struct {
	KeptAs  string       `yaml:"keep_as,omitempty"`
	Commits []CommitDesc `yaml:"commits"`
}

// CommitDesc is one commit: full file contents for its tree (not a diff
// against the previous commit), an identity naming a signer from the
// keyring in keys/, and a fixed timestamp. All three are required because
// byte-determinism depends on none of them ever coming from the
// environment (no time.Now, no ambient git config).
type CommitDesc struct {
	Author    string            `yaml:"author"`
	Timestamp time.Time         `yaml:"timestamp"`
	Message   string            `yaml:"message"`
	Files     map[string]string `yaml:"files"`
}

// Load parses a single fixture description from YAML.
func Load(data []byte) (*Description, error) {
	var d Description
	if err := yaml.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("fixtures: parse description: %w", err)
	}
	if d.Name == "" {
		return nil, fmt.Errorf("fixtures: description missing name")
	}
	if len(d.Refs) == 0 {
		return nil, fmt.Errorf("fixtures: description %q has no refs", d.Name)
	}
	// Every ref name and every keep_as must be unique across the whole
	// description: they all become real refs written into the same repo,
	// and a collision would make one silently overwrite another at
	// generation time (git only has one namespace per ref name) while the
	// manifest still recorded both writes as if they'd each landed.
	// Maps a ref name already claimed to a description of where it came
	// from, so a collision error can say which two things collided
	// instead of just "seen twice" — a keep_as colliding with a
	// differently-named ref reads very differently from an actual
	// duplicate `name:` entry, and a fixture author debugging the
	// rejection needs to know which one they're looking at.
	seenRefNames := map[string]string{}
	for _, r := range d.Refs {
		if r.Name == "" {
			return nil, fmt.Errorf("fixtures: description %q has a ref with no name", d.Name)
		}
		if src, dup := seenRefNames[r.Name]; dup {
			return nil, fmt.Errorf("fixtures: description %q: ref %q collides with %s", d.Name, r.Name, src)
		}
		seenRefNames[r.Name] = fmt.Sprintf("ref %q", r.Name)
		if len(r.History) == 0 {
			return nil, fmt.Errorf("fixtures: description %q ref %q has no history", d.Name, r.Name)
		}
		for gi, g := range r.History {
			if len(g.Commits) == 0 {
				return nil, fmt.Errorf("fixtures: description %q ref %q generation %d has no commits", d.Name, r.Name, gi)
			}
			if g.KeptAs != "" {
				if src, dup := seenRefNames[g.KeptAs]; dup {
					return nil, fmt.Errorf("fixtures: description %q: keep_as %q (ref %q generation %d) collides with %s", d.Name, g.KeptAs, r.Name, gi, src)
				}
				seenRefNames[g.KeptAs] = fmt.Sprintf("keep_as %q (ref %q generation %d)", g.KeptAs, r.Name, gi)
			}
			for ci, c := range g.Commits {
				if c.Author == "" {
					return nil, fmt.Errorf("fixtures: description %q ref %q generation %d commit %d has no author", d.Name, r.Name, gi, ci)
				}
				if c.Timestamp.IsZero() {
					return nil, fmt.Errorf("fixtures: description %q ref %q generation %d commit %d has no timestamp", d.Name, r.Name, gi, ci)
				}
			}
		}
	}
	return &d, nil
}
