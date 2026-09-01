// Package identity reads the current writer's identity and SSH signing-key
// configuration out of git config for use across the engine.
package identity

import (
	"context"
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

// NormalizePerson normalizes a person identifier string per spec/identifiers.md
// (trimmed leading/trailing whitespace, lowercase).
func NormalizePerson(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Author holds the commit author's display name and email address.
type Author struct {
	Name  string // user.name
	Email string // user.email
}

// SigningKey holds SSH commit-signing key configuration.
type SigningKey struct {
	Format  string // gpg.format, verbatim
	Value   string // user.signingKey: a path, or the literal after "key::"
	Literal bool   // true for the key:: form
}

// Identity represents the resolved writer identity and signing-key configuration
// for the local repository.
type Identity struct {
	WriterID       WriterID
	Author         Author
	Key            SigningKey
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
	name, ok := cfg["user.name"]
	if !ok || name == "" {
		return Identity{}, &ConfigError{
			Key:     "user.name",
			Problem: ErrMissing,
		}
	}

	// 3. Author Email: user.email
	email, ok := cfg["user.email"]
	if !ok || email == "" {
		return Identity{}, &ConfigError{
			Key:     "user.email",
			Problem: ErrMissing,
		}
	}

	baseIdent := Identity{
		WriterID: writerID,
		Author: Author{
			Name:  name,
			Email: email,
		},
	}

	// 4. GPG Format: gpg.format (must be ssh)
	gpgFormat, ok := cfg["gpg.format"]
	if !ok || gpgFormat == "" {
		return baseIdent, &ConfigError{
			Key:     "gpg.format",
			Value:   "",
			Problem: ErrUnsupportedFormat,
		}
	}
	if strings.ToLower(gpgFormat) != "ssh" {
		return baseIdent, &ConfigError{
			Key:     "gpg.format",
			Value:   gpgFormat,
			Problem: ErrUnsupportedFormat,
		}
	}

	// 5. Signing Key: user.signingKey
	rawSigningKey, ok := cfg["user.signingkey"]
	if !ok || rawSigningKey == "" {
		return baseIdent, &ConfigError{
			Key:     "user.signingKey",
			Problem: ErrMissing,
		}
	}
	var signingKey SigningKey
	signingKey.Format = gpgFormat
	if strings.HasPrefix(rawSigningKey, "key::") {
		literalKey := strings.TrimPrefix(rawSigningKey, "key::")
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

	// 6. Allowed Signers: gpg.ssh.allowedSignersFile (optional)
	allowedSigners := cfg["gpg.ssh.allowedsignersfile"]

	return Identity{
		WriterID: writerID,
		Author: Author{
			Name:  name,
			Email: email,
		},
		Key:            signingKey,
		AllowedSigners: allowedSigners,
	}, nil
}
