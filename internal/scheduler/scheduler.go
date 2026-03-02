package scheduler

import (
	"log"
	"strconv"
	"sync"
	"time"

	"rssbridge/internal/grouper"
	"rssbridge/internal/scraper"
	"rssbridge/internal/store"
)

// Scheduler drives periodic and manual site fetches.
type Scheduler struct {
	st     *store.Store
	wake   chan struct{}
	mu     sync.Mutex
}

// New creates a Scheduler.
func New(st *store.Store) *Scheduler {
	return &Scheduler{
		st:   st,
		wake: make(chan struct{}, 1),
	}
}

// Start runs the scheduler loop in a goroutine.
func (s *Scheduler) Start() {
	go s.loop()
}

// TriggerFetch schedules an immediate fetch for a site.
func (s *Scheduler) TriggerFetch(siteID int64) {
	if err := s.st.SetSiteNextFetch(siteID, time.Now().Add(-time.Second)); err != nil {
		log.Printf("scheduler: set next fetch: %v", err)
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Scheduler) loop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// Immediate first tick
	s.runDue()

	for {
		select {
		case <-ticker.C:
			s.runDue()
		case <-s.wake:
			s.runDue()
		}
	}
}

func (s *Scheduler) runDue() {
	// Prune old articles
	s.prune()

	sites, err := s.st.ListEnabledSites()
	if err != nil {
		log.Printf("scheduler: list sites: %v", err)
		return
	}
	now := time.Now()
	for _, site := range sites {
		if site.NextFetchAt == nil || now.After(*site.NextFetchAt) {
			go s.fetchSite(site)
		}
	}
}

func (s *Scheduler) fetchSite(site store.Site) {
	s.mu.Lock()
	defer s.mu.Unlock()

	log.Printf("scheduler: fetching %s (%s)", site.Name, site.URL)

	result := scraper.FetchSite(s.st, &site)

	errStr := ""
	if result.Err != nil {
		errStr = result.Err.Error()
		log.Printf("scheduler: fetch %s error: %v", site.Name, result.Err)
	} else {
		log.Printf("scheduler: %s — found=%d added=%d", site.Name, result.ArticlesFound, result.ArticlesAdded)
	}

	// Store native RSS if discovered
	if result.NativeRSSURL != "" && site.NativeRSSURL == "" {
		if err := s.st.SetSiteNativeRSS(site.ID, result.NativeRSSURL); err != nil {
			log.Printf("scheduler: set native rss: %v", err)
		}
	}

	// Group new articles
	if len(result.NewArticles) > 0 {
		if err := grouper.Group(s.st, result.NewArticles); err != nil {
			log.Printf("scheduler: grouper error: %v", err)
		}
	}

	// Log fetch
	fl := &store.FetchLog{
		SiteID:        site.ID,
		ArticlesFound: result.ArticlesFound,
		ArticlesAdded: result.ArticlesAdded,
		Error:         errStr,
	}
	if err := s.st.InsertFetchLog(fl); err != nil {
		log.Printf("scheduler: log fetch: %v", err)
	}

	// Update site timestamps
	next := time.Now().Add(time.Duration(site.FetchIntervalHours * float64(time.Hour)))
	if err := s.st.SetSiteFetched(site.ID, next); err != nil {
		log.Printf("scheduler: set site fetched: %v", err)
	}
}

func (s *Scheduler) prune() {
	val, err := s.st.GetSetting("prune_after_days")
	if err != nil {
		return
	}
	days, err := strconv.Atoi(val)
	if err != nil || days <= 0 {
		return
	}
	if err := s.st.PruneArticles(days); err != nil {
		log.Printf("scheduler: prune: %v", err)
	}
}
