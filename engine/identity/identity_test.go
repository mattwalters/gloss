package identity_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/writtendev/writ/engine/identity"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping test")
	}
}

type testEnv struct {
	repoDir       string
	globalCfgPath string
}

func setupTestEnv(t *testing.T) testEnv {
	t.Helper()
	requireGit(t)

	tempDir := t.TempDir()
	globalCfgPath := filepath.Join(tempDir, "global_gitconfig")
	if err := os.WriteFile(globalCfgPath, []byte(""), 0600); err != nil {
		t.Fatalf("writing empty global config: %v", err)
	}

	repoDir := filepath.Join(tempDir, "repo")
	if err := os.Mkdir(repoDir, 0755); err != nil {
		t.Fatalf("creating repo dir: %v", err)
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, string(out))
	}

	t.Setenv("GIT_CONFIG_GLOBAL", globalCfgPath)
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	return testEnv{
		repoDir:       repoDir,
		globalCfgPath: globalCfgPath,
	}
}

func setGitConfig(t *testing.T, dir, key, val string) {
	t.Helper()
	cmd := exec.Command("git", "config", key, val)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config %s %s failed: %v (%s)", key, val, err, string(out))
	}
}

func setFileConfig(t *testing.T, filePath, key, val string) {
	t.Helper()
	cmd := exec.Command("git", "config", "--file", filePath, key, val)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config --file %s %s %s failed: %v (%s)", filePath, key, val, err, string(out))
	}
}

func populateValidLocalConfig(t *testing.T, repoDir string) {
	t.Helper()
	setGitConfig(t, repoDir, "writ.writerId", "0123456789abcdef")
	setGitConfig(t, repoDir, "user.name", "Alice Example")
	setGitConfig(t, repoDir, "user.email", "alice@example.test")
	setGitConfig(t, repoDir, "gpg.format", "ssh")
	setGitConfig(t, repoDir, "user.signingKey", "/path/to/id_ed25519")
}

func TestLoad_ValidPathKey(t *testing.T) {
	env := setupTestEnv(t)
	populateValidLocalConfig(t, env.repoDir)
	setGitConfig(t, env.repoDir, "gpg.ssh.allowedSignersFile", "/path/to/allowed_signers")

	id, err := identity.Load(context.Background(), env.repoDir)
	if err != nil {
		t.Fatalf("Load unexpected error: %v", err)
	}

	if id.WriterID != "0123456789abcdef" {
		t.Errorf("id.WriterID = %q, want %q", id.WriterID, "0123456789abcdef")
	}
	if id.Author.Name != "Alice Example" {
		t.Errorf("id.Author.Name = %q, want %q", id.Author.Name, "Alice Example")
	}
	if id.Author.Email != "alice@example.test" {
		t.Errorf("id.Author.Email = %q, want %q", id.Author.Email, "alice@example.test")
	}
	if id.Key.Format != "ssh" {
		t.Errorf("id.Key.Format = %q, want %q", id.Key.Format, "ssh")
	}
	if id.Key.Value != "/path/to/id_ed25519" {
		t.Errorf("id.Key.Value = %q, want %q", id.Key.Value, "/path/to/id_ed25519")
	}
	if id.Key.Literal != false {
		t.Errorf("id.Key.Literal = %v, want false", id.Key.Literal)
	}
	if id.AllowedSigners != "/path/to/allowed_signers" {
		t.Errorf("id.AllowedSigners = %q, want %q", id.AllowedSigners, "/path/to/allowed_signers")
	}
}

func TestLoad_ValidLiteralKey(t *testing.T) {
	env := setupTestEnv(t)
	populateValidLocalConfig(t, env.repoDir)
	const literalKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKeyBlob"
	setGitConfig(t, env.repoDir, "user.signingKey", "key::"+literalKey)

	id, err := identity.Load(context.Background(), env.repoDir)
	if err != nil {
		t.Fatalf("Load unexpected error: %v", err)
	}

	if id.Key.Literal != true {
		t.Errorf("id.Key.Literal = %v, want true", id.Key.Literal)
	}
	if id.Key.Value != literalKey {
		t.Errorf("id.Key.Value = %q, want %q", id.Key.Value, literalKey)
	}
	if id.Key.Format != "ssh" {
		t.Errorf("id.Key.Format = %q, want %q", id.Key.Format, "ssh")
	}
	if id.AllowedSigners != "" {
		t.Errorf("id.AllowedSigners = %q, want empty string when unset", id.AllowedSigners)
	}
}

