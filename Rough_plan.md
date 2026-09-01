# Build a self-hosted image processing API with Goravel + govips

I want to build a production-oriented, headless image-processing API using **Goravel**.

There will be **no GUI/frontend/admin panel**. The application exposes an HTTP API only.

The image-processing implementation is now decided:

> **Use ****`govips`**** backed by ****`libvips`****.**

Do not replace govips with bimg, ImageMagick, disintegration/imaging, or another image-processing library unless you discover a concrete incompatibility with the requirements below. If you discover such an incompatibility, STOP and tell me before changing the stack.

All image processing must happen on our own server.

There must be no external image-processing SaaS/API/service such as Cloudinary, Imgix, ImageKit, Uploadcare, etc.

Using open-source libraries that run locally on our infrastructure is completely acceptable.

---

# IMPORTANT: investigate before implementing

Before writing significant code:

1. Inspect the existing repository.
2. Inspect the current Goravel version.
3. Inspect the current project structure.
4. Check the current official Goravel documentation for:

   * validation
   * queues/jobs/workers
   * filesystem/storage
   * configuration/environment variables
   * HTTP client/request handling
   * scheduling/background tasks
5. Check the current govips documentation/repository.
6. Check the installed/required libvips version and system dependencies.
7. Verify the current govips API instead of relying on old examples.
8. Identify any architectural decisions that remain ambiguous.

Use current official/primary documentation where possible.

Do NOT immediately generate the entire application.

First present me with:

* proposed architecture
* API design
* database schema
* queue/job architecture
* image-processing architecture
* storage architecture
* configuration/environment variables
* security concerns
* govips/libvips dependencies
* unresolved decisions

Then ask me the questions you need answered.

WAIT for my answers before implementing decisions that materially affect the architecture.

---

# Core purpose

The API accepts an **image URL** and a list of requested output sizes.

It downloads the source image, processes it locally using govips/libvips, converts every output to **WebP**, persists the generated files on our server, and returns JSON containing URLs for the generated outputs.

Conceptually:

Client
↓
Goravel API
↓
create processing request
↓
queue job
↓
worker
↓
download source image
↓
validate image
↓
govips/libvips processing
↓
generate requested WebP sizes
↓
store files
↓
update database
↓
GET status/result
↓
client receives generated URLs

---

# Image input

The API receives the image through a URL.

Example:

POST /api/v1/images

Content-Type: application/json

{
"image_url": "https://example.com/photo.jpg",
"sizes": [
{
"width": 400,
"height": 400
},
{
"width": 800,
"height": 600
},
{
"width": 1200,
"height": 900
}
]
}

Do NOT add base64 image input.

Do NOT add multipart upload unless we later decide to support it.

The primary input is an image URL.

---

# URL downloading

The server downloads the image from the supplied URL.

The download layer must have configurable:

* connection timeout
* total download timeout
* maximum download size
* redirect behavior
* HTTP status handling
* content validation

Do not download unlimited data.

Prefer a safe streaming/download-to-temporary-file approach rather than loading arbitrarily large remote files entirely into memory.

---

# SSRF protection

Right now, all URLs, but keep that in mind I may implement this in future, in different environments.

# Multiple sizes

A single API request may request multiple output sizes.

Example:

{
"sizes": [
{
"width": 400,
"height": 400
},
{
"width": 800,
"height": 600
},
{
"width": 1200,
"height": 900
}
]
}

Each requested size should result in a separate WebP output.

The maximum number of requested sizes MUST be configurable.

Example:

MAX_SIZES_PER_REQUEST=10

Do not hard-code this limit.

---

# IMPORTANT: evaluate one job vs many jobs

I specifically want you to evaluate these two architectures before implementation.

## Option A: one processing job per request

Example:

ProcessImageJob
↓
download source
↓
decode source
↓
generate 400x400
↓
generate 800x600
↓
generate 1200x900
↓
persist all outputs
↓
mark request completed

## Option B: multiple jobs per requested size

Example:

ProcessImageRequestJob
↓
download/prepare source
↓
dispatch:
├── GenerateImageJob 400x400
├── GenerateImageJob 800x600
└── GenerateImageJob 1200x900

Evaluate both architectures carefully.

Consider:

* repeated source downloading
* repeated image decoding
* memory consumption
* CPU utilization
* libvips internal threading
* queue throughput
* worker concurrency
* retry behavior
* partial failures
* database state management
* scalability
* complexity
* idempotency
* cleanup
* whether the same decoded source can efficiently produce multiple outputs

Do NOT simply choose one because it is easier.

Research current libvips threading/concurrency behavior and current Goravel queue capabilities.

Then give me a recommendation and explain why.

