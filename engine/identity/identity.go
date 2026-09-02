// Package identity reads the current writer's identity and SSH signing-key
// configuration out of git config for use across the engine.
package identity

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var writerIDRegexp = regexp.MustCompile(`^[0-9a-f]{16}$`)

// WriterID is an opaque 64-bit identifier (16 lowercase hex characters)
// representing a writer device namespace under refs/writ/<writer-id>/.
type WriterID string

// ParseWriterID parses and validates s as a WriterID matching ^[0-9a-f]{16}$.
func ParseWriterID(s string) (WriterID, error) {
	if !writerIDRegexp.MatchString(s) {
		return "", &ConfigError{
			Key:     "writ.writerId",
			Value:   s,
			Problem: ErrInvalid,
		}
	}
	return WriterID(s), nil
}

// Author holds the commit author's display name and email address.
type Author struct {
	Name  string // user.name
	Email string // user.email
}

// SigningKey holds SSH commit-signing key configuration.
type SigningKey struct {
	// Format is gpg.format canonicalized to lower case. git compares the
	// value case-insensitively and Load accepts any spelling of "ssh", so
	// carrying the user's spelling through would make every consumer choose
	// between EqualFold and a mismatch — and one of them chose wrong: a repo
	// configured gpg.format = SSH loaded fine and then had no signer.
	Format  string
	Value   string // user.signingKey: a path, or the literal after "key::", trimmed
	Literal bool   // true for the key:: form
}

// Identity represents the resolved writer identity and signing-key configuration
// for the local repository.
type Identity struct {
	WriterID WriterID
	Author   Author
	Key      SigningKey
	// PersonID is the local writer's person identifier per
	// spec/identifiers.md: writ.personId when set, otherwise
	// email:<normalized user.email>. It is the collaborative actor this
	// writer refers to itself as, and is unrelated to WriterID, which
	// partitions the git refspace.
	PersonID string
	// PersonIDErr records why PersonID is empty, and is nil when it is not.
	// Load does not fail on it — reads need no person identifier — so it is
	// carried here for the write paths, which must be able to tell "you set
	// writ.personId to something that is not a person identifier" apart from
	// "you set nothing at all". Reporting the second when the first is true
	// sends a user to look at a key they already configured.
	PersonIDErr    error
	AllowedSigners string // gpg.ssh.allowedSignersFile, "" when unset
}

