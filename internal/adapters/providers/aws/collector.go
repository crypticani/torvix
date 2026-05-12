package aws

import (
	"context"
	"time"

	"github.com/crypticani/cloudpulse/internal/config"
	common "github.com/crypticani/cloudpulse/internal/adapters/providers"
	"github.com/crypticani/cloudpulse/internal/domain"
)

type Collector struct {
	cfg config.Provider
}

func New(cfg config.Provider) *Collector {
	return &Collector{cfg: cfg}
}

func (c *Collector) Name() string { return "aws" }

func (c *Collector) Collect(ctx context.Context, since time.Time) ([]domain.RawBillingRecord, error) {
	_ = ctx
	_ = since
	return common.Sample(domain.ProviderAWS, c.cfg.Account, "AmazonEC2"), nil
}