I want the **best-practice architecture for this application**, not merely the simplest implementation.

A likely candidate is one processing job per request with all requested sizes processed inside it, but this MUST be evaluated rather than assumed.

---

# Image processing stack

The image-processing stack is:

Goravel
↓
ImageProcessingService
↓
ImageProcessor interface
↓
GovipsImageProcessor
↓
govips
↓
libvips

Use govips as the concrete implementation.

Do not spread govips-specific code throughout controllers/jobs/models.

The rest of the application should depend on our own application-level image-processing abstraction.

---

# Resize modes

The API should support multiple resize behaviors.

At minimum evaluate/support:

* contain
* cover
* fill/exact resize
* fit/inside
* crop

Example:

{
"width": 800,
"height": 800,
"mode": "cover"
}

The API terminology should be clear and stable.

Map the API-level concepts to the appropriate govips/libvips operations.

Do not expose raw libvips concepts unnecessarily.

Explain the semantics of each mode in the documentation.

Do not stretch images unless the client explicitly requests a mode that means exact/fill resizing.

---

# Upscaling

Upscaling must be supported as an option.

Example:

source:
400x300

requested:
1200x900

If upscaling is enabled:

400x300
↓
1200x900

If upscaling is disabled:

the application must not enlarge the image.

This is normal local image resizing/interpolation.

Do NOT use AI upscaling or an external service.

Determine whether this should be:

* a global environment configuration
* a per-request option
* or both

Recommend the best design.

The underlying govips/libvips capability should be used rather than implementing a custom interpolation algorithm.

---

# Output format

Every generated output MUST be WebP.

The client cannot choose another output format.

The API returns JSON containing URLs to the generated files.

Because processing is asynchronous, use a request/job resource model.

A likely design is:

POST /api/v1/images

Response:

{
"id": "...",
"status": "pending"
}

Then:

GET /api/v1/images/{id}

During processing:

{
"id": "...",
"status": "processing"
}

After completion:

{
"id": "...",
"status": "completed",
"images": [
{
"width": 400,
"height": 400,
"format": "webp",
"url": "..."
},
{
"width": 800,
"height": 600,
"format": "webp",
"url": "..."
}
]
}

Evaluate and improve this API design before implementation.

---

# Asynchronous processing

Image processing MUST NOT happen during the HTTP request.

The API request should:

1. validate the request
2. create a processing record
3. dispatch a queue job
4. return quickly

The worker performs the expensive operations.

Use Goravel's current queue/job/worker functionality.

Goravel supports queued jobs, retries, delayed jobs, queue selection and worker concurrency. Use the current recommended Goravel approach rather than inventing a custom queue mechanism.

Use appropriate retries for transient failures.

Do not endlessly retry permanent validation failures.

---

# Queue architecture

Evaluate whether separate queues are useful, for example:

* image_processing
* image_cleanup

Consider whether cleanup jobs should be isolated from image-processing jobs so a heavy image-processing workload cannot prevent cleanup from running.

Do not introduce unnecessary queues.

Recommend the simplest architecture that provides reliable operation.

---

# Persistence

Generated WebP files are persistent for a configurable period.

Example:

IMAGE_RETENTION_HOURS=24

The generated files should remain accessible for that period.

After expiration, they must be deleted by background cleanup.

Do NOT store the WebP binary in the database.

The actual WebP files must be stored on the server/filesystem or another self-hosted storage backend.

The database should store metadata and a storage path/reference.

For example:

ImageOutput:

* id
* processing_request_id
* width
* height
* format
* storage_path
* file_size
* created_at
* expires_at

Do not store image binary/blob data in the database.

Keep storage behind an abstraction so the storage backend can be changed later if necessary.

---

# Expiration

Store an explicit expiration timestamp for generated outputs.

Do not calculate expiration only at cleanup time using:

created_at + current ENV value

Instead, when an output is created, calculate and persist:

expires_at

based on the configured retention period at that time.

This ensures changing the environment variable later does not unexpectedly alter the lifetime of already-created images.

The cleanup system should delete records/files whose expiration timestamp has passed.

---

# Cleanup

Build a background cleanup mechanism.

Evaluate the cleanest Goravel-native architecture, potentially involving:

* scheduled cleanup dispatch
* cleanup job
* dedicated cleanup queue
* worker

The cleanup operation must be idempotent.

It must safely handle:

* already deleted files
* missing files
* missing database records
* deletion failures
* worker restarts
* application restarts
* repeated cleanup execution

Do not rely on the application process staying alive continuously.

---

# Image formats

Accept formats supported by the installed govips/libvips stack.

Investigate actual support rather than promising "every image format."

At minimum investigate:

