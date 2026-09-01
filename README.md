# image-resizer-goravel

A self-hosted, headless image-processing API. It accepts a source image URL
and a list of requested output sizes, downloads and processes the image
locally with [govips](https://github.com/davidbyttow/govips)/[libvips](https://www.libvips.org/),
converts every output to WebP, stores the files on the local filesystem, and
returns JSON with URLs to the generated files. There is no GUI, no
authentication, and no rate limiting - see "Known limitations" below.

Built on [Goravel](https://www.goravel.dev/) v1.18.

## Architecture

```
Client
  -> POST /api/v1/images                 (app/http/controllers)
  -> validate + persist request/sizes    (app/services)
  -> dispatch ProcessImageRequestJob      (image_processing queue)
  <- 202 { id, status: "pending" }

Worker (image_processing queue)
  -> download source once                (internal/download)
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
the only place that touches the filesystem disk directly.

### Why one job per request, not one job per size

`ProcessImageRequestJob` decodes the source image exactly once and generates
every requested size from that single decode, rather than dispatching a
separate job per size. Reasoning:

- **Decode once, resize many.** govips/libvips can produce multiple resized
  outputs from one decoded source without re-downloading or re-decoding.
  Splitting into per-size jobs would mean either re-downloading/re-decoding
  per job, or shipping decoded pixel data between jobs - both far more
  expensive than keeping the source open for one job's lifetime.
- **libvips is already internally multi-threaded** per operation
  (`VIPS_CONCURRENCY`, see "Worker concurrency" below). Stacking another
  layer of per-size job parallelism on top would multiply thread contention
  instead of adding throughput.
- **Simpler state and idempotency.** One job means one place that decides the
  request's final status (`completed` / `partially_completed` / `failed`).
  Per-size jobs would need a second layer of fan-out/fan-in bookkeeping to
  reconstruct the same thing.

A failure on one requested size does not abort the others: it's recorded
against that size and the job keeps going (see "Partial failures" below).

## Resize modes

| API `mode`  | Behavior                                                              | govips mapping                                   |
|-------------|------------------------------------------------------------------------|---------------------------------------------------|
| `contain` / `fit` (alias) | Fit entirely inside width x height. No crop, no stretch. Result may be smaller than the box on one axis. | thumbnail, `InterestingNone`, `SizeBoth`/`SizeDown` |
| `cover`     | Fill width x height exactly, cropping overflow, biased toward the visually "interesting" region. | thumbnail, `InterestingAttention` |
| `crop`      | Fill width x height exactly, cropping overflow from a fixed centre crop (no saliency detection). | thumbnail, `InterestingCentre` |
| `fill`      | Exact width x height. The only mode that stretches/distorts the aspect ratio. | thumbnail, `SizeForce` |

Default mode when omitted: `contain`.

## Upscaling

`ALLOW_UPSCALE` sets the server-wide default (`false` unless changed). Each
requested size may include `"allow_upscale": true|false` to override it for
that size only. When upscaling is disabled, `contain`/`cover`/`crop` use
libvips' `SizeDown` (never enlarge); `fill` clamps the requested box to the
source's own dimensions per axis before force-resizing, so it still won't
enlarge beyond the source even though it stretches.

## Animated sources

Only the first frame/page of an animated GIF or WebP source is processed
(`n=1` on load). Output is always a single static WebP - animated WebP output
is not supported.

## Metadata and orientation

Every output has EXIF/XMP/IPTC metadata stripped at encode time
(`WebpExportParams.StripMetadata`), so GPS coordinates, camera info,
timestamps, etc. never end up in a generated file. Source EXIF orientation is
applied (`autorotate`) before resizing, so outputs are never rotated
incorrectly.

## WebP quality

`IMAGE_DEFAULT_QUALITY` (default 80) is used unless a request supplies a
per-size `"quality"` override; any override is clamped to
`[IMAGE_QUALITY_MIN, IMAGE_QUALITY_MAX]` (defaults 40-95). Valid libvips/webp
quality range is 1-100.

## Partial failures

If some requested sizes succeed and others fail, the request's final status
is `partially_completed`; the successful outputs remain available and are
returned in `images`, and the failed sizes are reported in `errors`. The
request only becomes `failed` if none of its sizes succeeded.

## API

### `POST /api/v1/images`

```json
{
  "image_url": "https://example.com/photo.jpg",
  "sizes": [
    { "width": 400, "height": 400, "mode": "cover" },
    { "width": 800, "height": 600 },
    { "width": 1200, "height": 900, "allow_upscale": true, "quality": 90 }
  ]
}
```

Response `202`:

```json
{ "id": 1, "status": "pending" }
```

Validation errors return `422` with `{"errors": {...}}` (Goravel's standard
validation error shape). `sizes` accepts at most `MAX_SIZES_PER_REQUEST`
entries; `mode` must be one of `contain`, `fit`, `cover`, `crop`, `fill`.

### `GET /api/v1/images/{id}`

```json
{
  "id": 1,
  "status": "completed",
  "source_url": "https://example.com/photo.jpg",
  "created_at": "...",
  "completed_at": "...",
  "images": [
    { "width": 400, "height": 400, "mode": "cover", "format": "webp", "url": "http://localhost:3000/images/1/1.webp", "file_size": 34521 }
  ],
  "errors": []
}
```

`status` is one of `pending`, `processing`, `completed`, `partially_completed`,
`failed`. `errors` (only present when non-empty) lists sizes that failed with
a human-readable message.

## Retention and cleanup

Every `ImageOutput` gets an `expires_at` computed at creation time from
`IMAGE_RETENTION_HOURS` (default 24) - changing that env var later does not
change the lifetime of images that already exist. `CleanupExpiredImagesJob`
runs hourly (see `bootstrap/schedule.go`) on the dedicated `CLEANUP_QUEUE`, so
a backlog of heavy processing work can never delay cleanup. It is idempotent
and safe to run repeatedly or after a crash: a missing file or an
already-gone row is not an error.

## Supported source formats

Actual support depends on how the installed libvips was built. With a
standard `apt install libvips-dev` on a recent Debian/Ubuntu you should get
JPEG, PNG, WebP, GIF, TIFF, BMP, and SVG. AVIF, HEIC/HEIF, JPEG 2000, and
JPEG XL each require their own optional codec libraries
(`libheif`, `libopenjp2`, `libjxl`) to be present at libvips' build time -
verify with `vips -l | grep -i <format>` on your target machine before
relying on one of these. Output is always WebP regardless of source format.

## Configuration

All limits are centralized in `config/image.go` and read through
`app/support/imageconfig` - no other package reads these environment
variables directly. See `.env.example` for the full list with comments.

| Variable | Purpose |
|---|---|
| `IMAGE_DEFAULT_QUALITY`, `IMAGE_QUALITY_MIN`, `IMAGE_QUALITY_MAX` | WebP quality default and client-override bounds |
| `IMAGE_RETENTION_HOURS` | Output lifetime, applied at creation time |
| `ALLOW_UPSCALE` | Global upscale default (overridable per size) |
| `MAX_IMAGE_FILE_SIZE`, `MAX_SOURCE_DOWNLOAD_SIZE` | Download size caps |
| `MAX_IMAGE_WIDTH`, `MAX_IMAGE_HEIGHT` | Source dimension caps |
| `MAX_TOTAL_OUTPUT_PIXELS` | Per-requested-size pixel cap (decompression-bomb guard) |
| `MAX_SIZES_PER_REQUEST` | Sizes allowed per request |
| `MAX_SOURCE_DOWNLOAD_TIME`, `MAX_CONNECTION_TIMEOUT`, `MAX_REDIRECTS` | Download behavior |
| `MAX_CONCURRENT_IMAGE_JOBS`, `VIPS_CONCURRENCY` | Concurrency (see below) |
| `STORAGE_PATH` | Local disk root for generated files |
| `PROCESSING_QUEUE`, `CLEANUP_QUEUE` | Queue names |
| `QUEUE_CONNECTION` | `database` (real async) or `sync` (inline, tests only) |

## Worker concurrency

Two independent levels of parallelism are in play: how many
`ProcessImageRequestJob`s run at once (`MAX_CONCURRENT_IMAGE_JOBS`, a Goravel
worker concurrency setting - see `bootstrap/queue_runners.go`), and how many
threads libvips itself uses per operation (`VIPS_CONCURRENCY`). Don't
maximize both independently: keep roughly
`MAX_CONCURRENT_IMAGE_JOBS x VIPS_CONCURRENCY <= CPU cores` available to the
worker process. The defaults (4 x 2 = 8) target an 8-core worker; turn one
down before the other on smaller machines. This favors stable throughput over
maximum theoretical parallelism.

## Known limitation: SSRF

**This deployment does not restrict which hosts `image_url` may resolve to.**
Per an explicit product decision, no SSRF protection is implemented: the
server will fetch `http://127.0.0.1`, RFC1918 addresses, the cloud metadata
endpoint (`169.254.169.254`), etc. if given a URL that resolves there. Do not
expose this service to untrusted clients or the public internet without
adding host/IP allow-listing first - see the comment at the top of
`internal/download/downloader.go` for what that would need to cover
(resolved-IP checks, dialing the resolved IP directly to avoid DNS-rebinding,
re-validating on every redirect hop).

## Local development

### System dependencies

```bash
sudo apt-get install -y libvips-dev pkg-config
```

libvips 8.10+ is required by govips; 8.14+ is recommended for the dynamic
threadpool behavior referenced above. Check your version with `vips --version`.

A MySQL database is also required (see "Why a database is required" below).
The quickest way to get one locally:

```bash
docker compose up -d mysql
```

or point `DB_HOST`/`DB_PORT`/etc. in `.env` at any MySQL 8+ server you already have.

### Setup

```bash
cp .env.example .env
go mod tidy      # fetches govips and computes go.sum - requires the libvips-dev headers above
go run . artisan migrate
```

### Why a database is required

This isn't optional given the architecture: `POST /api/v1/images` returns
immediately (`status: pending`) and a worker processes the request later, so
`GET /api/v1/images/{id}` needs a durable place to read status/results from
in between - and to survive a worker restart. The queue itself is
database-backed too (`QUEUE_CONNECTION=database`, using the `jobs`/
`failed_jobs` tables): the only alternative queue driver is `sync`, which
would run jobs inline in the HTTP request and defeat the "don't process
during the request" requirement. Retention/cleanup (`expires_at` per output)
and retry idempotency also depend on persisted rows to check against. MySQL
was chosen here only because that's what this deployment has available;
Postgres or SQLite would work equally well - swap `config/database.go` and
the `goravel/mysql` import for the corresponding Goravel database driver
package if you'd rather use one of those.

### Running

```bash
go run .                                   # HTTP server + both queue workers + scheduler (see bootstrap/app.go)
```

The processing and cleanup queue workers are started automatically as part
of the application process (see `bootstrap/queue_runners.go`) - there is no
separate `queue:work` process to run for this project.

### Tests

```bash
go test ./...
```

`internal/download` and `app/support/imageconfig` have unit tests that run
without libvips or a real database. Tests exercising `internal/imageprocessing`
(actual resizing) and the full job/API flow require libvips installed and a
configured MySQL database (`DB_*` in `.env`), per the framework's
`tests.TestCase` bootstrap convention.

## Deviations from a from-scratch design

- Model IDs are Goravel's standard auto-incrementing `uint` (`orm.Model`)
  rather than UUIDs, to stay idiomatic with the framework's generators and
  conventions.
