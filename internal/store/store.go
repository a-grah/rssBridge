package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Site represents a monitored website.
type Site struct {
	ID                 int64
	Name               string
	URL                string
	Enabled            bool
	FetchIntervalHours float64
	KeywordsExclude    string
	NativeRSSURL       string
	LastFetchedAt      *time.Time
	NextFetchAt        *time.Time
	CreatedAt          time.Time
}

// Article represents a scraped article.
type Article struct {
	ID      int64
	SiteID  int64
	URL     string
	Title   string
	Summary string
	GroupID *int64
	AddedAt time.Time
}

// ArticleGroup represents a group of similar articles.
type ArticleGroup struct {
	ID                  int64
	RepresentativeTitle string
	CreatedAt           time.Time
}

// FetchLog represents a single fetch event.
type FetchLog struct {
	ID            int64
	SiteID        int64
	SiteName      string
	FetchedAt     time.Time
	ArticlesFound int
	ArticlesAdded int
	Error         string
}

// Store holds the database connection.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database and runs migrations.
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	ddl := `
CREATE TABLE IF NOT EXISTS sites (
  id                   INTEGER PRIMARY KEY AUTOINCREMENT,
  name                 TEXT NOT NULL,
  url                  TEXT NOT NULL UNIQUE,
  enabled              INTEGER NOT NULL DEFAULT 1,
  fetch_interval_hours REAL NOT NULL DEFAULT 12,
  keywords_exclude     TEXT,
  native_rss_url       TEXT,
  last_fetched_at      DATETIME,
  next_fetch_at        DATETIME,
  created_at           DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS article_groups (
  id                   INTEGER PRIMARY KEY AUTOINCREMENT,
  representative_title TEXT NOT NULL,
  created_at           DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS articles (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  site_id     INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  url         TEXT NOT NULL UNIQUE,
  title       TEXT NOT NULL,
  summary     TEXT,
  group_id    INTEGER REFERENCES article_groups(id),
  added_at    DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS fetch_log (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  site_id         INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  fetched_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
  articles_found  INTEGER,
  articles_added  INTEGER,
  error           TEXT
);

INSERT OR IGNORE INTO settings (key, value) VALUES
  ('default_interval_hours', '12'),
  ('prune_after_days', '30'),
  ('rss_max_items', '100'),
  ('port', '7171'),
  ('rss_title', 'rssBridge');
`
	_, err := s.db.Exec(ddl)
	return err
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// --- Settings ---

// GetSetting returns the value for a settings key.
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	return v, err
}

// GetAllSettings returns all settings as a map.
func (s *Store) GetAllSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// SetSetting upserts a setting.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// --- Sites ---

