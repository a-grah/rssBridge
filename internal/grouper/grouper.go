package grouper

import (
	"strings"
	"unicode"

	"rssbridge/internal/store"
)

// stopWords are common English words excluded from Jaccard computation.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "by": true, "from": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "have": true, "has": true, "had": true,
	"do": true, "does": true, "did": true, "will": true, "would": true, "could": true,
	"should": true, "may": true, "might": true, "can": true, "it": true, "its": true,
	"this": true, "that": true, "as": true, "not": true, "no": true, "so": true,
	"if": true, "how": true, "what": true, "when": true, "who": true, "which": true,
	"after": true, "before": true, "new": true, "says": true, "said": true,
}

const jaccardThreshold = 0.5

// Group assigns group_ids to new articles by comparing them with each other
// and with previously-grouped articles from the store.
func Group(st *store.Store, newArticles []store.Article) error {
	if len(newArticles) == 0 {
		return nil
	}

	// Load existing recent articles for cross-batch matching.
	existing, err := st.ListRecentArticles(500)
	if err != nil {
		return err
	}

	// Build a map of article ID → group ID for existing grouped articles.
	type groupEntry struct {
		id    int64
		title string
	}
	// groupID → representative title (from the first article in that group)
	knownGroups := map[int64]string{}
	for _, a := range existing {
		if a.GroupID != nil {
			if _, ok := knownGroups[*a.GroupID]; !ok {
				g, err := st.GetGroup(*a.GroupID)
				if err == nil {
					knownGroups[*a.GroupID] = g.RepresentativeTitle
				}
			}
		}
	}

	// For each new article, find if it matches an existing group or another new article.
	// We track assignments within this batch too.
	type pending struct {
		article store.Article
		groupID int64 // 0 = unassigned
	}
	batch := make([]pending, len(newArticles))
	for i, a := range newArticles {
		batch[i] = pending{article: a}
	}

	for i := range batch {
		tokI := tokenize(batch[i].article.Title)

		// 1. Try to match against existing groups.
		bestGroupID := int64(0)
		bestScore := 0.0
		for gID, gTitle := range knownGroups {
			score := jaccard(tokI, tokenize(gTitle))
			if score >= jaccardThreshold && score > bestScore {
				bestScore = score
				bestGroupID = gID
			}
		}
		if bestGroupID != 0 {
			batch[i].groupID = bestGroupID
			continue
		}

		// 2. Try to match against earlier articles in this batch.
		for j := 0; j < i; j++ {
			tokJ := tokenize(batch[j].article.Title)
			score := jaccard(tokI, tokJ)
			if score >= jaccardThreshold {
				if batch[j].groupID != 0 {
					batch[i].groupID = batch[j].groupID
				} else {
					// Create a new group for j (and i).
					gID, err := st.CreateGroup(batch[j].article.Title)
					if err != nil {
						continue
					}
					knownGroups[gID] = batch[j].article.Title
					batch[j].groupID = gID
					batch[i].groupID = gID
				}
				break
			}
		}
	}

	// Persist assignments.
	for _, p := range batch {
		if p.groupID == 0 {
			continue
		}
		if err := st.SetArticleGroup(p.article.ID, p.groupID); err != nil {
			return err
		}
	}
	return nil
}

// tokenize lowercases a title, splits on non-letters, removes stop words.
func tokenize(s string) map[string]bool {
	tokens := map[string]bool{}
	s = strings.ToLower(s)
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, f := range fields {
		if !stopWords[f] && len(f) > 1 {
			tokens[f] = true
		}
	}
	return tokens
}

// jaccard returns |A ∩ B| / |A ∪ B|.
func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
