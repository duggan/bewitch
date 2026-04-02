---
name: release
description: "Package Release. Prepare a new release by updating the version, changelog, debian packaging, and tagging for CI."
argument-hint: "<version> [description]"
---

# /release - Package Release

Prepare a new release by updating the version, changelog, and tagging for CI.

## Usage

```
/release <version> [description]
```

Examples:
- `/release 0.4.0` - prompts for change descriptions
- `/release 0.4.0 Added disk mount exclusion config` - uses provided description

## What This Skill Does

1. **Updates changelogs** via `/changelog` (`CHANGELOG.md`, docs site, `debian/changelog`)
2. **Updates `VERSION`** and `LATEST_STABLE` files with the new version
3. **Adds version** to the docs site version dropdown in `site/src/versions.ts`
4. **Commits** the version bump and changelog
5. **Tags** the release as `v<version>`
6. **Pushes** the commit and tag (after user confirmation)

CI handles the rest — the `release.yml` workflow triggers on `v*` tags and:
- Builds `.deb` and `.tar.gz` for amd64 and arm64
- Creates a GitHub Release with artifacts
- Uploads packages to R2 and generates signed APT repo metadata
- Builds and uploads versioned docs

The main site (changelog, version dropdown) deploys automatically via `deploy-site.yml` on push to `main`.

## Versioning

Two version files at the repo root:

- **`VERSION`** — the current version. Injected into both Go binaries at build time via `-ldflags "-X main.version=..."`. Both binaries support `--version`; `bewitch` also supports the `version` subcommand. Between releases, this is bumped to the next expected version.
- **`LATEST_STABLE`** — the latest stable release version. Used by the docs site to determine the version dropdown label. Updated only during a release (matches `VERSION` at release time, falls behind when `VERSION` is bumped for the next dev cycle).

When `VERSION` differs from `LATEST_STABLE`, the docs site shows a `-dev` entry at `/docs/dev` and the `deploy-site.yml` workflow uploads dev docs to R2. When they match, only stable docs are shown.

Version vs revision:
- **Version** (e.g., `0.2.0`): Bump for code changes
- **Revision** (e.g., `-1`, `-2`): Bump for packaging-only changes to same upstream version (lives in `debian/changelog` only)

## Instructions

When invoked:

1. Run `/changelog <version>` to update all changelogs (CHANGELOG.md, docs site, debian/changelog)
2. Update `VERSION` and `LATEST_STABLE` files with the new version
3. Add a new versioned-docs entry to `site/src/versions.ts`
4. Commit all changes
5. Tag the release as `v<version>`
6. Ask the user if they want to push now. If yes, run `git push origin main v<version>`
