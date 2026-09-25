package claudemd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// checksumCommentRe matches the checksum comment format the package
// documents (MINOR-3): a 64-character lowercase-hex SHA-256 sum inside the
// package's checksum comment markers.
var checksumCommentRe = regexp.MustCompile(`<!-- lucidvault:checksum:[0-9a-f]{64} -->`)

// legacyTemplateFmt is the pre-checksum ADR-025 pointer template, verbatim
// as it existed before the ADR-027 divergence guard. ADR-027 requires the
// guard to recognize a block in exactly this shape (any vault path) as
// generator-written and upgrade it; anything that merely resembles this
// shape must be treated as diverged. Fixed here, independent of the package
// under test, as the historical contract these tests pin down.
const legacyTemplateFmt = "<!-- lucidvault:start -->\n" +
	"## LucidVault Knowledge Base\n\n" +
	"You have a personal knowledge base at %s. It contains `AGENTS.md` — read that file and follow it. `AGENTS.md` is the single source of truth for the vault layout, retrieval strategy, and citation rules.\n" +
	"<!-- lucidvault:end -->"

func TestUpsert_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	status, err := Upsert(path, "/data/vault")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusWrote {
		t.Errorf("status = %v, want %v", status, StatusWrote)
	}

	content := readFile(t, path)
	assertContains(t, content, StartMarker)
	assertContains(t, content, EndMarker)
	assertContains(t, content, "/data/vault")
	assertContains(t, content, "## LucidVault Knowledge Base")
}

func TestUpsert_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if err := os.WriteFile(path, []byte("# My Config\n\nSome existing content.\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/vault")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusWrote {
		t.Errorf("status = %v, want %v", status, StatusWrote)
	}

	content := readFile(t, path)
	assertContains(t, content, "# My Config")
	assertContains(t, content, "Some existing content.")
	assertContains(t, content, StartMarker)
	assertContains(t, content, EndMarker)
}

// TestUpsert_UnknownBlockContent_TreatedAsDiverged exercises a marker block
// whose body is neither checksum-matched (no embedded checksum) nor a legacy
// pre-checksum template match ("old content" is not the ADR-025 pointer
// shape). Under the ADR-027 divergence guard this is user content and MUST be
// preserved byte-identical, with the call reporting a skip -- the opposite of
// the pre-guard behavior this test originally asserted (blind replacement,
// which is the silent data-loss bug ADR-027 exists to fix; the test was
// renamed from TestUpsert_ReplacesExisting in test round 2 because the old
// name described the pre-guard behavior it disproves, not the behavior it
// actually asserts).
func TestUpsert_UnknownBlockContent_TreatedAsDiverged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	old := "# Config\n\n" + StartMarker + "\nold content\n" + EndMarker + "\n\n# Footer\n"
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/new/vault")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusSkippedDiverged {
		t.Errorf("status = %v, want %v", status, StatusSkippedDiverged)
	}

	content := readFile(t, path)
	if content != old {
		t.Errorf("expected file to be left byte-identical when diverged\ngot:\n%s\nwant:\n%s", content, old)
	}
	assertContains(t, content, "old content")
	assertNotContains(t, content, "/new/vault")
}

func TestUpsert_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	var firstContent string
	for i := range 3 {
		status, err := Upsert(path, "/vault")
		if err != nil {
			t.Fatalf("Upsert (iteration %d): %v", i, err)
		}
		if status != StatusWrote {
			t.Errorf("iteration %d: status = %v, want %v", i, status, StatusWrote)
		}
		if i == 0 {
			firstContent = readFile(t, path)
		}
	}

	content := readFile(t, path)
	count := strings.Count(content, StartMarker)
	if count != 1 {
		t.Errorf("expected 1 start marker, got %d", count)
	}
	if content != firstContent {
		t.Errorf("expected file to converge after the first run and stay stable\nfirst:\n%s\nfinal:\n%s", firstContent, content)
	}
}

