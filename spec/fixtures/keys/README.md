# Fixture signing keys — throwaway, committed on purpose

Every key in this directory (`alice_ed25519`, `bob_ed25519`, and their
`.pub` halves) is a disposable ed25519 SSH keypair generated solely to
sign fixture commits deterministically. **They are not, and must never
become, a real credential.** Nobody's identity or access depends on them;
they exist only so `spec/fixtures`' generated repos have real,
verifiable signatures instead of forged or absent ones.

Private keys are committed intentionally — that's what lets fixture
generation be reproducible offline, in CI, without provisioning secrets.
Do not rotate, encrypt, or "fix" this. Do not reuse these keys for
anything outside this directory.

See `../README.md` for why signatures need to be ed25519 specifically
(determinism) and how the keyring is used.
