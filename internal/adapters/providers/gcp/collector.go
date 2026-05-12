package gcp

import (
	"context"
	"time"

	common "github.com/crypticani/cloudpulse/internal/adapters/providers"
	"github.com/crypticani/cloudpulse/internal/config"
	"github.com/crypticani/cloudpulse/internal/ports/providers"
)

type Collector struct {
	cfg config.Provider
}

func New(cfg config.Provider) *Collector {
	return &Collector{cfg: cfg}
}

func (c *Collector) Name() string { return "gcp" }

func (c *Collector) Collect(ctx context.Context, since time.Time) (providers.CollectResult, error) {
	_ = ctx
	_ = since
	return common.Sample("gcp", c.cfg.Account, "Compute Engine"), nil
}
