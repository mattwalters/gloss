package fixtures

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
)

var (
	flagUpdateGoldenVal bool
	flagUpdateVal       bool
)

func init() {
	if flag.CommandLine.Lookup("update-golden") == nil {
		flag.CommandLine.BoolVar(&flagUpdateGoldenVal, "update-golden", false, "update golden files instead of checking against them")
	}
	if flag.CommandLine.Lookup("update") == nil {
		flag.CommandLine.BoolVar(&flagUpdateVal, "update", false, "alias for -update-golden")
	}
}

// UpdateGolden reports whether golden file updates have been explicitly
// requested via the -update-golden / -update command-line flag or the
// WRIT_UPDATE_GOLDEN / UPDATE_GOLDEN environment variable.
func UpdateGolden() bool {
	if flagUpdateGoldenVal || flagUpdateVal {
		return true
	}
	if f := flag.CommandLine.Lookup("update-golden"); f != nil {
		if getter, ok := f.Value.(flag.Getter); ok {
			if v, ok := getter.Get().(bool); ok && v {
				return true
			}
		}
	}
	if f := flag.CommandLine.Lookup("update"); f != nil {
		if getter, ok := f.Value.(flag.Getter); ok {
			if v, ok := getter.Get().(bool); ok && v {
				return true
			}
		}
	}
	env := os.Getenv("WRIT_UPDATE_GOLDEN")
	if env == "" {
		env = os.Getenv("UPDATE_GOLDEN")
	}
	return env == "1" || strings.EqualFold(env, "true")
}

// TestReporter is the interface subset of *testing.T used to report fixture
// execution results, errors, diffs, and logs.
type TestReporter interface {
	Helper()
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
	Logf(format string, args ...any)
	TempDir() string
}

// Fixture represents a loaded conformance fixture and its freshly generated
// git repository on disk.
type Fixture struct {
	// Name is the unique name of the fixture (e.g. "linear-history").
	Name string

	// Description is the parsed declarative YAML description.
	Description *Description

	// RepoDir is the directory path containing the generated bare git repo.
	RepoDir string

	// Repo is the opened go-git repository instance.
	Repo *git.Repository

	// Manifest is the manifest produced when generating the repo.
	Manifest *Manifest

	labels map[string]string
}

// CommitSHA returns the full commit SHA corresponding to a commit ID/label
// defined in the fixture description, or false if the label is unknown.
func (f *Fixture) CommitSHA(label string) (string, bool) {
	if f.labels == nil {
		return "", false
	}
	sha, ok := f.labels[label]
	return sha, ok
}

// TargetRef returns the canonical ref name corresponding to the given commit SHA
// in the generated repo manifest. If the commit was kept under a keep_as ref,
// that ref name is returned. Otherwise, the containing ref name is returned.
func (f *Fixture) TargetRef(commitSHA string) string {
	if f.Manifest == nil {
		return ""
	}
	for _, gs := range f.Manifest.Generations {
		for _, c := range gs.Commits {
			if c.SHA == commitSHA {
				if gs.KeptAs != "" {
					return gs.KeptAs
				}
				return gs.Ref
			}
		}
	}
	for _, r := range f.Manifest.Refs {
		if r.Commit == commitSHA {
			return r.Name
		}
	}
	return ""
}

// RunnerFunc is the function that executes a fixture against the system under
// test (e.g., op codec, fold reducer, or SQLite projection) and returns
// byte-for-byte serialized golden output.
type RunnerFunc func(t *testing.T, fix *Fixture) ([]byte, error)

// ReporterRunnerFunc is a runner function that accepts a TestReporter.
type ReporterRunnerFunc func(t TestReporter, fix *Fixture) ([]byte, error)

// Family defines a conformance fixture family and how its fixtures are run
// and verified against golden files.
type Family struct {
	// Name identifies the fixture family (e.g., "manifest", "fold", "codec").
	Name string

	// Corpus is the set of fixture descriptions to run. If nil and CorpusDir
	// is empty, defaults to LoadCorpus() (the standard spec corpus).
	Corpus []*Description

	// CorpusDir optionally specifies a filesystem directory to load YAML
	// descriptions from, if Corpus is nil.
	CorpusDir string

	// Filter optionally selects a subset of descriptions to include.
	Filter func(desc *Description) bool

	// GoldenDir is the directory where golden files are stored.
	// Defaults to "testdata/golden/<Name>" (or "testdata/golden" for manifest).
	GoldenDir string

	// GoldenExt is the file extension for golden files. Defaults to ".json".
	GoldenExt string

	// UpdateGolden forces updating golden files if true. If false, respects
	// UpdateGolden() (the -update-golden CLI flag and env vars).
	UpdateGolden bool

	// Runner executes each fixture and returns the golden output bytes.
	Runner RunnerFunc
}

// TestRunner is the interface subset of *testing.T that supports subtests and reports.
type TestRunner interface {
	TestReporter
	Run(name string, f func(t *testing.T)) bool
	Skip(args ...any)
}

