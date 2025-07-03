package cos

import (
	"context"
	"os"
	"testing"

	storagedriver "github.com/distribution/distribution/v3/registry/storage/driver"
	"github.com/distribution/distribution/v3/registry/storage/driver/testsuites"
	"gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { check.TestingT(t) }

var cosDriverConstructor func() (*Driver, error)

var skipCheck func() string

func init() {
	secretID := os.Getenv("COS_SECRET_ID")
	secretKey := os.Getenv("COS_SECRET_KEY")
	bucket := os.Getenv("COS_BUCKET")
	region := os.Getenv("COS_REGION")
	secure := os.Getenv("COS_SECURE")
	root := os.Getenv("COS_ROOT")

	cosDriverConstructor = func() (*Driver, error) {
		parameters := map[string]interface{}{
			"secretid":  secretID,
			"secretkey": secretKey,
			"region":    region,
			"bucket":    bucket,
		}

		if secure != "" {
			parameters["secure"] = secure
		}

		if root != "" {
			parameters["rootdirectory"] = root
		}

		return FromParameters(context.Background(), parameters)
	}

	// Skip COS tests if environment variables aren't set
	skipCheck = func() string {
		if secretID == "" || secretKey == "" || bucket == "" || region == "" {
			return "Must set COS_SECRET_ID, COS_SECRET_KEY, COS_BUCKET, and COS_REGION to run COS tests"
		}
		return ""
	}

	testsuites.RegisterSuite(func() (storagedriver.StorageDriver, error) {
		return cosDriverConstructor()
	}, skipCheck)
}

func TestEmptyRootList(t *testing.T) {
	if skipCheck() != "" {
		t.Skip(skipCheck())
	}

	driver, err := cosDriverConstructor()
	if err != nil {
		t.Fatalf("unexpected error creating driver: %v", err)
	}
	defer driver.Delete(context.Background(), "/")

	// This test verifies that List("/") returns an empty list when the root is empty
	contents, err := driver.List(context.Background(), "/")
	if err != nil {
		t.Fatalf("unexpected error listing empty root: %v", err)
	}
	if len(contents) > 0 {
		t.Fatalf("expected empty list, got %d items", len(contents))
	}
}

func TestParameterValidation(t *testing.T) {
	tests := []struct {
		name       string
		parameters map[string]interface{}
		expectErr  bool
		errorMsg   string
	}{
		{
			name:       "missing secretid",
			parameters: map[string]interface{}{"secretkey": "key", "region": "region", "bucket": "bucket"},
			expectErr:  true,
			errorMsg:   "no secretid parameter provided",
		},
		{
			name:       "missing secretkey",
			parameters: map[string]interface{}{"secretid": "id", "region": "region", "bucket": "bucket"},
			expectErr:  true,
			errorMsg:   "no secretkey parameter provided",
		},
		{
			name:       "missing region",
			parameters: map[string]interface{}{"secretid": "id", "secretkey": "key", "bucket": "bucket"},
			expectErr:  true,
			errorMsg:   "no region parameter provided",
		},
		{
			name:       "missing bucket",
			parameters: map[string]interface{}{"secretid": "id", "secretkey": "key", "region": "region"},
			expectErr:  true,
			errorMsg:   "no bucket parameter provided",
		},
		{
			name:       "invalid chunksize",
			parameters: map[string]interface{}{"secretid": "id", "secretkey": "key", "region": "region", "bucket": "bucket", "chunksize": "not-a-number"},
			expectErr:  true,
			errorMsg:   "chunksize parameter must be an integer",
		},
		{
			name:       "chunksize too small",
			parameters: map[string]interface{}{"secretid": "id", "secretkey": "key", "region": "region", "bucket": "bucket", "chunksize": "1"},
			expectErr:  true,
			errorMsg:   "chunksize 1 must be at least",
		},
		{
			name:       "invalid secure",
			parameters: map[string]interface{}{"secretid": "id", "secretkey": "key", "region": "region", "bucket": "bucket", "secure": "not-a-bool"},
			expectErr:  true,
			errorMsg:   "secure parameter must be a boolean",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseParameters(test.parameters)
			if test.expectErr {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if test.errorMsg != "" && err.Error() != test.errorMsg && !contains(err.Error(), test.errorMsg) {
					t.Errorf("expected error containing %q, got %q", test.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr
}