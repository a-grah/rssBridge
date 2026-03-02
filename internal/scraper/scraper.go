package scraper

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
	"rssbridge/internal/store"
)

var httpClient = &http.Client{
	Timeout: 20 * time.Second,
}

// skipPaths are path prefixes that are unlikely to be article pages.
var skipPaths = []string{
	"/about", "/contact", "/privacy", "/terms", "/advertis",
	"/jobs", "/careers", "/help", "/faq", "/login", "/signup",
	"/register", "/search", "/tag/", "/tags/", "/category/",
	"/author/", "/page/", "/feed", "/rss", "/sitemap",
	"/cdn-cgi/", "/static/", "/assets/", "/img/", "/images/",
	"/css/", "/js/", "/fonts/", "/favicon",
}

// Result holds the outcome of scraping a single site.
type Result struct {
	NativeRSSURL  string
	ArticlesFound int
	ArticlesAdded int
	NewArticles   []store.Article
	Err           error
}

// FetchSite scrapes a site homepage, discovers article links, and inserts new articles.
// It returns candidates for grouping (newly inserted articles).
func FetchSite(st *store.Store, site *store.Site) Result {
	resp, err := httpClient.Get(site.URL)
	if err != nil {
		return Result{Err: fmt.Errorf("fetch homepage: %w", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return Result{Err: fmt.Errorf("read body: %w", err)}
	}

	base, err := url.Parse(site.URL)
	if err != nil {
		return Result{Err: fmt.Errorf("parse site url: %w", err)}
	}

	nativeRSS := extractNativeRSS(body, base)
	links := extractLinks(body, base)

	keywords := parseKeywords(site.KeywordsExclude)

	var result Result
	result.NativeRSSURL = nativeRSS

	for _, lnk := range links {
		result.ArticlesFound++

		// Dedup by URL
		exists, err := st.URLExists(lnk.href)
		if err != nil || exists {
			continue
		}

		// Fetch article page for summary
		title, summary := fetchArticleMeta(lnk.href, lnk.text)

		if title == "" {
			title = lnk.text
		}
		if title == "" {
			continue
		}

		// Apply keyword exclusion
		if matchesKeyword(title+" "+summary, keywords) {
			continue
		}

		a := &store.Article{
			SiteID:  site.ID,
			URL:     lnk.href,
			Title:   title,
			Summary: summary,
		}
		id, err := st.InsertArticle(a)
		if err != nil {
			continue
		}
		if id == 0 {
			// INSERT OR IGNORE — URL already existed
			continue
		}
		a.ID = id
		result.ArticlesAdded++
		result.NewArticles = append(result.NewArticles, *a)
	}

	return result
}

type link struct {
	href string
	text string
}

func extractNativeRSS(body []byte, base *url.URL) string {
	z := html.NewTokenizer(strings.NewReader(string(body)))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			t := z.Token()
			if t.Data == "link" {
				var rel, typ, href string
				for _, a := range t.Attr {
					switch a.Key {
					case "rel":
						rel = a.Val
					case "type":
						typ = a.Val
					case "href":
						href = a.Val
					}
				}
				if strings.Contains(rel, "alternate") &&
					(strings.Contains(typ, "rss") || strings.Contains(typ, "atom")) &&
					href != "" {
					resolved := resolveURL(base, href)
					return resolved
				}
			}
		}
	}
	return ""
}

func extractLinks(body []byte, base *url.URL) []link {
	var links []link
	seen := map[string]bool{}

	z := html.NewTokenizer(strings.NewReader(string(body)))
	var currentHref string
	var buf strings.Builder

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return links
		case html.StartTagToken:
			t := z.Token()
			if t.Data == "a" {
				for _, a := range t.Attr {
					if a.Key == "href" {
						currentHref = a.Val
						buf.Reset()
					}
				}
			}
		case html.TextToken:
			if currentHref != "" {
				buf.WriteString(strings.TrimSpace(z.Token().Data))
			}
		case html.EndTagToken:
			t := z.Token()
			if t.Data == "a" && currentHref != "" {
				href := resolveURL(base, currentHref)
				text := strings.TrimSpace(buf.String())
				currentHref = ""
				buf.Reset()

				if !isContentLink(href, base) {
					continue
				}
				if seen[href] {
					continue
				}
				seen[href] = true
				links = append(links, link{href: href, text: text})
			}
		}
	}
}

func isContentLink(href string, base *url.URL) bool {
	u, err := url.Parse(href)
	if err != nil {
		return false
	}
	// Must be same host
	if !strings.EqualFold(u.Host, base.Host) {
		return false
	}
	// Must have a non-trivial path
	p := strings.ToLower(u.Path)
	if p == "" || p == "/" {
		return false
	}
	for _, skip := range skipPaths {
		if strings.HasPrefix(p, skip) {
			return false
		}
	}
	// Skip bare hash or query-only URLs
	if strings.HasPrefix(href, "#") {
		return false
	}
	return true
}

func resolveURL(base *url.URL, href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(u)
	// Strip fragment
	resolved.Fragment = ""
	return resolved.String()
}

// fetchArticleMeta fetches the article page and extracts title + summary.
func fetchArticleMeta(articleURL, linkText string) (title, summary string) {
	resp, err := httpClient.Get(articleURL)
	if err != nil {
		return linkText, ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return linkText, ""
	}

	z := html.NewTokenizer(strings.NewReader(string(body)))

	var inTitle, inP bool
	var titleText, descMeta, firstP strings.Builder
	foundFirstP := false

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			switch t.Data {
			case "title":
				inTitle = true
			case "meta":
				var name, content string
				for _, a := range t.Attr {
					switch strings.ToLower(a.Key) {
					case "name":
						name = strings.ToLower(a.Val)
					case "content":
						content = a.Val
					}
				}
				if name == "description" && content != "" {
					descMeta.WriteString(content)
				}
			case "p":
				if !foundFirstP {
					inP = true
				}
			}
		case html.TextToken:
			text := strings.TrimSpace(z.Token().Data)
			if inTitle {
				titleText.WriteString(text)
			}
			if inP && !foundFirstP && text != "" {
				firstP.WriteString(text + " ")
			}
		case html.EndTagToken:
			t := z.Token()
			switch t.Data {
			case "title":
				inTitle = false
			case "p":
				if inP {
					inP = false
					foundFirstP = true
				}
			}
		}
		// Stop early once we have everything we need
		if titleText.Len() > 0 && (descMeta.Len() > 0 || foundFirstP) {
			break
		}
	}

	title = strings.TrimSpace(titleText.String())
	if title == "" {
		title = linkText
	}

	summary = strings.TrimSpace(descMeta.String())
	if summary == "" {
		summary = strings.TrimSpace(firstP.String())
	}
	if len(summary) > 500 {
		summary = summary[:500] + "…"
	}

	return title, summary
}

func parseKeywords(kw string) []string {
	var result []string
	for _, k := range strings.Split(kw, ",") {
		k = strings.TrimSpace(strings.ToLower(k))
		if k != "" {
			result = append(result, k)
		}
	}
	return result
}

func matchesKeyword(text string, keywords []string) bool {
	lower := strings.ToLower(text)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
