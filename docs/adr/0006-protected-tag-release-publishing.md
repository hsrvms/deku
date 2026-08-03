# Publish Releases from protected tags with native GitHub Actions

**Status:** accepted

Deku publishes stable Releases through a native GitHub Actions workflow triggered only by protected `vX.Y.Z` tags, rebuilding the tagged source after independent verification and requiring approval in a protected release environment. This avoids promoting pull-request artifacts or adding a release manager dependency while keeping provenance and publication permissions in the repository's existing GitHub trust boundary.

## Considered options

- Publish artifacts built by pull-request or main-branch CI. Rejected because those artifacts are not necessarily rebuilt from the exact Release source revision and can be stale or incorrectly attributed.
- Run a separate release manager or hosted build service. Rejected because it adds operational and dependency overhead before Deku has a second distribution backend.
- Publish on every push to `main`. Rejected because ordinary development changes should not become user-visible Releases.

## Consequences

Release publication depends on protected tag rules, a protected release environment, and narrowly scoped GitHub permissions. The workflow is a GitHub adapter rather than the owner of packaging semantics; versioning, archive creation, and checksums remain testable Go behavior. GitHub is the initial publication backend, and another backend would require a separately specified adapter.
