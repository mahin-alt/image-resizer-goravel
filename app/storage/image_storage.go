// Package storage is a thin wrapper around Goravel's filesystem facade,
// scoped to the "images" disk (see config/filesystems.go, rooted at
// STORAGE_PATH). Jobs and controllers depend on this instead of calling
// facades.Storage() directly, so the backend can change later without
// touching them.
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/goravel/framework/contracts/filesystem"

	"goravel/app/facades"
)

// disk holds the temporary uploaded source file until the worker deletes it
// post-processing. outputDisk holds the generated, permanent WebP outputs
// (S3-compatible - MinIO in dev, see config/filesystems.go's "s3" entry).
const (
	disk       = "images"
	outputDisk = "s3"
)

// OutputPath builds the storage path for one generated output: all sizes of
// the same request share the same hash (see NewOutputHash), so they resolve
// to the same filename across their own size folder -
// media/{width}x{height}/{hash}.webp. hash is our own generated value
// (never derived from client input), so there is no path-traversal surface.
func OutputPath(width, height int, hash string) string {
	return fmt.Sprintf("media/%dx%d/%s.webp", width, height, hash)
}

// NewOutputHash generates the random name shared by every output of one
// request. 16 bytes (128 bits) of crypto/rand, hex-encoded - collision odds
// are negligible and it never derives from client input or timing, so it
// can't be predicted or path-traversed.
func NewOutputHash() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate output hash: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// UploadDir is where an uploaded original source is stored temporarily
// (until the worker deletes it post-processing), keyed by request id.
func UploadDir(requestID uint) string {
	return fmt.Sprintf("uploads/%d", requestID)
}

// PutUploadedFile stores an uploaded file under dir, named deterministically
// as "source" + its own extension (never the client-supplied filename - no
// path-traversal surface), and returns the resulting storage path.
func PutUploadedFile(dir string, file filesystem.File) (string, error) {
	ext, err := file.Extension()
	if err != nil || ext == "" {
		ext = "bin"
	}
	return facades.Storage().Disk(disk).PutFileAs(dir, file, "source."+ext)
}

// Path resolves a storage path to a real filesystem path, for callers (the
// govips adapter) that need to open the file directly rather than through
// the storage abstraction.
func Path(path string) string {
	return facades.Storage().Disk(disk).Path(path)
}

// Put writes bytes to path on the images disk, replacing any existing
// content - callers get atomicity from the fact that the ImageOutput DB row
// (which is what GET /images/{id} reads) is only created/committed after
// this succeeds; see the job for the ordering.
func Put(path string, content []byte) error {
	return facades.Storage().Disk(disk).Put(path, string(content))
}

func Delete(path string) error {
	return facades.Storage().Disk(disk).Delete(path)
}

// DeleteDirectory removes a directory (and its contents) - used to clean up
// an upload's uploads/{request_id} folder once processing is done with it.
func DeleteDirectory(dir string) error {
	return facades.Storage().Disk(disk).DeleteDirectory(dir)
}

func Exists(path string) bool {
	return facades.Storage().Disk(disk).Exists(path)
}

func Url(path string) string {
	return facades.Storage().Disk(disk).Url(path)
}

// PutOutput writes a generated output's bytes to the permanent output disk
// (S3/MinIO). Outputs are never deleted, so - unlike Put above - there is no
// corresponding DeleteOutput.
func PutOutput(path string, content []byte) error {
	return facades.Storage().Disk(outputDisk).Put(path, string(content))
}

// OutputUrl is the only way callers read a generated output back - there is
// no backend-proxied download path, clients fetch straight from the "s3"
// disk via this URL.
func OutputUrl(path string) string {
	return facades.Storage().Disk(outputDisk).Url(path)
}
