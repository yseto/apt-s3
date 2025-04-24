package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/yseto/apt-s3/lambda/config"
	"github.com/yseto/apt-s3/lambda/lock"
	"github.com/yseto/apt-s3/lambda/packages"
	"github.com/yseto/apt-s3/lambda/release"
	"github.com/yseto/apt-s3/lambda/sign"
	"github.com/yseto/apt-s3/lambda/storage"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	awsS3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

func main() {
	lambda.Start(handler)
}

func handler(ctx context.Context, event events.S3Event) (err error) {
	cfg, err := awsConfig.LoadDefaultConfig(ctx)
	if err != nil {
		return
	}

	aptConfig, err := config.Load()
	if err != nil {
		return
	}

	s3client := awsS3.NewFromConfig(cfg, func(o *awsS3.Options) {
		o.UsePathStyle = true
		// clientLogMode := aws.LogRetries | aws.LogRequest | aws.LogResponse
		// o.ClientLogMode = clientLogMode
	})

	privKey, err := sign.ReadKey(ctx, s3client, aptConfig.PrivateKeyS3Url)
	if err != nil {
		return
	}

	lockHandler, err := lock.New(s3client, aptConfig.LockKeyS3Url)
	if err != nil {
		return
	}

	if err = lockHandler.GetLock(ctx); err != nil {
		return
	}
	defer func() {
		errL := lockHandler.UnLock(ctx)
		if errL != nil {
			err = errL
		}
	}()

	if len(event.Records) > 0 {
		for _, record := range event.Records {
			err := taskOfFile(ctx, s3client, aptConfig, record.S3.Bucket.Name, record.S3.Object.Key)
			if err != nil {
				return err
			}
		}
	} else {
		err = reGenerate(ctx, s3client, aptConfig)
		if err != nil {
			return
		}
	}

	return sign.Do(ctx, s3client, aptConfig, privKey)
}

func taskOfFile(ctx context.Context, s3client *awsS3.Client, aptConfig config.Config, bucket, key string) error {
	filename, err := storage.Download(ctx, s3client, bucket, key)
	if err != nil {
		return err
	}
	defer os.Remove(filename)

	s3 := &storage.S3{
		BucketName: aptConfig.DestS3Bucket,
		S3Client:   s3client,
	}

	process, duplicate, err := processFile(ctx, aptConfig, s3, filename, bucket, key)
	if err != nil {
		return err
	}

	if duplicate {
		return nil
	}

	return processPackages(ctx, aptConfig, s3, process, false)
}

type packagesLoad struct {
	ControlWithStat, CPU string
}

func processFile(ctx context.Context, aptConfig config.Config, fs storage.Impl, filename, bucket, key string) (p packagesLoad, duplicate bool, err error) {
	fd, err := os.Open(filename)
	if err != nil {
		return
	}
	defer fd.Close()

	r, err := packages.Load(fd, aptConfig.Components, filepath.Base(key))
	if err != nil {
		return
	}

	// set property
	p.CPU = r.CPU
	p.ControlWithStat = r.ControlWithStat

	// copy deb package
	destPath := filepath.Join(aptConfig.BaseDir, r.DestPath)
	if fs.ExistFile(ctx, destPath) {
		fmt.Printf("duplicated file: %s\n", key)
		duplicate = true
		err = nil
		return
	}

	err = fs.CopyFile(ctx, destPath, storage.Source{Bucket: bucket, Key: key})

	return
}

