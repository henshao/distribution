# Tencent Cloud COS Storage Driver

This directory contains the Tencent Cloud COS (Cloud Object Storage) storage driver implementation for Docker Distribution Registry.

## About

This storage driver is implemented using the [official Tencent Cloud COS Go SDK](https://github.com/tencentyun/cos-go-sdk-v5) and provides a backend for storing Docker registry data in Tencent Cloud COS.

## Configuration Parameters

The following parameters are supported in the registry configuration:

### Required Parameters

- `secretid`: Your Tencent Cloud API Secret ID
- `secretkey`: Your Tencent Cloud API Secret Key  
- `region`: The COS region (e.g., `ap-beijing`, `ap-shanghai`, `ap-guangzhou`)
- `bucket`: The COS bucket name

### Optional Parameters

- `appid`: Your Tencent Cloud APPID (will be automatically appended to bucket name if not present)
- `secure`: Use HTTPS for connections (default: `true`)
- `skipverify`: Skip SSL certificate verification (default: `false`)
- `chunksize`: Size for multipart uploads in bytes (default: `5242880` = 5MB)
- `rootdirectory`: Root directory in the bucket for storing registry data (default: empty)

## Example Configuration

```yaml
storage:
  cos:
    secretid: your-secret-id
    secretkey: your-secret-key
    region: ap-beijing
    bucket: your-registry-bucket
    appid: "1234567890"
    chunksize: 5242880
    rootdirectory: "/registry"
    secure: true
    skipverify: false
```

## Features

- Full implementation of the Docker Distribution storage driver interface
- Support for multipart uploads for large files
- Efficient file operations (read, write, delete, move)
- Directory listing and walking functionality
- Compatible with all standard Docker registry operations

## Requirements

- Valid Tencent Cloud account with COS access
- COS bucket created in the desired region
- Proper IAM permissions for the Secret ID/Key pair

### Required COS Permissions

Your Secret ID/Key should have at least the following permissions on the target bucket:

- `cos:PutObject`
- `cos:GetObject` 
- `cos:DeleteObject`
- `cos:GetBucket`
- `cos:PutObjectACL` (if using custom ACLs)

## Usage

1. Import the driver in your application:
   ```go
   import _ "github.com/distribution/distribution/v3/registry/storage/driver/cos"
   ```

2. Configure the registry to use the `cos` storage driver with the appropriate parameters.

3. The driver will automatically register itself with the storage driver factory.

## Testing

Basic unit tests are provided. To run the tests:

```bash
go test ./registry/storage/driver/cos/
```

For integration testing with a real COS bucket, you would need to set up the appropriate environment variables and credentials.

## Notes

- The driver automatically handles bucket name formatting with APPID if needed
- Multipart uploads are used for files larger than the configured chunk size
- The driver supports both HTTP and HTTPS connections
- All paths are stored relative to the configured root directory
- The driver implements standard Docker registry storage semantics