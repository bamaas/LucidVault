# Plan: `add_note` Never Overwrites an Existing Note

> Fixes issue #97. No new ADR: this is a data-loss bug fix inside an existing tool's
> contract — no new tool, no gating change, no new configuration. It reverses the
> overwrite behaviour that `TestHandleAddNote/overwrite_existing_note_with_same_title`
> (added in #54) asserted; that subtest is replaced, not deleted.

## Problem

`HandleAddNote` (`internal/mcpserver/tools.go`) derives
`filename := vault.GenerateSlug(title) + ".md"` and writes `notes/<filename>` with
`os.WriteFile`, which truncates an existing file. Two titles that slug identically
(`"AKS thoughts"` and `"AKS Thoughts!"` both become `aks-thoughts`) make the second
call silently destroy the first note. With LiveSync, the loss replicates to every
device.

Failing with an error was considered and rejected: the caller does not know the slug
rules, so it cannot tell which title is free and would have to guess and retry. MCP
also has no tool to update a note (`edit_page` targets wiki pages), so a second note
with a suffixed filename is the only way to add content through MCP anyway.

## Proposed Solution

On collision, pick the next free filename and return it:

```text
notes/aks-thoughts.md      ← first call
notes/aks-thoughts-2.md    ← second call with a colliding title
notes/aks-thoughts-3.md    ← third
```

- Create each candidate with `os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)`.
  `O_EXCL` makes the existence check and the create one atomic step, so two
  concurrent calls cannot both claim the same filename.
- On `errors.Is(err, fs.ErrExist)`, try the next suffix. Any other error is returned,
  wrapped.
- Cap attempts at `maxNoteFilenameAttempts = 100` (`<slug>.md` plus `-2` … `-100`).
  When exhausted, return an error naming the slug. It does not overwrite anything.
- The H1 heading and frontmatter stay as the caller gave them; only the filename
  changes.
- The returned filename is authoritative; the MCP tool result already reports it.

## Implementation Plan (TDD)

Failing tests first (spec-only subagent), then minimal implementation.

### 1. Handler — `internal/mcpserver/tools.go`

- Replace the `os.WriteFile` call in `HandleAddNote` with a small helper
  `createUniqueNoteFile(notesDir, slug string, data []byte) (string, error)` that
  loops over candidates as described above and returns the filename it created.
- If writing the content fails after a successful create, close and remove the
  empty file before returning the error, so no empty note is left behind.

### 2. Registration — `internal/mcpserver/server.go`

- `add_note` tool description: add "If a note with the same filename already exists,
  a numeric suffix is added (e.g. `-2`); the returned filename is authoritative."
- Keep the `agentsmd.ToolInfo` description in the same file in sync.

### 3. Tests — `internal/mcpserver/tools_test.go`

Replace the `overwrite existing note with same title` subtest with:

- **Colliding titles keep both notes:** `"AKS thoughts"` then `"AKS Thoughts!"` →
  filenames `aks-thoughts.md` and `aks-thoughts-2.md`; the first file still contains
  the original content; the second contains the new content.
- **Third collision:** a third colliding call returns `aks-thoughts-3.md`.
- **Exhausted suffixes:** pre-create `<slug>.md` and `<slug>-2.md` … `<slug>-100.md`;
  the call returns an error and no existing file changes.
- **Pre-existing user file:** a note created outside MCP (plain file in `notes/`) is
  not overwritten.

## Docs

- `README.md`: if it describes `add_note`, mention the suffix behaviour.
- `CLAUDE.md`: no change (no env var, structure or pipeline change).
- Regenerated `AGENTS.md` picks up the new tool description automatically.

## Out of Scope

- `HandleAddBookmark` also overwrites by slug, but on purpose (documented as dedup by
  slug) and only for unprocessed inbox files, not user content.

## Acceptance Criteria

- Calling `add_note` twice with colliding titles leaves the first note byte-for-byte
  intact and returns a different filename for the second.
- `mise run test` and `mise run lint` pass.
