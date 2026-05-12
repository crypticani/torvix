package oci

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/objectstorage"

	"github.com/crypticani/cloudpulse/internal/config"
)

type ObjectInfo struct {
	Name         string
	ETag         string
	Size         int64
	LastModified time.Time
}

type ObjectStorageClient interface {
	GetNamespace(ctx context.Context) (string, error)
	ListObjects(ctx context.Context, namespace, bucket, prefix string, limit int) ([]ObjectInfo, error)
	GetObject(ctx context.Context, namespace, bucket, objectName string) (io.ReadCloser, error)
}

type SDKObjectStorageClient struct {
	client      objectstorage.ObjectStorageClient
	compartment string
}

func NewObjectStorageClient(cfg config.Provider) (*SDKObjectStorageClient, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("oci bucket is required")
	}
	provider, err := configurationProvider(cfg)
	if err != nil {
		return nil, err
	}
	client, err := objectstorage.NewObjectStorageClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("create object storage client: %w", err)
	}
	retryPolicy := common.DefaultRetryPolicy()
	client.SetCustomClientConfiguration(common.CustomClientConfiguration{
		RetryPolicy: &retryPolicy,
	})
	return &SDKObjectStorageClient{client: client, compartment: cfg.Account}, nil
}

func configurationProvider(cfg config.Provider) (common.ConfigurationProvider, error) {
	switch {
	case cfg.ConfigFile != "" && cfg.ConfigProfile != "":
		return common.ConfigurationProviderFromFileWithProfile(cfg.ConfigFile, cfg.ConfigProfile, cfg.Passphrase)
	case cfg.ConfigFile != "":
		return common.ConfigurationProviderFromFile(cfg.ConfigFile, cfg.Passphrase)
	default:
		return common.DefaultConfigProvider(), nil
	}
}

func (c *SDKObjectStorageClient) GetNamespace(ctx context.Context) (string, error) {
	retryPolicy := common.DefaultRetryPolicy()
	req := objectstorage.GetNamespaceRequest{
		RequestMetadata: common.RequestMetadata{RetryPolicy: &retryPolicy},
	}
	if strings.HasPrefix(strings.TrimSpace(c.compartment), "ocid1.") {
		req.CompartmentId = common.String(c.compartment)
	}
	resp, err := c.client.GetNamespace(ctx, req)
	if err != nil {
		return "", fmt.Errorf("get namespace: %w", err)
	}
	if resp.Value == nil || strings.TrimSpace(*resp.Value) == "" {
		return "", fmt.Errorf("empty namespace returned by OCI")
	}
	return *resp.Value, nil
}

func (c *SDKObjectStorageClient) ListObjects(ctx context.Context, namespace, bucket, prefix string, limit int) ([]ObjectInfo, error) {
	fields := "name,etag,size,timeModified"
	startAfter := ""
	var out []ObjectInfo
	for {
		req := objectstorage.ListObjectsRequest{
			NamespaceName: common.String(namespace),
			BucketName:    common.String(bucket),
			Prefix:        common.String(prefix),
			Fields:        common.String(fields),
			StartAfter:    nil,
		}
		if startAfter != "" {
			req.StartAfter = common.String(startAfter)
		}
		resp, err := c.client.ListObjects(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("list objects: %w", err)
		}
		if len(resp.Objects) == 0 {
			break
		}
		for _, item := range resp.Objects {
			info := ObjectInfo{}
			if item.Name != nil {
				info.Name = *item.Name
			}
			if item.Etag != nil {
				info.ETag = *item.Etag
			}
			if item.Size != nil {
				info.Size = *item.Size
			}
			if item.TimeModified != nil {
				info.LastModified = item.TimeModified.Time
			}
			out = append(out, info)
			startAfter = info.Name
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
		if len(resp.Objects) == 0 {
			break
		}
	}
	return out, nil
}

func (c *SDKObjectStorageClient) GetObject(ctx context.Context, namespace, bucket, objectName string) (io.ReadCloser, error) {
	resp, err := c.client.GetObject(ctx, objectstorage.GetObjectRequest{
		NamespaceName: common.String(namespace),
		BucketName:    common.String(bucket),
		ObjectName:    common.String(objectName),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %s: %w", objectName, err)
	}
	return resp.Content, nil
}