// TestUpsert_PointerForm verifies the injected section is the minimal pointer
// (ADR-025): it carries the vault's absolute path and tells the agent to read
// AGENTS.md and follow it -- and it no longer duplicates the retrieval strategy
// steps or the per-directory file legend that AGENTS.md now owns.
func TestUpsert_PointerForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	status, err := Upsert(path, "/data/vault")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusWrote {
		t.Errorf("status = %v, want %v", status, StatusWrote)
	}

	content := readFile(t, path)

	// Markers preserved.
	assertContains(t, content, StartMarker)
	assertContains(t, content, EndMarker)

	// The one thing AGENTS.md cannot self-supply: the absolute vault path.
	assertContains(t, content, "/data/vault")

	// The pointer must direct the agent to AGENTS.md and to follow it.
	assertContains(t, content, "AGENTS.md")
	lower := strings.ToLower(content)
	if !strings.Contains(lower, "follow") {
		t.Errorf("pointer must instruct the agent to follow AGENTS.md; got:\n%s", content)
	}

	// The duplicated retrieval strategy and file legend must be gone -- they now
	// live solely in AGENTS.md (no drift).
	assertNotContains(t, content, "Retrieval Strategy")
	assertNotContains(t, content, "Grep index.md")
	assertNotContains(t, content, "Vault Structure")
	assertNotContains(t, content, "LLM-enriched summaries")
	assertNotContains(t, content, "Fetch a URL")
}

// TestUpsert_ChecksumRoundTrip_PicksUpNewPath verifies acceptance criterion 1
// and plan test-list item 1: a block Upsert itself wrote, re-run through
// Upsert unchanged, is recognized as generator-owned (its embedded checksum
// matches its current body) and rewritten -- including picking up a new
// vault path on the very next call, with no manual edit in between.
func TestUpsert_ChecksumRoundTrip_PicksUpNewPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	status1, err := Upsert(path, "/vault/a")
	if err != nil {
		t.Fatalf("Upsert (1st): %v", err)
	}
	if status1 != StatusWrote {
		t.Errorf("1st call: status = %v, want %v", status1, StatusWrote)
	}

	status2, err := Upsert(path, "/vault/b")
	if err != nil {
		t.Fatalf("Upsert (2nd): %v", err)
	}
	if status2 != StatusWrote {
		t.Errorf("2nd call: status = %v, want %v", status2, StatusWrote)
	}

	content := readFile(t, path)
	assertContains(t, content, "/vault/b")
	assertNotContains(t, content, "/vault/a")
	if count := strings.Count(content, StartMarker); count != 1 {
		t.Errorf("expected 1 start marker after rewrite, got %d", count)
	}
}

// TestUpsert_ChecksumMismatch_HandEditedBlockPreserved verifies acceptance
// criterion 2 and plan test-list item 2: a checksummed block whose body was
// hand-edited after Upsert wrote it (so its embedded checksum no longer
// matches its body) must be left byte-identical on the next Upsert call, and
// the call must signal skipped-diverged rather than an error.
func TestUpsert_ChecksumMismatch_HandEditedBlockPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if _, err := Upsert(path, "/vault/a"); err != nil {
		t.Fatalf("Upsert (seed): %v", err)
	}

	generated := readFile(t, path)
	handEdited := strings.Replace(generated, EndMarker, "\n\nA user wrote this sentence by hand.\n"+EndMarker, 1)
	if handEdited == generated {
		t.Fatalf("test setup: hand edit did not change content")
	}
	if err := os.WriteFile(path, []byte(handEdited), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/vault/b")
	if err != nil {
		t.Fatalf("Upsert (after hand edit): %v", err)
	}
	if status != StatusSkippedDiverged {
		t.Errorf("status = %v, want %v", status, StatusSkippedDiverged)
	}

	content := readFile(t, path)
	if content != handEdited {
		t.Errorf("expected file to be left byte-identical after divergence\ngot:\n%s\nwant:\n%s", content, handEdited)
	}
	assertContains(t, content, "A user wrote this sentence by hand.")
	assertNotContains(t, content, "/vault/b")
}

