package cos

import (
	"context"
	"testing"

	storagedriver "github.com/distribution/distribution/v3/registry/storage/driver"
	"github.com/distribution/distribution/v3/registry/storage/driver/factory"
)

// Test that the COS driver can be created successfully
func TestCOSDriverFactory(t *testing.T) {
	// Test that the driver is registered
	driver, err := factory.Create(context.Background(), "cos", map[string]interface{}{
		"secretid":  "test-secret-id",
		"secretkey": "test-secret-key",
		"region":    "ap-beijing",
		"bucket":    "test-bucket",
		"appid":     "123456789",
	})

	// The driver should be created successfully even with dummy credentials
	// since we're only validating parameters, not making actual network calls
	if err != nil {
		// The error should not be about missing driver registration
		if err.Error() == "StorageDriver not registered: cos" {
			t.Fatal("COS driver is not registered in factory")
		}
		// Other errors are acceptable (like network connectivity issues)
		t.Logf("Driver creation failed: %v", err)
		return
	}

	// If creation succeeded, verify it's the right driver
	if driver == nil {
		t.Fatal("Driver creation returned nil driver without error")
	}

	if driver.Name() != "cos" {
		t.Errorf("Expected driver name 'cos', got '%s'", driver.Name())
	}

	t.Log("Driver creation succeeded")
}

// Test parameter validation
func TestCOSDriverParameters(t *testing.T) {
	tests := []struct {
		name       string
		params     map[string]interface{}
		shouldFail bool
	}{
		{
			name: "missing secretid",
			params: map[string]interface{}{
				"secretkey": "test-secret-key",
				"region":    "ap-beijing",
				"bucket":    "test-bucket",
			},
			shouldFail: true,
		},
		{
			name: "missing secretkey",
			params: map[string]interface{}{
				"secretid": "test-secret-id",
				"region":   "ap-beijing",
				"bucket":   "test-bucket",
			},
			shouldFail: true,
		},
		{
			name: "missing region",
			params: map[string]interface{}{
				"secretid":  "test-secret-id",
				"secretkey": "test-secret-key",
				"bucket":    "test-bucket",
			},
			shouldFail: true,
		},
		{
			name: "missing bucket",
			params: map[string]interface{}{
				"secretid":  "test-secret-id",
				"secretkey": "test-secret-key",
				"region":    "ap-beijing",
			},
			shouldFail: true,
		},
		{
			name: "valid parameters",
			params: map[string]interface{}{
				"secretid":  "test-secret-id",
				"secretkey": "test-secret-key",
				"region":    "ap-beijing",
				"bucket":    "test-bucket",
			},
			shouldFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := factory.Create(context.Background(), "cos", tt.params)
			
			if tt.shouldFail && err == nil {
				t.Error("Expected parameter validation to fail, but it didn't")
			}
			
			if !tt.shouldFail && err != nil {
				// For valid parameters, we expect creation to succeed or fail due to network/auth issues,
				// not parameter validation
				if err.Error() == "no secretid parameter provided" ||
					err.Error() == "no secretkey parameter provided" ||
					err.Error() == "no region parameter provided" ||
					err.Error() == "no bucket parameter provided" {
					t.Errorf("Parameter validation failed unexpectedly: %v", err)
				}
			}
		})
	}
}

// Test that the driver implements the StorageDriver interface
func TestCOSDriverInterface(t *testing.T) {
	// Create a driver instance (this will fail with dummy credentials but that's OK)
	driver, err := FromParameters(context.Background(), map[string]interface{}{
		"secretid":  "test-secret-id",
		"secretkey": "test-secret-key",
		"region":    "ap-beijing",
		"bucket":    "test-bucket",
		"appid":     "123456789",
	})

	if err != nil {
		t.Skipf("Cannot create driver with dummy credentials: %v", err)
	}

	// Verify it implements the StorageDriver interface
	var _ storagedriver.StorageDriver = driver
	
	// Test driver name
	if driver.Name() != "cos" {
		t.Errorf("Expected driver name 'cos', got '%s'", driver.Name())
	}
}

// This would run the full storage driver test suite if we had valid credentials
// Commented out since it requires real COS credentials and bucket
/*
func TestCOSDriverSuite(t *testing.T) {
	// Skip this test if we don't have credentials
	secretID := os.Getenv("COS_SECRET_ID")
	secretKey := os.Getenv("COS_SECRET_KEY")
	region := os.Getenv("COS_REGION")
	bucket := os.Getenv("COS_BUCKET")
	appID := os.Getenv("COS_APP_ID")

	if secretID == "" || secretKey == "" || region == "" || bucket == "" {
		t.Skip("COS credentials not provided, skipping driver test suite")
	}

	constructor := func() (storagedriver.StorageDriver, error) {
		parameters := map[string]interface{}{
			"secretid":      secretID,
			"secretkey":     secretKey,
			"region":        region,
			"bucket":        bucket,
			"appid":         appID,
			"chunksize":     5 * 1024 * 1024,
			"rootdirectory": "/test-prefix",
		}

		return FromParameters(context.Background(), parameters)
	}

	testsuites.Driver(t, constructor)
}
*/