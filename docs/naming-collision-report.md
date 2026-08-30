# Naming collision report: "Gloss" and backups

Spike for GLS-5. Preliminary screen using free, publicly reachable search
surfaces (npm/crates.io registry APIs, pkg.go.dev, Homebrew's formula API,
Debian's package search, GitHub, and general web search). This is not a
substitute for a paid trademark search or legal opinion — USPTO TESS is not
reachable by automated tooling, so trademark findings below are best-effort
web search only.

Channels checked per name: npm, crates.io, pkg.go.dev, Homebrew (brew), apt
(Debian package search), GitHub (org/user handles + repos), a light,
non-exhaustive domain sample (`.dev`, plus `.io` for Gloss only), and
general/trademark web search. VISION.md lists five alternatives in this
order: Scholia, Cairn, Rings, Grain, Plumb; that list isn't stated to be
ranked, but absent other signal this report reads list order as priority
order and checks "Gloss" plus the first two, **Scholia** and **Cairn**, per
the DoD's "top two backups."

## Gloss

| Channel | Result |
| --- | --- |
| npm | Taken — `gloss`, a React higher-order-component styling library (v2.8.23). Unrelated domain, but the bare package name is unavailable. |
| crates.io | Taken — `gloss`, a thin OpenGL wrapper (last published 2021, ~1.6k downloads). Low relevance, low activity. |
| pkg.go.dev | No bare `gloss` module (Go namespaces by full import path), but several dormant `.../gloss` packages exist (`mdp/gloss`, `yene/gloss`, `wesleym/gloss` — all inactive since 2015; `diffuse/gloss`, `stergiotis/.../gloss` — low adoption). More importantly: **`charmbracelet/lipgloss`** ("Lip Gloss") is the dominant terminal-styling library in the Go ecosystem (~9,600 importers) and is the standard styling companion to **Bubble Tea** — the TUI framework this project has already committed to in AGENTS.md. "Gloss" vs. "Lip Gloss" inside the exact community our own TUI will live in is a real confusion risk, independent of trademark law. |
| Homebrew | Clean — no `gloss` formula. |
| apt (Debian) | Taken — `libghc-gloss-dev` etc. package the Haskell `gloss` library, a well-known 2D graphics/animation package in the Haskell ecosystem. Different community, real exact-name use. |
| GitHub | Org handle `gloss` is taken (GNU Linux Open Source Society, SASTRA University — small, low activity, one repo). Top-starred repos named "Gloss" are unrelated and mostly inactive (a deprecated Swift JSON library, a Clojure byte-handling library). |
| Domains | `gloss.io` is registered and parked for sale. `gloss.dev` resolves (TLS succeeds, no content) — registered. `.sh` not directly confirmed. |
| Trademark (web search only) | No exact "GLOSS" mark found in software/SaaS classes. "GLOSSGENIUS" is a live registered mark, but for salon/beauty booking software — different market, low confusion risk. **Not a substitute for a TESS search.** |

**Risk: Medium.** No single blocking hit, but real exact-name use on npm,
crates.io, Debian/Haskell, and the GitHub org handle means the bare name
won't be available everywhere. Of Gloss's own findings, the most concerning
is that our own planned TUI stack (Bubble Tea) already has a famous "Lip
Gloss" in its immediate orbit — a naming-adjacency risk, not a competing
product, which is why this still rates below Cairn's finding (an active
company shipping a competing kind of product under the exact name).

## Scholia

| Channel | Result |
| --- | --- |
| npm | Taken — `scholia`, a small local markdown-preview CLI. Low relevance. |
| crates.io | Taken — `scholia`, an RDF/semantic-triples library. Low relevance. |
| pkg.go.dev | One obscure, near-zero-adoption submodule (`odysseia-greek/.../scholia`). Negligible. |
| Homebrew | Clean. |
| apt (Debian) | Clean — no results. |
| GitHub | User handle `scholia` is taken (unrelated, near-empty account). The real conflict: **`WDscholia/scholia`** is an established, published tool — Wikidata-based scholarly profiles, active since ~2016, covered in arXiv/Springer papers, hosted at `scholia.toolforge.org`, well known in the Wikimedia/library-science community. |
| Domains | `scholia.dev` appears registered (resolves via Cloudflare). |
| Trademark | No hit found; unresearched via TESS. |

**Risk: Medium.** Clean on Homebrew/apt/pkg.go.dev, but the name is already
the identity of a real, moderately well-known academic tool in a
scholarly-annotation space adjacent to Gloss's own naming rationale ("gloss"
= marginalia). Confusion risk is more reputational/SEO than legal, since the
domains (academic tooling vs. dev tooling) differ.

## Cairn

| Channel | Result |
| --- | --- |
| npm | Taken — `cairn`, a React Native styling library. Low relevance. |
| crates.io | Taken — `cairn`, described as "build-gated version control for Rust projects (experimental)." Directly VCS-adjacent. |
| pkg.go.dev | Several active-looking `cairn`-suffixed modules from 2026, notably `EduCloud-Ecosystem/cairn` — a control-plane HTTP server with adapters for GitHub, GitLab, Forgejo, and GitHub Classroom. Git-platform tooling overlap. |
| Homebrew | Clean. |
| apt (Debian) | Clean — no results. |
| GitHub | Org handle `cairn` is taken by **Cairn Software**, an active, branded studio whose flagship repo is described as *"a terminal-based coding agent, built by Cairn."* This is a live company shipping developer CLI tooling under the exact name right now. |
| Domains | `cairn.dev` did not resolve — appears available. |
| Trademark | No hit found; unresearched via TESS. An operating company using the name in dev tooling is a common-law risk regardless of federal registration status. |

**Risk: High.** This is the sharpest conflict found across the whole survey
— sharper than anything found for Gloss itself: an active, named company is
currently shipping a developer CLI/agent product called Cairn, in an
adjacent corner of the same market Gloss is entering.

## Recommendation

**Keep.** Keep "Gloss" as the project name — neither backup clears the bar
for a rename, since Scholia carries a real (if adjacent-domain) collision
and Cairn's is worse than anything found for Gloss. Separately, treat the
package/org registries where the bare name is already taken as a qualifier
problem to solve at registration time, not a reason to rename the project:
register package/org names like `gloss-scm`, `getgloss`, or `glossvcs`
rather than assuming the bare string is available on npm/crates.io/GitHub,
and avoid describing UI styling work as "gloss" in our own docs/changelogs
to reduce day-to-day confusion with `lipgloss` in the Bubble Tea community
we're already part of. No blocking trademark was found in this preliminary
screen, but a professional USPTO/TESS search is recommended before any
public launch, domain spend, or registration.
