// Command gen is the one command that regenerates the whole fixture
// corpus: it builds every description under spec/fixtures/testdata/descriptions
// into a real git repository, and either writes or checks each one's
// manifest against the committed golden file in
// spec/fixtures/testdata/golden.
//
// Usage:
//
//	go run ./spec/fixtures/gen [-out DIR] [-update-golden]
//
// With no flags, every fixture is (re)built into -out (default:
// spec/fixtures/out, gitignored — inspect the repos there) and checked
// against its golden manifest; a mismatch exits non-zero. Pass
// -update-golden after intentionally changing a description to write new
// golden files instead of checking against the old ones.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/writtendev/writ/spec/fixtures"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gen:", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	out := fs.String("out", filepath.Join("spec", "fixtures", "out"), "directory to generate fixture repos into")
	updateGolden := fs.Bool("update-golden", false, "write golden manifests instead of checking against them")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	descs, err := fixtures.LoadCorpus()
	if err != nil {
		return err
	}

	if err := os.RemoveAll(*out); err != nil {
		return fmt.Errorf("clear %s: %w", *out, err)
	}

	mismatch := false
	for _, desc := range descs {
		repoDir := filepath.Join(*out, desc.Name)
		manifest, err := fixtures.Generate(desc, repoDir)
		if err != nil {
			return fmt.Errorf("generate %s: %w", desc.Name, err)
		}

		got, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal manifest for %s: %w", desc.Name, err)
		}
		got = append(got, '\n')

		goldenPath := filepath.Join("spec", "fixtures", "testdata", "golden", desc.Name+".json")
		if *updateGolden {
			want, err := os.ReadFile(goldenPath)
			if os.IsNotExist(err) {
				if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
					return fmt.Errorf("write golden for %s: %w", desc.Name, err)
				}
				fmt.Printf("[NEW GOLDEN] %s (%d bytes)\n", desc.Name, len(got))
				continue
			}
			if err != nil {
				return fmt.Errorf("read existing golden %s: %w", desc.Name, err)
			}
			if string(got) == string(want) {
				fmt.Printf("[UNCHANGED] %s\n", desc.Name)
				continue
			}
			if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
				return fmt.Errorf("write golden for %s: %w", desc.Name, err)
			}
			diff := fixtures.Diff(goldenPath+" (old)", want, goldenPath+" (new)", got)
			fmt.Printf("[UPDATED GOLDEN] %s:\n%s\n", desc.Name, diff)
			continue
		}

		want, err := os.ReadFile(goldenPath)
		if err != nil {
			return fmt.Errorf("read golden for %s (run with -update-golden if this is a new fixture): %w", desc.Name, err)
		}
		if string(got) != string(want) {
			diff := fixtures.Diff(goldenPath+" (golden)", want, "generated output", got)
			fmt.Printf("MISMATCH: %s does not match %s\n\n%s\n", desc.Name, goldenPath, diff)
			mismatch = true
			continue
		}
		fmt.Printf("ok: %s -> %s\n", desc.Name, repoDir)
	}

	if mismatch {
		return fmt.Errorf("one or more fixtures do not match their golden manifest; run with -update-golden if the change was intentional")
	}
	return nil
}
