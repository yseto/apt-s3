package sign

import (
	"context"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/yseto/apt-s3/lambda/config"
	"github.com/yseto/apt-s3/lambda/storage"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
)

func ReadKey(ctx context.Context, s3Client *s3.Client, s3url string) (*crypto.Key, error) {
	u, err := url.Parse(s3url)
	if err != nil {
		return nil, err
	}

	o, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(u.Host),
		Key:    aws.String(strings.TrimPrefix(u.Path, "/")),
	})
	if err != nil {
		return nil, err
	}
	defer o.Body.Close()

	b, err := io.ReadAll(o.Body)
	if err != nil {
		return nil, err
	}
	return crypto.NewPrivateKeyFromArmored(string(b), []byte{})
}

func Do(ctx context.Context, s3client *s3.Client, aptConfig config.Config, privKey *crypto.Key) error {
	s3 := &storage.S3{
		BucketName: aptConfig.DestS3Bucket,
		S3Client:   s3client,
	}
	distributionDirName := aptConfig.DistributionDirName()

	b, err := s3.ReadFile(ctx, filepath.Join(distributionDirName, "Release"))
	if err != nil {
		return err
	}

	pgp := crypto.PGP()
	signer, err := pgp.Sign().SigningKey(privKey).New()
	if err != nil {
		return err
	}

	// InRelease
	cleartextArmored, err := signer.SignCleartext(b)
	if err != nil {
		return err
	}

	if err := s3.WriteFile(ctx, filepath.Join(distributionDirName, "InRelease"), cleartextArmored); err != nil {
		return err
	}

	// Release.gpg
	signature, err := signer.Sign(b, crypto.Armor)
	if err != nil {
		return err
	}

	if err := s3.WriteFile(ctx, filepath.Join(distributionDirName, "Release.gpg"), signature); err != nil {
		return err
	}

	return nil
}
