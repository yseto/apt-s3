package lock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-lambda-go/lambdacontext"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/cenkalti/backoff/v5"
)

type lockState struct {
	AwsRequestID string `json:"AwsRequestID"`
}

func fromContext(ctx context.Context) (*lambdacontext.LambdaContext, bool) {
	lc, ok := lambdacontext.FromContext(ctx)
	return lc, ok
}

type Lock struct {
	s3Client    *s3.Client
	bucket, key string
}

func New(s3Client *s3.Client, s3url string) (*Lock, error) {
	u, err := url.Parse(s3url)
	if err != nil {
		return nil, err
	}

	return &Lock{
		s3Client: s3Client,
		bucket:   u.Host,
		key:      strings.TrimPrefix(u.Path, "/"),
	}, nil
}

func (l *Lock) GetLock(ctx context.Context) (err error) {
	lc, ok := fromContext(ctx)
	if !ok {
		return errors.New("can not get lambda context")
	}

	b, err := json.Marshal(lockState{AwsRequestID: lc.AwsRequestID})
	if err != nil {
		return
	}

	input := &s3.PutObjectInput{
		ContentType: aws.String("application/json"),
		Body:        bytes.NewReader(b),
		Bucket:      aws.String(l.bucket),
		Key:         aws.String(l.key),
		IfNoneMatch: aws.String("*"),
	}

	putObject := func() (string, error) {
		_, err = l.s3Client.PutObject(ctx, input)
		if err != nil {
			fmt.Println("retry", err)
		}
		return "", err
	}

	_, err = backoff.Retry(ctx, putObject, backoff.WithBackOff(backoff.NewExponentialBackOff()), backoff.WithMaxTries(100))

	return
}

func (l *Lock) UnLock(ctx context.Context) (err error) {
	state, err := l.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(l.bucket),
		Key:    aws.String(l.key),
	})
	if err != nil {
		return
	}
	defer state.Body.Close()

	data, err := io.ReadAll(state.Body)
	if err != nil {
		return
	}

	info := &lockState{}
	if err = json.Unmarshal(data, info); err != nil {
		return
	}

	lc, ok := fromContext(ctx)
	if !ok {
		return errors.New("can not get lambda context")
	}

	if info.AwsRequestID != lc.AwsRequestID {
		return fmt.Errorf("lock ID does not match")
	}

	_, err = l.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(l.bucket),
		Key:    aws.String(l.key),
	})
	return
}
