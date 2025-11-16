# Lemmy Media Scraper

A Go-based tool for scraping and downloading media (images, videos, and other files) from Lemmy instances. Features intelligent deduplication using content hashing and comprehensive metadata storage.

## Features

- **Multi-instance support**: Connect to any Lemmy instance
- **Community-specific scraping**: Target specific communities or scrape from the hot page
- **Intelligent deduplication**: Uses SHA-256 content hashing to avoid downloading duplicates
- **Comprehensive metadata**: Stores post details, community info, author data, and more in SQLite
- **Multiple run modes**: One-time execution or continuous monitoring
- **Flexible media filtering**: Choose which media types to download (images, videos, other)
- **Organized storage**: Files automatically organized by community
- **Smart pagination**: Configurable limits with optional stopping at previously seen posts

## Requirements

- Go 1.21 or later
- SQLite3
- A Lemmy account on the instance you want to scrape

## Installation

### Pre-built Binaries

Download the latest release for your platform from the [Releases page](https://github.com/neo1908/lemmy-image-scraper/releases).

**Linux (x86_64):**
```bash
wget https://github.com/neo1908/lemmy-image-scraper/releases/latest/download/lemmy-scraper_*_Linux_x86_64.tar.gz
tar -xzf lemmy-scraper_*_Linux_x86_64.tar.gz
./lemmy-scraper -config config.example.yaml
```

**macOS (Apple Silicon):**
```bash
wget https://github.com/neo1908/lemmy-image-scraper/releases/latest/download/lemmy-scraper_*_Darwin_arm64.tar.gz
tar -xzf lemmy-scraper_*_Darwin_arm64.tar.gz
./lemmy-scraper -config config.example.yaml
```

Each release includes:
- The `lemmy-scraper` binary
- `config.example.yaml` - Example configuration file
- `README.md` - Documentation

### Docker

Run with Docker Compose (recommended):

```bash
git clone https://github.com/neo1908/lemmy-image-scraper.git
cd lemmy-image-scraper
mkdir -p config downloads
cp config.docker.yaml config/config.yaml
# Edit config/config.yaml with your credentials
docker-compose up -d
```

Or use the pre-built image:

```bash
docker pull ghcr.io/neo1908/lemmy-image-scraper:latest
```

See [README.Docker.md](README.Docker.md) for detailed Docker deployment instructions.

### From Source

```bash
git clone https://github.com/neo1908/lemmy-image-scraper.git
cd lemmy-image-scraper
go build -o lemmy-scraper ./cmd/scraper
```

## Configuration

Create a `config.yaml` file based on the provided example:

```bash
cp config.example.yaml config.yaml
```

Edit `config.yaml` with your settings:

```yaml
lemmy:
  instance: "lemmy.ml"              # Lemmy instance (without https://)
  username: "your_username"          # Your Lemmy username
  password: "your_password"          # Your Lemmy password
  communities: []                    # Leave empty for hot page, or list communities

storage:
  base_directory: "./downloads"      # Where to save media files

database:
  path: "./lemmy-scraper.db"        # SQLite database path

scraper:
  max_posts_per_run: 100            # Maximum posts to scrape per run
  stop_at_seen_posts: true          # Stop when encountering seen posts
  sort_type: "Hot"                  # Hot, New, TopDay, TopWeek, etc.
  include_images: true              # Download images
  include_videos: true              # Download videos
  include_other_media: true         # Download other media types

run_mode:
  mode: "once"                      # "once" or "continuous"
  interval: "30m"                   # Interval for continuous mode
```

### Configuration Options

#### Lemmy Settings

- **instance**: The Lemmy instance hostname (e.g., `lemmy.ml`, `lemmy.world`)
- **username**: Your Lemmy account username (required for authentication)
- **password**: Your Lemmy account password
- **communities**: List of communities to scrape. Examples:
  - `[]` - Empty list scrapes from the instance hot page
  - `["technology", "linux"]` - Scrapes specific communities
  - `["technology@lemmy.ml", "linux@lemmy.world"]` - Scrapes communities from specific instances

#### Storage Settings

- **base_directory**: Root directory for downloaded media. Files are organized as:
  ```
  downloads/
  ├── technology/
  │   ├── 12345_image.jpg
  │   └── 12346_video.mp4
  └── linux/
      └── 12347_photo.png
  ```

#### Database Settings

- **path**: Location of the SQLite database file for tracking scraped media

#### Scraper Settings

- **max_posts_per_run**: Maximum number of posts to process per community/run
- **stop_at_seen_posts**: Stop scraping when encountering a previously processed post
- **sort_type**: How to sort posts. Options:
  - `Hot` - Currently trending posts
  - `New` - Newest posts first
  - `TopDay` - Top posts from the last day
  - `TopWeek` - Top posts from the last week
  - `TopMonth` - Top posts from the last month
  - `TopYear` - Top posts from the last year
  - `TopAll` - Top posts of all time
- **include_images**: Download image files
- **include_videos**: Download video files
- **include_other_media**: Download other media types

#### Run Mode Settings

- **mode**: Execution mode
  - `once` - Run once and exit (useful for cron jobs)
  - `continuous` - Run continuously on an interval
- **interval**: Time between runs in continuous mode (e.g., `5m`, `1h`, `30m`)

## Usage

### Basic Usage

Run with default config file:

```bash
./lemmy-scraper
```

Specify a custom config file:

```bash
./lemmy-scraper -config /path/to/config.yaml
```

Enable verbose logging:

```bash
./lemmy-scraper -verbose
```

### View Statistics

Display statistics about downloaded media:

```bash
./lemmy-scraper -stats
```

Output example:
```
=== Lemmy Media Scraper Statistics ===

Total media files: 245

By media type:
  image: 198
  video: 42
  other: 5

Top communities:
  technology: 89
  linux: 67
  programming: 45
  ...
```

### Running as a Service

#### Using systemd (Linux)

Create a systemd service file at `/etc/systemd/system/lemmy-scraper.service`:

```ini
[Unit]
Description=Lemmy Media Scraper
After=network.target

[Service]
Type=simple
User=your-user
WorkingDirectory=/path/to/lemmy-image-scraper
ExecStart=/path/to/lemmy-image-scraper/lemmy-scraper -config /path/to/config.yaml
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl enable lemmy-scraper
sudo systemctl start lemmy-scraper
sudo systemctl status lemmy-scraper
```

#### Using cron (One-time mode)

Add to crontab for hourly execution:

```bash
0 * * * * cd /path/to/lemmy-image-scraper && ./lemmy-scraper -config config.yaml
```

## How It Works

1. **Authentication**: Connects to the specified Lemmy instance and authenticates using your credentials
2. **Post Retrieval**: Fetches posts from either:
   - The instance's hot page (if no communities specified)
   - Specific communities (if listed in config)
3. **Media Extraction**: Identifies media URLs in posts:
   - Direct post URLs (e.g., image/video links)
   - Thumbnail URLs
   - Embedded video URLs
4. **Deduplication**: Before downloading:
   - Downloads the file content
   - Computes SHA-256 hash
   - Checks if hash exists in database
   - Skips if already downloaded
5. **Storage**: If new:
   - Saves file to `{base_directory}/{community_name}/{post_id}_{filename}`
   - Records metadata in SQLite database
6. **Metadata**: Stores comprehensive information:
   - Post details (ID, title, URL, score, creation date)
   - Community info (name, ID)
   - Author info (name, ID)
   - File info (path, size, hash, type)
   - Download timestamp

## Database Schema

The SQLite database contains a single table with the following structure:

```sql
CREATE TABLE scraped_media (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    post_id INTEGER NOT NULL,
    post_title TEXT NOT NULL,
    community_name TEXT NOT NULL,
    community_id INTEGER NOT NULL,
    author_name TEXT NOT NULL,
    author_id INTEGER NOT NULL,
    media_url TEXT NOT NULL,
    media_hash TEXT NOT NULL UNIQUE,
    file_name TEXT NOT NULL,
    file_path TEXT NOT NULL,
    file_size INTEGER NOT NULL,
    media_type TEXT NOT NULL,
    post_url TEXT NOT NULL,
    post_score INTEGER NOT NULL,
    post_created DATETIME NOT NULL,
    downloaded_at DATETIME NOT NULL,
    UNIQUE(post_id, media_url)
);
```

## Examples

### Scrape specific communities once

```yaml
lemmy:
  instance: "lemmy.ml"
  username: "myuser"
  password: "mypass"
  communities: ["technology", "linux", "programming"]

run_mode:
  mode: "once"
```

### Continuous monitoring of hot page

```yaml
lemmy:
  instance: "lemmy.world"
  username: "myuser"
  password: "mypass"
  communities: []  # Empty = hot page

run_mode:
  mode: "continuous"
  interval: "15m"
```

### Download only images from specific communities

```yaml
lemmy:
  instance: "lemmy.ml"
  username: "myuser"
  password: "mypass"
  communities: ["pics", "photography"]

scraper:
  include_images: true
  include_videos: false
  include_other_media: false
```

## Troubleshooting

### Authentication fails

- Verify your username and password are correct
- Check if the Lemmy instance is accessible
- Ensure the instance URL doesn't include `https://` or trailing slashes

### No media being downloaded

- Enable verbose logging with `-verbose` flag
- Check if posts actually contain media URLs
- Verify media type filters are enabled
- Try scraping from a community known to have media content

### Database locked errors

- Ensure only one instance of the scraper is running
- Check file permissions on the database file
- If using continuous mode, ensure the database path is accessible

## Project Structure

```
lemmy-image-scraper/
├── cmd/
│   └── scraper/          # Main application entry point
│       └── main.go
├── internal/
│   ├── api/             # Lemmy API client
│   ├── config/          # Configuration management
│   ├── database/        # SQLite database operations
│   ├── downloader/      # Media download and deduplication
│   └── scraper/         # Core scraping logic
├── pkg/
│   └── models/          # Data models
├── config.example.yaml  # Example configuration
├── go.mod
├── go.sum
└── README.md
```

## Contributing

Contributions are welcome! Please feel free to submit issues or pull requests.

## License

MIT License - See LICENSE file for details

## Disclaimer

This tool is for personal use only. Please respect the Lemmy instance's terms of service and be mindful of rate limiting. Always use reasonable scraping intervals to avoid overloading the server.
