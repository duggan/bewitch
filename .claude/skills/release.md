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
2. **Updates `VERSION`** file with the new version
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

The `VERSION` file at the repo root is the single source of truth. It is injected into both Go binaries (`bewitch` and `bewitchd`) at build time via `-ldflags "-X main.version=..."`. Both binaries support `--version`; `bewitch` also supports the `version` subcommand.

- **Version** (e.g., `0.2.0`): Bump for code changes
- **Revision** (e.g., `-1`, `-2`): Bump for packaging-only changes to same upstream version (lives in `debian/changelog` only)

## Instructions

When invoked:

1. Run `/changelog <version>` to update all changelogs (CHANGELOG.md, docs site, debian/changelog)
2. Update the `VERSION` file with the new version
3. Update `site/src/versions.ts`: add a new versioned-docs entry and update the first entry's label to `v<version>` (the `/docs` path always shows the latest stable version)
4. Commit all changes
5. Tag the release as `v<version>`
6. Ask the user if they want to push now. If yes, run `git push origin main v<version>`