* JPEG/JPG
* PNG
* WebP
* GIF
* TIFF
* BMP
* AVIF
* HEIC/HEIF
* SVG
* JPEG 2000
* JPEG XL

The exact support depends on the libvips build and installed loaders/codecs.

Document exactly which formats the deployment supports.

Output is always WebP.

---

# Animated images

Before implementation, ASK ME how animated source images should behave.

Examples:

* animated GIF
* animated WebP

Possible policies:

1. reject animated images
2. process only the first frame
3. preserve animation

Do not silently choose one.

---

# Metadata

Strip metadata from generated outputs by default.

Do not preserve EXIF by default.

This includes potentially sensitive metadata such as:

* GPS coordinates
* camera information
* timestamps
* software information

The output should be a clean WebP containing the image data necessary for the result.

Use the appropriate govips/libvips metadata handling rather than manually attempting to remove arbitrary metadata fields one by one.

---

# Image orientation

Handle EXIF orientation correctly before resizing/cropping.

The conceptual pipeline should be:

download
↓
decode
↓
auto-orient
↓
resize/crop
↓
strip metadata
↓
encode WebP
↓
store

Do not generate incorrectly rotated outputs because EXIF orientation was ignored.

---

# WebP quality

The WebP encoder should support configurable quality.

Example:

IMAGE_DEFAULT_QUALITY=80

Research the current govips/libvips WebP encoder options.

Determine:

* valid quality range
* default quality
* whether quality should be globally configurable
* whether clients can override it
* whether client overrides need min/max limits

Quality controls the tradeoff between visual fidelity and resulting file size.

Do not treat quality as a resize parameter.

---

# Resource limits

All important resource limits MUST be configurable through environment variables.

At minimum:

MAX_IMAGE_FILE_SIZE
MAX_IMAGE_WIDTH
MAX_IMAGE_HEIGHT
MAX_SIZES_PER_REQUEST

Also evaluate:

MAX_SOURCE_DOWNLOAD_SIZE
MAX_SOURCE_DOWNLOAD_TIME
MAX_CONNECTION_TIMEOUT
MAX_REDIRECTS
MAX_TOTAL_OUTPUT_PIXELS
MAX_CONCURRENT_IMAGE_JOBS

Recommend sensible defaults.

Do not hard-code these limits.

Centralize configuration rather than reading environment variables throughout application code.

---

# Security

There is intentionally:

* NO authentication
* NO API rate limiting

Do not add those unless you identify a critical requirement that I need to decide on.

However, the application processes untrusted remote images and must therefore be defensive.

Do not trust:

* URL extension
* HTTP Content-Type alone
* filenames

Validate the actual image.

Protect against:

* malformed images
* huge dimensions
* decompression/resource exhaustion
* oversized downloads
* slow downloads
* malicious redirects
* SSRF
* internal/private network access

---

# Database

Design the database around processing requests and generated outputs.

Potential conceptual models:

ImageProcessingRequest

* id
* source_url
* status
* error_message
* requested_sizes/configuration
* created_at
* started_at
* completed_at

ImageOutput

* id
* processing_request_id
* width
* height
* resize_mode
* format
* storage_path
* file_size
* created_at
* expires_at

Do not blindly use this exact schema.

Evaluate normalization and whether requested resize parameters should be stored as JSON or normalized records.

Do not store image binary data in the database.

---

# Processing state

Design a clear state machine.

Possible states:

pending
processing
completed
failed
expired

Evaluate whether `partially_completed` is necessary if individual outputs can fail.

Important question:

If one requested size fails while the others succeed, determine whether:

A. the entire request is failed and outputs are removed

or

B. the request becomes partially completed and successful outputs remain available

ASK ME before finalizing this behavior.

---

# Job reliability and idempotency

Jobs may be retried.

Design image-processing jobs so that retries do not create duplicate outputs or corrupt existing outputs.

Consider:

* deterministic output paths
* unique output identifiers
* temporary files
* atomic file moves/renames
* database transactions where appropriate
* checking whether an output already exists
* cleaning up partially generated files

Do not assume a job executes exactly once.

---

# Temporary files

If downloading source images to disk before processing, use a safe temporary-file strategy.

Temporary source files must be cleaned up after processing, including when processing fails.

Do not leave unbounded temporary files after worker crashes or failures.

Consider whether a periodic temporary-file cleanup mechanism is necessary.

---

# Concurrency

This application uses:

Goravel queue workers
+
govips
+
libvips internal threading

Do not independently maximize all three levels of concurrency.

Research current libvips threading behavior.

Evaluate:

* number of Goravel worker jobs running simultaneously
* libvips concurrency
* CPU core count
* memory limits
* expected image sizes

Recommend sensible defaults/configuration.

The goal is stable throughput rather than maximum theoretical parallelism.

