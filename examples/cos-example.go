// Example demonstrating how to use the Tencent Cloud COS storage driver
package main

import (
	"context"
	"fmt"
	"log"

	_ "github.com/distribution/distribution/v3/registry/storage/driver/cos"
	"github.com/distribution/distribution/v3/registry/storage/driver/factory"
)

func main() {
	// COS driver parameters
	parameters := map[string]interface{}{
		"secretid":       "your-secret-id",
		"secretkey":      "your-secret-key",
		"region":         "ap-guangzhou",
		"bucket":         "your-bucket-name",
		"rootdirectory":  "/registry",
		"chunksize":      16777216, // 16MB
		"maxconcurrency": 10,
		"secure":         true,
	}

	// Create the COS storage driver
	driver, err := factory.Create(context.Background(), "cos", parameters)
	if err != nil {
		log.Fatalf("Failed to create COS driver: %v", err)
	}

	fmt.Printf("Successfully created COS storage driver: %s\n", driver.Name())

	// Example: Write content
	ctx := context.Background()
	path := "/test/example.txt"
	content := []byte("Hello from Tencent Cloud COS!")

	err = driver.PutContent(ctx, path, content)
	if err != nil {
		log.Fatalf("Failed to write content: %v", err)
	}
	fmt.Printf("Successfully wrote content to %s\n", path)

	// Example: Read content
	data, err := driver.GetContent(ctx, path)
	if err != nil {
		log.Fatalf("Failed to read content: %v", err)
	}
	fmt.Printf("Read content: %s\n", string(data))

	// Example: Get file info
	info, err := driver.Stat(ctx, path)
	if err != nil {
		log.Fatalf("Failed to stat file: %v", err)
	}
	fmt.Printf("File info - Path: %s, Size: %d, ModTime: %v\n",
		info.Path(), info.Size(), info.ModTime())

	// Example: List files
	files, err := driver.List(ctx, "/test")
	if err != nil {
		log.Fatalf("Failed to list files: %v", err)
	}
	fmt.Printf("Files in /test: %v\n", files)

	// Example: Delete file
	err = driver.Delete(ctx, path)
	if err != nil {
		log.Fatalf("Failed to delete file: %v", err)
	}
	fmt.Printf("Successfully deleted %s\n", path)
}