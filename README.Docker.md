# Docker Deployment Guide

This guide explains how to run the Lemmy Image Scraper using Docker and Docker Compose.

## Quick Start

1. **Create the config directory and add your configuration:**

```bash
mkdir -p config downloads
cp config.docker.yaml config/config.yaml
```

2. **Edit the configuration file:**

```bash
nano config/config.yaml
```

At minimum, update these fields:
- `lemmy.instance`: Your Lemmy instance (e.g., "lemmy.ml")
- `lemmy.username`: Your Lemmy username
- `lemmy.password`: Your Lemmy password
- `lemmy.communities`: List of communities to scrape (or leave empty for instance hot page)

3. **Run with Docker Compose:**

```bash
docker-compose up -d
```

4. **View logs:**

```bash
docker-compose logs -f
```

5. **Access the web UI (if enabled):**

Open your browser to `http://localhost:8080`

## Building the Docker Image

### Using Docker Compose (Recommended)

```bash
docker-compose build
docker-compose up -d
```

### Using Docker directly

```bash
# Build the image
docker build -t lemmy-scraper:latest .

# Run the container
docker run -d \
  --name lemmy-scraper \
  -v $(pwd)/config:/config:ro \
  -v $(pwd)/downloads:/downloads:rw \
  -p 8080:8080 \
  lemmy-scraper:latest
```

## Volume Mounts

The Docker setup uses two volumes:

### 1. Config Volume (`/config`)

- **Purpose:** Stores the configuration file
- **Mount:** `./config:/config:ro` (read-only)
- **Contents:** `config.yaml`

This volume is mounted as read-only for security.

### 2. Downloads Volume (`/downloads`)

- **Purpose:** Stores downloaded media and database
- **Mount:** `./downloads:/downloads:rw` (read-write)
- **Contents:**
  - `lemmy-scraper.db` - SQLite database
  - `{community_name}/` - Community directories with downloaded media

This volume is mounted as read-write to allow the scraper to save files.

## Configuration for Docker

The `config.docker.yaml` file is pre-configured with Docker-appropriate defaults:

- **Storage path:** `/downloads` (maps to mounted volume)
- **Database path:** `/downloads/lemmy-scraper.db` (persists with downloaded media)
- **Web server host:** `0.0.0.0` (allows access from outside container)
- **Web server port:** `8080` (matches exposed port)
- **Run mode:** `continuous` (keeps container running)

## Docker Compose Configuration

The `docker-compose.yml` includes:

- **Automatic restart:** `restart: unless-stopped`
- **Health checks:** Monitors process health
- **Port mapping:** `8080:8080` for web UI
- **Logging:** Rotated JSON logs (max 10MB × 3 files)
- **Resource limits:** Optional CPU and memory limits (commented out)

### Customizing docker-compose.yml

#### Change the web UI port

```yaml
ports:
  - "3000:8080"  # Access at http://localhost:3000
```

#### Enable resource limits

Uncomment the `deploy` section and adjust values:

```yaml
deploy:
  resources:
    limits:
      cpus: '2'
      memory: 1G
```

#### Use named volumes instead of bind mounts

Uncomment the volumes section at the bottom and update the service:

```yaml
volumes:
  - config:/config:ro
  - downloads:/downloads:rw
```

## Running Different Modes

### One-time scrape

Modify `config.yaml`:

```yaml
run_mode:
  mode: "once"
```

Run:

```bash
docker-compose run --rm lemmy-scraper
```

### Continuous scraping (default)

```yaml
run_mode:
  mode: "continuous"
  interval: "30m"
```

### Enable verbose logging

```bash
docker-compose run --rm lemmy-scraper -verbose
```

### View statistics

```bash
docker-compose exec lemmy-scraper /app/lemmy-scraper -stats -config /config/config.yaml
```

## Troubleshooting

### Permission issues with volumes

If you encounter permission errors, ensure the volumes are writable:

```bash
chmod -R 777 downloads  # Or set appropriate user permissions
```

The container runs as user `scraper` (UID 1000) by default.

### Container keeps restarting

Check logs for errors:

```bash
docker-compose logs lemmy-scraper
```

Common issues:
- Missing or invalid config file
- Invalid credentials
- Network connectivity to Lemmy instance

### Cannot access web UI

1. Verify the web server is enabled in `config.yaml`:
   ```yaml
   web_server:
     enabled: true
     host: "0.0.0.0"
     port: 8080
   ```

2. Verify the port is mapped in `docker-compose.yml`:
   ```yaml
   ports:
     - "8080:8080"
   ```

3. Check if the container is running:
   ```bash
   docker-compose ps
   ```

## Updating

To update to the latest version:

```bash
# Pull latest code
git pull

# Rebuild and restart
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

## Backup

Your data is stored in the `downloads` volume. To backup:

```bash
# Stop the container
docker-compose down

# Backup the downloads directory
tar -czf lemmy-scraper-backup-$(date +%Y%m%d).tar.gz downloads/

# Restart
docker-compose up -d
```

## Environment Variables

Optional environment variables you can set in `docker-compose.yml`:

- `TZ`: Timezone (default: `UTC`)
- `CONFIG_PATH`: Path to config file (default: `/config/config.yaml`)

Example:

```yaml
environment:
  - TZ=America/New_York
  - CONFIG_PATH=/config/custom-config.yaml
```

## Security Considerations

1. **Config volume is read-only:** Prevents accidental modification of credentials
2. **Non-root user:** Container runs as UID 1000 (scraper user)
3. **Network isolation:** Only exposes port 8080 (web UI)
4. **Credentials:** Store config.yaml securely and never commit it to version control

## Production Deployment

For production deployments:

1. **Use Docker secrets** for sensitive data:
   ```yaml
   secrets:
     lemmy_password:
       file: ./secrets/lemmy_password.txt
   ```

2. **Enable resource limits** to prevent resource exhaustion

3. **Use a reverse proxy** (nginx, Traefik) for HTTPS and authentication

4. **Set up monitoring** with health check endpoints

5. **Configure log rotation** to prevent disk space issues

6. **Regular backups** of the downloads volume
