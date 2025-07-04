// Package cos provides a storagedriver.StorageDriver implementation to
// store blobs in Tencent Cloud Object Storage.
//
// This package leverages the official Tencent Cloud COS client library for interfacing with
// COS.
//
// Because COS is a key, value store the Stat call does not support last modification
// time for directories (directories are an abstraction for key, value stores)
package cos

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"

	storagedriver "github.com/distribution/distribution/v3/registry/storage/driver"
	"github.com/distribution/distribution/v3/registry/storage/driver/base"
	"github.com/distribution/distribution/v3/registry/storage/driver/factory"
)

const driverName = "cos"

// minChunkSize defines the minimum multipart upload chunk size
// COS API requires multipart upload chunks to be at least 1MB
const minChunkSize = 1 * 1024 * 1024

const defaultChunkSize = 5 * minChunkSize

const (
	// defaultMultipartCopyChunkSize defines the default chunk size for all
	// but the last Upload Part - Copy operation of a multipart copy.
	// Empirically, 32 MB is optimal.
	defaultMultipartCopyChunkSize = 32 * 1024 * 1024

	// defaultMultipartCopyMaxConcurrency defines the default maximum number
	// of concurrent Upload Part - Copy operations for a multipart copy.
	defaultMultipartCopyMaxConcurrency = 100

	// defaultMultipartCopyThresholdSize defines the default object size
	// above which multipart copy will be used. (PUT Object - Copy is used
	// for objects at or below this size.)  Empirically, 32 MB is optimal.
	defaultMultipartCopyThresholdSize = 32 * 1024 * 1024
)

// listMax is the largest amount of objects you can request from COS in a list call
const listMax = 1000

// maxChunkSize defines the maximum multipart upload chunk size allowed
const maxChunkSize = 2 * 1024 * 1024 * 1024 // 2GB

// DriverParameters A struct that encapsulates all of the driver parameters after all values have been set
type DriverParameters struct {
	SecretID      string
	SecretKey     string
	Region        string
	Bucket        string
	AppID         string
	Secure        bool
	SkipVerify    bool
	ChunkSize     int
	RootDirectory string
}

func init() {
	factory.Register(driverName, &cosDriverFactory{})
}

// cosDriverFactory implements the factory.StorageDriverFactory interface
type cosDriverFactory struct{}

func (factory *cosDriverFactory) Create(ctx context.Context, parameters map[string]interface{}) (storagedriver.StorageDriver, error) {
	return FromParameters(ctx, parameters)
}

var _ storagedriver.StorageDriver = &driver{}

type driver struct {
	Client        *cos.Client
	Bucket        string
	ChunkSize     int
	RootDirectory string
	pool          *sync.Pool
}

type baseEmbed struct {
	base.Base
}

// Driver is a storagedriver.StorageDriver implementation backed by Tencent Cloud COS
// Objects are stored at absolute keys in the provided bucket.
type Driver struct {
	baseEmbed
}

// FromParameters constructs a new Driver with a given parameters map
// Required parameters:
// - secretid
// - secretkey
// - region
// - bucket
func FromParameters(ctx context.Context, parameters map[string]interface{}) (*Driver, error) {
	secretID := parameters["secretid"]
	if secretID == nil || fmt.Sprint(secretID) == "" {
		return nil, fmt.Errorf("no secretid parameter provided")
	}

	secretKey := parameters["secretkey"]
	if secretKey == nil || fmt.Sprint(secretKey) == "" {
		return nil, fmt.Errorf("no secretkey parameter provided")
	}

	region := parameters["region"]
	if region == nil || fmt.Sprint(region) == "" {
		return nil, fmt.Errorf("no region parameter provided")
	}

	bucket := parameters["bucket"]
	if bucket == nil || fmt.Sprint(bucket) == "" {
		return nil, fmt.Errorf("no bucket parameter provided")
	}

	appID := parameters["appid"]
	if appID == nil {
		appID = ""
	}

	secureBool := true
	secure := parameters["secure"]
	switch secure := secure.(type) {
	case string:
		b, err := strconv.ParseBool(secure)
		if err != nil {
			return nil, fmt.Errorf("the secure parameter should be a boolean")
		}
		secureBool = b
	case bool:
		secureBool = secure
	case nil:
		// do nothing
	default:
		return nil, fmt.Errorf("the secure parameter should be a boolean")
	}

	skipVerifyBool := false
	skipVerify := parameters["skipverify"]
	switch skipVerify := skipVerify.(type) {
	case string:
		b, err := strconv.ParseBool(skipVerify)
		if err != nil {
			return nil, fmt.Errorf("the skipVerify parameter should be a boolean")
		}
		skipVerifyBool = b
	case bool:
		skipVerifyBool = skipVerify
	case nil:
		// do nothing
	default:
		return nil, fmt.Errorf("the skipVerify parameter should be a boolean")
	}

	chunkSize, err := getParameterAsInteger(parameters, "chunksize", defaultChunkSize, minChunkSize, maxChunkSize)
	if err != nil {
		return nil, err
	}

	rootDirectory := parameters["rootdirectory"]
	if rootDirectory == nil {
		rootDirectory = ""
	}

	params := DriverParameters{
		SecretID:      fmt.Sprint(secretID),
		SecretKey:     fmt.Sprint(secretKey),
		Region:        fmt.Sprint(region),
		Bucket:        fmt.Sprint(bucket),
		AppID:         fmt.Sprint(appID),
		Secure:        secureBool,
		SkipVerify:    skipVerifyBool,
		ChunkSize:     chunkSize,
		RootDirectory: fmt.Sprint(rootDirectory),
	}

	return New(ctx, params)
}

