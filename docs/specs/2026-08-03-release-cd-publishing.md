# Release and CD Publishing

**Status:** Implemented; `v0.0.2` published successfully from a protected tag

**Implementation record:** The first protected-tag run published `v0.0.2` from source revision `4ed5583753a3639aa11b3edb6c541b85c123eb17` on 2026-08-03. The run completed tagged-source verification, all five platform builds, packaging, checksum validation, provenance attestation, protected-environment approval, and GitHub Release publication. Post-release checks confirmed all five archive checksums, archive-root executable layout, the release binary's `--version` output, and GitHub artifact attestations for every archive.

**Scope:** Versioned distribution of the Deku CLI from protected source tags.

## Problem Statement

Deku is currently a pre-release Go CLI with no supported installation path. Users need release artifacts that can be associated with an exact source revision, verified for transfer integrity, and traced to the release build without exposing Provider credentials or relying on artifacts produced by pull-request jobs.

Release automation must improve distribution without becoming part of the Agent runtime or changing the v0 chat experience.

## Solution

Deku publishes stable Releases only from protected `vX.Y.Z` tags whose source revision is reachable from `main`. A release workflow independently verifies the tagged source, builds the supported platform matrix from that tag, packages the binaries, generates SHA-256 checksums, produces provenance attestations, and publishes a GitHub Release.

Publication occurs only after a protected release environment approval. The workflow uses narrowly scoped GitHub permissions and OIDC for attestations. It does not use Provider credentials or make live model calls.

The workflow is a thin GitHub adapter over testable Go versioning and packaging behavior. Release identity and artifact bytes are never taken from pull-request build outputs, and an existing Release is never overwritten with newly built bytes.

## Domain Rules

### Release identity

- A stable Release Version is exactly `X.Y.Z` and is represented by the protected Git tag `vX.Y.Z`.
- Tags containing prerelease or build metadata, such as `v0.1.0-rc.1`, are outside this specification.
- The tag must resolve to a source revision reachable from `main`.
- A tag is immutable after publication. Moving or deleting a published tag is not a rollback mechanism.
- A rerun may recover an incomplete publication for the same source revision, but may not replace an already published artifact with different bytes.

### Version reporting

- `deku --version` prints the Release Version for a release build and exits successfully without loading Provider configuration or creating a Session.
- Development builds report `dev` unless a build explicitly supplies another version.
- The reported version is injected from the release tag during the release build; it is not inferred from the runtime directory or current Git checkout.

### Supported platforms

The initial Supported Platform matrix is:

| OS | Architecture | Archive format | Executable name |
| --- | --- | --- | --- |
| Linux | amd64 | `.tar.gz` | `deku` |
| Linux | arm64 | `.tar.gz` | `deku` |
| macOS | amd64 | `.tar.gz` | `deku` |
| macOS | arm64 | `.tar.gz` | `deku` |
| Windows | amd64 | `.zip` | `deku.exe` |

Producing an artifact for another Target Platform does not make that platform supported.

### Artifact layout and integrity

- Archive names use `deku_<version>_<os>_<arch>`, where `<version>` omits the leading `v` from the tag.
- Each archive contains its executable at the archive root.
- The Release includes one `SHA256SUMS` file covering every Release Artifact archive.
- Checksums are calculated over the final archive bytes and are not treated as authenticity proofs.
- Archive creation uses stable ordering and normalized metadata where the format permits it, so packaging behavior is deterministic for the same inputs.

### Verification and publication

- Before publication, the workflow runs the repository validation suite (`make ci`) and the Go vulnerability scan (`govulncheck ./...`) against the tagged source.
- Artifacts are built from the tag's checked-out source revision after verification. Pull-request artifacts are never promoted into a Release.
- Each Release Artifact receives provenance identifying the source revision and build inputs.
- The release workflow uses OIDC-backed attestations. Standalone archive signing is outside this specification.
- Build jobs use read-only repository access. Only the publication job receives permission to create or update the GitHub Release, attest artifacts, and obtain the protected release environment approval.
- The workflow is implemented with pinned GitHub Action revisions and must pass `actionlint`.

### Release notes

The GitHub Release uses generated notes based on changes since the previous stable tag. The publication approval is the human review gate for the generated notes and artifact set.

### Failure, withdrawal, and revocation

