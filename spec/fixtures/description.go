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

// ExpectDesc specifies the declared machine-readable expectation for an op commit:
// either accept, or reject with a reason from the closed set.
type ExpectDesc struct {
	Accept bool   `yaml:"accept,omitempty"`
	Reject string `yaml:"reject,omitempty"`
}

func (e *ExpectDesc) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		if value.Value == "accept" {
			e.Accept = true
			e.Reject = ""
			return nil
		}
		return fmt.Errorf("invalid expect value %q: scalar must be 'accept'", value.Value)
	}
	if value.Kind == yaml.MappingNode {
		var m struct {
			Accept bool   `yaml:"accept"`
			Reject string `yaml:"reject"`
		}
		if err := value.Decode(&m); err != nil {
			return err
		}
		if m.Accept && m.Reject != "" {
			return fmt.Errorf("expect cannot specify both accept and reject")
		}
		if !m.Accept && m.Reject == "" {
			return fmt.Errorf("expect must specify either accept: true or reject: <reason>")
		}
		e.Accept = m.Accept
		e.Reject = m.Reject
		return nil
	}
	return fmt.Errorf("expect must be a scalar ('accept') or a mapping ({reject: <reason>})")
}

// CommitDesc is one commit: full file contents or an op block for its tree,
// an identity naming a signer from the keyring in keys/, a fixed timestamp,
// and optional parent labels, committer override, signing override, or tamper instruction.
type CommitDesc struct {
	ID        string            `yaml:"id,omitempty"`
	Parents   []string          `yaml:"parents,omitempty"`
	Author    string            `yaml:"author"`
	Committer string            `yaml:"committer,omitempty"`
	Timestamp time.Time         `yaml:"timestamp"`
	Message   string            `yaml:"message,omitempty"`
	Files     map[string]string `yaml:"files,omitempty"`
	Op        *OpDesc           `yaml:"op,omitempty"`
	SignAs    string            `yaml:"sign_as,omitempty"`
	Tamper    string            `yaml:"tamper,omitempty"`
	Unsigned  bool              `yaml:"unsigned,omitempty"`
	Expect    *ExpectDesc       `yaml:"expect,omitempty"`
}

var validRejectReasons = map[string]bool{
	"wrong-key":             true,
	"payload-mutated":       true,
	"corrupted-signature":   true,
	"unsigned":              true,
	"non-canonical-payload": true,
	"duplicate-key":         true,
	"lone-surrogate":        true,
	"schema-violation":      true,
	"extra-tree-entry":      true,
	"op-json-subdirectory":  true,
	"missing-op-json":       true,
	"invalid-op-json-mode":  true,
	"committer-mismatch":    true,
}

// Load parses a single fixture description from YAML and validates its integrity.
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

	seenRefNames := map[string]string{}
	seenLabels := map[string]bool{}

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
				if c.ID != "" {
					if seenLabels[c.ID] {
						return nil, fmt.Errorf("fixtures: description %q: duplicate commit id %q", d.Name, c.ID)
					}
					seenLabels[c.ID] = true
				}
				if c.Parents != nil {
					for _, p := range c.Parents {
						if !seenLabels[p] {
							return nil, fmt.Errorf("fixtures: description %q: commit references unknown or forward parent label %q", d.Name, p)
						}
					}
				}
				if c.Op != nil {
					if len(c.Files) > 0 {
						return nil, fmt.Errorf("fixtures: description %q ref %q generation %d commit %d specifies both 'op' and 'files'", d.Name, r.Name, gi, ci)
					}
					if c.Message != "" {
						return nil, fmt.Errorf("fixtures: description %q ref %q generation %d commit %d specifies both 'op' and 'message'", d.Name, r.Name, gi, ci)
					}
					if c.Op.ObjectID == "" {
						return nil, fmt.Errorf("fixtures: description %q ref %q generation %d commit %d op missing object_id", d.Name, r.Name, gi, ci)
					}
					if c.Op.ObjectType == "" {
						return nil, fmt.Errorf("fixtures: description %q ref %q generation %d commit %d op missing object_type", d.Name, r.Name, gi, ci)
					}
					if c.Op.OpType == "" {
						return nil, fmt.Errorf("fixtures: description %q ref %q generation %d commit %d op missing op_type", d.Name, r.Name, gi, ci)
					}
				}
				if c.SignAs != "" {
					if _, err := lookupIdentity(c.SignAs); err != nil {
						return nil, fmt.Errorf("fixtures: description %q ref %q generation %d commit %d invalid sign_as: %w", d.Name, r.Name, gi, ci, err)
					}
				}
				if c.Tamper != "" {
					if !IsValidTamper(c.Tamper) {
						return nil, fmt.Errorf("fixtures: description %q ref %q generation %d commit %d invalid tamper %q (must be closed enum)", d.Name, r.Name, gi, ci, c.Tamper)
					}
				}
				if c.Expect != nil && c.Expect.Reject != "" {
					if !validRejectReasons[c.Expect.Reject] {
						return nil, fmt.Errorf("fixtures: description %q ref %q generation %d commit %d invalid reject reason %q (must be closed enum)", d.Name, r.Name, gi, ci, c.Expect.Reject)
					}
				}
			}
		}
	}
	return &d, nil
}
