package claudemd

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	StartMarker = "<!-- lucidvault:start -->"
	EndMarker   = "<!-- lucidvault:end -->"
)

var markerRe = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(StartMarker) + `.*?` + regexp.QuoteMeta(EndMarker))

const sectionTemplate = `<!-- lucidvault:start -->
## LucidVault Knowledge Base

You have a personal knowledge base of saved articles and bookmarks at %s.

### User Context
Read ` + "`%s/soul.md`" + ` first if it exists. It describes who the user is and how they prefer answers. Use it to tailor your responses.

### Retrieval Strategy
When the user asks about any topic, technology, or concept:
1. **Grep index.md** — Do not read the full index. Grep ` + "`%s/index.md`" + ` for keywords or tags relevant to the question.
2. **Read wiki pages** — Open matching pages from ` + "`%s/wiki/`" + `. These are LLM-enriched summaries with tags, key takeaways, and wiki-links.
3. **Check personal notes** — Search ` + "`%s/notes/`" + ` by keyword if wiki pages don't fully answer the question. Grep for keywords, do not scan the directory.
4. **Read raw pages** — Only read from ` + "`%s/raw/`" + ` if the wiki and notes lack detail. Raw files are much larger.
5. **Fetch a URL (last resort)** — If the vault has no answer, offer to fetch a URL from a vault page or one the user provides. Ask before fetching.

Never read all files in a directory. Always grep for keywords first.

### Vault Structure
- ` + "`soul.md`" + ` — User profile and preferences
- ` + "`index.md`" + ` — Master catalog of all pages (start here)
- ` + "`wiki/`" + ` — LLM-enriched summaries (preferred reading)
- ` + "`raw/`" + ` — Full scraped source content (use sparingly)
- ` + "`notes/`" + ` — Personal notes (search by keyword only)
<!-- lucidvault:end -->`

// Upsert inserts or replaces the LucidVault section in a CLAUDE.md file.
func Upsert(claudeMDPath, vaultPath string) error {
	section := fmt.Sprintf(sectionTemplate, vaultPath, vaultPath, vaultPath, vaultPath, vaultPath, vaultPath)

	data, err := os.ReadFile(claudeMDPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", claudeMDPath, err)
	}

	content := string(data)

	if markerRe.MatchString(content) {
		content = markerRe.ReplaceAllString(content, section)
	} else {
		if len(content) > 0 && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if len(content) > 0 {
			content += "\n"
		}
		content += section + "\n"
	}

	if err := os.WriteFile(claudeMDPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", claudeMDPath, err)
	}

	return nil
}