// ListSites returns all sites.
func (s *Store) ListSites() ([]Site, error) {
	rows, err := s.db.Query(`
		SELECT id, name, url, enabled, fetch_interval_hours,
		       COALESCE(keywords_exclude,''), COALESCE(native_rss_url,''),
		       last_fetched_at, next_fetch_at, created_at
		FROM sites ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sites []Site
	for rows.Next() {
		var st Site
		var lastFetched, nextFetch sql.NullString
		if err := rows.Scan(&st.ID, &st.Name, &st.URL, &st.Enabled, &st.FetchIntervalHours,
			&st.KeywordsExclude, &st.NativeRSSURL, &lastFetched, &nextFetch, &st.CreatedAt); err != nil {
			return nil, err
		}
		if lastFetched.Valid {
			t, _ := time.Parse("2006-01-02T15:04:05Z", lastFetched.String)
			if t.IsZero() {
				t, _ = time.Parse("2006-01-02 15:04:05", lastFetched.String)
			}
			st.LastFetchedAt = &t
		}
		if nextFetch.Valid {
			t, _ := time.Parse("2006-01-02T15:04:05Z", nextFetch.String)
			if t.IsZero() {
				t, _ = time.Parse("2006-01-02 15:04:05", nextFetch.String)
			}
			st.NextFetchAt = &t
		}
		sites = append(sites, st)
	}
	return sites, rows.Err()
}

// ListEnabledSites returns only enabled sites.
func (s *Store) ListEnabledSites() ([]Site, error) {
	rows, err := s.db.Query(`
		SELECT id, name, url, enabled, fetch_interval_hours,
		       COALESCE(keywords_exclude,''), COALESCE(native_rss_url,''),
		       last_fetched_at, next_fetch_at, created_at
		FROM sites WHERE enabled=1 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sites []Site
	for rows.Next() {
		var st Site
		var lastFetched, nextFetch sql.NullString
		if err := rows.Scan(&st.ID, &st.Name, &st.URL, &st.Enabled, &st.FetchIntervalHours,
			&st.KeywordsExclude, &st.NativeRSSURL, &lastFetched, &nextFetch, &st.CreatedAt); err != nil {
			return nil, err
		}
		if lastFetched.Valid {
			t, _ := time.Parse("2006-01-02T15:04:05Z", lastFetched.String)
			if t.IsZero() {
				t, _ = time.Parse("2006-01-02 15:04:05", lastFetched.String)
			}
			st.LastFetchedAt = &t
		}
		if nextFetch.Valid {
			t, _ := time.Parse("2006-01-02T15:04:05Z", nextFetch.String)
			if t.IsZero() {
				t, _ = time.Parse("2006-01-02 15:04:05", nextFetch.String)
			}
			st.NextFetchAt = &t
		}
		sites = append(sites, st)
	}
	return sites, rows.Err()
}

// GetSite returns a single site by ID.
func (s *Store) GetSite(id int64) (*Site, error) {
	var st Site
	var lastFetched, nextFetch sql.NullString
	err := s.db.QueryRow(`
		SELECT id, name, url, enabled, fetch_interval_hours,
		       COALESCE(keywords_exclude,''), COALESCE(native_rss_url,''),
		       last_fetched_at, next_fetch_at, created_at
		FROM sites WHERE id=?`, id).Scan(
		&st.ID, &st.Name, &st.URL, &st.Enabled, &st.FetchIntervalHours,
		&st.KeywordsExclude, &st.NativeRSSURL, &lastFetched, &nextFetch, &st.CreatedAt)
	if err != nil {
		return nil, err
	}
	if lastFetched.Valid {
		t, _ := time.Parse("2006-01-02T15:04:05Z", lastFetched.String)
		if t.IsZero() {
			t, _ = time.Parse("2006-01-02 15:04:05", lastFetched.String)
		}
		st.LastFetchedAt = &t
	}
	if nextFetch.Valid {
		t, _ := time.Parse("2006-01-02T15:04:05Z", nextFetch.String)
		if t.IsZero() {
			t, _ = time.Parse("2006-01-02 15:04:05", nextFetch.String)
		}
		st.NextFetchAt = &t
	}
	return &st, nil
}

// CreateSite inserts a new site.
func (s *Store) CreateSite(st *Site) (int64, error) {
	res, err := s.db.Exec(`
		INSERT INTO sites(name,url,enabled,fetch_interval_hours,keywords_exclude)
		VALUES(?,?,?,?,?)`,
		st.Name, st.URL, boolInt(st.Enabled), st.FetchIntervalHours, st.KeywordsExclude)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateSite updates an existing site.
func (s *Store) UpdateSite(st *Site) error {
	_, err := s.db.Exec(`
		UPDATE sites SET name=?, url=?, enabled=?, fetch_interval_hours=?, keywords_exclude=?
		WHERE id=?`,
		st.Name, st.URL, boolInt(st.Enabled), st.FetchIntervalHours, st.KeywordsExclude, st.ID)
	return err
}

// DeleteSite removes a site and its articles/logs via cascade.
func (s *Store) DeleteSite(id int64) error {
	_, err := s.db.Exec(`DELETE FROM sites WHERE id=?`, id)
	return err
}

// SetSiteNativeRSS sets the native_rss_url for a site.
func (s *Store) SetSiteNativeRSS(id int64, rssURL string) error {
	_, err := s.db.Exec(`UPDATE sites SET native_rss_url=? WHERE id=?`, rssURL, id)
	return err
}

// SetSiteFetched updates last_fetched_at and next_fetch_at.
func (s *Store) SetSiteFetched(id int64, next time.Time) error {
	_, err := s.db.Exec(
		`UPDATE sites SET last_fetched_at=CURRENT_TIMESTAMP, next_fetch_at=? WHERE id=?`,
		next.UTC().Format("2006-01-02T15:04:05Z"), id)
	return err
}

// SetSiteNextFetch sets next_fetch_at (used by manual trigger).
func (s *Store) SetSiteNextFetch(id int64, t time.Time) error {
	_, err := s.db.Exec(`UPDATE sites SET next_fetch_at=? WHERE id=?`,
		t.UTC().Format("2006-01-02T15:04:05Z"), id)
	return err
}

// --- Articles ---

// URLExists returns true if an article URL is already stored.
func (s *Store) URLExists(url string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM articles WHERE url=?`, url).Scan(&n)
	return n > 0, err
}

// InsertArticle inserts an article and returns its ID.
func (s *Store) InsertArticle(a *Article) (int64, error) {
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO articles(site_id,url,title,summary) VALUES(?,?,?,?)`,
		a.SiteID, a.URL, a.Title, a.Summary)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetArticleGroup sets the group_id for an article.
func (s *Store) SetArticleGroup(articleID, groupID int64) error {
	_, err := s.db.Exec(`UPDATE articles SET group_id=? WHERE id=?`, groupID, articleID)
	return err
}

// ListRecentArticles returns the most recent articles up to limit.
func (s *Store) ListRecentArticles(limit int) ([]Article, error) {
	rows, err := s.db.Query(`
		SELECT id, site_id, url, title, COALESCE(summary,''), group_id, added_at
		FROM articles ORDER BY added_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArticles(rows)
}

// ListArticlesBySite returns all articles for a site.
func (s *Store) ListArticlesBySite(siteID int64) ([]Article, error) {
	rows, err := s.db.Query(`
		SELECT id, site_id, url, title, COALESCE(summary,''), group_id, added_at
		FROM articles WHERE site_id=? ORDER BY added_at DESC`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArticles(rows)
}

func scanArticles(rows *sql.Rows) ([]Article, error) {
	var articles []Article
	for rows.Next() {
		var a Article
		var groupID sql.NullInt64
		if err := rows.Scan(&a.ID, &a.SiteID, &a.URL, &a.Title, &a.Summary, &groupID, &a.AddedAt); err != nil {
			return nil, err
		}
		if groupID.Valid {
			g := groupID.Int64
			a.GroupID = &g
		}
		articles = append(articles, a)
	}
	return articles, rows.Err()
}

// PruneArticles deletes articles older than n days.
func (s *Store) PruneArticles(days int) error {
	_, err := s.db.Exec(
		`DELETE FROM articles WHERE added_at < datetime('now', ? || ' days')`,
		fmt.Sprintf("-%d", days))
	return err
}

// --- Article Groups ---

// CreateGroup inserts a new article group.
func (s *Store) CreateGroup(title string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO article_groups(representative_title) VALUES(?)`, title)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetGroup returns a group by ID.
func (s *Store) GetGroup(id int64) (*ArticleGroup, error) {
	var g ArticleGroup
	err := s.db.QueryRow(`SELECT id, representative_title, created_at FROM article_groups WHERE id=?`, id).
		Scan(&g.ID, &g.RepresentativeTitle, &g.CreatedAt)
	return &g, err
}

// ListGroupArticles returns all articles in a group.
func (s *Store) ListGroupArticles(groupID int64) ([]Article, error) {
	rows, err := s.db.Query(`
		SELECT id, site_id, url, title, COALESCE(summary,''), group_id, added_at
		FROM articles WHERE group_id=? ORDER BY added_at DESC`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArticles(rows)
}

// --- Fetch Log ---

// InsertFetchLog records a fetch event.
func (s *Store) InsertFetchLog(l *FetchLog) error {
	_, err := s.db.Exec(
		`INSERT INTO fetch_log(site_id,articles_found,articles_added,error) VALUES(?,?,?,?)`,
		l.SiteID, l.ArticlesFound, l.ArticlesAdded, l.Error)
	return err
}

// ListFetchLog returns recent fetch log entries.
func (s *Store) ListFetchLog(limit int) ([]FetchLog, error) {
	rows, err := s.db.Query(`
		SELECT fl.id, fl.site_id, COALESCE(s.name,'deleted'), fl.fetched_at,
		       fl.articles_found, fl.articles_added, COALESCE(fl.error,'')
		FROM fetch_log fl
		LEFT JOIN sites s ON s.id=fl.site_id
		ORDER BY fl.fetched_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var logs []FetchLog
	for rows.Next() {
		var l FetchLog
		if err := rows.Scan(&l.ID, &l.SiteID, &l.SiteName, &l.FetchedAt,
			&l.ArticlesFound, &l.ArticlesAdded, &l.Error); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// --- Stats ---

// DashboardStats holds summary counts for the admin dashboard.
type DashboardStats struct {
	TotalSites    int
	TotalArticles int
	TotalGroups   int
}

// GetDashboardStats returns aggregate counts.
func (s *Store) GetDashboardStats() (DashboardStats, error) {
	var ds DashboardStats
	s.db.QueryRow(`SELECT COUNT(*) FROM sites`).Scan(&ds.TotalSites)
	s.db.QueryRow(`SELECT COUNT(*) FROM articles`).Scan(&ds.TotalArticles)
	s.db.QueryRow(`SELECT COUNT(DISTINCT group_id) FROM articles WHERE group_id IS NOT NULL`).Scan(&ds.TotalGroups)
	return ds, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