- A failed, cancelled, or rejected publication leaves no published Release and does not mutate the source tag.
- A defect in a published Release is handled by publishing a new patched Release Version. Existing artifacts are not replaced in place.
- Ordinary withdrawal removes the Release from the recommended distribution path while preserving its historical record and leaving already downloaded copies unchanged.
- Security revocation explicitly marks the affected Release or Release Artifact as untrusted. The historical record remains; a fixed Release is published separately. Installed copies cannot be revoked remotely.

## User Stories

1. As a developer, I want to download a Deku archive for my operating system and architecture, so that I can install Deku without a Go toolchain.
2. As a developer, I want `deku --version` to identify the installed Release Version, so that I can confirm what I am running.
3. As a developer, I want a checksum for each archive, so that I can detect an incomplete or corrupted download.
4. As a developer, I want release provenance tied to a source revision, so that I can inspect how an artifact was produced.
5. As a maintainer, I want release automation to run only from protected stable tags, so that ordinary pushes and pull requests cannot publish releases.
6. As a maintainer, I want the release workflow to repeat validation on the tagged source, so that a previously passing pull-request artifact is not trusted as the release input.
7. As a maintainer, I want a protected approval before publication, so that a failed or misleading release cannot become public without human review.
8. As a maintainer, I want failed publication to be recoverable without replacing published bytes, so that reruns cannot silently change a Release.
9. As a maintainer, I want a documented withdrawal and revocation procedure, so that a defective or compromised Release can be handled without rewriting repository history.

## Implementation Decisions

- Release version parsing, version reporting, archive naming, archive creation, and checksum generation belong behind a small testable Go module. The GitHub Actions workflow invokes this module rather than embedding packaging rules in shell steps.
- The version-reporting path remains inside the CLI, but it must be independent of Provider configuration and Session startup.
- The release workflow uses native GitHub Actions and the repository's existing Go toolchain. No third-party release manager is introduced.
- The workflow is split into verification, build/package, attestation, and publication responsibilities. The protected release environment gates publication rather than ordinary validation.
- No new runtime dependency is required. Packaging uses the Go standard library and existing system archive tools only where their behavior is covered by deterministic tests.

## Testing Decisions

- Version behavior is tested through the CLI entry point: `--version` succeeds without Provider configuration, release builds print the injected version, and development builds print `dev`.
- Packaging behavior is tested through the release module interface with known inputs and independent expected archive names, archive contents, and SHA-256 values.
- Tests cover every Supported Platform, invalid Release Versions, missing inputs, checksum changes after archive-byte changes, and deterministic repeated packaging.
- A release dry run uses a temporary directory and never publishes to GitHub. It verifies the complete artifact set and checksum file.
- Workflow tests and `actionlint` verify tag filters, pinned actions, permissions, protected-environment placement, absence of Provider secrets, and that publication consumes artifacts built from the tag.
- Repository validation remains `make ci`, `govulncheck ./...`, and `actionlint`. Release-specific tests must run without Provider credentials or live model calls.

## Out of Scope

- Prerelease, nightly, or per-commit Releases.
- Package-manager formulas, installers, container images, and automatic update checks.
- Standalone archive signatures.
- Building or supporting platforms outside the initial matrix.
- Provider credentials, live model calls, or Agent behavior in release jobs.
- Replacing or deleting published artifacts as a rollback mechanism.
- Automatic release withdrawal or revocation detection.

## Acceptance Criteria

- [x] A valid protected `vX.Y.Z` tag is the only publication trigger.
- [x] Tags that do not match the stable version form or are not reachable from `main` cannot publish.
- [x] The tagged source passes `make ci` and `govulncheck ./...` before artifact publication.
- [x] `deku --version` reports the tag version in release builds and does not require Provider configuration.
- [x] Five archives are produced with the specified names, formats, executable names, and platform targets.
- [x] `SHA256SUMS` contains the correct digest for every archive.
- [x] Repeating packaging with the same inputs produces the same archive bytes and checksums.
- [x] Provenance attestations are generated for the Release Artifacts using OIDC and least-privilege permissions.
- [x] Publication is gated by the protected release environment and generated release notes.
- [x] The workflow does not use Provider credentials or live model calls.
- [x] A rerun cannot replace bytes in an already published Release.
- [x] Withdrawal and revocation procedures are documented for maintainers.
- [x] `gofmt`, `go vet ./...`, `go test ./...`, `go test -race ./...`, configured static analysis, `actionlint`, and release dry-run checks pass.