// TestLoad_WhitespaceOnlyAuthor pins the guard on user.name and user.email.
// git config stores a whitespace-only value verbatim, so a raw == "" check let
// one through as a configured identity — and that identity is written into the
// commit author of every appended op and used as the principal signature
// verification matches against allowed-signers. Whitespace-only is unset.
func TestLoad_WhitespaceOnlyAuthor(t *testing.T) {
	values := []struct {
		name  string
		value string
	}{
		{"spaces", "   "},
		{"tab", "\t"},
	}

	for _, key := range []string{"user.name", "user.email"} {
		for _, v := range values {
			t.Run(key+"/"+v.name, func(t *testing.T) {
				env := setupTestEnv(t)
				populateValidLocalConfig(t, env.repoDir)
				setGitConfig(t, env.repoDir, key, v.value)

				// The value really did survive the round trip through git;
				// otherwise this test would pass for the wrong reason.
				cfg, err := identity.ReadGitConfig(context.Background(), env.repoDir)
				if err != nil {
					t.Fatalf("ReadGitConfig: %v", err)
				}
				if got := cfg[strings.ToLower(key)]; got != v.value {
					t.Fatalf("git stored %s as %q, want %q verbatim", key, got, v.value)
				}

				_, err = identity.Load(context.Background(), env.repoDir)
				if err == nil {
					t.Fatalf("Load accepted whitespace-only %s", key)
				}
				if !errors.Is(err, identity.ErrMissing) {
					t.Errorf("Load error = %v, want errors.Is ErrMissing", err)
				}
				var cfgErr *identity.ConfigError
				if !errors.As(err, &cfgErr) {
					t.Fatalf("Load error is %T, want *identity.ConfigError", err)
				}
				if cfgErr.Key != key {
					t.Errorf("cfgErr.Key = %q, want %q", cfgErr.Key, key)
				}
			})
		}
	}
}

// TestLoad_TrimsAuthorPadding is the other half of the guard above: a padded
// value is configured, so Load succeeds, but the padding does not travel into
// the commit author or the verification principal.
func TestLoad_TrimsAuthorPadding(t *testing.T) {
	env := setupTestEnv(t)
	populateValidLocalConfig(t, env.repoDir)
	setGitConfig(t, env.repoDir, "user.name", "  Alice Example  ")
	setGitConfig(t, env.repoDir, "user.email", "  alice@example.test\t")

	id, err := identity.Load(context.Background(), env.repoDir)
	if err != nil {
		t.Fatalf("Load unexpected error: %v", err)
	}
	if id.Author.Name != "Alice Example" {
		t.Errorf("id.Author.Name = %q, want %q", id.Author.Name, "Alice Example")
	}
	if id.Author.Email != "alice@example.test" {
		t.Errorf("id.Author.Email = %q, want %q", id.Author.Email, "alice@example.test")
	}
	// The person identifier already trimmed; the author now agrees with it.
	if id.PersonID != "email:alice@example.test" {
		t.Errorf("id.PersonID = %q, want %q", id.PersonID, "email:alice@example.test")
	}
}

