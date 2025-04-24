package main

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/localstack"
	"github.com/testcontainers/testcontainers-go/network"
)

type helper struct {
	localstackUrl, s3wwwUrl string

	container testcontainers.Container
}

func localstackHelper(t *testing.T) helper {
	t.Helper()

	ctx := t.Context()

	mynet, _ := network.New(ctx)
	testcontainers.CleanupNetwork(t, mynet)

	localstackContainer, _ := localstack.Run(
		ctx,
		"localstack/localstack:4.3",
		testcontainers.WithEnv(map[string]string{"SERVICES": "s3,lambda"}),
		network.WithNetwork([]string{mynet.Name}, mynet),
		testcontainers.WithReuseByName("localstack"),
		testcontainers.WithFiles(
			testcontainers.ContainerFile{
				HostFilePath:      "handler.zip",
				ContainerFilePath: "/tmp/function.zip",
			},
			testcontainers.ContainerFile{
				HostFilePath:      "testdata/private-key",
				ContainerFilePath: "/tmp/private-key",
			},
		),
	)
	testcontainers.CleanupContainer(t, localstackContainer)
	localstackPort, err := localstackContainer.MappedPort(ctx, nat.Port("4566/tcp"))
	if err != nil {
		t.Fatal(err)
	}

	lambdaName := "handler"

	lambdaCommands := [][]string{
		{"awslocal", "s3", "mb", "s3://incoming"},
		{"awslocal", "s3", "mb", "s3://repository"},
		{"awslocal", "s3", "mb", "s3://config"},
		{"awslocal", "s3", "cp", "/tmp/private-key", "s3://config/private.key"},
		{
			"awslocal", "lambda", "create-function",
			"--function-name", lambdaName,
			"--runtime", "provided.al2023",
			"--role", "arn:aws:iam::000000000000:role/lambda-role",
			"--zip-file", "fileb:///tmp/function.zip",
			"--handler", "bootstrap",
		},
		{"awslocal", "lambda", "wait", "function-active-v2", "--function-name", lambdaName},
		{
			"awslocal", "lambda", "update-function-configuration",
			"--function-name", lambdaName,
			"--environment", `Variables={APT_BASE_DIR="",APT_DISTRIBUTION=mackerel,APT_ORIGIN=mackerel,APT_LABEL=mackerel,APT_SUITE=mackerel,APT_CODENAME=mackerel,APT_COMPONENTS=contrib,APT_DESCRIPTION="mackerel repository for Debian",APT_S3BUCKET="repository",APT_PRIVATE_KEY_S3URL="s3://config/private.key",APT_LOCK_KEY_S3URL="s3://config/lockfile"}`,
			"--timeout", "120",
		},
		{
			"awslocal", "s3api", "put-bucket-notification-configuration",
			"--bucket", "incoming",
			"--notification-configuration", `{"LambdaFunctionConfigurations":[{"LambdaFunctionArn":"arn:aws:lambda:us-east-1:000000000000:function:handler","Events":["s3:ObjectCreated:Put"]}]}`,
		},
	}
	for _, cmd := range lambdaCommands {
		_, _, err := localstackContainer.Exec(ctx, cmd)
		if err != nil {
			t.Fatalf("failed to execute command %v: %s", cmd, err)
		}
	}

	s3wwwGcr := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "y4m4/s3www:latest",
			ExposedPorts: []string{"8080/tcp"},
			Networks:     []string{mynet.Name},
			Cmd: []string{
				"-endpoint", "http://localstack:4566",
				"-accessKey", "DUMMY",
				"-secretKey", "DUMMY",
				"-bucket", "repository",
				"-address", "0.0.0.0:8080",
			},
			Name: "s3www",
		},
		Started: true,
	}
	s3wwwContainer, _ := testcontainers.GenericContainer(ctx, s3wwwGcr)
	testcontainers.CleanupContainer(t, s3wwwContainer)
	s3wwwPort, _ := s3wwwContainer.MappedPort(ctx, nat.Port("8080/tcp"))

	debianGcr := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:    "debian:bookworm-slim",
			Networks: []string{mynet.Name},
			Cmd:      []string{"/bin/bash", "-c", "while true; do sleep 1 ;done"},
			Files: []testcontainers.ContainerFile{
				{
					HostFilePath:      "testdata/public-key",
					ContainerFilePath: "/tmp/tester.asc",
				},
				{
					HostFilePath:      "testdata/tester.list",
					ContainerFilePath: "/tmp/tester.list",
				},
			},
		},
		Started: true,
	}
	debContainer, _ := testcontainers.GenericContainer(ctx, debianGcr)
	testcontainers.CleanupContainer(t, debContainer)

	return helper{
		localstackUrl: "http://localhost" + ":" + localstackPort.Port(),
		s3wwwUrl:      "http://localhost" + ":" + s3wwwPort.Port(),

		container: debContainer,
	}
}

