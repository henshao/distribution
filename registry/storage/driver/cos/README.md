# Tencent Cloud COS Storage Driver

This storage driver uses [Tencent Cloud COS (Cloud Object Storage)](https://intl.cloud.tencent.com/product/cos) for object storage in the Docker Registry.

## Configuration

To use the COS storage driver, you need to configure the following parameters in your registry configuration:

```yaml
storage:
  cos:
    secretid: your-tencent-cloud-secret-id
    secretkey: your-tencent-cloud-secret-key
    region: ap-guangzhou
    bucket: your-cos-bucket-name
    rootdirectory: /registry  # optional
    chunksize: 16777216      # optional, default 16MB
    maxconcurrency: 10       # optional, default 10
    secure: true            # optional, default true (HTTPS)
```

## Required Parameters

- **secretid**: Your Tencent Cloud SecretId for API authentication
- **secretkey**: Your Tencent Cloud SecretKey for API authentication
- **region**: The COS region where your bucket is located (e.g., ap-guangzhou, ap-shanghai, ap-beijing)
- **bucket**: The name of your COS bucket

## Optional Parameters

- **rootdirectory**: The root directory within the bucket where registry files will be stored. Defaults to the bucket root.
- **chunksize**: The size of chunks for multipart uploads. Must be between 5MB and 100MB. Default is 16MB.
- **maxconcurrency**: Maximum number of concurrent operations. Default is 10.
- **secure**: Whether to use HTTPS (true) or HTTP (false). Default is true.

## Creating a COS Bucket

Before using this driver, you need to create a COS bucket:

1. Log in to the [Tencent Cloud Console](https://console.cloud.tencent.com/)
2. Navigate to the COS service
3. Click "Create Bucket"
4. Choose a unique bucket name and select the appropriate region
5. Configure access permissions as needed

## Getting Credentials

To obtain your SecretId and SecretKey:

1. Go to the [CAM Console](https://console.cloud.tencent.com/cam/capi)
2. Create a new API key or use an existing one
3. Note down the SecretId and SecretKey

## Running the Registry

Once configured, you can run the registry with:

```bash
docker run -d -p 5000:5000 \
  -v /path/to/config.yml:/etc/docker/registry/config.yml \
  --name registry \
  registry:2
```

## Environment Variables

You can also configure the driver using environment variables:

```bash
export REGISTRY_STORAGE=cos
export REGISTRY_STORAGE_COS_SECRETID=your-secret-id
export REGISTRY_STORAGE_COS_SECRETKEY=your-secret-key
export REGISTRY_STORAGE_COS_REGION=ap-guangzhou
export REGISTRY_STORAGE_COS_BUCKET=your-bucket-name
```

## Testing

To run the COS driver tests, set the following environment variables:

```bash
export COS_SECRET_ID=your-secret-id
export COS_SECRET_KEY=your-secret-key
export COS_BUCKET=test-bucket
export COS_REGION=ap-guangzhou
export COS_SECURE=true  # optional

go test ./registry/storage/driver/cos -v
```

## Limitations

- The driver does not support true append operations for existing objects. Attempting to append to an existing object will return an error.
- Large file uploads use multipart upload with configurable chunk sizes.
- The driver supports presigned URLs for redirect operations.

## Performance Considerations

- Adjust `chunksize` based on your network conditions and file sizes
- Increase `maxconcurrency` for better performance with many small files
- Use lifecycle policies in COS to transition old data to cheaper storage classes