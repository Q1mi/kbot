// Package objstore 封装审计冷分区归档与导出使用的 MinIO/S3 对象存储。
package objstore

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client 是 MinIO 客户端的薄封装。
type Client struct {
	mc     *minio.Client
	bucket string
}

// New 按 endpoint(http(s)://host:port)+ 凭证创建客户端,并确保 bucket 存在。
func New(ctx context.Context, endpoint, accessKey, secretKey, bucket string) (*Client, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("objstore: 空 endpoint")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	secure := u.Scheme == "https"
	host := u.Host
	if host == "" {
		host = strings.TrimPrefix(endpoint, "http://")
	}

	mc, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("minio new: %w", err)
	}
	c := &Client{mc: mc, bucket: bucket}
	if err := c.ensureBucket(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) ensureBucket(ctx context.Context) error {
	ok, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("bucket exists: %w", err)
	}
	if !ok {
		if err := c.mc.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("make bucket: %w", err)
		}
	}
	return nil
}

// Put 上传一个对象。
func (c *Client) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	return nil
}

// PresignedGet 返回一个有时效的下载 URL。
func (c *Client) PresignedGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := c.mc.PresignedGetObject(ctx, c.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign %s: %w", key, err)
	}
	return u.String(), nil
}

// Bucket 返回桶名。
func (c *Client) Bucket() string { return c.bucket }