// TestUpsert_ChecksumMismatch_PathEditedInPlace_HandEditedBlockPreserved is a
// sibling of TestUpsert_ChecksumMismatch_HandEditedBlockPreserved (MINOR-7,
// test round 2): instead of appending a trailing sentence, the hand edit
// replaces the vault path in place, leaving the rest of the body untouched.
// The mechanism is the same (the embedded checksum no longer matches the
// body) but this pins the specific, plausible edit shape of a user
// correcting "their" path by hand rather than adding prose.
func TestUpsert_ChecksumMismatch_PathEditedInPlace_HandEditedBlockPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if _, err := Upsert(path, "/vault/a"); err != nil {
		t.Fatalf("Upsert (seed): %v", err)
	}

	generated := readFile(t, path)
	handEdited := strings.Replace(generated, "/vault/a", "/vault/hand-corrected", 1)
	if handEdited == generated {
		t.Fatalf("test setup: hand edit did not change content")
	}
	if err := os.WriteFile(path, []byte(handEdited), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/vault/b")
	if err != nil {
		t.Fatalf("Upsert (after hand edit): %v", err)
	}
	if status != StatusSkippedDiverged {
		t.Errorf("status = %v, want %v", status, StatusSkippedDiverged)
	}

	content := readFile(t, path)
	if content != handEdited {
		t.Errorf("expected file to be left byte-identical after divergence\ngot:\n%s\nwant:\n%s", content, handEdited)
	}
	assertContains(t, content, "/vault/hand-corrected")
	assertNotContains(t, content, "/vault/b")
}

// TestUpsert_ChecksumCommentDeleted_TreatedAsDiverged verifies the other
// plausible hand-edit shape (MINOR-7, test round 2): a user deletes the
// checksum comment line entirely but leaves the rest of the generated body
// intact. With no checksum present, isGeneratorOwned's checksumRe branch
// cannot match; the body also isn't the pre-checksum legacyBodyFmt shape
// (post-ADR-028 bodyFmt reads differently), so it falls through to
// legacyBodyRe, fails that too, and the block is treated as diverged rather
// than panicking or being silently accepted as generator-owned.
func TestUpsert_ChecksumCommentDeleted_TreatedAsDiverged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if _, err := Upsert(path, "/vault/a"); err != nil {
		t.Fatalf("Upsert (seed): %v", err)
	}

	generated := readFile(t, path)
	checksumLine := checksumCommentRe.FindString(generated)
	if checksumLine == "" {
		t.Fatalf("test setup: could not find checksum comment in generated content:\n%s", generated)
	}
	noChecksum := strings.Replace(generated, checksumLine+"\n", "", 1)
	if noChecksum == generated {
		t.Fatalf("test setup: removing the checksum comment did not change content")
	}
	if err := os.WriteFile(path, []byte(noChecksum), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/vault/b")
	if err != nil {
		t.Fatalf("Upsert (after checksum comment deleted): %v", err)
	}
	if status != StatusSkippedDiverged {
		t.Errorf("status = %v, want %v", status, StatusSkippedDiverged)
	}

	content := readFile(t, path)
	if content != noChecksum {
		t.Errorf("expected file to be left byte-identical when the checksum comment is deleted\ngot:\n%s\nwant:\n%s", content, noChecksum)
	}
	assertNotContains(t, content, "/vault/b")
}

