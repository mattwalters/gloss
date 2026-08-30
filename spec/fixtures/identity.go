package fixtures

import "fmt"

// identity is a fixed, fixture-only author/signer. Timestamps come from
// the description, never from these — identity only fixes name, email,
// and signing key.
type identity struct {
	Name    string
	Email   string
	KeyFile string // basename under keys/, e.g. "alice_ed25519"
}

// identities is the fixed cast of fixture authors. Every description in
// this repo must reference one of these by name; the set is intentionally
// small and closed so the keyring in keys/ never needs to grow silently.
var identities = map[string]identity{
	"alice": {Name: "Alice Example", Email: "alice@example.test", KeyFile: "alice_ed25519"},
	"bob":   {Name: "Bob Example", Email: "bob@example.test", KeyFile: "bob_ed25519"},
}

func lookupIdentity(name string) (identity, error) {
	id, ok := identities[name]
	if !ok {
		return identity{}, fmt.Errorf("fixtures: unknown identity %q (see identity.go for the fixed cast)", name)
	}
	return id, nil
}
