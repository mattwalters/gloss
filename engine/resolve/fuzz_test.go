package resolve_test

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/writtendev/writ/engine/resolve"
	"github.com/writtendev/writ/spec"
)

const (
	anchorSchemaID     = "https://writ.dev/spec/anchor.schema.json"
	resolutionSchemaID = "https://writ.dev/spec/resolution.schema.json"
)

var getCompiledResolutionSchema = sync.OnceValue(func() *jsonschema.Schema {
	rawAnchor, err := spec.FS.ReadFile("schemas/anchor.schema.json")
	if err != nil {
		panic(err)
	}
	anchorDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawAnchor))
	if err != nil {
		panic(err)
	}

	rawRes, err := spec.FS.ReadFile("schemas/resolution.schema.json")
	if err != nil {
		panic(err)
	}
	resDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawRes))
	if err != nil {
		panic(err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(anchorSchemaID, anchorDoc); err != nil {
		panic(err)
	}
	if err := c.AddResource(resolutionSchemaID, resDoc); err != nil {
		panic(err)
	}
	sch, err := c.Compile(resolutionSchemaID)
	if err != nil {
		panic(err)
	}
	return sch
})

func FuzzResolve(f *testing.F) {
	// Seed with resolution vectors
	if cases, err := spec.ResolutionVectors(); err == nil {
		for _, c := range cases {
			var filesMapJSON []byte
			if targetBytes, err := json.Marshal(c.Target.Files); err == nil {
				filesMapJSON = targetBytes
			}
			f.Add([]byte(c.Anchor), filesMapJSON)
		}
	}

	// Seed with anchor vectors
	if entries, err := spec.FS.ReadDir("testdata/anchors/valid"); err == nil {
		for _, e := range entries {
			if raw, err := spec.FS.ReadFile("testdata/anchors/valid/" + e.Name()); err == nil {
				f.Add(raw, []byte(`{"main.go":"package main\n"}`))
			}
		}
	}

	sch := getCompiledResolutionSchema()

	f.Fuzz(func(t *testing.T, anchorData []byte, targetData []byte) {
		anchor, err := resolve.ParseAnchor(anchorData)
		if err != nil {
			return
		}

		var filesMap map[string]string
		_ = json.Unmarshal(targetData, &filesMap)
		files := make(map[string][]byte, len(filesMap))
		for k, v := range filesMap {
			files[k] = []byte(v)
		}

		tree := resolve.NewTree(files, resolve.SHA1)
		outcome := resolve.Resolve(anchor, tree)

		// Assert outcome validates against resolution.schema.json if anchor had at least one side
		if anchor.Old != nil || anchor.New != nil {
			outcomeJSON, err := json.Marshal(outcome)
			if err != nil {
				t.Fatalf("marshaling outcome: %v", err)
			}
			inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(outcomeJSON))
			if err != nil {
				t.Fatalf("decoding outcome JSON for schema validation: %v", err)
			}
			if err := sch.Validate(inst); err != nil {
				t.Fatalf("resolution outcome failed schema validation: %v\noutcome: %s", err, string(outcomeJSON))
			}
		}
	})
}
