# Release Runbook

This runbook is for Deku maintainers. Release publication is tag-only and requires approval through the protected `release` environment.

## Before creating a Release

1. Confirm the change is merged to `main` and the working tree is clean.
2. Run the local release dry run:

   ```sh
   VERSION=0.1.0 make release-dry-run
   ```

3. Confirm the target version is a new stable `X.Y.Z` version. Prerelease tags are not supported.
4. Confirm the repository's protected tag rules and `release` environment approval are active.

## Create a Release

Create and push an annotated tag for the exact `main` commit to be released:

```sh
git switch main
git pull --ff-only origin main
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

The Release workflow then:

1. Verifies the tag and its ancestry from `main`.
2. Runs `make ci` and `govulncheck ./...` on the tagged source.
3. Cross-compiles the five Supported Platforms.
4. Creates archives and `SHA256SUMS`.
5. Generates provenance attestations.
6. Waits for approval in the protected `release` environment.
7. Creates or resumes a draft GitHub Release, verifies existing asset bytes, uploads missing assets, and publishes it with generated notes.

Provider credentials are not needed by the workflow and must not be added as repository or environment secrets for release publication.

## Verify a published Release

After publication, verify the downloaded bytes rather than trusting the workflow result alone:

1. Download the Release assets:

   ```sh
   VERSION=0.1.0
   tmpdir=$(mktemp -d)
   gh release download "v${VERSION}" --repo hsrvms/deku --dir "$tmpdir"
   ```

2. Verify the checksums and inspect one archive:

   ```sh
   (cd "$tmpdir" && sha256sum -c SHA256SUMS)
   tar -xzf "$tmpdir/deku_${VERSION}_linux_amd64.tar.gz" -C "$tmpdir"
   "$tmpdir/deku" --version
   ```

   The checksum command should report `OK` for each archive, and the version command should print the Release Version without requiring Provider configuration. Use `shasum -a 256 -c` on systems without `sha256sum`.

3. Verify provenance for each downloaded archive with a current GitHub CLI:

   ```sh
   gh attestation verify "$tmpdir/deku_${VERSION}_linux_amd64.tar.gz" --repo hsrvms/deku
   ```

   Repeat the attestation check for the other platform archives. A successful check verifies the archive's signed provenance and its association with the repository; it does not replace checksum verification.

Remove the temporary directory after verification:

```sh
rm -rf "$tmpdir"
```

## Recovery and reruns

A failed workflow may be rerun for the same tag. Existing GitHub Release assets are downloaded and compared byte-for-byte; an asset with different bytes causes the workflow to stop rather than overwrite it.

Do not move or delete the tag to recover a failed Release. If the source or artifact bytes are wrong after publication, create a new patched Release Version.

## Withdrawal

For a non-security defect:

1. Keep the tag and historical GitHub Release record.
2. Mark the Release as withdrawn in its notes.
3. Remove it from the recommended download path as appropriate.
4. Publish a corrected Release Version.

Do not describe withdrawal as a correction to the existing artifact bytes. Copies already downloaded by users are unchanged.

## Revocation

For a security issue:

1. Mark the affected Release or Release Artifact as revoked and explain the reason in the release notes and security communication.
2. Remove the affected assets from ordinary download availability when appropriate, while preserving the historical record and incident evidence.
3. Publish a fixed Release Version.
4. Communicate that installed copies cannot be revoked remotely and must be replaced by users.

Revocation is a loss-of-trust declaration, not an instruction to move a tag or replace assets in place.