// Run executes all fixtures in the family as subtests of t, comparing
// runner output byte-for-byte against golden files.
func Run(t *testing.T, family Family) {
	t.Helper()
	runFamily(t, family)
}

func runFamily(t TestRunner, family Family) {
	t.Helper()

	if family.Runner == nil {
		t.Fatalf("fixtures: Family.Runner must not be nil")
		return
	}

	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not on PATH; fixture generation needs it to sign commits")
	}

	corpus := family.Corpus
	if corpus == nil {
		var err error
		if family.CorpusDir != "" {
			corpus, err = LoadCorpusFromDir(family.CorpusDir)
		} else {
			corpus, err = LoadCorpus()
		}
		if err != nil {
			t.Fatalf("fixtures: load corpus: %v", err)
		}
	}
	if len(corpus) == 0 {
		t.Fatalf("fixtures: no fixture descriptions found")
		return
	}

	goldenDir := resolveGoldenDir(family.GoldenDir, family.Name)
	goldenExt := resolveGoldenExt(family.GoldenExt)
	isUpdating := family.UpdateGolden || UpdateGolden()

	matchedCount := 0
	for _, desc := range corpus {
		desc := desc
		if family.Filter != nil && !family.Filter(desc) {
			continue
		}
		matchedCount++

		t.Run(desc.Name, func(t *testing.T) {
			runner := func(tr TestReporter, fix *Fixture) ([]byte, error) {
				return family.Runner(t, fix)
			}
			runSingleFixture(t, desc, goldenDir, goldenExt, isUpdating, runner)
		})
	}

	if matchedCount == 0 {
		t.Fatalf("fixtures: no fixtures matched the specified filter")
	}
}

// RunFamily is a convenience wrapper that runs a fixture family with the
// default spec corpus and standard golden directory.
func RunFamily(t *testing.T, name string, runner RunnerFunc) {
	t.Helper()
	Run(t, Family{
		Name:   name,
		Runner: runner,
	})
}

func resolveGoldenDir(configuredDir, familyName string) string {
	if configuredDir != "" {
		return configuredDir
	}
	if familyName != "" && familyName != "manifest" {
		return filepath.Join("testdata", "golden", familyName)
	}
	return filepath.Join("testdata", "golden")
}

func resolveGoldenExt(ext string) string {
	if ext == "" {
		return ".json"
	}
	if !strings.HasPrefix(ext, ".") {
		return "." + ext
	}
	return ext
}

func runSingleFixture(t TestReporter, desc *Description, goldenDir, goldenExt string, isUpdating bool, runner ReporterRunnerFunc) {
	t.Helper()

	repoDir := filepath.Join(t.TempDir(), "repo")
	manifest, err := Generate(desc, repoDir)
	if err != nil {
		t.Fatalf("fixtures: generate %s: %v", desc.Name, err)
		return
	}

	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("fixtures: open repo %s: %v", desc.Name, err)
		return
	}

	labels := make(map[string]string)
	genIdx := 0
	for _, ref := range desc.Refs {
		for _, gen := range ref.History {
			if genIdx < len(manifest.Generations) {
				gs := manifest.Generations[genIdx]
				for ci, cd := range gen.Commits {
					if cd.ID != "" && ci < len(gs.Commits) {
						labels[cd.ID] = gs.Commits[ci].SHA
					}
				}
			}
			genIdx++
		}
	}

	fix := &Fixture{
		Name:        desc.Name,
		Description: desc,
		RepoDir:     repoDir,
		Repo:        repo,
		Manifest:    manifest,
		labels:      labels,
	}

	got, err := runner(t, fix)
	if err != nil {
		t.Fatalf("fixtures: runner %s: %v", desc.Name, err)
		return
	}

	goldenPath := filepath.Join(goldenDir, desc.Name+goldenExt)

	if isUpdating {
		want, err := os.ReadFile(goldenPath)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
				t.Fatalf("fixtures: mkdir for %s: %v", goldenPath, err)
				return
			}
			if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
				t.Fatalf("fixtures: write new golden %s: %v", goldenPath, err)
				return
			}
			t.Logf("[NEW GOLDEN] created %s (%d bytes)", goldenPath, len(got))
			return
		}
		if err != nil {
			t.Fatalf("fixtures: read existing golden %s: %v", goldenPath, err)
			return
		}

		if bytes.Equal(got, want) {
			t.Logf("[UNCHANGED] %s", goldenPath)
			return
		}

		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("fixtures: update golden %s: %v", goldenPath, err)
			return
		}
		diff := Diff(goldenPath+" (old)", want, goldenPath+" (new)", got)
		t.Logf("[UPDATED GOLDEN] %s:\n%s", goldenPath, diff)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		t.Fatalf("golden file %s does not exist; run with -update-golden to create it", goldenPath)
		return
	}
	if err != nil {
		t.Fatalf("fixtures: read golden %s: %v", goldenPath, err)
		return
	}

	if !bytes.Equal(got, want) {
		diff := Diff(goldenPath+" (golden)", want, "got (actual output)", got)
		t.Errorf("fixture %s output does not match golden file %s\n\n%s", desc.Name, goldenPath, diff)
	}
}
