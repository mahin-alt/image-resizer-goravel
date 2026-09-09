# image-resizer-goravel

A self-hosted, headless image-processing API. It accepts a source image as a
local file upload and a list of requested output sizes, processes the image
locally with [govips](https://github.com/davidbyttow/govips)/[libvips](https://www.libvips.org/),
converts every output to WebP, stores the files on the local filesystem (or
S3-compatible storage), and returns JSON with URLs to the generated files.
There is no GUI, no authentication, and no rate limiting.

Built on [Goravel](https://www.goravel.dev/) v1.18.

This is the backend half of the [image-resizer-goravel](../) project. The
Vue frontend lives in [`image-resizer-goravel-frontend`](../image-resizer-goravel-frontend)
and talks to this API.

## Contents

- [Architecture](#architecture)
- [Resize modes](#resize-modes)
- [API](#api)
- [Getting started](#getting-started)
  - [Option A: Docker (no Go required)](#option-a-docker-no-go-required)
  - [Option B: native Go + libvips](#option-b-native-go--libvips)
- [Configuration](#configuration)
- [Tests](#tests)
- [Deviations from a from-scratch design](#deviations-from-a-from-scratch-design)

## Architecture

```
Client
  -> POST /api/v1/images                 (app/http/controllers)
  -> validate + persist request/sizes    (app/services)
  -> dispatch ProcessImageRequestJob      (image_processing queue)
  <- 202 { id, status: "pending" }

Worker (image_processing queue)
  -> read uploaded source once           (app/storage)
  -> decode once, autorotate, first-frame-only
  -> for each requested size:
       govips thumbnail (mode-specific)  (internal/imageprocessing)
       -> strip metadata, encode WebP
       -> store file                     (app/storage)
       -> record ImageOutput row
  -> finalize request status

Scheduler (hourly) -> CleanupExpiredImagesJob (image_cleanup queue)
  -> delete files/rows whose expires_at has passed

Client
  -> GET /api/v1/images/{id}
  <- { status, images: [...], errors: [...] }
```

Layering: controllers are thin (validate, delegate, respond). `app/services`
orchestrates persistence + dispatch. `app/jobs` orchestrates the worker-side
pipeline. `internal/imageprocessing` is the only place that imports govips -
everything else depends on its `ImageProcessor` interface. `app/storage` is
the only place that touches the filesystem/S3 disk directly.

### Why one job per request, not one job per size

`ProcessImageRequestJob` decodes the source image exactly once and generates
every requested size from that single decode, rather than dispatching a
separate job per size:

- **Decode once, resize many.** govips/libvips can produce multiple resized
  outputs from one decoded source without re-reading or re-decoding it.
- **libvips is already internally multi-threaded** per operation
  (`VIPS_CONCURRENCY`, see [Worker concurrency](#worker-concurrency)).
  Stacking per-size job parallelism on top would multiply thread contention
  instead of adding throughput.
- **Simpler state and idempotency.** One job means one place that decides the
  request's final status (`completed` / `partially_completed` / `failed`).

A failure on one requested size does not abort the others (see
[Partial failures](#partial-failures)).

## Resize modes

| API `mode`  | Behavior                                                              | govips mapping                                   |
|-------------|------------------------------------------------------------------------|---------------------------------------------------|
| `contain` / `fit` (alias) | Fit entirely inside width x height. No crop, no stretch. Result may be smaller than the box on one axis. | thumbnail, `InterestingNone`, `SizeBoth`/`SizeDown` |
| `cover`     | Fill width x height exactly, cropping overflow, biased toward the visually "interesting" region. | thumbnail, `InterestingAttention` |
| `crop`      | Fill width x height exactly, cropping overflow from a fixed centre crop (no saliency detection). | thumbnail, `InterestingCentre` |
| `fill`      | Exact width x height. The only mode that stretches/distorts the aspect ratio. | thumbnail, `SizeForce` |

Default mode when omitted: `contain`.

### Upscaling

`ALLOW_UPSCALE` sets whether outputs may be enlarged beyond the source
image's dimensions (`true` unless changed). It applies to every request -
there is no per-request override.

### Animated sources

Only the first frame/page of an animated GIF or WebP source is processed.
Output is always a single static WebP.

### Metadata and orientation

Every output has EXIF/XMP/IPTC metadata stripped at encode time, so GPS
coordinates, camera info, timestamps, etc. never end up in a generated file.
Source EXIF orientation is applied (autorotate) before resizing.

### WebP quality

`IMAGE_DEFAULT_QUALITY` (default 80) is used unless a request supplies a
per-size `"quality"` override; any override is clamped to
`[IMAGE_QUALITY_MIN, IMAGE_QUALITY_MAX]` (defaults 0-100).

### Partial failures

If some requested sizes succeed and others fail, the request's final status
is `partially_completed`; successful outputs are still returned in `images`,
and failed sizes are reported in `errors`. The request only becomes `failed`
if none of its sizes succeeded.

## API

### `POST /api/v1/images`

`Content-Type: multipart/form-data`, with an `image` file field plus a `data`
text field holding `{ "sizes": [...] }` as a JSON string:

```bash
curl -X POST http://localhost:3000/api/v1/images \
  -F 'data={"sizes":[{"width":400,"height":400,"mode":"cover"},{"width":800,"height":600},{"width":1200,"height":900,"quality":90}]}' \
  -F 'image=@photo.jpg'
```

Uploaded file size is capped by `MAX_IMAGE_FILE_SIZE`. The uploaded original
is deleted once processing finishes - only the generated WebP outputs are
retained. There's no URL-based input: the server never makes an outbound
request to fetch an image, so there's no SSRF surface.

Response `202`:

```json
{ "id": 1, "status": "pending" }
```

Validation errors return `422` with `{"errors": {...}}`. `sizes` accepts at
most `MAX_SIZES_PER_REQUEST` entries; `mode` must be one of `contain`, `fit`,
`cover`, `crop`, `fill`.

### `GET /api/v1/images/{id}`

```json
{
  "status": "completed",
  "id": 1,
  "input_type": "upload",
  "source_image": { "width": 550, "height": 368 },
  "images": [
    {
      "url": "http://localhost:3000/images/1/1.webp",
      "download_url": "http://localhost:3000/api/v1/images/1/outputs/1/download",
      "width": 400,
      "height": 400,
      "mode": "cover",
      "format": "webp",
      "file_size": 34521
    }
  ],
  "errors": [],
  "created_at": "...",
  "completed_at": "..."
}
```

`url` renders inline in a browser; `download_url` sends
`Content-Disposition: attachment` instead. `status` is one of `pending`,
`processing`, `completed`, `partially_completed`, `failed`.

A ready-to-import request collection is at
[`postman/image-resizer-goravel.postman_collection.json`](postman/image-resizer-goravel.postman_collection.json)
(resize modes, upscaling, quality, and error-case examples).

### Retention and cleanup

Every `ImageOutput` gets an `expires_at` computed at creation time from
`IMAGE_RETENTION_SECONDS` (default 86400, i.e. 24h). `CleanupExpiredImagesJob`
runs every minute on the dedicated `CLEANUP_QUEUE`, so a backlog of heavy
processing work never delays cleanup.

### Supported source formats

With a standard `libvips-dev` install you get JPEG, PNG, WebP, GIF, TIFF,
BMP, and SVG. AVIF, HEIC/HEIF, JPEG 2000, and JPEG XL each require their own
optional codec libraries (`libheif`, `libopenjp2`, `libjxl`) to be present at
libvips' build time. Output is always WebP regardless of source format.

## Getting started

There are two ways to run this locally, depending on whether you already
have a Go toolchain set up.

### Option A: Docker (no Go required)

Everything (API, MySQL, MinIO object storage) runs in containers - you only
need [Docker](https://docs.docker.com/get-docker/) and
[Docker Compose](https://docs.docker.com/compose/install/) (bundled with
Docker Desktop; on Linux install the `docker-compose-plugin` package).

1. **Install Docker**, if you haven't already:
   - macOS/Windows: install [Docker Desktop](https://www.docker.com/products/docker-desktop/).
   - Linux: follow the [Docker Engine install guide](https://docs.docker.com/engine/install/) for your distro, then `sudo usermod -aG docker $USER` and re-login so you can run `docker` without `sudo`.
   - Verify: `docker --version` and `docker compose version` both print a version.

2. **Clone the repo and enter it:**
   ```bash
   git clone <your-fork-or-repo-url>.git
   cd image-resizer-goravel-backend
   ```

3. **Create your `.env`:**
   ```bash
   cp .env.example .env
   ```
   The defaults already match the `docker-compose.yml` services (MySQL host
   `mysql`, MinIO for S3-compatible storage), so no edits are required to get
   running. Generate an `APP_KEY` if you want one (optional for local dev):
   ```bash
   openssl rand -base64 32
   ```
   and paste the result as `APP_KEY=` in `.env`.

4. **Build and start everything:**
   ```bash
   docker compose up -d --build
   ```
   This starts three services: `goravel` (the API, port `3000`), `mysql`
   (port `3306`), and `minio` (ports `9000` API / `9001` console,
   credentials `minioadmin` / `minioadmin`) plus a one-shot `minio-init`
   container that creates the `images` bucket.

5. **Run database migrations** (the app container doesn't do this
   automatically):
   ```bash
   docker compose exec goravel go run . artisan migrate
   ```
   > If your image doesn't have the Go toolchain baked in for this exec
   > step, run migrations by exec-ing into the container with the compiled
   > binary instead: `docker compose exec goravel /www/main artisan migrate`.

6. **Verify it's up:**
   ```bash
   curl -X POST http://localhost:3000/api/v1/images \
     -F 'data={"sizes":[{"width":200,"height":200}]}' \
     -F 'image=@/path/to/any/image.jpg'
   ```
   You should get back `{"id":1,"status":"pending"}`. Poll
   `GET http://localhost:3000/api/v1/images/1` until `status` becomes
   `completed`.

7. **Stop everything:**
   ```bash
   docker compose down        # stop containers, keep data
   docker compose down -v     # stop and wipe MySQL/MinIO volumes too
   ```

If port `3306` (MySQL) or `9000`/`9001` (MinIO) are already taken on your
machine by another service, edit the `ports:` mappings in
`docker-compose.yml` (left side only, e.g. `"3307:3306"`) before running
`docker compose up`.

### Option B: native Go + libvips

Choose this if you're already comfortable with Go and want faster
edit/rebuild cycles than a container rebuild gives you.

**Prerequisites:**
- [Go 1.25+](https://go.dev/doc/install)
- libvips 8.10+ (8.14+ recommended), via system package:
  ```bash
  # Debian/Ubuntu
  sudo apt-get install -y libvips-dev pkg-config

  # macOS (Homebrew)
  brew install vips pkg-config
  ```
  Check with `vips --version`.
- A MySQL 8+ database - either run just the `mysql` service from Docker Compose:
  ```bash
  docker compose up -d mysql
  ```
  or point `DB_HOST`/`DB_PORT`/etc. in `.env` at any MySQL 8+ server you
  already have.

**Setup:**
```bash
cp .env.example .env
go mod tidy      # fetches govips and computes go.sum - requires libvips-dev headers above
go run . artisan migrate
```

**Run:**
```bash
go run .   # HTTP server + both queue workers + scheduler, all in one process
```

There is no separate `queue:work` process for this project - the processing
and cleanup queue workers start automatically as part of the application
process (see `bootstrap/queue_runners.go`).

### Why a database is required

`POST /api/v1/images` returns immediately (`status: pending`) and a worker
processes the request later, so `GET /api/v1/images/{id}` needs a durable
place to read status/results from in between - and to survive a worker
restart. The queue itself is database-backed too
(`QUEUE_CONNECTION=database`, using the `jobs`/`failed_jobs` tables): the
only alternative queue driver is `sync`, which runs jobs inline in the HTTP
request and defeats the "don't process during the request" requirement.
MySQL was chosen here only because that's what this deployment has
available; Postgres or SQLite would work equally well - swap
`config/database.go` and the `goravel/mysql` import for the corresponding
Goravel database driver package if you'd rather use one of those.

## Configuration

All limits are centralized in `config/image.go` and read through
`app/support/imageconfig`. See [`.env.example`](.env.example) for the full,
commented list.

| Variable | Purpose |
|---|---|
| `APP_KEY` | Application encryption key (generate with `openssl rand -base64 32`) |
| `APP_PORT` | HTTP port (default `3000`) |
| `DB_*` | MySQL connection |
| `IMAGE_DEFAULT_QUALITY`, `IMAGE_QUALITY_MIN`, `IMAGE_QUALITY_MAX` | WebP quality default and client-override bounds |
| `IMAGE_RETENTION_SECONDS` | Output lifetime in seconds, applied at creation time |
| `ALLOW_UPSCALE` | Whether outputs may be enlarged beyond the source |
| `MAX_IMAGE_FILE_SIZE` | Uploaded source file size cap (bytes) |
| `MAX_IMAGE_WIDTH`, `MAX_IMAGE_HEIGHT` | Source dimension caps |
| `MAX_TOTAL_OUTPUT_PIXELS` | Per-requested-size pixel cap (decompression-bomb guard) |
| `MAX_SIZES_PER_REQUEST` | Sizes allowed per request |
| `MAX_CONCURRENT_IMAGE_JOBS`, `VIPS_CONCURRENCY` | Concurrency, see below |
| `STORAGE_PATH` | Local disk root for generated files (the "images" disk) |
| `PROCESSING_QUEUE`, `CLEANUP_QUEUE` | Queue names |
| `QUEUE_CONNECTION` | `database` (real async) or `sync` (inline, tests only) |
| `AWS_*` | S3-compatible storage (MinIO locally, real S3/MinIO in prod) - see `config/filesystems.go` |

### Worker concurrency

Two independent levels of parallelism are in play: how many
`ProcessImageRequestJob`s run at once (`MAX_CONCURRENT_IMAGE_JOBS`), and how
many threads libvips itself uses per operation (`VIPS_CONCURRENCY`). Keep
roughly `MAX_CONCURRENT_IMAGE_JOBS x VIPS_CONCURRENCY <= CPU cores` available
to the worker process. The defaults (4 x 2 = 8) target an 8-core worker;
turn one down before the other on smaller machines.

## Tests

```bash
go test ./...
```

`app/support/imageconfig` has unit tests that run without libvips or a real
database. Tests exercising `internal/imageprocessing` (actual resizing) and
the full job/API flow require libvips installed and a configured MySQL
database (`DB_*` in `.env`).

## Deviations from a from-scratch design

- Model IDs are Goravel's standard auto-incrementing `uint` (`orm.Model`)
  rather than UUIDs, to stay idiomatic with the framework's generators and
  conventions.