// TestUpsert_LegacyTemplateShape_Upgraded verifies acceptance criterion 3 and
// plan test-list item 3: a legacy (pre-checksum) block whose body exactly
// matches the ADR-025 pointer template, carrying any vault path, is provably
// generator-written per ADR-027 and must be upgraded -- rewritten with the
// new template and an embedded checksum -- on the next Upsert.
func TestUpsert_LegacyTemplateShape_Upgraded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	legacyBlock := fmt.Sprintf(legacyTemplateFmt, "/legacy/vault")
	old := "# Config\n\n" + legacyBlock + "\n\n# Footer\n"
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/upgraded/vault")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusWrote {
		t.Errorf("status = %v, want %v", status, StatusWrote)
	}

	content := readFile(t, path)
	// HasPrefix/HasSuffix (rather than Contains) pin the surrounding content
	// to its exact position, catching a shifted splice index that Contains
	// alone would miss.
	if !strings.HasPrefix(content, "# Config") {
		t.Errorf("expected content to start with %q\ngot:\n%s", "# Config", content)
	}
	if !strings.HasSuffix(content, "# Footer\n") {
		t.Errorf("expected content to end with %q\ngot:\n%s", "# Footer\n", content)
	}
	assertContains(t, content, "/upgraded/vault")
	assertNotContains(t, content, "/legacy/vault")
	if count := strings.Count(content, StartMarker); count != 1 {
		t.Errorf("expected 1 start marker after upgrade, got %d", count)
	}

	// The emitted block must carry a checksum comment in the documented
	// format so a future run can recognize it as generator-owned.
	if !checksumCommentRe.MatchString(content) {
		t.Errorf("expected emitted block to contain a checksum comment matching %s\ngot:\n%s", checksumCommentRe.String(), content)
	}

	// Round-trip: a third Upsert call on the now-upgraded (checksummed)
	// block must still report StatusWrote -- the upgrade itself must not be
	// mistaken for a diverged block on the very next call.
	status3, err := Upsert(path, "/upgraded/vault")
	if err != nil {
		t.Fatalf("Upsert (3rd, round trip after upgrade): %v", err)
	}
	if status3 != StatusWrote {
		t.Errorf("3rd call status = %v, want %v (upgraded block must be round-trippable)", status3, StatusWrote)
	}
}

// TestUpsert_LegacyTemplateWithAddedProse_TreatedAsDiverged verifies
// acceptance criterion 4 and plan test-list item 4: a legacy block carrying
// the ADR-025 template shape PLUS user-added prose no longer matches that
// shape exactly, so ADR-027 requires it be treated as diverged -- left
// byte-identical, with the call reporting skipped-diverged.
func TestUpsert_LegacyTemplateWithAddedProse_TreatedAsDiverged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	legacyBlock := fmt.Sprintf(legacyTemplateFmt, "/legacy/vault")
	withProse := strings.Replace(legacyBlock, EndMarker, "\n\nAlso check the inbox weekly.\n"+EndMarker, 1)
	old := "# Config\n\n" + withProse + "\n\n# Footer\n"
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/upgraded/vault")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusSkippedDiverged {
		t.Errorf("status = %v, want %v", status, StatusSkippedDiverged)
	}

	content := readFile(t, path)
	if content != old {
		t.Errorf("expected file to be left byte-identical when diverged\ngot:\n%s\nwant:\n%s", content, old)
	}
	assertContains(t, content, "Also check the inbox weekly.")
	assertNotContains(t, content, "/upgraded/vault")
}

// TestUpsert_LegacyTemplateWithProseOnPathLine_TreatedAsDiverged verifies the
// CRITICAL fix to buildLegacyBodyRe: a legacy block where the user inserted
// prose ON THE PATH LINE itself -- right after the path, still before "It
// contains" on that same line -- not a trailing sentence after the block (as
// TestUpsert_LegacyTemplateWithAddedProse_TreatedAsDiverged covers). Before
// the fix, the legacy-shape regex's greedy [^\n]* accepted arbitrary same-line
// prose as "the path", so a legacy block a user annotated inline (e.g. a
// mounted-read-only caveat) was misclassified as generator-owned and silently
// overwritten -- exactly the failure ADR-027 exists to prevent.
func TestUpsert_LegacyTemplateWithProseOnPathLine_TreatedAsDiverged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	// The inserted prose sits where a real path would go, before the
	// ". It contains" sentence boundary continues on the same line.
	pathWithProse := "/legacy/vault (mounted read-only, ask before writing)"
	legacyBlock := fmt.Sprintf(legacyTemplateFmt, pathWithProse)
	old := "# Config\n\n" + legacyBlock + "\n\n# Footer\n"
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/upgraded/vault")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusSkippedDiverged {
		t.Errorf("status = %v, want %v", status, StatusSkippedDiverged)
	}

	content := readFile(t, path)
	if content != old {
		t.Errorf("expected file to be left byte-identical when user prose is inserted on the path line\ngot:\n%s\nwant:\n%s", content, old)
	}
	assertContains(t, content, "mounted read-only, ask before writing")
	assertNotContains(t, content, "/upgraded/vault")
}

