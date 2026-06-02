package rules

import (
	"math"
	"testing"
	"time"

	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/waste"
)

func TestEvaluateOCIDetachedBlockVolume(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	created := now.AddDate(0, 0, -21)
	cfg := testConfig()
	result := EvaluateOCI(EvaluationInput{
		Config: cfg,
		Now:    now,
		Resources: []waste.Resource{{
			Provider:           domain.ProviderOCI,
			ResourceID:         "volume-1",
			ResourceName:       "dev-unused-volume",
			ResourceType:       waste.ResourceBlockVolume,
			Region:             "ap-mumbai-1",
			ScopeName:          "dev",
			LifecycleState:     "AVAILABLE",
			AvailabilityDomain: "AD-1",
			TimeCreated:        &created,
			Raw:                map[string]any{"volume_size_gb": 100},
		}},
		Costs: map[string]waste.CostSignal{
			"volume-1": {Last7dCost: 14, Last30dCost: 42, HasLast7d: true, HasLast30d: true, Currency: "USD"},
		},
	})

	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Findings))
	}
	finding := result.Findings[0]
	if finding.RuleID != waste.RuleOCIDetachedBlockVolume {
		t.Fatalf("expected rule %s, got %s", waste.RuleOCIDetachedBlockVolume, finding.RuleID)
	}
	if finding.Severity != "high" {
		t.Fatalf("expected high severity, got %s", finding.Severity)
	}
	if finding.Confidence != 0.95 {
		t.Fatalf("expected confidence 0.95, got %.2f", finding.Confidence)
	}
	if finding.EstimatedMonthlyWaste == nil || math.Abs(*finding.EstimatedMonthlyWaste-60) > 0.001 {
		t.Fatalf("expected estimated waste 60, got %v", finding.EstimatedMonthlyWaste)
	}
	if finding.Evidence["attachment_count"] != 0 {
		t.Fatalf("expected attachment_count=0, got %v", finding.Evidence["attachment_count"])
	}
}

func TestEvaluateOCIAttachedBlockVolumeIsNotFinding(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	created := now.AddDate(0, 0, -21)
	result := EvaluateOCI(EvaluationInput{
		Config: testConfig(),
		Now:    now,
		Resources: []waste.Resource{{
			Provider:       domain.ProviderOCI,
			ResourceID:     "volume-1",
			ResourceType:   waste.ResourceBlockVolume,
			LifecycleState: "AVAILABLE",
			TimeCreated:    &created,
		}},
		Relationships: []waste.Relationship{{
			Provider:         domain.ProviderOCI,
			SourceResourceID: "volume-1",
			TargetResourceID: "instance-1",
			RelationshipType: waste.RelationshipBlockVolumeAttachedToInstance,
		}},
		Costs: map[string]waste.CostSignal{
			"volume-1": {Last7dCost: 14, HasLast7d: true},
		},
	})
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(result.Findings))
	}
}

func TestEvaluateOCIExclusionTagSkipsResource(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	created := now.AddDate(0, 0, -21)
	result := EvaluateOCI(EvaluationInput{
		Config: testConfig(),
		Now:    now,
		Resources: []waste.Resource{{
			Provider:       domain.ProviderOCI,
			ResourceID:     "volume-1",
			ResourceType:   waste.ResourceBlockVolume,
			LifecycleState: "AVAILABLE",
			TimeCreated:    &created,
			Tags:           map[string]string{"torvix:waste-ignore": "true"},
		}},
		Costs: map[string]waste.CostSignal{"volume-1": {Last7dCost: 14, HasLast7d: true}},
	})
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(result.Findings))
	}
	if result.Skipped != 1 {
		t.Fatalf("expected 1 skipped resource, got %d", result.Skipped)
	}
}