// Load reads the current writer's identity out of git config in repoDir.
// It executes git config to resolve system, global, and local repository
// configuration with standard git precedence and include directives.
//
// Load is called once and the value passed down — it must not be re-invoked
// per op append or per sign.
func Load(ctx context.Context, repoDir string) (Identity, error) {
	cfg, err := readGitConfig(ctx, repoDir)
	if err != nil {
		return Identity{}, err
	}

	// 1. Writer ID: writ.writerId
	rawWriterID, ok := cfg["writ.writerid"]
	if !ok || rawWriterID == "" {
		return Identity{}, &ConfigError{
			Key:     "writ.writerId",
			Problem: ErrMissing,
		}
	}
	writerID, err := ParseWriterID(rawWriterID)
	if err != nil {
		return Identity{}, err
	}

	// 2. Author Name: user.name
	//
	// Trim before the emptiness check, the way DerivePersonID and the two
	// Ensure* helpers in this package already do. git config stores a
	// whitespace-only value verbatim, so a raw == "" guard lets one through as
	// a configured identity — and this identity is not merely displayed: it
	// becomes the commit author on every appended op, and user.email becomes
	// the principal signature verification matches against allowed-signers. A
	// set key with nothing in it is nothing configured, so it falls through to
	// the missing-config path. The trimmed value is what the identity carries,
	// matching git's own ident parsing and the person identifier derived from
	// the same key below.
	name := strings.TrimSpace(cfg["user.name"])
	if name == "" {
		return Identity{}, &ConfigError{
			Key:     "user.name",
			Problem: ErrMissing,
		}
	}

	// 3. Author Email: user.email
	email := strings.TrimSpace(cfg["user.email"])
	if email == "" {
		return Identity{}, &ConfigError{
			Key:     "user.email",
			Problem: ErrMissing,
		}
	}

	// 3b. Person identifier: writ.personId, else email:<normalized user.email>.
	// A configuration that yields no conforming identifier is not fatal here:
	// reading a repository does not need one, and failing Load would take the
	// whole store down over a field only the write paths use. The write paths
	// refuse instead — but they refuse with PersonIDErr, not with a guess, so
	// a misconfigured writ.personId is reported as misconfigured rather than
	// as absent. Discarding this error is how a user who did set the key gets
	// told to set it.
	personID, personIDErr := DerivePersonID(cfg)

	baseIdent := Identity{
		WriterID: writerID,
		Author: Author{
			Name:  name,
			Email: email,
		},
		PersonID:    personID,
		PersonIDErr: personIDErr,
	}

	// 4. GPG Format: gpg.format (must be ssh)
	//
	// Unset and unsupported are two different states and get two different
	// errors, the way the user.signingKey branch below already does it. A user
	// who has configured nothing is not told their configuration is wrong —
	// that reads as writ having found something broken rather than something
	// absent, and it is the first screen a new user sees. A user who has set
	// openpgp made a deliberate choice writ is asking them to reconsider,
	// which is actionable in a different way and so says something different.
	gpgFormat := strings.TrimSpace(cfg["gpg.format"])
	if gpgFormat == "" {
		return baseIdent, &ConfigError{
			Key:     "gpg.format",
			Problem: fmt.Errorf("%w: writ signs with ssh, so set it to ssh", ErrMissing),
		}
	}
	if !strings.EqualFold(gpgFormat, "ssh") {
		return baseIdent, &ConfigError{
			Key:     "gpg.format",
			Value:   gpgFormat,
			Problem: fmt.Errorf("%w: writ signs with ssh", ErrUnsupportedFormat),
		}
	}
	// Carry the canonical spelling, not the configured one. Accepting SSH
	// here and then comparing the field against "ssh" elsewhere is how a repo
	// with gpg.format = SSH passed init and had no signer at write time.
	gpgFormat = strings.ToLower(gpgFormat)

	// 5. Signing Key: user.signingKey
	//
	// Trimmed for the same reason user.name and user.email are: git stores a
	// whitespace-only value verbatim, and a raw guard accepts one as a key
	// path. Reporting the repository as configured and then failing at the
	// first signed write — with a subprocess error naming a file called "   "
	// — is worse than saying "not configured" up front.
	rawSigningKey := strings.TrimSpace(cfg["user.signingkey"])
	if rawSigningKey == "" {
		return baseIdent, &ConfigError{
			Key:     "user.signingKey",
			Problem: ErrMissing,
		}
	}
	var signingKey SigningKey
	signingKey.Format = gpgFormat
	if strings.HasPrefix(rawSigningKey, "key::") {
		literalKey := strings.TrimSpace(strings.TrimPrefix(rawSigningKey, "key::"))
		if literalKey == "" {
			return baseIdent, &ConfigError{
				Key:     "user.signingKey",
				Value:   rawSigningKey,
				Problem: ErrInvalid,
			}
		}
		signingKey.Value = literalKey
		signingKey.Literal = true
	} else {
		signingKey.Value = rawSigningKey
		signingKey.Literal = false
	}

	// 6. Allowed Signers: gpg.ssh.allowedSignersFile (optional). Trimmed:
	// unset and whitespace-only are the same absence, and the difference
	// between them is otherwise a trust-store load against a garbage path.
	allowedSigners := strings.TrimSpace(cfg["gpg.ssh.allowedsignersfile"])

	return Identity{
		WriterID: writerID,
		Author: Author{
			Name:  name,
			Email: email,
		},
		PersonID:       personID,
		PersonIDErr:    personIDErr,
		Key:            signingKey,
		AllowedSigners: allowedSigners,
	}, nil
}
