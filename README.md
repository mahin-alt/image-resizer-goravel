# image-resizer-goravel

A self-hosted, headless image-processing API. It accepts a source image as a
local file upload and a list of requested output sizes, processes the image
with [govips](https://github.com/davidbyttow/govips)/[libvips](https://www.libvips.org/),
converts every output to WebP, uploads the generated files to S3-compatible
object storage, and returns JSON with URLs to the generated files. There is
no GUI, no authentication, and no rate limiting.

Built on [Goravel](https://www.goravel.dev/) v1.18 (Go 1.25).

This is the backend half of the `image-resizer-goravel` project. A Vue
frontend project (`image-resizer-goravel-frontend`) is intended to talk to
this API; a set of static HTML/JS demo frontends is also bundled under
[`public/frontend`](public/frontend).

## Contents

- [Tech stack](#tech-stack)
- [Architecture](#architecture)
- [Resize modes](#resize-modes)
- [API endpoints](#api-endpoints)
- [Database schema](#database-schema)
- [Getting started](#getting-started)
- [Environment variables](#environment-variables)
- [Running tests](#running-tests)
- [Deployment / Docker notes](#deployment--docker-notes)

## Tech stack

- **Language / framework:** Go 1.25, [Goravel](https://www.goravel.dev/) v1.18 (a Laravel-inspired Go framework)
- **HTTP:** `goravel/gin` (Gin-based HTTP driver)
- **Image processing:** `davidbyttow/govips` v2 bindings over libvips
- **Database:** MySQL 8 by default (`goravel/mysql`); `goravel/postgres` is also wired up and selectable via `DB_CONNECTION`
- **Object storage:** S3-compatible storage via `goravel/s3` (a locally patched fork in `third_party/goravel-s3`, needed for path-style endpoint support against MinIO); MinIO is used in local/dev via Docker Compose
- **Queue:** Goravel's database-backed queue driver (`jobs`/`failed_jobs` tables) — used only for a still-registered but currently idle cleanup queue (see [Architecture](#architecture))
- **AI facade:** `goravel/openai` is wired up as a facade (`app/facades/ai.go`, `config/ai.go`) but is not used by any current endpoint
- **gRPC:** scaffolding exists (`config/grpc.go`, `routes/grpc.go`, `app/facades/grpc.go`) but no service is registered
- **Testing:** Go's standard `testing` package + `stretchr/testify` (suites)

## Architecture

Processing is **synchronous**: a client's `POST /api/v1/images` call blocks
until every requested size has been generated (or failed) and the final
result is returned directly in the same response. There is no
pending/polling workflow for a fresh request.

```
Client
  -> POST /api/v1/images                       (app/http/controllers)
  -> validate + persist request/sizes           (app/services)
  -> ProcessImageRequestSync (inline, in-request)
       -> decode source once, autorotate, first-frame-only
       -> for each requested size:
            govips thumbnail (mode-specific)     (internal/imageprocessing)
            -> strip metadata, encode WebP
            -> upload to S3-compatible "s3" disk (app/storage)
            -> record ImageOutput row
       -> finalize request status
  <- 200 { status, id, images: [...], errors: [...] }

Scheduler (every minute) -> services.SweepStaleProcessingRequests
  -> marks a request "failed" if it's been stuck at "processing" without a
     worker heartbeat for longer than STALE_PROCESSING_TIMEOUT_SECONDS
     (recovers from a process crash/kill mid-request)
```

Layering: controllers are thin (validate, delegate, respond).
`app/services` orchestrates persistence + synchronous processing.
`app/jobs` holds the actual per-request processing pipeline (invoked
directly, not dispatched to a queue, for the current image-processing
flow). `internal/imageprocessing` is the only place that imports govips —
everything else depends on its `ImageProcessor` interface. `app/storage` is
the only place that touches the filesystem/S3 disk directly.

Concurrency across simultaneous requests is bounded by an in-process
semaphore (`MAX_CONCURRENT_IMAGE_JOBS`) inside `services.ProcessImageRequestSync`,
not by a queue worker pool.

### Legacy / currently-idle pieces

A few pieces from an earlier, asynchronous design are still present in the
code but not active in the current request flow:

- **`CleanupExpiredImagesJob`** (`app/jobs/cleanup_expired_images_job.go`)
  and its dedicated `CLEANUP_QUEUE` worker (`bootstrap/queue_runners.go`)
  are registered and running, but nothing schedules or dispatches work to
  them — generated outputs are treated as permanent now. They're kept in
  place in case retention-based cleanup is reintroduced.
- **URL-based input** (`ImageProcessingRequest.InputType`/`InputTypeURL`,
  `SourceURL`) exists at the model layer, but the current
  `POST /api/v1/images` request/validation only accepts a multipart file
  upload (`InputTypeUpload`) — there is no way to submit an `image_url`
  through the current API surface.
- `ImageOutput.ExpiresAt` is still computed and stored, but nothing deletes
  outputs once they expire (see the idle cleanup job above).

## Resize modes

| API `mode` | Behavior | govips mapping |
|---|---|---|
| `contain` / `fit` (alias) | Fit entirely inside width x height. No crop, no stretch. Result may be smaller than the box on one axis. | thumbnail, `InterestingNone`, `SizeBoth`/`SizeDown` |
| `cover` | Fill width x height exactly, cropping overflow, biased toward the visually "interesting" region. | thumbnail, `InterestingAttention` |
| `crop` | Fill width x height exactly, cropping overflow from a fixed centre crop (no saliency detection). | thumbnail, `InterestingCentre` |
| `fill` | Exact width x height. The only mode that stretches/distorts the aspect ratio. | thumbnail, `SizeForce` |

Default mode when omitted: `contain`.

- **Upscaling:** `ALLOW_UPSCALE` sets whether outputs may be enlarged beyond
  the source image's dimensions (default `true`). It applies process-wide;
  there is no per-request override.
- **Animated sources:** only the first frame/page of an animated GIF or
  WebP source is processed. Output is always a single static WebP.
- **Metadata/orientation:** EXIF/XMP/IPTC metadata is stripped at encode
  time; source EXIF orientation is applied (autorotate) before resizing.
- **Quality:** `IMAGE_DEFAULT_QUALITY` (default 80) is used unless a
  request supplies a per-size `"quality"` override; overrides are clamped
  to `[IMAGE_QUALITY_MIN, IMAGE_QUALITY_MAX]` (defaults 0-100).
- **Partial failures:** if some requested sizes succeed and others fail,
  the request's final status is `partially_completed`; successful outputs
  are still returned in `images`, failed ones in `errors`. The request only
  becomes `failed` if none of its sizes succeeded.
- **Duplicate sizes rejected:** a request asking for the same width x
  height more than once is rejected outright with a `422`.
- **Supported source formats:** with a standard `libvips-dev` install:
  JPEG, PNG, WebP, GIF, TIFF, BMP, SVG. AVIF/HEIC/JPEG 2000/JPEG XL each
  need their own optional codec libraries present at libvips' build time.
  Output is always WebP regardless of source format.

## API endpoints

Base path: `/api/v1` (registered in `routes/api.go`, handled by
`app/http/controllers/image_controller.go`). No authentication is enforced
on any endpoint.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/v1/images` | Upload a source image + requested sizes; processes synchronously and returns the final result. |
| `GET` | `/api/v1/images/{id}` | Look up a previously processed request by id. |
| `POST` | `/api/v1/images/{id}/retry` | Re-run processing for a request currently `failed` or `partially_completed`; blocks and returns the final result. |

There is also a non-versioned `GET /users` route (`routes/web.go`,
`app/http/controllers/user_controller.go`) that returns a static
`{"Hello": "Goravel"}` — a leftover framework scaffold route, not part of
the image API. `GET /` renders the default Goravel welcome page, and
`public/` is served statically (the bundled demo frontends live under
`public/frontend`).

### `POST /api/v1/images`

`Content-Type: multipart/form-data`, with an `image` file field plus a
`data` text field holding `{ "sizes": [...] }` as a JSON string:

```bash
curl -X POST http://localhost:3000/api/v1/images \
  -F 'data={"sizes":[{"width":400,"height":400,"mode":"cover"},{"width":800,"height":600},{"width":1200,"height":900,"quality":90}]}' \
  -F 'image=@photo.jpg'
```

- Uploaded file size is capped by `MAX_IMAGE_FILE_SIZE`.
- The uploaded original is deleted once processing finishes — only
  generated WebP outputs are retained.
- There's no URL-based input in the current API — see
  [Legacy / currently-idle pieces](#legacy--currently-idle-pieces).
- `sizes` accepts at most `MAX_SIZES_PER_REQUEST` entries; `mode` must be
  one of `contain`, `fit`, `cover`, `crop`, `fill`.
- Validation errors return `422` with `{"errors": {...}}`.
- A successful call returns `200` (not `202` — this is synchronous) with
  the full result body, same shape as `GET /api/v1/images/{id}` below.

### `GET /api/v1/images/{id}`

```json
{
  "status": "completed",
  "id": 1,
  "input_type": "upload",
  "source_image": { "width": 550, "height": 368 },
  "images": [
    {
      "url": "http://localhost:9000/images/media/400x400/<hash>.webp",
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

`status` is one of `pending`, `processing`, `completed`,
`partially_completed`, `failed` (`expired` also exists as a model-level
status but nothing currently sets it, since output expiry cleanup is idle).
Each output's `url` points directly at the S3-compatible object storage
bucket — the backend does not proxy or serve generated files itself.

A ready-to-import request collection is at
[`postman/image-resizer-goravel.postman_collection.json`](postman/image-resizer-goravel.postman_collection.json).

## Database schema

MySQL (or Postgres) tables, defined via the migrations in
`database/migrations/`:

- **`image_processing_requests`** — one row per API request: `status`,
  `input_type` (`upload`/`url`), `source_url`, `source_file_path`,
  `error_message`, `started_at`/`completed_at`/`last_heartbeat_at`,
  `source_width`/`source_height`, `output_hash`.
- **`image_processing_request_sizes`** — one row per requested output size
  within a request: `width`, `height`, `mode`, `allow_upscale`, `quality`,
  `status`, `error_message`.
- **`image_outputs`** — one row per successfully generated file: `width`,
  `height`, `mode`, `format`, `storage_path`, `file_size`, `expires_at`.
- **`jobs`** / **`failed_jobs`** — Goravel's standard database queue tables
  (used by the still-registered but currently idle cleanup queue).

Model IDs are Goravel's standard auto-incrementing `uint` (`orm.Model`)
rather than UUIDs.

## Getting started

### Option A: Docker (no Go required)

```bash
git clone <your-fork-or-repo-url>.git
cd image-resizer-goravel-backend
cp .env.example .env
docker compose up -d --build
docker compose exec goravel go run . artisan migrate
```

This starts three services (`docker-compose.yml`): `goravel` (the API,
port `3000`), `mysql` (port `3306`), and `minio` (ports `9000` API / `9001`
console, credentials `minioadmin`/`minioadmin`) plus a one-shot
`minio-init` container that creates and publicizes the `images` bucket.

Verify:

```bash
curl -X POST http://localhost:3000/api/v1/images \
  -F 'data={"sizes":[{"width":200,"height":200}]}' \
  -F 'image=@/path/to/any/image.jpg'
```

Stop with `docker compose down` (add `-v` to also wipe MySQL/MinIO
volumes).

### Option B: native Go + libvips

Prerequisites: Go 1.25+, libvips 8.10+ (`libvips-dev`/`vips` via your
package manager), and a MySQL 8+ (or Postgres) database — e.g.
`docker compose up -d mysql minio`.

```bash
cp .env.example .env
go mod tidy      # fetches govips; requires libvips-dev headers
go run . artisan migrate
go run .         # HTTP server + cleanup-queue worker + scheduler, all in one process
```

There is no separate `queue:work` process needed for image processing
itself (it runs inline in the request); the cleanup-queue worker still
starts automatically as part of the application process
(`bootstrap/queue_runners.go`).

## Environment variables

All image-processing limits are centralized in `config/image.go` and read
through `app/support/imageconfig`. See [`.env.example`](.env.example) for
the full, commented list.

| Variable | Purpose |
|---|---|
| `APP_NAME`, `APP_ENV`, `APP_KEY`, `APP_DEBUG`, `APP_TIMEZONE` | Standard Goravel app config |
| `APP_URL`, `APP_HOST`, `APP_PORT` | HTTP server binding (default port `3000`) |
| `JWT_SECRET` | Present in scaffolding (`config/jwt.go`); not used by any current endpoint |
| `DB_CONNECTION`, `DB_HOST`, `DB_PORT`, `DB_DATABASE`, `DB_USERNAME`, `DB_PASSWORD`, `DB_SSLMODE`, `DB_SCHEMA` | Database connection (`mysql` or `postgres`) |
| `GRPC_HOST`, `GRPC_PORT` | gRPC scaffolding, no service currently registered |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_BUCKET`, `AWS_DEFAULT_REGION`, `AWS_ENDPOINT`, `AWS_URL`, `AWS_USE_PATH_STYLE_ENDPOINT` | S3-compatible object storage for generated outputs (MinIO locally) |
| `MAIL_*` | Mail config scaffolding, not used by the image API |
| `QUEUE_CONNECTION` | `database` (default) or `sync` |
| `PROCESSING_QUEUE`, `CLEANUP_QUEUE` | Queue names; only the cleanup queue's worker currently runs |
| `IMAGE_DEFAULT_QUALITY`, `IMAGE_QUALITY_MIN`, `IMAGE_QUALITY_MAX` | WebP quality default and client-override bounds |
| `IMAGE_RETENTION_SECONDS` | Stored on each output as `expires_at`, but not currently acted on |
| `ALLOW_UPSCALE` | Whether outputs may be enlarged beyond the source |
| `MAX_IMAGE_FILE_SIZE` | Uploaded source file size cap (bytes) |
| `MAX_IMAGE_WIDTH`, `MAX_IMAGE_HEIGHT` | Source/requested dimension caps |
| `MAX_TOTAL_OUTPUT_PIXELS` | Per-requested-size pixel cap (decompression-bomb guard) |
| `MAX_SIZES_PER_REQUEST` | Sizes allowed per request |
| `MAX_CONCURRENT_IMAGE_JOBS` | In-process semaphore bounding concurrent synchronous processing |
| `VIPS_CONCURRENCY` | Threads libvips uses per operation |
| `HEARTBEAT_INTERVAL_SECONDS`, `STALE_PROCESSING_TIMEOUT_SECONDS` | Stale-request recovery (see [Architecture](#architecture)) |
| `STORAGE_PATH` | Local disk root used for temporary uploaded originals (the "images" disk) |

### Worker/processing concurrency

Two independent levels of parallelism: how many requests
`ProcessImageRequestSync` runs at once (`MAX_CONCURRENT_IMAGE_JOBS`), and
how many threads libvips itself uses per operation (`VIPS_CONCURRENCY`).
Keep roughly `MAX_CONCURRENT_IMAGE_JOBS x VIPS_CONCURRENCY <= CPU cores`
available to the process. Defaults (4 x 2 = 8) target an 8-core machine.

## Running tests

```bash
go test ./...
```

`app/support/imageconfig` has unit tests that run without libvips or a real
database (`app/support/imageconfig/imageconfig_test.go`). The
`tests/feature` suite uses `stretchr/testify`'s suite package
(`tests/test_case.go` provides shared setup); exercising the full
image-processing/API flow requires libvips installed and a configured
database.

## Deployment / Docker notes

- `Dockerfile` builds the Go binary and bundles libvips for the image
  processing runtime.
- `docker-compose.yml` wires up the app, MySQL, and MinIO for local/dev use
  (see [Getting started](#getting-started)); swap `AWS_*` env vars to point
  at a real S3-compatible endpoint/bucket in production.
- The `goravel/s3` dependency is replaced (`go.mod`) with a local patched
  fork in `third_party/goravel-s3` that fixes `use_path_style` handling —
  required for MinIO/self-hosted S3-compatible endpoints that don't support
  bucket-as-subdomain routing.
- Migrations are not run automatically on container start; run
  `artisan migrate` manually after bringing the stack up.