func TestLoad_WriterIDPrecedence(t *testing.T) {
	t.Run("local_only", func(t *testing.T) {
		env := setupTestEnv(t)
		populateValidLocalConfig(t, env.repoDir)
		setGitConfig(t, env.repoDir, "writ.writerId", "1111111111111111")

		id, err := identity.Load(context.Background(), env.repoDir)
		if err != nil {
			t.Fatalf("Load unexpected error: %v", err)
		}
		if id.WriterID != "1111111111111111" {
			t.Errorf("id.WriterID = %q, want \"1111111111111111\"", id.WriterID)
		}
	})

	t.Run("global_only", func(t *testing.T) {
		env := setupTestEnv(t)
		populateValidLocalConfig(t, env.repoDir)
		// Remove local writerId and set in global config
		cmd := exec.Command("git", "config", "--unset", "writ.writerId")
		cmd.Dir = env.repoDir
		_ = cmd.Run()

		setFileConfig(t, env.globalCfgPath, "writ.writerId", "2222222222222222")

		id, err := identity.Load(context.Background(), env.repoDir)
		if err != nil {
			t.Fatalf("Load unexpected error: %v", err)
		}
		if id.WriterID != "2222222222222222" {
			t.Errorf("id.WriterID = %q, want \"2222222222222222\"", id.WriterID)
		}
	})

	t.Run("both_differing_local_wins", func(t *testing.T) {
		env := setupTestEnv(t)
		populateValidLocalConfig(t, env.repoDir)
		setFileConfig(t, env.globalCfgPath, "writ.writerId", "2222222222222222")
		setGitConfig(t, env.repoDir, "writ.writerId", "3333333333333333")

		id, err := identity.Load(context.Background(), env.repoDir)
		if err != nil {
			t.Fatalf("Load unexpected error: %v", err)
		}
		if id.WriterID != "3333333333333333" {
			t.Errorf("id.WriterID = %q, want local value \"3333333333333333\"", id.WriterID)
		}
	})
}

func TestLoad_IncludePath(t *testing.T) {
	env := setupTestEnv(t)
	populateValidLocalConfig(t, env.repoDir)

	// Remove local writerId
	cmd := exec.Command("git", "config", "--unset", "writ.writerId")
	cmd.Dir = env.repoDir
	_ = cmd.Run()

	includedPath := filepath.Join(t.TempDir(), "included_gitconfig")
	setFileConfig(t, includedPath, "writ.writerId", "4444444444444444")
	setFileConfig(t, env.globalCfgPath, "include.path", includedPath)

	id, err := identity.Load(context.Background(), env.repoDir)
	if err != nil {
		t.Fatalf("Load unexpected error with include.path: %v", err)
	}
	if id.WriterID != "4444444444444444" {
		t.Errorf("id.WriterID = %q, want included value \"4444444444444444\"", id.WriterID)
	}
}

func TestLoad_MissingConfig(t *testing.T) {
	keys := []struct {
		configKey string
		lookupKey string
	}{
		{"writ.writerId", "writ.writerId"},
		{"user.name", "user.name"},
		{"user.email", "user.email"},
		{"user.signingKey", "user.signingKey"},
	}

	for _, k := range keys {
		t.Run(k.configKey, func(t *testing.T) {
			env := setupTestEnv(t)
			populateValidLocalConfig(t, env.repoDir)

			cmd := exec.Command("git", "config", "--unset", k.configKey)
			cmd.Dir = env.repoDir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git config --unset %s failed: %v (%s)", k.configKey, err, string(out))
			}

			_, err := identity.Load(context.Background(), env.repoDir)
			if err == nil {
				t.Fatalf("Load succeeded despite missing %s", k.configKey)
			}
			if !errors.Is(err, identity.ErrMissing) {
				t.Errorf("Load error = %v, want errors.Is ErrMissing", err)
			}
			var cfgErr *identity.ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("Load error is %T, want *identity.ConfigError", err)
			}
			if cfgErr.Key != k.lookupKey {
				t.Errorf("cfgErr.Key = %q, want %q", cfgErr.Key, k.lookupKey)
			}
			errMsg := err.Error()
			if !strings.Contains(errMsg, k.lookupKey) {
				t.Errorf("errMsg %q does not name key %q", errMsg, k.lookupKey)
			}
			if !strings.Contains(errMsg, "writ init") {
				t.Errorf("errMsg %q does not mention 'writ init'", errMsg)
			}
		})
	}
}

func TestLoad_InvalidWriterID(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"uppercase", "0123456789ABCDEF"},
		{"too_short", "0123456789abcde"},
		{"non_hex", "0123456789abcdeg"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTestEnv(t)
			populateValidLocalConfig(t, env.repoDir)
			setGitConfig(t, env.repoDir, "writ.writerId", tc.value)

			_, err := identity.Load(context.Background(), env.repoDir)
			if err == nil {
				t.Fatalf("Load succeeded with invalid writer-id %q", tc.value)
			}
			if !errors.Is(err, identity.ErrInvalid) {
				t.Errorf("Load error = %v, want errors.Is ErrInvalid", err)
			}
			var cfgErr *identity.ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("Load error is %T, want *identity.ConfigError", err)
			}
			if cfgErr.Key != "writ.writerId" {
				t.Errorf("cfgErr.Key = %q, want \"writ.writerId\"", cfgErr.Key)
			}
			if cfgErr.Value != tc.value {
				t.Errorf("cfgErr.Value = %q, want %q", cfgErr.Value, tc.value)
			}
		})
	}
}

