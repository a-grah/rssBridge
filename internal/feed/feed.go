package feed

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"rssbridge/internal/store"
)

// RSS is the top-level RSS 2.0 structure.
type RSS struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel Channel  `xml:"channel"`
}

// Channel is the RSS channel element.
type Channel struct {
	Title         string `xml:"title"`
	Link          string `xml:"link"`
	Description   string `xml:"description"`
	Language      string `xml:"language"`
	LastBuildDate string `xml:"lastBuildDate"`
	Items         []Item `xml:"item"`
}

// Item is an RSS item element.
type Item struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
}

// Build constructs an RSS feed from the store.
func Build(st *store.Store) ([]byte, error) {
	settings, err := st.GetAllSettings()
	if err != nil {
		return nil, fmt.Errorf("get settings: %w", err)
	}

	maxItems := 100
	fmt.Sscanf(settings["rss_max_items"], "%d", &maxItems)
	rssTitle := settings["rss_title"]
	if rssTitle == "" {
		rssTitle = "rssBridge"
	}

	articles, err := st.ListRecentArticles(maxItems * 3) // fetch extra to account for grouped
	if err != nil {
		return nil, fmt.Errorf("list articles: %w", err)
	}

	// Build items, handling groups.
	groupSeen := map[int64]bool{}
	var items []Item
	siteNativeRSS := map[int64]string{} // siteID → nativeRSSURL

	sites, err := st.ListSites()
	if err == nil {
		for _, s := range sites {
			if s.NativeRSSURL != "" {
				siteNativeRSS[s.ID] = s.NativeRSSURL
				// Emit advisory note for native RSS
				items = append(items, Item{
					Title: fmt.Sprintf("Note: %s has a native RSS feed", s.Name),
					Link:  s.NativeRSSURL,
					Description: fmt.Sprintf(
						"%s offers a native RSS feed at %s — consider subscribing directly.",
						s.Name, s.NativeRSSURL),
					GUID:    "native-rss-" + s.NativeRSSURL,
					PubDate: time.Now().UTC().Format(time.RFC1123Z),
				})
			}
		}
	}

	for _, a := range articles {
		if len(items) >= maxItems {
			break
		}

		if a.GroupID != nil {
			gid := *a.GroupID
			if groupSeen[gid] {
				continue
			}
			groupSeen[gid] = true

			// Build merged item for the group
			groupArticles, err := st.ListGroupArticles(gid)
			if err != nil {
				continue
			}
			group, err := st.GetGroup(gid)
			if err != nil {
				continue
			}

			var desc strings.Builder
			desc.WriteString("<ul>")
			for _, ga := range groupArticles {
				desc.WriteString(fmt.Sprintf("<li><a href=%q>%s</a>", ga.URL, xmlEscape(ga.Title)))
				if ga.Summary != "" {
					desc.WriteString(" — " + xmlEscape(ga.Summary))
				}
				desc.WriteString("</li>")
			}
			desc.WriteString("</ul>")

			items = append(items, Item{
				Title:       group.RepresentativeTitle,
				Link:        groupArticles[0].URL,
				Description: desc.String(),
				GUID:        fmt.Sprintf("group:%d", gid),
				PubDate:     a.AddedAt.UTC().Format(time.RFC1123Z),
			})
		} else {
			items = append(items, Item{
				Title:       a.Title,
				Link:        a.URL,
				Description: a.Summary,
				GUID:        a.URL,
				PubDate:     a.AddedAt.UTC().Format(time.RFC1123Z),
			})
		}
	}

	rss := RSS{
		Version: "2.0",
		Channel: Channel{
			Title:         rssTitle,
			Link:          "http://localhost:" + settings["port"] + "/admin",
			Description:   "Aggregated articles from sites without RSS feeds",
			Language:      "en",
			LastBuildDate: time.Now().UTC().Format(time.RFC1123Z),
			Items:         items,
		},
	}

	out, err := xml.MarshalIndent(rss, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), out...), nil
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
