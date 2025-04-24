package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Impl interface {
	ExistFile(ctx context.Context, name string) bool
	FindDeb(ctx context.Context, root string) (findList []string, err error)
	FindPackages(ctx context.Context, root string) (findList []string, err error)
	ReadFile(ctx context.Context, name string) ([]byte, error)
	WriteFile(ctx context.Context, name string, data []byte) error
	CopyFile(ctx context.Context, key string, source Source) error
}

type S3 struct {
	S3Client   *s3.Client
	BucketName string
}

func (s *S3) WriteFile(ctx context.Context, name string, data []byte) error {
	_, err := s.S3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.BucketName),
		Key:    aws.String(name),
		Body:   bytes.NewReader(data),
	})
	return err
}

type Source struct {
	Bucket, Key string
}

func (s *S3) CopyFile(ctx context.Context, key string, source Source) error {
	_, err := s.S3Client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(s.BucketName),
		CopySource: aws.String(fmt.Sprintf("%v/%v", source.Bucket, source.Key)),
		Key:        aws.String(key),
	})

	return err
}

func (s *S3) ExistFile(ctx context.Context, name string) bool {
	_, err := s.S3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.BucketName),
		Key:    aws.String(name),
	})
	if err != nil {
		var noKey *types.NoSuchKey
		if errors.As(err, &noKey) {
			return false
		}
		return false // TODO
	}
	return true
}

func (s *S3) ReadFile(ctx context.Context, name string) ([]byte, error) {
	result, err := s.S3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.BucketName),
		Key:    aws.String(name),
	})
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	return io.ReadAll(result.Body)
}

func (s *S3) findKeys(ctx context.Context, prefix string, fn func(key string) bool) (findList []string, err error) {
	objectPaginator := s3.NewListObjectsV2Paginator(s.S3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.BucketName),
		Prefix: aws.String(prefix),
	})
	for objectPaginator.HasMorePages() {
		output, err := objectPaginator.NextPage(ctx)
		if err != nil {
			var noBucket *types.NoSuchBucket
			if errors.As(err, &noBucket) {
				return nil, noBucket
			}
			break
		}

		for _, content := range output.Contents {
			if fn(*content.Key) {
				findList = append(findList, *content.Key)
			}
		}
	}

	return
}

func (s *S3) FindPackages(ctx context.Context, root string) (findList []string, err error) {
	return s.findKeys(ctx, filepath.Join(root, "dists"), func(key string) bool {
		return strings.HasPrefix(filepath.Base(key), "Packages")
	})
}

func (s *S3) FindDeb(ctx context.Context, root string) (findList []string, err error) {
	return s.findKeys(ctx, filepath.Join(root, "pool"), func(key string) bool {
		return strings.HasSuffix(filepath.Base(key), ".deb")
	})
}

func Download(ctx context.Context, s3Client *s3.Client, bucket, key string) (fn string, err error) {
	f, err := os.CreateTemp("", "temp*")
	if err != nil {
		return
	}
	fn = f.Name()

	downloader := manager.NewDownloader(s3Client)
	_, err = downloader.Download(ctx, f, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return
	}
	err = f.Close()
	if err != nil {
		return
	}

	return
}
