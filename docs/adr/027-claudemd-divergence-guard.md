# 027 — Never Overwrite a Diverged CLAUDE.md Block (stateless hash guard)

## Status

Accepted

## Context

`claudemd.Upsert` replaces everything between `<!-- lucidvault:start -->` and `<!-- lucidvault:end -->` on every pipeline start. The target is a user-editable file (`~/.claude/CLAUDE.md`), and users do edit inside the markers — a live instance was found with hand-authored retrieval and web-search guidance in that region, which the next run would have discarded with no warning and no backup. A generator that owns a region of a user-editable file must be able to tell "content I wrote" from "content the user wrote".

## Decision

Embed a checksum of the generated body as an HTML comment inside the emitted block; on the next run, rewrite the block only when its current body still matches that checksum — or, for a legacy block carrying no checksum, when the body still matches a known historical template shape — and otherwise skip the write and log a warning naming the file.

## Consequences

- Untouched blocks still self-heal and still upgrade to newer templates — the mechanism of ADR-008 and ADR-025 is preserved for the common case.
- A user-edited block is never silently destroyed; divergence surfaces as a `slog.Warn` instead of data loss.
- Stateless: the checksum travels inside the file, so the guard works on a second machine, in a `scratch` image, and on a file copied or synced between hosts. No database row, no sidecar state, no new env var.
- A legacy block written before this ADR carries no checksum, so it is classified by shape instead: if its body still matches the pre-checksum template (the ADR-025 pointer, with any vault path), it is provably generator-written and is upgraded in place. Every other legacy body is treated as diverged and left alone. Without this, the existing install base — which is exactly the population carrying the wrong vault path — would be frozen on the bad block forever, and the guard would protect the defect it was added to make fixable.
- Shape-matching is deliberately exact, not fuzzy: a body that merely resembles the old template is user-edited and must survive. False negatives cost a warning; false positives cost the user's writing.
- The emitted block gains one comment line. It is inside the markers, so it stays invisible in rendered Markdown.
- Rejected: an opt-in env var (pushes the burden to every deployment and makes "does nothing" the default); append-only (safe, but freezes every block at the template version that first created it).
