package aws

import (
	"context"
	"log/slog"

	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/waste"
)

type WasteStub struct {
	logger *slog.Logger
}

func NewWasteStub(logger *slog.Logger) *WasteStub {
	if logger == nil {
		logger = slog.Default()
	}
	return &WasteStub{logger: logger}
}

func (s *WasteStub) Provider() domain.Provider {
	return domain.ProviderAWS
}

func (s *WasteStub) Sync(context.Context) (waste.InventoryResult, error) {
	message := "AWS waste detection is not implemented in Phase 1. Cost Explorer support remains available."
	s.logger.Info(message, "provider", "aws")
	return waste.InventoryResult{Provider: domain.ProviderAWS, Skipped: true, Message: message}, nil
}

// TODO: Phase 2 AWS waste detection should add EC2 stopped/idle instances,
// unattached EBS volumes, unused Elastic IPs, idle load balancers, idle RDS
// databases, CloudWatch utilization metrics, tag-based exclusions, and
// multi-account/region scanning.
