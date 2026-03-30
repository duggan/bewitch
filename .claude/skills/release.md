# /release - Package Release

Prepare a new release by updating the version, changelog, building the package, and publishing the APT repository.

## Usage

```
/release <version> [description]
```

Examples:
- `/release 0.2.0` - prompts for change descriptions
- `/release 0.2.0 Added disk mount exclusion config` - uses provided description

## What This Skill Does

1. **Updates changelogs** via `/changelog` (`CHANGELOG.md`, docs site, `debian/changelog`)
2. **Updates `VERSION`** file with the new version
3. **Builds** the `.deb` package via `make deb-docker`
4. **Uploads** the `.deb` to Cloudflare R2 via `make apt-upload`
5. **Generates** signed APT repository metadata via `make apt-repo`
6. **Builds** the site via `cd site && bun run build`
7. **Commits** the version bump and changelog

## Versioning

The `VERSION` file at the repo root is the single source of truth. It is injected into both Go binaries (`bewitch` and `bewitchd`) at build time via `-ldflags "-X main.version=..."`. Both binaries support `--version`; `bewitch` also supports the `version` subcommand.

- **Version** (e.g., `0.2.0`): Bump for code changes
- **Revision** (e.g., `-1`, `-2`): Bump for packaging-only changes to same upstream version (lives in `debian/changelog` only)

## Instructions

When invoked:

1. Run `/changelog <version>` to update all changelogs (CHANGELOG.md, docs site, debian/changelog)
2. Update the `VERSION` file with the new version
3. Add the new version to the docs site version dropdown in `site/src/versions.ts`
4. Commit all changes
5. Ask the user if they want to build and publish now. If yes:
   - Run `make deb-docker` to build the `.deb` in Docker
   - Run `make apt-upload` to upload `.deb` files to R2
   - Run `make apt-repo` to generate signed APT repo metadata
   - Run `cd site && bun run build` to rebuild the site with updated metadata
   - Remind the user to deploy `site/dist/` to Cloudflare Pages

## APT Repository

The APT repo is split across two services:
- **Cloudflare Pages** serves repo metadata (`/apt/dists/stable/...`) and the GPG key (`/gpg`)
- **Cloudflare R2** stores the `.deb` files (`/apt/pool/...`), proxied via a Pages Function

Scripts:
- `scripts/build-apt-repo.sh` — generates Packages, Release, InRelease, Release.gpg from `.deb` files, signs with GPG key
- `scripts/upload-pool.sh` — uploads `.deb` files to R2 via wrangler

GPG signing key: `BEWITCH_GPG_KEY` env var (default: `"bewitch"`)
R2 bucket: `BEWITCH_R2_BUCKET` env var (default: `"bewitch-apt"`)

## Prerequisites

- GPG signing key `bewitch` in local keyring
- `wrangler` CLI authenticated with Cloudflare
- Docker (for `make deb-docker`)
- `bun` (for site build)
