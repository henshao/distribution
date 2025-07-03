// Package cos provides a storagedriver.StorageDriver implementation to
// store blobs in Tencent Cloud COS (Cloud Object Storage).
package cos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/distribution/distribution/v3/internal/dcontext"
	storagedriver "github.com/distribution/distribution/v3/registry/storage/driver"
	"github.com/distribution/distribution/v3/registry/storage/driver/base"
	"github.com/distribution/distribution/v3/registry/storage/driver/factory"
	"github.com/tencentyun/cos-go-sdk-v5"
)

const (
	driverName            = "cos"
	defaultChunkSize      = 16 * 1024 * 1024 // 16MB chunks
	minChunkSize          = 5 * 1024 * 1024  // 5MB min multipart upload chunk size
	maxChunkSize          = 100 * 1024 * 1024 // 100MB max chunk size
	defaultMaxConcurrency = 10
	minConcurrency        = 1
)

func init() {
	factory.Register(driverName, &cosDriverFactory{})
}

// cosDriverFactory implements the factory.StorageDriverFactory interface
type cosDriverFactory struct{}

func (factory *cosDriverFactory) Create(ctx context.Context, parameters map[string]interface{}) (storagedriver.StorageDriver, error) {
	return FromParameters(ctx, parameters)
}

// DriverParameters represents all configuration options available for the COS driver
type DriverParameters struct {
	SecretID       string
	SecretKey      string
	Region         string
	Bucket         string
	RootDirectory  string
	ChunkSize      int
	MaxConcurrency int
	Secure         bool // use HTTPS by default
}

// FromParameters constructs a new Driver with a given parameters map
// Required parameters:
// - secretid
// - secretkey
// - region
// - bucket
func FromParameters(ctx context.Context, parameters map[string]interface{}) (*Driver, error) {
	params, err := parseParameters(parameters)
	if err != nil {
		return nil, err
	}
	return New(ctx, params)
}

func parseParameters(parameters map[string]interface{}) (*DriverParameters, error) {
	// SecretID (required)
	secretID := parameters["secretid"]
	if secretID == nil || fmt.Sprint(secretID) == "" {
		return nil, fmt.Errorf("no secretid parameter provided")
	}

	// SecretKey (required)
	secretKey := parameters["secretkey"]
	if secretKey == nil || fmt.Sprint(secretKey) == "" {
		return nil, fmt.Errorf("no secretkey parameter provided")
	}

	// Region (required)
	region := parameters["region"]
	if region == nil || fmt.Sprint(region) == "" {
		return nil, fmt.Errorf("no region parameter provided")
	}

	// Bucket (required)
	bucket := parameters["bucket"]
	if bucket == nil || fmt.Sprint(bucket) == "" {
		return nil, fmt.Errorf("no bucket parameter provided")
	}

	// RootDirectory (optional)
	rootDirectory := parameters["rootdirectory"]
	if rootDirectory == nil {
		rootDirectory = ""
	}

	// ChunkSize (optional)
	chunkSize := defaultChunkSize
	if chunkSizeParam := parameters["chunksize"]; chunkSizeParam != nil {
		if cs, err := strconv.Atoi(fmt.Sprint(chunkSizeParam)); err != nil {
			return nil, fmt.Errorf("chunksize parameter must be an integer, %v invalid", chunkSizeParam)
		} else if cs < minChunkSize {
			return nil, fmt.Errorf("chunksize %d must be at least %d", cs, minChunkSize)
		} else if cs > maxChunkSize {
			return nil, fmt.Errorf("chunksize %d must be at most %d", cs, maxChunkSize)
		} else {
			chunkSize = cs
		}
	}

	// MaxConcurrency (optional)
	maxConcurrency := defaultMaxConcurrency
	if maxConcurrencyParam := parameters["maxconcurrency"]; maxConcurrencyParam != nil {
		if mc, err := strconv.Atoi(fmt.Sprint(maxConcurrencyParam)); err != nil {
			return nil, fmt.Errorf("maxconcurrency parameter must be an integer, %v invalid", maxConcurrencyParam)
		} else if mc < minConcurrency {
			return nil, fmt.Errorf("maxconcurrency %d must be at least %d", mc, minConcurrency)
		} else {
			maxConcurrency = mc
		}
	}

	// Secure (optional, default true)
	secure := true
	if secureParam := parameters["secure"]; secureParam != nil {
		switch s := secureParam.(type) {
		case string:
			b, err := strconv.ParseBool(s)
			if err != nil {
				return nil, fmt.Errorf("secure parameter must be a boolean")
			}
			secure = b
		case bool:
			secure = s
		default:
			return nil, fmt.Errorf("secure parameter must be a boolean")
		}
	}

	return &DriverParameters{
		SecretID:       fmt.Sprint(secretID),
		SecretKey:      fmt.Sprint(secretKey),
		Region:         fmt.Sprint(region),
		Bucket:         fmt.Sprint(bucket),
		RootDirectory:  fmt.Sprint(rootDirectory),
		ChunkSize:      chunkSize,
		MaxConcurrency: maxConcurrency,
		Secure:         secure,
	}, nil
}