// TestUpsert_LegacyPathWithSpace_TreatedAsDiverged pins the accepted ADR-027
// false-negative trade-off documented on buildLegacyBodyRe (MINOR-3, test
// round 2): the legacy-shape regex captures the path as [^\s]+ (no
// whitespace), not [^\n]* (anything but a newline), to close the CRITICAL-1
// hole where whitespace-free user prose appended to a legacy path (see
// TestUpsert_LegacyTemplateWithProseOnPathLine_TreatedAsDiverged above) was
// swallowed into the "path" capture and silently overwritten.
//
// The cost of that fix is this case: a REAL legacy vault path that itself
// contains a literal space -- e.g. a macOS path like "/Users/bas/My Vault" --
// is indistinguishable, by shape alone, from a legacy path with appended
// prose. Both are a path-shaped run of characters followed by a space and
// more text. ADR-027 is deliberately biased toward false negatives (a block
// frozen with a warning) over false positives (a user's block silently
// destroyed): "False negatives cost a warning; false positives cost the
// user's writing." So this block is never auto-upgraded -- it stays
// checksum-less and diverged on every future run -- and that is accepted, not
// a bug.
func TestUpsert_LegacyPathWithSpace_TreatedAsDiverged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	pathWithSpace := "/Users/bas/My Vault"
	legacyBlock := fmt.Sprintf(legacyTemplateFmt, pathWithSpace)
	old := "# Config\n\n" + legacyBlock + "\n\n# Footer\n"
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/upgraded/vault")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusSkippedDiverged {
		t.Errorf("status = %v, want %v (a legacy path containing a space is an accepted false negative, not an upgrade)", status, StatusSkippedDiverged)
	}

	content := readFile(t, path)
	if content != old {
		t.Errorf("expected file to be left byte-identical when the legacy path contains a space\ngot:\n%s\nwant:\n%s", content, old)
	}
	assertContains(t, content, pathWithSpace)
	assertNotContains(t, content, "/upgraded/vault")
}

// TestUpsert_NoMarkerBlock_AppendsAndPreservesContent verifies acceptance
// criterion 5 and plan test-list item 6: a file with no marker block gets
// one appended, with every byte of the pre-existing content preserved as a
// prefix, and the call reports a write.
func TestUpsert_NoMarkerBlock_AppendsAndPreservesContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	existing := "# My Config\n\n- some bullet\n- another bullet\n"
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/vault")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusWrote {
		t.Errorf("status = %v, want %v", status, StatusWrote)
	}

	content := readFile(t, path)
	if !strings.HasPrefix(content, existing) {
		t.Errorf("expected pre-existing content preserved verbatim as a prefix\ngot:\n%s\nwant prefix:\n%s", content, existing)
	}
	assertContains(t, content, StartMarker)
	assertContains(t, content, EndMarker)
}

