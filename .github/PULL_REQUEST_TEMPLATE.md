**What this changes and why**

**Checklist**

- [ ] `go test ./...` passes, including the conformance fixtures
- [ ] If this touches fold behaviour, the op envelope, or canonical
      encoding: spec text, fixtures, and implementation all land in
      this one PR (see [CONTRIBUTING.md](../CONTRIBUTING.md#fixtures-are-the-spec))
- [ ] Unknown op types/fields are still preserved and ignored, not
      dropped
- [ ] No new dependency without a reason stated in this description