---

# Architecture

Prefer this conceptual architecture:

HTTP Controller
↓
Request validation / DTO
↓
Application service
↓
Processing request persistence
↓
Queue dispatch
↓
Image Processing Job
↓
Image Download Service
↓
Image Validation
↓
Image Processing Service
↓
ImageProcessor interface
↓
GovipsImageProcessor
↓
govips
↓
libvips
↓
Storage abstraction
↓
Database metadata

Cleanup should be independent:

Scheduler/trigger
↓
Cleanup Job
↓
find expired outputs
↓
delete files
↓
delete/update metadata

Keep controllers thin.

Jobs should orchestrate work rather than contain giant amounts of image-processing code.

Do not create excessive abstractions just for the sake of abstraction.

---

# Storage

For V1, local server filesystem storage is acceptable.

Use an application storage abstraction.

Do not hard-code absolute filesystem paths throughout the application.

Generated output paths should be deterministic and safe.

Example conceptual structure:

storage/images/{request-id}/{output-id}.webp

The exact structure can be improved.

Do not use client-provided filenames directly as storage paths.

---

# Testing

Create tests for:

## API

* valid request
* missing image URL
* invalid URL
* invalid dimensions
* unsupported resize mode
* too many sizes
* oversized configuration
* invalid quality
* invalid request data

## Downloading

* successful download
* HTTP error
* timeout
* oversized response
* redirect handling
* blocked private/internal destination
* invalid image content

## Processing

* JPEG → WebP
* PNG → WebP
* WebP → WebP
* supported formats
* unsupported formats
* corrupted image
* orientation correction
* metadata stripping
* each resize mode
* upscaling enabled
* upscaling disabled
* multiple output sizes
* WebP quality

## Jobs

* dispatch
* successful processing
* failure
* retry
* idempotent retry
* partial processing behavior

## Cleanup

* expired output deleted
* non-expired output retained
* missing file handled safely
* repeated cleanup is safe
* expired database record handling

---

# Logging

Use structured logging for:

* request created
* job dispatched
* download started
* download failed
* image validation failed
* processing started
* processing completed
* output generated
* processing failed
* cleanup started
* cleanup completed
* cleanup failed

Include identifiers such as:

* processing request ID
* output ID
* job ID

Do not log sensitive data unnecessarily.

---

# Configuration

Centralize configuration.

Do not scatter environment-variable reads across controllers/services/jobs.

At minimum configure:

IMAGE_DEFAULT_QUALITY
IMAGE_RETENTION_HOURS
MAX_IMAGE_FILE_SIZE
MAX_IMAGE_WIDTH
MAX_IMAGE_HEIGHT
MAX_SIZES_PER_REQUEST
MAX_SOURCE_DOWNLOAD_SIZE
MAX_SOURCE_DOWNLOAD_TIME
MAX_CONNECTION_TIMEOUT
MAX_REDIRECTS
ALLOW_UPSCALE
STORAGE_PATH
PROCESSING_QUEUE
CLEANUP_QUEUE

Add additional configuration only when justified.

Provide a complete `.env.example`.

---

# Documentation

Create/update documentation explaining:

* project purpose
* architecture
* required system dependencies
* libvips installation
* govips requirements
* local development
* environment variables
* database setup
* queue setup
* worker startup
* cleanup mechanism
* API endpoints
* request examples
* response examples
* resize modes
* upscaling
* WebP quality
* supported source formats
* metadata behavior
* retention
* error responses
* SSRF protection
* production deployment
* worker concurrency

---

# Development workflow

Do not implement everything in one giant change.

Work incrementally.

Recommended order:

1. inspect repository
2. verify Goravel version and APIs
3. verify govips/libvips installation and integration
4. propose architecture
5. resolve open questions with me
6. create configuration
7. create database schema/migrations
8. implement image processing abstraction
9. implement govips adapter
10. implement download/validation layer
11. implement queue/job processing
12. implement storage
13. implement API/status endpoints
14. implement cleanup
15. implement tests
16. document everything

At each significant step, keep the project buildable/testable.

Do not silently make major architectural decisions.

---

# Most important instruction

Before implementation, explicitly answer:

**Should multiple requested sizes be processed by one job or by multiple jobs?**

Analyze both approaches in the context of:

* govips
* libvips
* Goravel workers
* CPU/memory usage
* repeated decoding
* queue throughput
* retries
* partial failures
* idempotency
* scalability

Then recommend the best-practice architecture for this project.

Do not assume "more jobs = more performance."

I want the recommendation backed by the actual behavior of libvips/govips and Goravel's queue model.

After presenting the recommendation and all unresolved decisions, WAIT for my confirmation before implementing the final architecture.
