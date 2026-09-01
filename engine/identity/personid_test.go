package identity_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/writtendev/writ/engine/identity"
)

func TestDerivePersonID(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]string
		want string
	}{
		{
			name: "derived from user.email",
			cfg:  map[string]string{"user.email": "Alice@Example.COM"},
			want: "email:alice@example.com",
		},
		{
			name: "writ.personId overrides",
			cfg: map[string]string{
				"user.email":    "alice@example.com",
				"writ.personid": "  User:Alice  ",
				"user.name":     "Alice",
				"writ.writerid": "0123456789abcdef",
				"gpg.format":    "ssh",
			},
			want: "user:alice",
		},
		{
			name: "an unknown scheme is honoured, not second-guessed",
			cfg:  map[string]string{"writ.personid": "keybase:alice"},
			want: "keybase:alice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := identity.DerivePersonID(tc.cfg)
			if err != nil {
				t.Fatalf("DerivePersonID: %v", err)
			}
			if got != tc.want {
				t.Errorf("DerivePersonID = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDerivePersonIDErrors covers the two ways derivation fails. Both name the
// config key to set, because the only thing a user can do about either is set
// one — and neither may fall back to something that is not a person identifier.
func TestDerivePersonIDErrors(t *testing.T) {
	cases := []struct {
		name    string
		cfg     map[string]string
		wantKey string
		wantErr error
	}{
		{
			name:    "nothing to derive from",
			cfg:     map[string]string{"user.name": "Alice"},
			wantKey: identity.PersonIDKey,
			wantErr: identity.ErrMissing,
		},
		{
			name:    "whitespace user.email derives an empty value",
			cfg:     map[string]string{"user.email": "   "},
			wantKey: identity.PersonIDKey,
			wantErr: identity.ErrMissing,
		},
		{
			name:    "writ.personId with no scheme",
			cfg:     map[string]string{"writ.personid": "alice@example.com"},
			wantKey: identity.PersonIDKey,
			wantErr: identity.ErrInvalid,
		},
		{
			name:    "writ.personId with an over-long scheme",
			cfg:     map[string]string{"writ.personid": strings.Repeat("a", 33) + ":alice"},
			wantKey: identity.PersonIDKey,
			wantErr: identity.ErrInvalid,
		},
		{
			name:    "user.email too long to bound",
			cfg:     map[string]string{"user.email": strings.Repeat("a", 321)},
			wantKey: "user.email",
			wantErr: identity.ErrInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := identity.DerivePersonID(tc.cfg)
			if err == nil {
				t.Fatalf("DerivePersonID = %q, want an error", got)
			}
			if got != "" {
				t.Errorf("DerivePersonID returned %q alongside an error; a failed derivation must yield nothing, never a repaired identifier", got)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want one wrapping %v", err, tc.wantErr)
			}
			var cfgErr *identity.ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("error = %v, want a *identity.ConfigError", err)
			}
			if cfgErr.Key != tc.wantKey {
				t.Errorf("ConfigError.Key = %q, want %q", cfgErr.Key, tc.wantKey)
			}
		})
	}
}

// TestDerivePersonIDWhitespaceEmailIsNotSilentlyEmpty guards the specific shape
// of bug this derivation invites: "   " is a non-empty config value that
// normalizes to nothing, so a raw != "" check would produce "email:" — a
// schema-invalid identifier written into a signed, permanent op.
func TestDerivePersonIDWhitespaceEmailIsNotSilentlyEmpty(t *testing.T) {
	got, err := identity.DerivePersonID(map[string]string{"user.email": "   "})
	if err == nil {
		t.Fatalf("DerivePersonID = %q, want an error", got)
	}
	if strings.HasPrefix(got, "email:") {
		t.Errorf("DerivePersonID = %q, want no identifier at all", got)
	}
}