func TestLoad_InvalidSigningKey_EmptyLiteral(t *testing.T) {
	env := setupTestEnv(t)
	populateValidLocalConfig(t, env.repoDir)
	setGitConfig(t, env.repoDir, "user.signingKey", "key::")

	_, err := identity.Load(context.Background(), env.repoDir)
	if err == nil {
		t.Fatal("Load succeeded with empty literal signing key 'key::'")
	}
	if !errors.Is(err, identity.ErrInvalid) {
		t.Errorf("Load error = %v, want errors.Is ErrInvalid", err)
	}
	var cfgErr *identity.ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Load error is %T, want *identity.ConfigError", err)
	}
	if cfgErr.Key != "user.signingKey" {
		t.Errorf("cfgErr.Key = %q, want \"user.signingKey\"", cfgErr.Key)
	}
	if cfgErr.Value != "key::" {
		t.Errorf("cfgErr.Value = %q, want \"key::\"", cfgErr.Value)
	}
}

func TestLoad_UnsupportedGPGFormat(t *testing.T) {
	cases := []struct {
		name      string
		format    string
		unset     bool
		wantValue string
	}{
		{"unset", "", true, ""},
		{"openpgp", "openpgp", false, "openpgp"},
		{"x509", "x509", false, "x509"},
		{"custom", "custom-crypto", false, "custom-crypto"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTestEnv(t)
			populateValidLocalConfig(t, env.repoDir)

			if tc.unset {
				cmd := exec.Command("git", "config", "--unset", "gpg.format")
				cmd.Dir = env.repoDir
				_ = cmd.Run()
			} else {
				setGitConfig(t, env.repoDir, "gpg.format", tc.format)
			}

			_, err := identity.Load(context.Background(), env.repoDir)
			if err == nil {
				t.Fatalf("Load succeeded with unsupported gpg.format (format=%q, unset=%v)", tc.format, tc.unset)
			}
			if !errors.Is(err, identity.ErrUnsupportedFormat) {
				t.Errorf("Load error = %v, want errors.Is ErrUnsupportedFormat", err)
			}
			var cfgErr *identity.ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("Load error is %T, want *identity.ConfigError", err)
			}
			if cfgErr.Key != "gpg.format" {
				t.Errorf("cfgErr.Key = %q, want \"gpg.format\"", cfgErr.Key)
			}
			if cfgErr.Value != tc.wantValue {
				t.Errorf("cfgErr.Value = %q, want %q", cfgErr.Value, tc.wantValue)
			}
		})
	}
}

func TestLoad_NonRepoDirectory(t *testing.T) {
	requireGit(t)
	nonRepoDir := t.TempDir()

	_, err := identity.Load(context.Background(), nonRepoDir)
	if err == nil {
		t.Fatal("Load on non-repo directory succeeded, expected error")
	}
	var cfgErr *identity.ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Load on non-repo dir returned %T (%v), want *identity.ConfigError", err, err)
	}
}

func TestLoad_GitNotOnPath(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)

	_, err := identity.Load(context.Background(), emptyDir)
	if err == nil {
		t.Fatal("Load with git not on PATH succeeded, expected error")
	}
	var cfgErr *identity.ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Load with git missing returned %T (%v), want *identity.ConfigError", err, err)
	}
}

func TestLoad_ContextCancelled(t *testing.T) {
	env := setupTestEnv(t)
	populateValidLocalConfig(t, env.repoDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := identity.Load(ctx, env.repoDir)
	if err == nil {
		t.Fatal("Load with cancelled context succeeded, expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Load error = %v, want errors.Is context.Canceled", err)
	}
	var cfgErr *identity.ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Load error is %T, want *identity.ConfigError", err)
	}
}
