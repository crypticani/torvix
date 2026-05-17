package gcp

import (
	"context"
	"fmt"
	"time"

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

func (c *Collector) Collect(_ context.Context, _ time.Time) (providers.CollectResult, error) {
	return providers.CollectResult{}, fmt.Errorf("gcp collector: not implemented — disable this provider or contribute a real implementation")
}
