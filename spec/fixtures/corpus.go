package fixtures

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

//go:embed testdata/descriptions/*.yaml
var descriptionsFS embed.FS

const descriptionsDir = "testdata/descriptions"

// LoadCorpus loads and parses every fixture description checked into
// testdata/descriptions, sorted by filename so corpus order never
// depends on directory-listing order.
func LoadCorpus() ([]*Description, error) {
	entries, err := fs.ReadDir(descriptionsFS, descriptionsDir)
	if err != nil {
		return nil, fmt.Errorf("fixtures: list descriptions: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	descs := make([]*Description, 0, len(names))
	for _, name := range names {
		data, err := descriptionsFS.ReadFile(descriptionsDir + "/" + name)
		if err != nil {
			return nil, fmt.Errorf("fixtures: read %s: %w", name, err)
		}
		d, err := Load(data)
		if err != nil {
			return nil, fmt.Errorf("fixtures: %s: %w", name, err)
		}
		descs = append(descs, d)
	}
	return descs, nil
}
