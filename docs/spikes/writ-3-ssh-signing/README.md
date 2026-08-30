# WRIT-3 spike: SSH commit-signing via go-git + system git interop

Prototype and findings for whether writ can sign op-commits with a user's
existing SSH key through go-git, and have that interoperate cleanly with
system git. Run it with `go test ./... -v` from this directory.

Answer: yes, but go-git contributes nothing to the cryptography — it only
constructs and stores the op-commit object correctly. All actual signing and
verification goes through system `ssh-keygen -Y sign` / `-Y verify`.

## What the three tests prove

- `TestSignInGo_VerifyWithSystemGit` — builds a commit through go-git
  (empty tree, author/committer, message), signs its
  `EncodeWithoutSignature` payload with `ssh-keygen -Y sign`, writes the
  signed object and moves a branch ref, all through go-git's `Storer`, then
  confirms system `git verify-commit` accepts it.
- `TestSignWithSystemGit_VerifyInGo` — the reverse: a normal `git commit -S`
  (`gpg.format=ssh`) read back through go-git's `CommitObject`, whose
  decoded `PGPSignature` and reconstructed payload verify against
  `ssh-keygen -Y verify`.
- `TestAgentHeldKeySigning` — generates a key, loads it into a fresh
  `ssh-agent`, deletes the private key file, and signs successfully with
  only the `.pub` file on disk. This is the shape a hardware-backed or
  agent-forwarded key takes; a design that assumes a readable private-key
  file breaks for it, and this one doesn't.

## Findings and gaps for WRIT-22

**1. go-git has no SSH signing or verification — only PGP.** `object.Commit`
stores the signature as an opaque string under the `gpgsig` header
regardless of format, so building and reading SSH-signed commits works
fine. But `Commit.Verify(armoredKeyRing)`
(`plumbing/object/commit.go:499`) is hardcoded to
`openpgp.CheckArmoredDetachedSignature` — it will never accept an SSH
signature. WRIT-22 cannot use it; verification has to be hand-rolled, either
by shelling to `ssh-keygen -Y verify` (what test 2 does) or by hand-parsing
the `PROTOCOL.sshsig` blob format in pure Go (see finding 4).

**2. Verification cannot live inside Fold.** AGENTS.md is explicit: "Fold
is pure and deterministic: ops in, state out, no I/O." The only
verification path this spike could get working is a subprocess call to
`ssh-keygen -Y verify` — I/O by definition, and even a future pure-Go
verifier would still need to read the trust store (allowed-signers-style
mapping of key to identity) from somewhere. That means op-signature
verification has to happen once, at the boundary where an op is read into
the DAG store (or lazily and memoized in the projection), producing a
`verified bool` (or richer status) that travels with the op as data. Fold
itself must only ever consume that precomputed result, never call out to
verify anything. This is a real design constraint for WRIT-22, not just an
implementation detail — the codec's sign/verify surface and the DAG
store's ingest path need to agree on where verification is cached before
either is built.

**3. Prefer explicit allowed-signers + `ssh-keygen -Y verify` over mutating
repo git config and shelling to `git verify-commit`.** Test 1's approach
(write `gpg.ssh.allowedSignersFile` into the repo's git config, then run
`git verify-commit`) works, but it's global, mutable, per-repo state — not
something a library folding ops from many writer namespaces concurrently
wants to be touching. Test 2's approach (call `ssh-keygen -Y verify -f
<allowed_signers> -I <principal> -n git -s <sigfile>` directly, no repo
config involved) takes the trust store as an explicit argument per call and
is the one that generalizes to the engine's needs.

**4. Open question, not attempted here: pure-Go verification.**
`golang.org/x/crypto/ssh` is already a transitive dependency (via go-git's
SSH transport) and exposes the primitives (`ssh.ParsePublicKey`,
`PublicKey.Verify`) needed to implement `PROTOCOL.sshsig` — the signed
payload is `"SSHSIG" || namespace || reserved || hash_alg || H(message)`,
wrapped in a documented outer envelope. Doing so would drop the runtime
dependency on `ssh-keygen` being on `PATH` for verification (signing still
needs either `ssh-keygen` or an agent conversation). Left as a WRIT-22
decision: the shelling approach is boring, direct, and already proven
end-to-end in this spike; hand-rolling the format trades a subprocess
dependency for ~150 lines of wire-format parsing that needs its own
fixture coverage. Signing has the same tradeoff but is less avoidable —
producing a valid sshsig signature from a bare Ed25519/ECDSA key is
straightforward, but agent-backed and hardware-backed keys (the case
finding 5 covers) go through the SSH agent protocol either way, which
`ssh-keygen`/`ssh-add` already speak correctly.

**5. Agent-held keys work exactly like normal git usage expects.**
`ssh-keygen -Y sign -f <path>` accepts a path to the *public* key and falls
back to querying `SSH_AUTH_SOCK` for the matching private key when the
private file isn't there. No special-casing needed on writ's side beyond
not assuming a private key file exists.

**6. `ssh-keygen -Y sign`/`-Y verify` require OpenSSH 8.2+ (2020).** Not
tested against older or non-OpenSSH `ssh-keygen` implementations. Worth a
version check with a clear error message rather than a confusing signing
failure, and worth confirming during the ref-namespace host-compatibility
spike's environment sweep whether any target platform ships an
`ssh-keygen` older than this.

**7. Subprocess cost not measured.** Each sign/verify call here is a fresh
process spawn (single digit milliseconds locally). Fine for interactive
CLI use or per-op signing; not benchmarked for fold-time bulk verification
of a large op history. Flagging per finding 2: the answer regardless is to
verify once at ingest and cache the result, not to re-verify per fold, so
this should matter less than it might first appear — but worth confirming
once WRIT-22 has real fixture-sized histories to try it against.

**8. Namespace: `"git"`.** Signing under the same reserved namespace git
itself uses for commit signing (rather than inventing a `writ` namespace)
means op-commits stay verifiable with plain `git verify-commit` too, not
just writ's own code — and hosts that render commit-signature badges may
pick it up for free, since ops are stored as ordinary signed git commits.
Worth keeping unless a concrete reason to diverge shows up.