// TestUpsert_AbsentVaultFallbackSentence verifies acceptance criteria 7-8 and
// plan test-list item 8: the emitted block carries the passed-in path, points
// at AGENTS.md with an instruction to follow it, and the ADR-028 absent-vault
// fallback sentence -- which may name only the always-on MCP tools
// (search_wiki, related_notes, expand_graph) and must never promise the
// MCP_READ_TOOLS-gated content-read tools, nor restate retrieval
// strategy/file-legend content that belongs solely to AGENTS.md.
func TestUpsert_AbsentVaultFallbackSentence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if _, err := Upsert(path, "/data/vault"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	content := readFile(t, path)

	assertContains(t, content, "/data/vault")
	assertContains(t, content, "AGENTS.md")
	lower := strings.ToLower(content)
	if !strings.Contains(lower, "follow") {
		t.Errorf("pointer must instruct the agent to follow AGENTS.md; got:\n%s", content)
	}

	// ADR-028: the absent-vault sentence itself must tell the agent to
	// report the vault as unavailable rather than guess or fabricate. This
	// is distinct from the MCP-tools sentence below -- deleting this clause
	// from bodyFmt while keeping the MCP-tools sentence intact must fail
	// this test.
	if !strings.Contains(lower, "does not exist") && !strings.Contains(lower, "not mounted") {
		t.Errorf("pointer must tell the agent when the vault path does not exist or is not mounted; got:\n%s", content)
	}
	if !strings.Contains(lower, "unavailable") {
		t.Errorf("pointer must instruct the agent to report the vault as unavailable instead of guessing; got:\n%s", content)
	}

	// Always-on tools the absent-vault sentence may name (ADR-028).
	assertContains(t, content, "search_wiki")
	assertContains(t, content, "related_notes")
	assertContains(t, content, "expand_graph")

	// MCP_READ_TOOLS-gated content-read tools (default off) must never be
	// promised -- promising them contradicts ADR-023's native-first default.
	for _, forbidden := range []string{
		"read_wiki", "grep_vault", "read_note", "read_raw", "vault_overview", "get_soul",
	} {
		assertNotContains(t, content, forbidden)
	}

	// Retrieval strategy and the file legend belong solely to AGENTS.md
	// (ADR-025); restating them here would reintroduce the drift ADR-025
	// exists to eliminate.
	assertNotContains(t, content, "Retrieval Strategy")
	assertNotContains(t, content, "Grep index.md")
	assertNotContains(t, content, "Vault Structure")
}

// TestUpsert_EmptyFile_AppendsWithNoLeadingBlankLines verifies the "target
// file exists but is empty" edge case: the block is appended with no leading
// blank lines.
func TestUpsert_EmptyFile_AppendsWithNoLeadingBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/vault")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusWrote {
		t.Errorf("status = %v, want %v", status, StatusWrote)
	}

	content := readFile(t, path)
	if !strings.HasPrefix(content, StartMarker) {
		t.Errorf("expected block to start the file with no leading blank lines, got:\n%q", content)
	}
}

// TestUpsert_UnterminatedMarkers_AppendsRatherThanSwallowingContent verifies
// the destructive-failure-mode edge case called out explicitly in the plan:
// a start marker with no matching end marker must NOT be matched by the
// marker regex -- a greedy/unanchored match could otherwise swallow
// everything after the dangling start marker to the end of the file. The
// block must instead be appended, and all existing content -- including the
// dangling start marker itself -- must survive untouched.
func TestUpsert_UnterminatedMarkers_AppendsRatherThanSwallowingContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	old := "# Config\n\n" + StartMarker + "\nsome dangling content\nthat must survive\n"
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/vault")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusWrote {
		t.Errorf("status = %v, want %v", status, StatusWrote)
	}

	content := readFile(t, path)
	// HasPrefix (rather than two separate Contains checks) pins the dangling
	// content's exact bytes and order, not merely their presence -- a
	// reordering or partial rewrite of it would be caught.
	if !strings.HasPrefix(content, old) {
		t.Errorf("expected original (dangling) content preserved verbatim as a prefix, with the new block appended after it\ngot:\n%s\nwant prefix:\n%s", content, old)
	}

	if count := strings.Count(content, StartMarker); count != 2 {
		t.Errorf("expected 2 start markers (dangling original + newly appended), got %d\ncontent:\n%s", count, content)
	}
	if count := strings.Count(content, EndMarker); count != 1 {
		t.Errorf("expected 1 end marker (from the newly appended block only), got %d\ncontent:\n%s", count, content)
	}
}

