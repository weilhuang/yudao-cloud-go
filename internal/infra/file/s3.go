package file

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// s3Client 走 S3 协议，覆盖七牛、阿里云、腾讯云、MinIO。公开桶返回自定义域名，私有桶返回预签名地址。
type s3Client struct {
	api     *s3.Client
	presign *s3.PresignClient
	cfg     ClientConfig
}

func newS3(cfg ClientConfig, httpClient *http.Client) (*s3Client, error) {
	normalized, err := normalizeS3(cfg)
	if err != nil {
		return nil, err
	}
	options := s3.Options{
		Region:       normalized.Region,
		BaseEndpoint: aws.String(endpointURL(normalized.Endpoint)),
		Credentials:  credentials.NewStaticCredentialsProvider(normalized.AccessKey, normalized.AccessSecret, ""),
		UsePathStyle: normalized.EnablePathStyleAccess,
		// 七牛不接受 AWS 默认的分块校验和，只在协议强制要求时才计算。
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
	}
	if httpClient != nil {
		options.HTTPClient = httpClient
	}
	api := s3.New(options)
	return &s3Client{api: api, presign: s3.NewPresignClient(api), cfg: normalized}, nil
}

func normalizeS3(cfg ClientConfig) (ClientConfig, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.AccessSecret == "" {
		return cfg, &Error{Code: 400, Msg: "S3 的 endpoint、bucket、密钥不能为空"}
	}
	if strings.Contains(cfg.Endpoint, "qiniucs.com") && strings.TrimSpace(cfg.Domain) == "" {
		return cfg, &Error{Code: 400, Msg: "七牛存储器的 domain 不能为空"}
	}
	if cfg.Region == "" {
		cfg.Region = resolveRegion(cfg.Endpoint)
	}
	if cfg.Domain == "" {
		cfg.Domain = buildDomain(cfg)
	}
	return cfg, nil
}

func (c *s3Client) Upload(ctx context.Context, content []byte, objectPath, contentType string) (string, error) {
	input := &s3.PutObjectInput{
		Bucket:        aws.String(c.cfg.Bucket),
		Key:           aws.String(objectPath),
		Body:          bytes.NewReader(content),
		ContentLength: aws.Int64(int64(len(content))),
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	if _, err := c.api.PutObject(ctx, input); err != nil {
		return "", err
	}
	return c.PresignGet(ctx, objectPath, 0)
}

func (c *s3Client) Delete(ctx context.Context, objectPath string) error {
	_, err := c.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.cfg.Bucket), Key: aws.String(objectPath)})
	return err
}

func (c *s3Client) Get(ctx context.Context, objectPath string) ([]byte, error) {
	out, err := c.api.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.cfg.Bucket), Key: aws.String(objectPath)})
	if err != nil {
		var re *smithyhttp.ResponseError
		if errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (c *s3Client) PresignPut(ctx context.Context, objectPath string) (string, error) {
	out, err := c.presign.PresignPutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(c.cfg.Bucket), Key: aws.String(objectPath)}, func(o *s3.PresignOptions) {
		o.Expires = 24 * time.Hour
	})
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

func (c *s3Client) PresignGet(ctx context.Context, objectPath string, seconds int) (string, error) {
	if c.cfg.EnablePublicAccess == nil || *c.cfg.EnablePublicAccess {
		return publicVisitURL(c.cfg, objectPath), nil
	}
	expire := 24 * time.Hour
	if seconds > 0 {
		expire = time.Duration(seconds) * time.Second
	}
	out, err := c.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.cfg.Bucket), Key: aws.String(objectPath)}, func(o *s3.PresignOptions) {
		o.Expires = expire
	})
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

func publicVisitURL(cfg ClientConfig, objectPath string) string {
	domain := cfg.Domain
	if domain == "" {
		domain = buildDomain(cfg)
	}
	return joinDomain(domain, encodeObjectPath(objectPath))
}

func buildDomain(cfg ClientConfig) string {
	endpoint := cfg.Endpoint
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return joinDomain(endpoint, cfg.Bucket)
	}
	return "https://" + cfg.Bucket + "." + endpoint
}

func endpointURL(endpoint string) string {
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return endpoint
	}
	return "https://" + endpoint
}

func resolveRegion(endpoint string) string {
	host := endpoint
	if strings.Contains(host, "://") {
		if i := strings.Index(host, "://"); i >= 0 {
			host = host[i+3:]
		}
	}
	if slash := strings.Index(host, "/"); slash >= 0 {
		host = host[:slash]
	}
	if strings.Contains(host, "amazonaws.com") && strings.HasPrefix(host, "s3.") {
		region := strings.TrimSuffix(strings.TrimPrefix(host, "s3."), ".amazonaws.com")
		if region != "" && region != "accelerate" {
			return region
		}
	}
	if strings.HasPrefix(host, "oss-") && strings.Contains(host, "aliyuncs.com") {
		return strings.TrimSuffix(strings.TrimPrefix(host, "oss-"), ".aliyuncs.com")
	}
	if strings.HasPrefix(host, "cos.") && strings.Contains(host, "myqcloud.com") {
		return strings.TrimSuffix(strings.TrimPrefix(host, "cos."), ".myqcloud.com")
	}
	return "us-east-1"
}
