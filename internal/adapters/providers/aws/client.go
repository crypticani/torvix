package aws

import (
	"context"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/crypticani/torvix/internal/config"
)

type CostExplorerClient interface {
	GetCostAndUsage(ctx context.Context, input *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
}

type S3Client interface {
	ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(ctx context.Context, input *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

func NewCostExplorerClient(ctx context.Context, cfg config.AWSProvider) (CostExplorerClient, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.WithDefaults().Region))
	if err != nil {
		return nil, err
	}
	return costexplorer.NewFromConfig(awsCfg), nil
}

func NewS3Client(ctx context.Context, cfg config.AWSProvider) (S3Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.WithDefaults().CURRegion))
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(awsCfg), nil
}