// TestUpsert_TwoMarkerPairs_OnlyFirstTouched verifies the "two marker pairs
// in one file" edge case: only the first pair is replaced/upgraded, the
// second is left completely untouched, and the count of marker pairs must
// not grow.
func TestUpsert_TwoMarkerPairs_OnlyFirstTouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if _, err := Upsert(path, "/vault/old"); err != nil {
		t.Fatalf("Upsert (seed first block): %v", err)
	}
	firstBlock := readFile(t, path)

	secondBlock := StartMarker + "\nSECOND-BLOCK-SENTINEL\n" + EndMarker
	combined := firstBlock + "\n\n# Between\n\n" + secondBlock + "\n\n# Footer\n"
	if err := os.WriteFile(path, []byte(combined), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/vault/new")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusWrote {
		t.Errorf("status = %v, want %v", status, StatusWrote)
	}

	content := readFile(t, path)
	if count := strings.Count(content, StartMarker); count != 2 {
		t.Errorf("expected marker pair count to stay at 2, got %d start markers\ncontent:\n%s", count, content)
	}
	if count := strings.Count(content, EndMarker); count != 2 {
		t.Errorf("expected marker pair count to stay at 2, got %d end markers\ncontent:\n%s", count, content)
	}

	assertContains(t, content, "/vault/new")
	assertNotContains(t, content, "/vault/old")
	assertContains(t, content, "SECOND-BLOCK-SENTINEL")
	assertContains(t, content, "# Between")
	// HasPrefix/HasSuffix (rather than Contains) pin the rewritten first
	// block to the very start of the file and the untouched footer to the
	// very end, catching a shifted splice index that Contains alone would
	// miss.
	if !strings.HasPrefix(content, StartMarker) {
		t.Errorf("expected content to start with the rewritten first block (%q)\ngot:\n%s", StartMarker, content)
	}
	if !strings.HasSuffix(content, "# Footer\n") {
		t.Errorf("expected content to end with %q\ngot:\n%s", "# Footer\n", content)
	}
}

// TestUpsert_VaultPathWithRegexMetacharacters verifies vault paths
// containing regex metacharacters and '%' survive templating and
// shape-matching intact: they must not break Sprintf-style formatting of the
// template, must not break the checksum round trip, and must not break
// legacy-shape regex matching (an unescaped metacharacter in the path could
// otherwise turn the shape match into something that matches too much, too
// little, or panics).
func TestUpsert_VaultPathWithRegexMetacharacters(t *testing.T) {
	trickyPaths := []string{
		"/vault/a.b",
		"/vault/(parenthesized)",
		"/vault/$HOME",
		"/vault/100%full",
		"/vault/a.b(c)$d%e",
	}

	for _, trickyPath := range trickyPaths {
		t.Run(trickyPath, func(t *testing.T) {
			t.Run("checksum round trip", func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "CLAUDE.md")

				status1, err := Upsert(path, trickyPath)
				if err != nil {
					t.Fatalf("Upsert (1st): %v", err)
				}
				if status1 != StatusWrote {
					t.Errorf("1st call: status = %v, want %v", status1, StatusWrote)
				}

				status2, err := Upsert(path, trickyPath)
				if err != nil {
					t.Fatalf("Upsert (2nd, round trip): %v", err)
				}
				if status2 != StatusWrote {
					t.Errorf("2nd call: status = %v, want %v", status2, StatusWrote)
				}

				content := readFile(t, path)
				assertContains(t, content, trickyPath)
				if count := strings.Count(content, StartMarker); count != 1 {
					t.Errorf("expected 1 start marker, got %d\ncontent:\n%s", count, content)
				}
			})

			t.Run("legacy shape upgrade", func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "CLAUDE.md")

				legacyBlock := fmt.Sprintf(legacyTemplateFmt, trickyPath)
				old := "# Config\n\n" + legacyBlock + "\n\n# Footer\n"
				if err := os.WriteFile(path, []byte(old), 0644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}

				status, err := Upsert(path, "/vault/new")
				if err != nil {
					t.Fatalf("Upsert: %v", err)
				}
				if status != StatusWrote {
					t.Errorf("status = %v, want %v", status, StatusWrote)
				}

				content := readFile(t, path)
				assertContains(t, content, "/vault/new")
				assertNotContains(t, content, trickyPath)
				if count := strings.Count(content, StartMarker); count != 1 {
					t.Errorf("expected 1 start marker after upgrade, got %d\ncontent:\n%s", count, content)
				}
			})
		})
	}
}

