---
name: changelog
description: "Update Changelog. Add entries to CHANGELOG.md, the docs site changelog page, and debian/changelog for a new version."
argument-hint: "<version> [description...]"
---

# /changelog - Update Changelog

Add entries to the project changelog (`CHANGELOG.md`), the docs site changelog page (`site/src/pages/docs/changelog.tsx`), and the Debian changelog (`debian/changelog`).

## Usage

```
/changelog <version> [description...]
```

Examples:
- `/changelog 0.4.0` - analyses commits since last tag and drafts entries
- `/changelog 0.4.0 Added disk IOPS collector, fixed alert debounce race` - uses provided descriptions

## What This Skill Does

1. **Reads** the current `CHANGELOG.md` and recent git history
2. **Drafts** a new version entry with categorised changes (Added, Changed, Fixed, Removed)
3. **Updates** `CHANGELOG.md` with the new entry below the `# Changelog` header
4. **Updates** `site/src/pages/docs/changelog.tsx` with matching HTML content
5. **Updates** `debian/changelog` with a new entry in strict Debian format

## Changelog Format

`CHANGELOG.md` follows [Keep a Changelog](https://keepachangelog.com/) conventions:

```markdown
## [0.4.0] - 2026-04-01

### Added

- New feature description

### Changed

- Breaking change or behavioural change

### Fixed

- Bug fix description

### Removed

- Removed feature or deprecated item
```

Rules:
- Versions are sorted newest-first
- Each version has a date in `YYYY-MM-DD` format
- Changes are grouped under Added, Changed, Fixed, Removed (omit empty groups)
- Entries are concise but human-readable — not raw commit messages
- Related commits should be consolidated into a single entry

## Docs Site Changelog Format

`site/src/pages/docs/changelog.tsx` mirrors the markdown content as JSX. Each version section uses:

```tsx
<h2>0.4.0</h2>
<p class="text-muted text-sm">2026-04-01</p>
<h3>Added</h3>
<ul>
  <li>New feature description</li>
</ul>
```

Rules:
- Version headers are `<h2>` (without the `v` prefix or brackets)
- Date is a `<p>` with `text-muted text-sm` classes
- Category headers are `<h3>`
- Entries are `<li>` inside `<ul>`
- Inline code uses `<code>` tags
- New version sections go after the opening `<p>` (the GitHub link paragraph) and before existing versions

## Debian Changelog Format

`debian/changelog` follows strict Debian format:

```
bewitch (<version>-1) unstable; urgency=medium

  * Change description 1
  * Change description 2

 -- Ross <ross@example.com>  <RFC 2822 timestamp>
```

Format rules:
- Version in parentheses after package name, with `-1` revision suffix
- Two spaces before each `*` bullet
- One space between `--` and maintainer name
- Two spaces between email `>` and timestamp
- Timestamp must be RFC 2822 format (e.g., `Tue, 04 Feb 2026 12:00:00 +0000`)
- New entries are prepended (newest first)
- Get the maintainer name and email from the existing entries

## Instructions

When invoked:

1. Read `CHANGELOG.md`, `site/src/pages/docs/changelog.tsx`, and `debian/changelog` to understand the current state
2. If no description was provided, check git history since the last tag:
   ```bash
   git log --oneline $(git describe --tags --abbrev=0)..HEAD
   ```
3. Draft changelog entries from the commits, grouping related changes and writing them in human-friendly language. Ask the user to review and adjust before writing.
4. Once confirmed, update `CHANGELOG.md`:
   - Insert the new version section after the `# Changelog` header line and its description
   - Keep existing entries unchanged
5. Update `site/src/pages/docs/changelog.tsx`:
   - Insert the new version section as JSX after the GitHub link `<p>` and before the first existing `<h2>`
6. Update `debian/changelog`:
   - Prepend a new entry using the maintainer info from the existing top entry
   - Use the current timestamp in RFC 2822 format
   - Use the same change descriptions as `CHANGELOG.md` (flattened, without category headers)
7. Do NOT commit — the user or `/release` skill will handle that
