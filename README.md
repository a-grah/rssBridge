# rssBridge

A self-hosted Go web app that polls sites that don't offer RSS feeds, extracts article headlines, groups similar stories, and serves a standard RSS 2.0 feed for any reader (NetNewsWire, Reeder, etc.).

Runs as a single binary on localhost with no system dependencies.

## Features

- Scrapes homepages and extracts article links
- Fetches per-article meta descriptions as summaries
- Detects and surfaces native RSS feeds when a site has one
- Groups similar stories using Jaccard title similarity
- Serves a standard RSS 2.0 feed
- Per-site fetch intervals and keyword exclusion filters
- Background scheduler with configurable pruning
- Dark-themed admin panel with manual "Fetch Now" controls

## Requirements

- Go 1.23+

No CGO. No system libraries. No external services.

## Install

```bash
git clone https://github.com/yourname/rssBridge
cd rssBridge
go build -o rssBridge .
./rssBridge
```

Open `http://localhost:7171/admin`.

## Usage

```
./rssBridge [flags]

  --port  string   HTTP port (default: value in DB, fallback 7171)
  --db    string   Path to SQLite database (default: rssbridge.db)
```

Port can also be set via the `RSSBRIDGE_PORT` environment variable or through the Settings page in the admin panel.

## Admin Panel

| Path | Description |
|---|---|
| `/admin` | Dashboard — site statuses, recent fetches |
| `/admin/sites` | Add, edit, and delete monitored sites |
| `/admin/settings` | Global settings |
| `/admin/log` | Full fetch history |
| `/rss` | The RSS 2.0 feed |

## Adding a Site

1. Go to **Sites → Add New Site**
2. Enter a name and homepage URL (e.g. `https://news.ycombinator.com`)
3. Set a fetch interval in hours (default 12)
4. Optionally add comma-separated keywords to exclude (e.g. `crypto, sponsored`)
5. Click **Add Site & Fetch** — a fetch runs immediately

## Settings

| Key | Default | Description |
|---|---|---|
| `rss_title` | `rssBridge` | Channel title in the RSS feed |
| `rss_max_items` | `100` | Max items in the feed |
| `default_interval_hours` | `12` | Default fetch interval for new sites |
| `prune_after_days` | `30` | Auto-delete articles older than N days (0 = off) |
| `port` | `7171` | HTTP server port |

## RSS Feed

Subscribe to `http://localhost:7171/rss` in your RSS reader.

If a monitored site has its own native RSS feed, rssBridge detects it and shows an advisory item in the feed with a direct link so you can subscribe to the source instead.

## Dependencies

- [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) — pure-Go SQLite driver, no CGO
- [`golang.org/x/net/html`](https://pkg.go.dev/golang.org/x/net/html) — HTML tokenizer