// generate `Packages`
func processPackages(ctx context.Context, aptConfig config.Config, fs storage.Impl, p packagesLoad, overwrite bool) (err error) {
	re := regexp.MustCompile(".*/binary-(.*)/Packages.*")

	// dists/$DIST/$COMP/binary-$ARCH/Packages
	packagePath := filepath.Join(aptConfig.DirName(), fmt.Sprintf("binary-%s", p.CPU), "Packages")

	appendWrite := fs.ExistFile(ctx, packagePath)
	if overwrite {
		appendWrite = false
	}

	data := []byte(p.ControlWithStat)
	if appendWrite {
		var b []byte
		b, err = fs.ReadFile(ctx, packagePath)
		if err != nil {
			return
		}

		data = bytes.Join([][]byte{b, data}, []byte("\r\n"))
	}

	if err = fs.WriteFile(ctx, packagePath, data); err != nil {
		return
	}

	buf := bytes.NewBuffer([]byte{})
	gw := gzip.NewWriter(buf)
	gw.Write(data)
	err = gw.Close()
	if err != nil {
		return err
	}

	err = fs.WriteFile(ctx, packagePath+".gz", buf.Bytes())
	if err != nil {
		return err
	}

	filePaths, err := fs.FindPackages(ctx, aptConfig.BaseDir)
	if err != nil {
		return err
	}

	var md5Sum, sha1Sum, sha256Sum []release.Hash

	var archs = make(map[string]bool, 0)

	for i := range filePaths {
		var (
			md5hash    = md5.New()
			sha1hash   = sha1.New()
			sha256hash = sha256.New()
		)

		path, err := filepath.Rel(aptConfig.DistributionDirName(), filePaths[i])
		if err != nil {
			return err
		}

		if res := re.FindStringSubmatch(path); len(res) == 2 {
			archs[res[1]] = true
		}

		fd, err := fs.ReadFile(ctx, filePaths[i])
		if err != nil {
			return err
		}

		size, err := io.Copy(io.MultiWriter(md5hash, sha1hash, sha256hash), bytes.NewBuffer(fd))
		if err != nil {
			return err
		}

		md5Sum = append(md5Sum, release.Hash{
			Hash:     hex.EncodeToString(md5hash.Sum(nil)),
			Size:     size,
			Filename: path,
		})
		sha1Sum = append(sha1Sum, release.Hash{
			Hash:     hex.EncodeToString(sha1hash.Sum(nil)),
			Size:     size,
			Filename: path,
		})
		sha256Sum = append(sha256Sum, release.Hash{
			Hash:     hex.EncodeToString(sha256hash.Sum(nil)),
			Size:     size,
			Filename: path,
		})
	}

	rel, err := release.Generate(release.Release{
		Origin:        aptConfig.Origin,
		Label:         aptConfig.Label,
		Suite:         aptConfig.Suite,
		CodeName:      aptConfig.CodeName,
		Date:          time.Now().Format(time.RFC1123),
		Architectures: slices.Sorted(maps.Keys(archs)),
		Components:    aptConfig.Components,
		Description:   aptConfig.Description,
		MD5Sum:        md5Sum,
		SHA1:          sha1Sum,
		SHA256:        sha256Sum,
	})
	if err != nil {
		return err
	}

	err = fs.WriteFile(ctx, filepath.Join(aptConfig.DistributionDirName(), "Release"), []byte(rel))
	if err != nil {
		return err
	}

	return nil
}

func reGenerate(ctx context.Context, s3client *awsS3.Client, aptConfig config.Config) error {
	s3 := &storage.S3{
		BucketName: aptConfig.DestS3Bucket,
		S3Client:   s3client,
	}

	list, err := s3.FindDeb(ctx, aptConfig.BaseDir)
	if err != nil {
		return err
	}

	re := regexp.MustCompile(".*/?pool/(.*?)") // components

	var info = make(map[string][]string, 0)

	slices.Sort(list)

	for _, path := range list {
		var components string
		if res := re.FindStringSubmatch(path); len(res) == 2 {
			components = res[1]
		} else {
			continue
		}

		filename, err := storage.Download(ctx, s3client, aptConfig.DestS3Bucket, path)
		if err != nil {
			return err
		}
		defer os.Remove(filename)

		fd, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer fd.Close()

		r, err := packages.Load(fd, components, filepath.Base(path))
		if err != nil {
			return err
		}

		info[r.CPU] = append(info[r.CPU], r.ControlWithStat)
	}

	for cpu, contents := range info {
		control := strings.Join(contents, "\r\n")

		err = processPackages(ctx, aptConfig, s3, packagesLoad{CPU: cpu, ControlWithStat: control}, true)
		if err != nil {
			return err
		}
	}

	return nil
}