type integer interface{ signed | unsigned }

type signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type unsigned interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

// getParameterAsInteger converts parameters[name] to T (using defaultValue if
// nil) and ensures it is in the range of min and max.
func getParameterAsInteger[T integer](parameters map[string]any, name string, defaultValue, min, max T) (T, error) {
	v := defaultValue
	if p := parameters[name]; p != nil {
		if _, err := fmt.Sscanf(fmt.Sprint(p), "%d", &v); err != nil {
			return 0, fmt.Errorf("%s parameter must be an integer, %v invalid", name, p)
		}
	}
	if v < min || v > max {
		return 0, fmt.Errorf("the %s %#v parameter should be a number between %d and %d (inclusive)", name, v, min, max)
	}
	return v, nil
}

// New constructs a new driver
func New(ctx context.Context, params DriverParameters) (*Driver, error) {
	// Build bucket URL
	var bucketURL string
	scheme := "https"
	if !params.Secure {
		scheme = "http"
	}

	bucketName := params.Bucket
	if params.AppID != "" && !strings.HasSuffix(bucketName, "-"+params.AppID) {
		bucketName = fmt.Sprintf("%s-%s", bucketName, params.AppID)
	}

	bucketURL = fmt.Sprintf("%s://%s.cos.%s.myqcloud.com", scheme, bucketName, params.Region)
	u, err := url.Parse(bucketURL)
	if err != nil {
		return nil, fmt.Errorf("invalid bucket URL: %v", err)
	}

	// Create COS client
	client := cos.NewClient(&cos.BaseURL{BucketURL: u}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  params.SecretID,
			SecretKey: params.SecretKey,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: params.SkipVerify},
			},
		},
	})

	d := &driver{
		Client:        client,
		Bucket:        bucketName,
		ChunkSize:     params.ChunkSize,
		RootDirectory: strings.TrimRight(params.RootDirectory, "/"),
		pool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, params.ChunkSize)
			},
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

func (d *driver) Name() string {
	return driverName
}