// TestUpsert_ReadOnlyFile_ReturnsWrappedError verifies the "file is
// read-only" edge case: Upsert must return a wrapped error (not swallow it or
// log-and-return it), and on failure the returned status must be the zero
// value so a caller cannot mistake a failure for a write or a skip.
func TestUpsert_ReadOnlyFile_ReturnsWrappedError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions do not block writes")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if err := os.WriteFile(path, []byte("# Config\n"), 0o444); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	status, err := Upsert(path, "/vault")
	if err == nil {
		t.Fatal("expected an error for a read-only target file, got nil")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("expected wrapped error to name the file path %q, got: %v", path, err)
	}
	if status != StatusUnknown {
		t.Errorf("status on failure = %v, want zero value %v", status, StatusUnknown)
	}
}

// TestUpsert_TargetIsDirectory_ReturnsWrappedError verifies Upsert's
// non-IsNotExist read-error branch (internal/claudemd/claudemd.go): passing
// a directory as claudeMDPath makes os.ReadFile fail with an error that is
// not os.IsNotExist, distinct from TestUpsert_ReadOnlyFile_ReturnsWrappedError
// which exercises the write-error path.
func TestUpsert_TargetIsDirectory_ReturnsWrappedError(t *testing.T) {
	dir := t.TempDir()

	status, err := Upsert(dir, "/vault")
	if err == nil {
		t.Fatal("expected an error when the target path is a directory, got nil")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("expected wrapped error to name the path %q, got: %v", dir, err)
	}
	if status != StatusUnknown {
		t.Errorf("status on failure = %v, want zero value %v", status, StatusUnknown)
	}
}

// TestUpsert_WhitespaceOnlyEditInsideChecksummedBlock_StillGeneratorOwned
// pins an intentional leniency in isGeneratorOwned: because the
// reconstructed body is compared to its checksum only after strings.TrimSpace,
// a purely-whitespace change at the very start or end of the body (e.g. an
// extra blank line inserted right after the start marker, which some editors
// do automatically) does not trip the ADR-027 divergence guard. This
// documents the behavior as an intentional leniency rather than leaving it an
// unpinned accident.
func TestUpsert_WhitespaceOnlyEditInsideChecksummedBlock_StillGeneratorOwned(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	if _, err := Upsert(path, "/vault/a"); err != nil {
		t.Fatalf("Upsert (seed): %v", err)
	}

	generated := readFile(t, path)
	// Insert extra blank lines right after the start marker -- whitespace
	// only, no content change.
	whitespaceEdited := strings.Replace(generated, StartMarker+"\n", StartMarker+"\n\n\n", 1)
	if whitespaceEdited == generated {
		t.Fatalf("test setup: whitespace edit did not change content")
	}
	if err := os.WriteFile(path, []byte(whitespaceEdited), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/vault/b")
	if err != nil {
		t.Fatalf("Upsert (after whitespace edit): %v", err)
	}
	if status != StatusWrote {
		t.Errorf("status = %v, want %v (whitespace-only edits at the body's boundary must not trip the divergence guard)", status, StatusWrote)
	}

	content := readFile(t, path)
	assertContains(t, content, "/vault/b")
}

// TestUpsert_AppendsToExisting_NoTrailingNewline pins appendSection's
// no-trailing-newline branch: when the pre-existing content does not end in
// "\n", Upsert must add the missing newline before the blank-line separator,
// so the block is neither appended directly onto the last line of existing
// content nor preceded by more than one blank line.
func TestUpsert_AppendsToExisting_NoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")

	existing := "# My Config\n\nSome existing content without trailing newline"
	if err := os.WriteFile(path, []byte(existing), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	status, err := Upsert(path, "/vault")
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if status != StatusWrote {
		t.Errorf("status = %v, want %v", status, StatusWrote)
	}

	content := readFile(t, path)
	want := existing + "\n\n" + StartMarker
	if !strings.HasPrefix(content, want) {
		t.Errorf("expected existing content followed by exactly one blank line then the block\ngot:\n%s\nwant prefix:\n%s", content, want)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(data)
}

func assertContains(t *testing.T, content, substr string) {
	t.Helper()
	if !strings.Contains(content, substr) {
		t.Errorf("expected content to contain %q", substr)
	}
}

func assertNotContains(t *testing.T, content, substr string) {
	t.Helper()
	if strings.Contains(content, substr) {
		t.Errorf("expected content NOT to contain %q", substr)
	}
}