type driver struct {
	Client         *cos.Client
	Bucket         string
	RootDirectory  string
	ChunkSize      int
	MaxConcurrency int
	pool           *sync.Pool
}

type baseEmbed struct {
	base.Base
}

// Driver is a storagedriver.StorageDriver implementation backed by Tencent Cloud COS
type Driver struct {
	baseEmbed
}

// New constructs a new Driver with the given parameters
func New(ctx context.Context, params *DriverParameters) (*Driver, error) {
	// Construct the bucket URL
	protocol := "https"
	if !params.Secure {
		protocol = "http"
	}
	bucketURL := fmt.Sprintf("%s://%s.cos.%s.myqcloud.com", protocol, params.Bucket, params.Region)
	u, err := url.Parse(bucketURL)
	if err != nil {
		return nil, fmt.Errorf("invalid bucket URL: %v", err)
	}

	// Create the COS client
	b := &cos.BaseURL{BucketURL: u}
	client := cos.NewClient(b, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  params.SecretID,
			SecretKey: params.SecretKey,
		},
	})

	// Test bucket access
	if _, _, err := client.Bucket.Head(ctx); err != nil {
		return nil, fmt.Errorf("unable to access bucket %s in region %s: %v", params.Bucket, params.Region, err)
	}

	d := &driver{
		Client:         client,
		Bucket:         params.Bucket,
		RootDirectory:  params.RootDirectory,
		ChunkSize:      params.ChunkSize,
		MaxConcurrency: params.MaxConcurrency,
		pool: &sync.Pool{
			New: func() interface{} { return &bytes.Buffer{} },
		},
	}

	return &Driver{
		baseEmbed: baseEmbed{
			Base: base.Base{
				StorageDriver: d,
			},
		},
	}, nil
}

// Implement the storagedriver.StorageDriver interface

func (d *driver) Name() string {
	return driverName
}