func (d *driver) GetContent(ctx context.Context, path string) ([]byte, error) {
	reader, err := d.Reader(ctx, path, 0)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

func (d *driver) PutContent(ctx context.Context, path string, contents []byte) error {
	writer, err := d.Writer(ctx, path, false)
	if err != nil {
		return err
	}
	defer writer.Close()

	_, err = writer.Write(contents)
	if err != nil {
		writer.Cancel(ctx)
		return err
	}

	return writer.Commit(ctx)
}

func (d *driver) Reader(ctx context.Context, path string, offset int64) (io.ReadCloser, error) {
	cosPath := d.cosPath(path)

	resp, err := d.Client.Object.Get(ctx, cosPath, &cos.ObjectGetOptions{
		Range: fmt.Sprintf("bytes=%d-", offset),
	})
	if err != nil {
		return nil, parseError(path, err)
	}

	return resp.Body, nil
}

func (d *driver) Writer(ctx context.Context, path string, append bool) (storagedriver.FileWriter, error) {
	cosPath := d.cosPath(path)

	if append {
		// Check if file exists to get current size
		head, err := d.Client.Object.Head(ctx, cosPath, nil)
		if err != nil {
			// File doesn't exist, start fresh
			return d.newWriter(ctx, cosPath, ""), nil
		}
		
		contentLength := head.Header.Get("Content-Length")
		size, _ := strconv.ParseInt(contentLength, 10, 64)
		if size > 0 {
			return nil, fmt.Errorf("cos driver does not support appending to existing objects")
		}
	}

	return d.newWriter(ctx, cosPath, ""), nil
}

func (d *driver) Stat(ctx context.Context, path string) (storagedriver.FileInfo, error) {
	cosPath := d.cosPath(path)

	// Try to get object metadata first
	head, err := d.Client.Object.Head(ctx, cosPath, nil)
	if err == nil {
		contentLength := head.Header.Get("Content-Length")
		size, _ := strconv.ParseInt(contentLength, 10, 64)
		
		lastModified := head.Header.Get("Last-Modified")
		modTime, _ := time.Parse(time.RFC1123, lastModified)

		return storagedriver.FileInfoInternal{
			FileInfoFields: storagedriver.FileInfoFields{
				Path:    path,
				Size:    size,
				ModTime: modTime,
				IsDir:   false,
			},
		}, nil
	}

	// If object doesn't exist, check if it's a directory by listing objects with prefix
	opt := &cos.BucketGetOptions{
		Prefix:  cosPath + "/",
		MaxKeys: 1,
	}

	result, _, err := d.Client.Bucket.Get(ctx, opt)
	if err != nil {
		return nil, parseError(path, err)
	}

	if len(result.Contents) > 0 {
		return storagedriver.FileInfoInternal{
			FileInfoFields: storagedriver.FileInfoFields{
				Path:    path,
				Size:    0,
				ModTime: time.Now(),
				IsDir:   true,
			},
		}, nil
	}

	return nil, storagedriver.PathNotFoundError{Path: path, DriverName: driverName}
}

func (d *driver) List(ctx context.Context, opath string) ([]string, error) {
	prefix := ""
	if opath != "/" {
		prefix = d.cosPath(opath) + "/"
	}

	opt := &cos.BucketGetOptions{
		Prefix:    prefix,
		Delimiter: "/",
		MaxKeys:   listMax,
	}

	var files []string
	var marker string

	for {
		if marker != "" {
			opt.Marker = marker
		}

		result, _, err := d.Client.Bucket.Get(ctx, opt)
		if err != nil {
			return nil, parseError(opath, err)
		}

		// Add files
		for _, obj := range result.Contents {
			name := obj.Key
			if prefix != "" {
				name = strings.TrimPrefix(name, prefix)
			}
			if name != "" {
				files = append(files, name)
			}
		}

		// Add directories
		for _, commonPrefix := range result.CommonPrefixes {
			name := strings.TrimPrefix(commonPrefix, prefix)
			name = strings.TrimSuffix(name, "/")
			if name != "" {
				files = append(files, name)
			}
		}

		if !result.IsTruncated {
			break
		}
		marker = result.NextMarker
	}

	return files, nil
}

func (d *driver) Move(ctx context.Context, sourcePath, destPath string) error {
	cosSourcePath := d.cosPath(sourcePath)
	cosDestPath := d.cosPath(destPath)

	// Copy object
	sourceURL := fmt.Sprintf("%s/%s", d.Bucket, cosSourcePath)
	_, _, err := d.Client.Object.Copy(ctx, cosDestPath, sourceURL, nil)
	if err != nil {
		return parseError(sourcePath, err)
	}

	// Delete source
	_, err = d.Client.Object.Delete(ctx, cosSourcePath)
	if err != nil {
		return parseError(sourcePath, err)
	}

	return nil
}

func (d *driver) Delete(ctx context.Context, path string) error {
	cosPath := d.cosPath(path)

	// Check if it's a directory by listing objects with prefix
	opt := &cos.BucketGetOptions{
		Prefix:  cosPath + "/",
		MaxKeys: listMax,
	}

	for {
		result, _, err := d.Client.Bucket.Get(ctx, opt)
		if err != nil {
			return parseError(path, err)
		}

		if len(result.Contents) == 0 {
			break
		}

		// Delete objects in batches
		var objectsToDelete []cos.Object
		for _, obj := range result.Contents {
			objectsToDelete = append(objectsToDelete, cos.Object{Key: obj.Key})
		}

		deleteOpt := &cos.ObjectDeleteMultiOptions{
			Objects: objectsToDelete,
		}

		_, _, err = d.Client.Object.DeleteMulti(ctx, deleteOpt)
		if err != nil {
			return parseError(path, err)
		}

		if !result.IsTruncated {
			break
		}
		opt.Marker = result.NextMarker
	}

	// Try to delete the object itself (in case it's a file)
	d.Client.Object.Delete(ctx, cosPath)

	return nil
}

func (d *driver) RedirectURL(r *http.Request, path string) (string, error) {
	return "", nil
}

func (d *driver) Walk(ctx context.Context, from string, f storagedriver.WalkFn, options ...func(*storagedriver.WalkOptions)) error {
	walkOptions := &storagedriver.WalkOptions{}
	for _, option := range options {
		option(walkOptions)
	}

	prefix := d.cosPath(from)
	if prefix == "" {
		prefix = ""
	} else if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	marker := walkOptions.StartAfterHint
	if marker != "" {
		marker = d.cosPath(marker)
	}

	opt := &cos.BucketGetOptions{
		Prefix:  prefix,
		MaxKeys: listMax,
		Marker:  marker,
	}

	for {
		result, _, err := d.Client.Bucket.Get(ctx, opt)
		if err != nil {
			return parseError(from, err)
		}

		for _, obj := range result.Contents {
			objectPath := obj.Key
			if prefix != "" {
				objectPath = strings.TrimPrefix(objectPath, prefix)
			}

			if objectPath == "" {
				continue
			}

			fullPath := filepath.Join(from, objectPath)
			modTime, _ := time.Parse(time.RFC3339, obj.LastModified)

			fileInfo := storagedriver.FileInfoInternal{
				FileInfoFields: storagedriver.FileInfoFields{
					Path:    fullPath,
					Size:    obj.Size,
					ModTime: modTime,
					IsDir:   false,
				},
			}

			if err := f(fileInfo); err != nil {
				return err
			}
		}

		if !result.IsTruncated {
			break
		}
		opt.Marker = result.NextMarker
	}

	return nil
}

func (d *driver) cosPath(path string) string {
	if d.RootDirectory == "" {
		return strings.TrimLeft(path, "/")
	}
	return strings.TrimLeft(d.RootDirectory+"/"+strings.TrimLeft(path, "/"), "/")
}

func parseError(path string, err error) error {
	if cosErr, ok := err.(*cos.ErrorResponse); ok {
		switch cosErr.Code {
		case "NoSuchKey", "NoSuchBucket":
			return storagedriver.PathNotFoundError{Path: path, DriverName: driverName}
		}
	}

	return storagedriver.Error{
		DriverName: driverName,
		Detail:     err,
	}
}

type writer struct {
	ctx       context.Context
	driver    *driver
	key       string
	uploadID  string
	parts     []cos.Object
	size      int64
	buf       *bytes.Buffer
	closed    bool
	committed bool
	cancelled bool
}

func (d *driver) newWriter(ctx context.Context, key, uploadID string) storagedriver.FileWriter {
	return &writer{
		ctx:      ctx,
		driver:   d,
		key:      key,
		uploadID: uploadID,
		buf:      &bytes.Buffer{},
	}
}

func (w *writer) Write(p []byte) (int, error) {
	if w.closed {
		return 0, fmt.Errorf("writer closed")
	}

	return w.buf.Write(p)
}

func (w *writer) Size() int64 {
	return w.size + int64(w.buf.Len())
}

func (w *writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return nil
}

func (w *writer) Cancel(ctx context.Context) error {
	if w.cancelled {
		return nil
	}
	w.cancelled = true

	if w.uploadID != "" {
		w.driver.Client.Object.AbortMultipartUpload(ctx, w.key, w.uploadID)
	}

	return nil
}

func (w *writer) Commit(ctx context.Context) error {
	if w.committed {
		return nil
	}
	if w.cancelled {
		return fmt.Errorf("writer cancelled")
	}

	w.committed = true

	if w.buf.Len() == 0 {
		// Empty file
		_, err := w.driver.Client.Object.Put(ctx, w.key, strings.NewReader(""), nil)
		return err
	}

	if w.uploadID == "" && w.Size() <= int64(w.driver.ChunkSize) {
		// Single part upload
		_, err := w.driver.Client.Object.Put(ctx, w.key, w.buf, nil)
		return err
	}

	// Multipart upload
	if w.uploadID == "" {
		result, _, err := w.driver.Client.Object.InitiateMultipartUpload(ctx, w.key, nil)
		if err != nil {
			return err
		}
		w.uploadID = result.UploadID
	}

	// Upload current buffer as a part
	if w.buf.Len() > 0 {
		partNumber := len(w.parts) + 1
		resp, err := w.driver.Client.Object.UploadPart(ctx, w.key, w.uploadID, partNumber, w.buf, nil)
		if err != nil {
			return err
		}

		w.parts = append(w.parts, cos.Object{
			ETag: resp.Header.Get("ETag"),
		})
	}

	// Complete multipart upload
	completeOpt := &cos.CompleteMultipartUploadOptions{}
	for i, part := range w.parts {
		completeOpt.Parts = append(completeOpt.Parts, cos.Object{
			PartNumber: i + 1,
			ETag:       part.ETag,
		})
	}

	_, _, err := w.driver.Client.Object.CompleteMultipartUpload(ctx, w.key, w.uploadID, completeOpt)
	return err
}