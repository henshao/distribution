---
description: Explains how to use the Tencent Cloud COS storage driver
keywords: registry, service, driver, images, storage, cos, tencentcloud, qcloud
title: Tencent Cloud COS storage driver
---

# Tencent Cloud COS storage driver

This storage backend uses [Tencent Cloud COS (Cloud Object Storage)](https://intl.cloud.tencent.com/product/cos) for object storage.

## Parameters

| Parameter      | Required | Description |
|:---------------|:---------|:------------|
| `secretid`     | yes      | Your Tencent Cloud SecretId for the COS service. |
| `secretkey`    | yes      | Your Tencent Cloud SecretKey for the COS service. |
| `region`       | yes      | The COS region where your bucket is located. For example `ap-guangzhou` for Guangzhou region. |
| `bucket`       | yes      | The name of your COS bucket where you wish to store objects. The bucket must exist prior to the driver initialization. |
| `rootdirectory`| no       | The root directory tree in which all registry files are stored. Defaults to the empty string (bucket root). |
| `chunksize`    | no       | The COS API requires multipart upload chunks to be at least 5MB. This parameter specifies the default chunk size to use for upload. The default value is 16MB. The minimum value is 5MB, and the maximum value is 100MB. |
| `maxconcurrency` | no     | The maximum number of concurrent operations. Default is 10. |
| `secure`       | no       | Whether to use HTTPS or HTTP. Default is true (HTTPS). Set to false for HTTP. |

## Example Configuration

The following is an example configuration for the COS storage driver:

```yaml
storage:
  cos:
    secretid: your-secret-id
    secretkey: your-secret-key
    region: ap-guangzhou
    bucket: registry-bucket
    rootdirectory: /registry
    chunksize: 16777216  # 16MB
    maxconcurrency: 10
    secure: true
```

## Creating a Bucket

Before using the COS storage driver, you need to create a bucket in the Tencent Cloud Console:

1. Log in to the [Tencent Cloud Console](https://console.cloud.tencent.com/)
2. Go to the COS service page
3. Click "Create Bucket"
4. Choose a unique bucket name and select the region
5. Configure the bucket permissions according to your needs

## IAM Permissions

When using Tencent Cloud COS, it's recommended to create a dedicated sub-account with minimal required permissions. The following permissions are required for the registry:

- `cos:PutObject` - Upload objects
- `cos:GetObject` - Download objects
- `cos:DeleteObject` - Delete objects
- `cos:ListBucket` - List objects in bucket
- `cos:InitiateMultipartUpload` - Start multipart upload
- `cos:UploadPart` - Upload part
- `cos:CompleteMultipartUpload` - Complete multipart upload
- `cos:AbortMultipartUpload` - Abort multipart upload

## Limitations

- COS does not support true append operations for existing objects. The driver returns an error for append operations on existing objects.
- Large file uploads use multipart upload with configurable chunk sizes.
- The driver uses presigned URLs for redirect operations when supported by the client.

## Cross-Region Replication

Tencent Cloud COS supports cross-region replication. You can configure this feature in the COS console to replicate your registry data to other regions for disaster recovery.

## Storage Classes

By default, objects are stored using the STANDARD storage class. You can configure lifecycle policies in the COS console to transition objects to other storage classes (STANDARD_IA, ARCHIVE) based on access patterns to optimize costs.