// GetContent retrieves the content stored at "path" as a []byte.
func (d *driver) GetContent(ctx context.Context, path string) ([]byte, error) {
	reader, err := d.Reader(ctx, path, 0)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// PutContent stores the []byte content at a location designated by "path".
func (d *driver) PutContent(ctx context.Context, path string, content []byte) error {
	cosPath := d.cosPath(path)
	_, err := d.Client.Object.Put(ctx, cosPath, bytes.NewReader(content), nil)
	return parseError(path, err)
}

// Reader retrieves an io.ReadCloser for the content stored at "path" with a
// given byte offset.
func (d *driver) Reader(ctx context.Context, path string, offset int64) (io.ReadCloser, error) {
	cosPath := d.cosPath(path)
	
	opt := &cos.ObjectGetOptions{}
	if offset > 0 {
		opt.Range = fmt.Sprintf("bytes=%d-", offset)
	}
	
	resp, err := d.Client.Object.Get(ctx, cosPath, opt)
	if err != nil {
		return nil, parseError(path, err)
	}
	
	return resp.Body, nil
}

// Writer returns a FileWriter which will store the content written to it
// at the location designated by "path" after the call to Commit.
func (d *driver) Writer(ctx context.Context, path string, append bool) (storagedriver.FileWriter, error) {
	key := d.cosPath(path)
	
	if !append {
		// Start a new multipart upload
		result, _, err := d.Client.Object.InitiateMultipartUpload(ctx, key, nil)
		if err != nil {
			return nil, parseError(path, err)
		}
		return d.newWriter(ctx, key, result.UploadID, nil), nil
	}
	
	// For append mode, we need to check if there's an existing multipart upload
	// or if we need to start a new one for an existing object
	
	// First, check if the object exists
	_, err := d.Client.Object.Head(ctx, key, nil)
	if err != nil {
		// Object doesn't exist, start a new multipart upload
		result, _, err := d.Client.Object.InitiateMultipartUpload(ctx, key, nil)
		if err != nil {
			return nil, parseError(path, err)
		}
		return d.newWriter(ctx, key, result.UploadID, nil), nil
	}
	
	// Object exists, we need to handle append
	// COS doesn't support true append, so we'll need to re-upload with existing content
	// For now, return an error for append mode on existing objects
	return nil, errors.New("append mode not supported for existing objects")
}

// Stat retrieves the FileInfo for the given path, including the current size
// in bytes and the creation time.
func (d *driver) Stat(ctx context.Context, path string) (storagedriver.FileInfo, error) {
	cosPath := d.cosPath(path)
	
	// Try to get object metadata
	resp, err := d.Client.Object.Head(ctx, cosPath, nil)
	if err != nil {
		// Check if it's a directory by listing with prefix
		opt := &cos.BucketGetOptions{
			Prefix:    cosPath + "/",
			Delimiter: "/",
			MaxKeys:   1,
		}
		
		result, _, listErr := d.Client.Bucket.Get(ctx, opt)
		if listErr != nil {
			return nil, parseError(path, err)
		}
		
		if len(result.Contents) > 0 || len(result.CommonPrefixes) > 0 {
			// It's a directory
			return storagedriver.FileInfoInternal{
				FileInfoFields: storagedriver.FileInfoFields{
					Path:  path,
					IsDir: true,
				},
			}, nil
		}
		
		return nil, parseError(path, err)
	}
	
	// Parse modification time
	var modTime time.Time
	if lastModified := resp.Header.Get("Last-Modified"); lastModified != "" {
		modTime, _ = time.Parse(http.TimeFormat, lastModified)
	}
	
	// Parse content length
	size := int64(0)
	if contentLength := resp.Header.Get("Content-Length"); contentLength != "" {
		size, _ = strconv.ParseInt(contentLength, 10, 64)
	}
	
	return storagedriver.FileInfoInternal{
		FileInfoFields: storagedriver.FileInfoFields{
			Path:    path,
			Size:    size,
			ModTime: modTime,
			IsDir:   false,
		},
	}, nil
}

// List returns a list of the objects that are direct descendants of the given path.
func (d *driver) List(ctx context.Context, opath string) ([]string, error) {
	path := opath
	if path != "/" && path[len(path)-1] != '/' {
		path = path + "/"
	}
	
	prefix := ""
	if d.RootDirectory != "" {
		prefix = d.RootDirectory + "/"
	}
	if path != "/" {
		prefix = prefix + strings.TrimPrefix(path, "/")
	}
	
	opt := &cos.BucketGetOptions{
		Prefix:    prefix,
		Delimiter: "/",
		MaxKeys:   1000,
	}
	
	files := []string{}
	
	for {
		result, _, err := d.Client.Bucket.Get(ctx, opt)
		if err != nil {
			return nil, parseError(path, err)
		}
		
		for _, obj := range result.Contents {
			name := obj.Key
			name = strings.TrimPrefix(name, d.RootDirectory)
			if !strings.HasPrefix(name, "/") {
				name = "/" + name
			}
			
			// Skip the directory object itself
			if name != path {
				files = append(files, name)
			}
		}
		
		for _, prefix := range result.CommonPrefixes {
			name := prefix.Prefix
			name = strings.TrimPrefix(name, d.RootDirectory)
			if !strings.HasPrefix(name, "/") {
				name = "/" + name
			}
			name = strings.TrimSuffix(name, "/")
			
			if name != "" && name != path {
				files = append(files, name)
			}
		}
		
		if !result.IsTruncated {
			break
		}
		
		opt.Marker = result.NextMarker
	}
	
	return files, nil
}

// Move moves an object stored at sourcePath to destPath, removing the original object.
func (d *driver) Move(ctx context.Context, sourcePath string, destPath string) error {
	// COS supports server-side copy
	sourceKey := d.cosPath(sourcePath)
	destKey := d.cosPath(destPath)
	
	sourceURL := fmt.Sprintf("%s.cos.%s.myqcloud.com/%s", d.Bucket, d.Client.BaseURL.BucketURL.Host, sourceKey)
	_, _, err := d.Client.Object.Copy(ctx, destKey, sourceURL, nil)
	if err != nil {
		return parseError(sourcePath, err)
	}
	
	// Delete the source object
	_, err = d.Client.Object.Delete(ctx, sourceKey)
	if err != nil {
		// Try to delete the copied object to maintain consistency
		d.Client.Object.Delete(ctx, destKey)
		return parseError(sourcePath, err)
	}
	
	return nil
}

// Delete recursively deletes all objects stored at "path" and its subpaths.
func (d *driver) Delete(ctx context.Context, path string) error {
	cosPath := d.cosPath(path)
	
	// First try to delete as a single object
	_, err := d.Client.Object.Delete(ctx, cosPath)
	if err == nil {
		return nil
	}
	
	// If single delete failed, try recursive delete
	opt := &cos.BucketGetOptions{
		Prefix:  cosPath,
		MaxKeys: 1000,
	}
	
	for {
		result, _, err := d.Client.Bucket.Get(ctx, opt)
		if err != nil {
			return parseError(path, err)
		}
		
		// Delete objects in batches
		if len(result.Contents) > 0 {
			objects := make([]cos.Object, 0, len(result.Contents))
			for _, obj := range result.Contents {
				objects = append(objects, cos.Object{Key: obj.Key})
			}
			
			deleteOpt := &cos.ObjectDeleteMultiOptions{
				Objects: objects,
				Quiet:   true,
			}
			
			_, _, err := d.Client.Object.DeleteMulti(ctx, deleteOpt)
			if err != nil {
				return parseError(path, err)
			}
		}
		
		if !result.IsTruncated {
			break
		}
		
		opt.Marker = result.NextMarker
	}
	
	return nil
}

// RedirectURL returns a URL which may be used to retrieve the content stored at the given path.
func (d *driver) RedirectURL(r *http.Request, path string) (string, error) {
	// COS supports presigned URLs
	cosPath := d.cosPath(path)
	
	presignedURL, err := d.Client.Object.GetPresignedURL(
		context.Background(),
		http.MethodGet,
		cosPath,
		cos.PresignedURLOptions{
			Query:  &url.Values{},
			Header: &http.Header{},
		},
	)
	if err != nil {
		return "", err
	}
	
	return presignedURL.String(), nil
}

// Walk traverses a filesystem defined within driver, starting
// from the given path, calling f on each file
func (d *driver) Walk(ctx context.Context, path string, f storagedriver.WalkFn, options ...func(*storagedriver.WalkOptions)) error {
	return storagedriver.WalkFallback(ctx, d, path, f, options...)
}

// cosPath returns the absolute path of a key within the Driver's storage.
func (d *driver) cosPath(path string) string {
	if d.RootDirectory == "" {
		return strings.TrimPrefix(path, "/")
	}
	
	return strings.TrimPrefix(d.RootDirectory+path, "/")
}

// writer implements storagedriver.FileWriter interface for COS.
type writer struct {
	ctx            context.Context
	driver         *driver
	key            string
	uploadID       string
	parts          []cos.Object
	size           int64
	buf            *bytes.Buffer
	closed         bool
	committed      bool
	cancelled      bool
}

func (d *driver) newWriter(ctx context.Context, key, uploadID string, parts []cos.Object) storagedriver.FileWriter {
	buf := d.pool.Get().(*bytes.Buffer)
	buf.Reset()
	
	return &writer{
		ctx:      ctx,
		driver:   d,
		key:      key,
		uploadID: uploadID,
		parts:    parts,
		buf:      buf,
	}
}

func (w *writer) Write(p []byte) (int, error) {
	if w.closed {
		return 0, fmt.Errorf("already closed")
	}
	if w.cancelled {
		return 0, fmt.Errorf("already cancelled")
	}
	if w.committed {
		return 0, fmt.Errorf("already committed")
	}
	
	n, err := w.buf.Write(p)
	if err != nil {
		return n, err
	}
	
	w.size += int64(n)
	
	// Upload part if buffer is full
	for w.buf.Len() >= w.driver.ChunkSize {
		if err := w.uploadPart(); err != nil {
			return n, err
		}
	}
	
	return n, nil
}

func (w *writer) uploadPart() error {
	if w.buf.Len() == 0 {
		return nil
	}
	
	partNumber := len(w.parts) + 1
	data := make([]byte, w.buf.Len())
	copy(data, w.buf.Bytes())
	w.buf.Reset()
	
	result, _, err := w.driver.Client.Object.UploadPart(
		w.ctx,
		w.key,
		w.uploadID,
		partNumber,
		bytes.NewReader(data),
		&cos.ObjectUploadPartOptions{
			ContentLength: int64(len(data)),
		},
	)
	if err != nil {
		return err
	}
	
	w.parts = append(w.parts, cos.Object{
		PartNumber: partNumber,
		ETag:       result.Header.Get("ETag"),
	})
	
	return nil
}

func (w *writer) Size() int64 {
	return w.size
}

func (w *writer) Close() error {
	if w.closed {
		return nil
	}
	
	w.closed = true
	
	// Return buffer to pool
	if w.buf != nil {
		w.driver.pool.Put(w.buf)
		w.buf = nil
	}
	
	return nil
}

func (w *writer) Cancel(ctx context.Context) error {
	if w.closed {
		return nil
	}
	
	w.cancelled = true
	w.Close()
	
	// Abort multipart upload
	_, err := w.driver.Client.Object.AbortMultipartUpload(ctx, w.key, w.uploadID)
	return err
}

func (w *writer) Commit(ctx context.Context) error {
	if w.closed && !w.committed && !w.cancelled {
		return fmt.Errorf("already closed")
	}
	if w.committed {
		return fmt.Errorf("already committed")
	}
	if w.cancelled {
		return fmt.Errorf("already cancelled")
	}
	
	// Upload remaining data
	if err := w.uploadPart(); err != nil {
		return err
	}
	
	// Complete multipart upload
	opt := &cos.CompleteMultipartUploadOptions{
		Parts: w.parts,
	}
	
	_, _, err := w.driver.Client.Object.CompleteMultipartUpload(ctx, w.key, w.uploadID, opt)
	if err != nil {
		return err
	}
	
	w.committed = true
	return w.Close()
}

// parseError converts COS errors to the appropriate driver errors
func parseError(path string, err error) error {
	if err == nil {
		return nil
	}
	
	if cosErr, ok := err.(*cos.ErrorResponse); ok {
		switch cosErr.Code {
		case "NoSuchKey":
			return storagedriver.PathNotFoundError{Path: path}
		case "AccessDenied":
			return fmt.Errorf("access denied: %s", cosErr.Message)
		case "BucketNotFound":
			return fmt.Errorf("bucket not found: %s", cosErr.Message)
		default:
			return fmt.Errorf("cos error: %s - %s", cosErr.Code, cosErr.Message)
		}
	}
	
	return err
}