func TestEvaluateOCIStoppedComputeWithPaidStorage(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	instanceCreated := now.AddDate(0, 0, -6)
	volumeCreated := now.AddDate(0, 0, -20)
	result := EvaluateOCI(EvaluationInput{
		Config: testConfig(),
		Now:    now,
		Resources: []waste.Resource{
			{
				Provider:       domain.ProviderOCI,
				ResourceID:     "instance-1",
				ResourceName:   "stopped-dev",
				ResourceType:   waste.ResourceComputeInstance,
				LifecycleState: "STOPPED",
				TimeCreated:    &instanceCreated,
			},
			{
				Provider:       domain.ProviderOCI,
				ResourceID:     "boot-volume-1",
				ResourceType:   waste.ResourceBootVolume,
				LifecycleState: "AVAILABLE",
				TimeCreated:    &volumeCreated,
			},
		},
		Relationships: []waste.Relationship{{
			Provider:         domain.ProviderOCI,
			SourceResourceID: "boot-volume-1",
			TargetResourceID: "instance-1",
			RelationshipType: waste.RelationshipBootVolumeAttachedToInstance,
		}},
		Costs: map[string]waste.CostSignal{
			"boot-volume-1": {Last7dCost: 7, HasLast7d: true, Currency: "USD"},
		},
	})

	if len(result.Findings) != 1 {
		t.Fatalf("expected only stopped compute finding, got %d", len(result.Findings))
	}
	var stopped waste.Finding
	for _, finding := range result.Findings {
		if finding.RuleID == waste.RuleOCIStoppedComputePaidStorage {
			stopped = finding
		}
	}
	if stopped.RuleID == "" {
		t.Fatal("expected stopped compute finding")
	}
	if stopped.Confidence != 0.85 {
		t.Fatalf("expected confidence 0.85, got %.2f", stopped.Confidence)
	}
	if stopped.EstimatedMonthlyWaste == nil || math.Abs(*stopped.EstimatedMonthlyWaste-30) > 0.001 {
		t.Fatalf("expected estimated waste 30, got %v", stopped.EstimatedMonthlyWaste)
	}
}

func TestEvaluateOCIUnusedReservedPublicIP(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	created := now.AddDate(0, 0, -14)
	result := EvaluateOCI(EvaluationInput{
		Config: testConfig(),
		Now:    now,
		Resources: []waste.Resource{{
			Provider:       domain.ProviderOCI,
			ResourceID:     "public-ip-1",
			ResourceName:   "unused-public-ip",
			ResourceType:   waste.ResourceReservedPublicIP,
			LifecycleState: "AVAILABLE",
			TimeCreated:    &created,
		}},
		Costs: map[string]waste.CostSignal{
			"public-ip-1": {Last30dCost: 5, HasLast30d: true, Currency: "USD"},
		},
	})
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(result.Findings))
	}
	if result.Findings[0].RuleID != waste.RuleOCIUnusedReservedPublicIP {
		t.Fatalf("expected public IP rule, got %s", result.Findings[0].RuleID)
	}
	if result.Findings[0].Confidence != 0.90 {
		t.Fatalf("expected confidence 0.90, got %.2f", result.Findings[0].Confidence)
	}
}

func TestEvaluateOCIHighConfidenceNoCostStillFindsDetachedVolume(t *testing.T) {
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	created := now.AddDate(0, 0, -21)
	result := EvaluateOCI(EvaluationInput{
		Config: testConfig(),
		Now:    now,
		Resources: []waste.Resource{{
			Provider:       domain.ProviderOCI,
			ResourceID:     "volume-1",
			ResourceType:   waste.ResourceBlockVolume,
			LifecycleState: "AVAILABLE",
			TimeCreated:    &created,
		}},
	})
	if len(result.Findings) != 1 {
		t.Fatalf("expected finding without cost, got %d", len(result.Findings))
	}
	if result.Findings[0].Confidence != 0.85 {
		t.Fatalf("expected confidence 0.85, got %.2f", result.Findings[0].Confidence)
	}
	if result.Findings[0].EstimatedMonthlyWaste != nil {
		t.Fatalf("expected nil estimate, got %v", *result.Findings[0].EstimatedMonthlyWaste)
	}
}

func testConfig() waste.Config {
	return waste.Config{
		Provider:               domain.ProviderOCI,
		MinResourceAgeDays:     7,
		StoppedInstanceMinDays: 3,
		MinCostThreshold:       0,
		HighMonthlyThreshold:   50,
		Currency:               "USD",
		EnableTagExclusions:    true,
		ExclusionTagKeys:       []string{"torvix:ignore", "torvix:waste-ignore", "finops:ignore", "keep", "retain", "do-not-delete"},
	}
}
