# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Go-based Lemmy media scraper that downloads images, videos, and other media from Lemmy instances with intelligent content-based deduplication using SHA-256 hashing. The scraper stores comprehensive metadata in SQLite and organizes downloaded files by community.

## Build and Run Commands

**Build the scraper:**
```bash
go build -o lemmy-scraper ./cmd/scraper
```

**Run with default config:**
```bash
./lemmy-scraper
```

**Run with custom config:**
```bash
./lemmy-scraper -config /path/to/config.yaml
```

**Enable verbose logging:**
```bash
./lemmy-scraper -verbose
```

**View statistics:**
```bash
./lemmy-scraper -stats
```

**Run tests:**
```bash
go test ./...
```

**Run tests with verbose output:**
```bash
go test -v ./...
```

**Format code:**
```bash
go fmt ./...
```

**Build the web UI:**
```bash
cd web && npm install && npm run build
```

**Run web UI in development mode:**
```bash
cd web && npm run dev
```

## Architecture

### Package Structure

The codebase follows a clean architecture with clear separation of concerns:

- **cmd/scraper/** - Application entry point that orchestrates the scraping workflow
- **internal/config/** - Configuration loading and validation from YAML files
- **internal/api/** - Lemmy API client with JWT authentication
- **internal/database/** - SQLite database operations and schema management
- **internal/downloader/** - Media downloading and content-based deduplication
- **internal/scraper/** - Core scraping logic with pagination support
- **internal/web/** - Optional HTTP server for web UI and API endpoints
- **pkg/models/** - Shared data models for Lemmy API responses and database records
- **web/** - SvelteKit web UI for browsing downloaded media

### Key Architectural Patterns

**Deduplication Strategy:**
The scraper uses content-based deduplication, not URL-based. Files are downloaded to memory first, SHA-256 hashed, then checked against the database before writing to disk. This prevents duplicate downloads even if the same media has different URLs.

**Two-Level Tracking:**
1. **scraped_posts table** - Tracks all processed posts (with or without media) to enable intelligent pagination stopping
2. **scraped_media table** - Tracks individual downloaded media files with full metadata

This dual-tracking enables the `stop_at_seen_posts` and `skip_seen_posts` features to work correctly.

**Pagination Model:**
The scraper supports fetching more than the Lemmy API's 50-post limit per request by implementing pagination. It tracks consecutive seen posts and stops intelligently based on the `seen_posts_threshold` config (default: 5).

**Run Modes:**
- **once** - Single execution (useful for cron jobs)
- **continuous** - Runs on a timer interval with graceful shutdown on SIGTERM/SIGINT

### Configuration System

Configuration is loaded from YAML with validation and sensible defaults:
- Required fields are validated at startup (instance, username, password, storage paths)
- Optional fields have defaults set via `SetDefaults()` method
- Sort types are normalized to match Lemmy API expectations (e.g., "hot" → "Hot")

### Media Type Detection

Media URLs are identified by:
1. File extensions (.jpg, .mp4, .webm, etc.)
2. Known media hosting services (pictrs, imgur, redd.it)

The scraper prioritizes quality:
1. Main post URL (highest quality)
2. Embedded video URL
3. Thumbnail URL (fallback only)

### Database Schema

**scraped_media table:**
- Unique constraint on `media_hash` prevents duplicate downloads
- Composite unique constraint on `(post_id, media_url)` prevents duplicate records from same post
- Indexes on hash, post_id, community_name, and downloaded_at for query performance

**scraped_posts table:**
- Tracks post_id as primary key
- Records whether post had media and count of downloaded items
- Enables idempotent scraping behavior

### Web UI Architecture

The optional web interface consists of two components:

**Backend (Go HTTP Server):**
- Serves RESTful API endpoints for querying the SQLite database
- Serves static media files from the downloads directory
- Serves the compiled SvelteKit frontend
- Runs in a goroutine alongside the scraper
- API endpoints:
  - `GET /api/media` - Paginated media list with filtering (community, type, sort)
  - `GET /api/media/:id` - Individual media item details
  - `GET /api/stats` - Overall statistics
  - `GET /api/communities` - List of communities with media counts
  - `GET /media/{community}/{filename}` - Serve actual media files

**Frontend (SvelteKit + Skeleton UI):**
- Modern Svelte 5 with runes syntax (`$state`, `$derived`)
- Skeleton UI component library with Tailwind CSS
- TypeScript for type safety
- Features:
  - Responsive grid layout for media thumbnails
  - Filtering by community and media type
  - Sorting by download date, post date, file size, or score
  - Pagination for large media libraries
  - Modal viewer for full-size images and videos
  - Statistics dashboard showing totals and breakdowns

**Integration:**
- Web server is completely optional (disabled by default)
- When enabled in "once" mode, the scraper runs first, then the web server stays up
- In "continuous" mode, the web server runs concurrently with scheduled scrapes
- CORS is enabled to allow development mode (SvelteKit dev server) to access the API
- Production builds are served as static files from `web/build/`

## Development Guidelines

### Adding New Features

When modifying the scraper behavior:
- Update both the `ScraperConfig` struct in `internal/config/config.go` and the example YAML
- Add validation in the `Validate()` method if the field is required
- Add defaults in the `SetDefaults()` method if the field is optional

### Working with the API Client

The Lemmy API client (`internal/api/client.go`) uses JWT authentication:
- Login once at startup, store the JWT token
- Include `Authorization: Bearer <token>` header in all subsequent requests
- API uses v3 endpoints (`/api/v3/...`)

### Database Operations

When adding new database queries:
- Use prepared statements (the `?` placeholder syntax)
- Handle `sql.ErrNoRows` separately from other errors
- Add appropriate indexes for new query patterns
- Remember to update the schema version if changing table structure

### Error Handling Philosophy

The scraper is designed to be fault-tolerant:
- Individual post failures don't stop the entire scrape
- Media download errors are logged but don't crash the application
- In continuous mode, errors in one run don't prevent subsequent runs

### Logging

Uses logrus with two levels:
- **Info** (default) - High-level progress and summary statistics
- **Debug** (`-verbose` flag) - Detailed operation logs including API requests and individual post processing

## File Organization

Downloaded media is organized as:
```
{base_directory}/
  {community_name}/
    {post_id}_{original_filename}
```

This structure allows easy browsing by community while preserving post IDs for cross-referencing with the database.

## Configuration Format

Key configuration sections:

**lemmy.communities:**
- Empty list `[]` scrapes from instance hot page
- Can specify communities as simple names `["technology"]` or fully qualified `["technology@lemmy.ml"]`

**scraper.max_posts_per_run:**
- Total posts across all pages
- If pagination disabled, automatically capped at 50 (API max)

**scraper.stop_at_seen_posts vs skip_seen_posts:**
- `stop_at_seen_posts: true` - Stop scraping after hitting threshold of consecutive seen posts
- `skip_seen_posts: true` - Skip seen posts but continue scraping (use with caution on large communities)

**scraper.seen_posts_threshold:**
- Number of consecutive seen posts before stopping (when `stop_at_seen_posts: true`)
- Default: 5
- Prevents premature stopping while ensuring efficiency

**web_server.enabled:**
- Set to `true` to enable the web UI for browsing downloaded media
- Default: `false` (disabled)
- When enabled, starts an HTTP server alongside the scraper

**web_server.host:**
- Host/interface to bind the web server to
- Default: `localhost` (only accessible from local machine)
- Use `0.0.0.0` to allow external network access

**web_server.port:**
- TCP port for the web server
- Default: `8080`
- Access the web UI at `http://{host}:{port}`

## Web UI Development

When working on the web interface:

**Development workflow:**
1. Start the Go backend with web server enabled
2. In a separate terminal, run `cd web && npm run dev` for hot-reload frontend development
3. The dev server (usually port 5173) will proxy API calls to the Go backend (port 8080)

**Building for production:**
1. Run `cd web && npm run build` to create optimized static files
2. The Go server will automatically serve these from `web/build/`
3. If build doesn't exist, Go server shows a helpful message with build instructions

**Modifying the API:**
- Add new endpoints in `internal/web/server.go`
- Update TypeScript interfaces in the SvelteKit pages as needed
- Remember to handle CORS for development mode

**Styling with Skeleton:**
- Use Skeleton's semantic class names (e.g., `btn`, `card`, `badge`)
- Theme colors use `variant-*` classes (e.g., `variant-filled-primary`)
- Surface colors auto-adapt to light/dark mode with `surface-*-token` classes