func Test_main(t *testing.T) {
	ctx := t.Context()
	e := localstackHelper(t)

	t.Log(e.localstackUrl)

	awsCfg, _ := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("DUMMY", "DUMMY", "")),
		config.WithRegion("us-east-1"),
	)
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = &e.localstackUrl
	})

	t.Run("upload", func(t *testing.T) {
		fd, err := os.Open("testdata/mkr_0.60.0-1.v2_amd64.deb")
		if err != nil {
			t.Fatal(err)
		}
		defer fd.Close()

		_, err = client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String("incoming"),
			Key:    aws.String("mkr_0.60.0-1.v2_amd64.deb"),
			Body:   fd,
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	time.Sleep(5 * time.Second)

	t.Run("check InRelease", func(t *testing.T) {
		resp, err := http.Get(e.s3wwwUrl + "/dists/mackerel/InRelease")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		content := string(b)
		// t.Log(content)

		if !strings.Contains(content, "contrib/binary-amd64/Packages") {
			t.Fatal()
		}
	})

	t.Run("check Packages", func(t *testing.T) {
		resp, err := http.Get(e.s3wwwUrl + "/dists/mackerel/contrib/binary-amd64/Packages")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		content := string(b)
		// t.Log(content)

		if !strings.Contains(content, "Package: mkr") {
			t.Fatal()
		}
		if !strings.Contains(content, "Version: 0.60.0-1.v2") {
			t.Fatal()
		}
	})

	t.Run("add apt repository", func(t *testing.T) {
		cmds := [][]string{
			{"apt-get", "update"},
			{"apt-get", "install", "--no-install-recommends", "-y", "gpg"},

			{"gpg", "--dearmor", "-o", "/etc/apt/keyrings/tester.gpg", "/tmp/tester.asc"},

			{"cp", "/tmp/tester.list", "/etc/apt/sources.list.d/tester.list"},

			{"apt-get", "update"},
		}

		for _, cmd := range cmds {
			_, _, err := e.container.Exec(ctx, cmd)
			if err != nil {
				t.Fatalf("failed to execute command %v: %s", cmd, err)
			}
		}
	})

	t.Run("read apt repository", func(t *testing.T) {
		cmd := []string{"apt", "list", "mkr", "-a"}
		_, out, err := e.container.Exec(ctx, cmd)
		if err != nil {
			t.Fatalf("failed to execute command %v: %s", cmd, err)
		}

		res, err := io.ReadAll(out)
		if err != nil {
			t.Fatal(err)
		}
		content := string(res)
		t.Log(content)

		if !strings.Contains(content, "mkr/mackerel") {
			t.Fatal()
		}
		if !strings.Contains(content, "0.60.0-1.v2") {
			t.Fatal()
		}
	})

	t.Run("upload 2", func(t *testing.T) {
		fd, err := os.Open("testdata/mkr_0.59.2-1.v2_amd64.deb")
		if err != nil {
			t.Fatal(err)
		}
		defer fd.Close()

		_, err = client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String("incoming"),
			Key:    aws.String("mkr_0.59.2-1.v2_amd64.deb"),
			Body:   fd,
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	time.Sleep(5 * time.Second)

	t.Run("apt-get update", func(t *testing.T) {
		cmd := []string{"apt-get", "update"}
		_, _, err := e.container.Exec(ctx, cmd)
		if err != nil {
			t.Fatalf("failed to execute command %v: %s", cmd, err)
		}
	})

	t.Run("read apt repository one more", func(t *testing.T) {
		cmd := []string{"apt", "list", "mkr", "-a"}
		_, out, err := e.container.Exec(ctx, cmd)
		if err != nil {
			t.Fatalf("failed to execute command %v: %s", cmd, err)
		}

		res, err := io.ReadAll(out)
		if err != nil {
			t.Fatal(err)
		}
		content := string(res)
		t.Log(content)

		if !strings.Contains(content, "mkr/mackerel") {
			t.Fatal()
		}
		if !strings.Contains(content, "0.60.0-1.v2") {
			t.Fatal()
		}
		if !strings.Contains(content, "0.59.2-1.v2") {
			t.Fatal()
		}
	})
}
