package codec_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/writtendev/writ/engine/codec"
	"github.com/writtendev/writ/spec"
)

type forwardCompatIndexEntry struct {
	Disposition string   `json:"disposition"`
	Rules       []string `json:"rules"`
	Reason      string   `json:"reason"`
}

func loadTestProfile(t *testing.T) codec.Profile {
	t.Helper()
	raw, err := spec.FS.ReadFile("testdata/forward-compat/reader-profile.json")
	if err != nil {
		t.Fatalf("reading reader profile: %v", err)
	}
	var p codec.Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("decoding reader profile: %v", err)
	}
	return p
}

func loadTestForwardCompatIndex(t *testing.T) map[string]forwardCompatIndexEntry {
	t.Helper()
	raw, err := spec.FS.ReadFile("testdata/forward-compat/index.json")
	if err != nil {
		t.Fatalf("reading forward-compat index.json: %v", err)
	}
	var index map[string]forwardCompatIndexEntry
	if err := json.Unmarshal(raw, &index); err != nil {
		t.Fatalf("decoding forward-compat index.json: %v", err)
	}
	return index
}

func TestForwardCompatOps(t *testing.T) {
	profile := loadTestProfile(t)
	index := loadTestForwardCompatIndex(t)

	entries, err := spec.FS.ReadDir("testdata/forward-compat/ops")
	if err != nil {
		t.Fatalf("reading forward-compat ops dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no forward-compat ops found")
	}

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			entry, ok := index[name]
			if !ok {
				t.Fatalf("instance %s missing from index.json", name)
			}

			raw, err := spec.FS.ReadFile("testdata/forward-compat/ops/" + name)
			if err != nil {
				t.Fatalf("reading %s: %v", name, err)
			}

			env, err := codec.DecodePayload(raw)
			if err != nil {
				t.Fatalf("DecodePayload failed on forward-compat op %s: %v", name, err)
			}

			disp := profile.Classify(env)
			if string(disp) != entry.Disposition {
				t.Errorf("disposition mismatch for %s: got %q, want %q (reason: %s)", name, disp, entry.Disposition, entry.Reason)
			}

			// Unknown-field preservation (FC-2, FC-3, FC-11)
			reencoded, err := codec.EncodePayload(env)
			if err != nil {
				t.Fatalf("EncodePayload failed to re-encode %s: %v", name, err)
			}

			if !bytes.Equal(reencoded, raw) {
				t.Errorf("re-encoded bytes do not match original for %s:\n got:  %s\n want: %s", name, reencoded, raw)
			}
		})
	}
}

func TestUnknownTopLevelFieldPreserved(t *testing.T) {
	raw, err := spec.FS.ReadFile("testdata/forward-compat/ops/unknown-top-level-field.json")
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}

	env, err := codec.DecodePayload(raw)
	if err != nil {
		t.Fatalf("DecodePayload failed: %v", err)
	}

	if env.Unknown == nil {
		t.Fatal("expected non-nil Unknown map for unknown-top-level-field.json")
	}
	clientInfo, ok := env.Unknown["client_info"]
	if !ok {
		t.Fatal("missing 'client_info' in env.Unknown")
	}
	if string(clientInfo) != `{"build":42,"name":"writ-web"}` {
		t.Errorf("unexpected client_info value: %s", clientInfo)
	}
}
