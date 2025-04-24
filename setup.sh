#!/bin/bash

cd "$(dirname "$0")"

export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1 AWS_ENDPOINT_URL=http://localhost:4566 AWS_PAGER=""

cd lambda/
GOOS=linux GOARCH=amd64 go build -o ./bootstrap ./main.go
zip -0 ./handler.zip ./bootstrap
cd ../

aws lambda get-function --function-name handler --output text --query Configuration.FunctionArn

if [ "$?" -eq 0 ]; then

aws lambda update-function-code \
  --function-name handler \
  --zip-file fileb://lambda/handler.zip \
  --publish

else

aws lambda create-function \
  --function-name handler \
  --runtime provided.al2023 \
  --role arn:aws:iam::000000000000:role/my-app \
  --handler bootstrap \
  --zip-file fileb://lambda/handler.zip

fi

aws lambda wait function-active-v2 --function-name handler

aws s3 cp lambda/testdata/private-key s3://config/private.key

aws lambda update-function-configuration --function-name handler \
  --environment 'Variables={APT_BASE_DIR="",APT_DISTRIBUTION=mackerel,APT_ORIGIN=mackerel,APT_LABEL=mackerel,APT_SUITE=mackerel,APT_CODENAME=mackerel,APT_COMPONENTS=contrib,APT_DESCRIPTION="mackerel repository for Debian",APT_S3BUCKET="repository",APT_PRIVATE_KEY_S3URL="s3://config/private.key",APT_LOCK_KEY_S3URL="s3://config/lockfile"}' \
  --timeout 120

aws s3api put-bucket-notification-configuration \
    --bucket incoming \
    --notification-configuration '{"LambdaFunctionConfigurations":[{"LambdaFunctionArn":"arn:aws:lambda:us-east-1:000000000000:function:handler","Events":["s3:ObjectCreated:Put"]}]}'

