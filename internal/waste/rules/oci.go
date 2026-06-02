package rules

import (
	"math"
	"strings"
	"time"

	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/waste"
)

type EvaluationInput struct {
	Config        waste.Config
	Now           time.Time
	Resources     []waste.Resource
	Relationships []waste.Relationship
	Costs         map[string]waste.CostSignal
}

type EvaluationResult struct {
	Findings []waste.Finding
	Skipped  int
}

func EvaluateOCI(input EvaluationInput) EvaluationResult {
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cfg := input.Config
	if cfg.Provider == "" {
		cfg.Provider = domain.ProviderOCI
	}
	byID := make(map[string]waste.Resource, len(input.Resources))
	for _, resource := range input.Resources {
		if resource.Provider != "" && resource.Provider != domain.ProviderOCI {
			continue
		}
		byID[resource.ResourceID] = resource
	}
	relationshipsBySource := relationshipsBySource(input.Relationships)
	relationshipsByTarget := relationshipsByTarget(input.Relationships)

	var result EvaluationResult
	for _, resource := range input.Resources {
		if resource.Provider != "" && resource.Provider != domain.ProviderOCI {
			continue
		}
		if hasExclusionTag(resource, cfg) {
			result.Skipped++
			continue
		}
		switch resource.ResourceType {
		case waste.ResourceBlockVolume:
			if finding, ok := detachedVolumeFinding(resource, cfg, now, input.Costs[resource.ResourceID], relationshipsBySource[resource.ResourceID], waste.RuleOCIDetachedBlockVolume, "Block Storage", "Detached block volume is possibly still generating cost.", "Review the detached block volume. If no backup or restore requirement exists, delete it after owner approval."); ok {
				result.Findings = append(result.Findings, finding)
			}
		case waste.ResourceBootVolume:
			if finding, ok := detachedVolumeFinding(resource, cfg, now, input.Costs[resource.ResourceID], relationshipsBySource[resource.ResourceID], waste.RuleOCIDetachedBootVolume, "Block Storage", "Detached boot volume is possibly still generating cost.", "Review the detached boot volume. If the instance is no longer required and no restore requirement exists, delete it after approval."); ok {
				result.Findings = append(result.Findings, finding)
			}
		case waste.ResourceComputeInstance:
			if finding, ok := stoppedComputeFinding(resource, cfg, now, input.Costs, relationshipsByTarget[resource.ResourceID], byID); ok {
				result.Findings = append(result.Findings, finding)
			}
		case waste.ResourcePublicIP, waste.ResourceReservedPublicIP:
			if finding, ok := unusedReservedPublicIPFinding(resource, cfg, now, input.Costs[resource.ResourceID], relationshipsBySource[resource.ResourceID]); ok {
				result.Findings = append(result.Findings, finding)
			}
		}
	}
	return result
}

func RuleCatalog() []waste.RuleInfo {
	return []waste.RuleInfo{
		{
			RuleID:         waste.RuleOCIDetachedBlockVolume,
			Provider:       string(domain.ProviderOCI),
			ResourceType:   waste.ResourceBlockVolume,
			SeverityBasis:  "estimated monthly waste",
			Description:    "Detects active OCI block volumes with no instance attachment.",
			Recommendation: "Review the detached block volume and delete it after owner approval if no restore requirement exists.",
		},
		{
			RuleID:         waste.RuleOCIDetachedBootVolume,
			Provider:       string(domain.ProviderOCI),
			ResourceType:   waste.ResourceBootVolume,
			SeverityBasis:  "estimated monthly waste",
			Description:    "Detects active OCI boot volumes with no instance attachment.",
			Recommendation: "Review the detached boot volume and delete it after approval if no restore requirement exists.",
		},
		{
			RuleID:         waste.RuleOCIStoppedComputePaidStorage,
			Provider:       string(domain.ProviderOCI),
			ResourceType:   waste.ResourceComputeInstance,
			SeverityBasis:  "attached storage cost",
			Description:    "Detects stopped OCI compute instances whose attached storage may still generate cost.",
			Recommendation: "Review the stopped instance and its attached storage, then terminate/delete unused resources after backup and approval.",
		},
		{
			RuleID:         waste.RuleOCIUnusedReservedPublicIP,
			Provider:       string(domain.ProviderOCI),
			ResourceType:   waste.ResourceReservedPublicIP,
			SeverityBasis:  "estimated monthly waste",
			Description:    "Detects active reserved public IPs with no assignment.",
			Recommendation: "Release the unused reserved public IP if it is not needed.",
		},
	}
}

func detachedVolumeFinding(resource waste.Resource, cfg waste.Config, now time.Time, cost waste.CostSignal, relationships []waste.Relationship, ruleID, service, summary, recommendation string) (waste.Finding, bool) {
	if !isActiveStorageState(resource.LifecycleState) || ageDays(resource, now) <= cfg.MinResourceAgeDays {
		return waste.Finding{}, false
	}
	attachmentType := waste.RelationshipBlockVolumeAttachedToInstance
	if ruleID == waste.RuleOCIDetachedBootVolume {
		attachmentType = waste.RelationshipBootVolumeAttachedToInstance
	}
	attachmentCount := countRelationships(relationships, attachmentType)
	if attachmentCount > 0 {
		return waste.Finding{}, false
	}
	estimate, hasCost := estimateMonthlyWaste(cost)
	if hasCost && observedCost(cost) <= cfg.MinCostThreshold {
		return waste.Finding{}, false
	}
	confidence := 0.85
	if hasCost {
		confidence = 0.95
	}
	evidence := baseEvidence(resource, now, cost, estimate)
	evidence["attachment_count"] = attachmentCount
	return waste.Finding{
		Provider:              domain.ProviderOCI,
		ResourceID:            resource.ResourceID,
		ResourceName:          resource.ResourceName,
		ResourceType:          resource.ResourceType,
		Region:                resource.Region,
		ScopeID:               resource.ScopeID,
		ScopeName:             resource.ScopeName,
		Service:               service,
		RuleID:                ruleID,
		Severity:              severity(estimate, cfg),
		Confidence:            confidence,
		EstimatedMonthlyWaste: estimate,
		Currency:              currency(cost, cfg),
		Summary:               summary,
		Recommendation:        recommendation,
		Evidence:              evidence,
		Status:                waste.StatusOpen,
		DetectedAt:            now,
		LastSeenAt:            now,
	}, true
}

func stoppedComputeFinding(resource waste.Resource, cfg waste.Config, now time.Time, costs map[string]waste.CostSignal, attached []waste.Relationship, resources map[string]waste.Resource) (waste.Finding, bool) {
	if !strings.EqualFold(resource.LifecycleState, "STOPPED") || ageDays(resource, now) <= cfg.StoppedInstanceMinDays {
		return waste.Finding{}, false
	}
	var attachedStorage []string
	var last7, last30 float64
	var hasCost bool
	for _, rel := range attached {
		if rel.RelationshipType != waste.RelationshipBlockVolumeAttachedToInstance && rel.RelationshipType != waste.RelationshipBootVolumeAttachedToInstance {
			continue
		}
		source := rel.SourceResourceID
		if storage, ok := resources[source]; !ok || hasExclusionTag(storage, cfg) {
			continue
		}
		attachedStorage = append(attachedStorage, source)
		cost := costs[source]
		if cost.HasLast7d {
			last7 += cost.Last7dCost
			hasCost = true
		}
		if cost.HasLast30d {
			last30 += cost.Last30dCost
			hasCost = true
		}
	}
	if len(attachedStorage) == 0 {
		return waste.Finding{}, false
	}
	cost := waste.CostSignal{
		Last7dCost:  last7,
		Last30dCost: last30,
		HasLast7d:   last7 > 0,
		HasLast30d:  last30 > 0,
		Currency:    cfg.Currency,
	}
	estimate, hasEstimatedCost := estimateMonthlyWaste(cost)
	confidence := 0.70
	if hasCost || hasEstimatedCost {
		confidence = 0.85
	}
	evidence := baseEvidence(resource, now, cost, estimate)
	evidence["attached_storage_count"] = len(attachedStorage)
	evidence["attached_storage_resource_ids"] = attachedStorage
	if !hasCost {
		evidence["cost_data_unavailable"] = true
	}
	return waste.Finding{
		Provider:              domain.ProviderOCI,
		ResourceID:            resource.ResourceID,
		ResourceName:          resource.ResourceName,
		ResourceType:          resource.ResourceType,
		Region:                resource.Region,
		ScopeID:               resource.ScopeID,
		ScopeName:             resource.ScopeName,
		Service:               "Compute",
		RuleID:                waste.RuleOCIStoppedComputePaidStorage,
		Severity:              severity(estimate, cfg),
		Confidence:            confidence,
		EstimatedMonthlyWaste: estimate,
		Currency:              cfg.Currency,
		Summary:               "Stopped compute instance has attached storage that may still generate cost.",
		Recommendation:        "Review the stopped instance and its attached storage. If the workload is no longer required, terminate the instance and delete unused volumes after backup/approval.",
		Evidence:              evidence,
		Status:                waste.StatusOpen,
		DetectedAt:            now,
		LastSeenAt:            now,
	}, true
}

func unusedReservedPublicIPFinding(resource waste.Resource, cfg waste.Config, now time.Time, cost waste.CostSignal, relationships []waste.Relationship) (waste.Finding, bool) {
	if !isActivePublicIPState(resource.LifecycleState) || ageDays(resource, now) <= cfg.MinResourceAgeDays {
		return waste.Finding{}, false
	}
	if countRelationships(relationships, waste.RelationshipPublicIPAssignedToPrivateIP) > 0 {
		return waste.Finding{}, false
	}
	estimate, hasCost := estimateMonthlyWaste(cost)
	if hasCost && observedCost(cost) <= cfg.MinCostThreshold {
		return waste.Finding{}, false
	}
	confidence := 0.75
	if hasCost {
		confidence = 0.90
	}
	evidence := baseEvidence(resource, now, cost, estimate)
	evidence["assigned"] = false
	return waste.Finding{
		Provider:              domain.ProviderOCI,
		ResourceID:            resource.ResourceID,
		ResourceName:          resource.ResourceName,
		ResourceType:          resource.ResourceType,
		Region:                resource.Region,
		ScopeID:               resource.ScopeID,
		ScopeName:             resource.ScopeName,
		Service:               "Networking",
		RuleID:                waste.RuleOCIUnusedReservedPublicIP,
		Severity:              severity(estimate, cfg),
		Confidence:            confidence,
		EstimatedMonthlyWaste: estimate,
		Currency:              currency(cost, cfg),
		Summary:               "Unused reserved public IP is possibly generating cost.",
		Recommendation:        "Release the unused reserved public IP if it is not needed.",
		Evidence:              evidence,
		Status:                waste.StatusOpen,
		DetectedAt:            now,
		LastSeenAt:            now,
	}, true
}

func relationshipsBySource(rels []waste.Relationship) map[string][]waste.Relationship {
	out := make(map[string][]waste.Relationship)
	for _, rel := range rels {
		out[rel.SourceResourceID] = append(out[rel.SourceResourceID], rel)
	}
	return out
}

func relationshipsByTarget(rels []waste.Relationship) map[string][]waste.Relationship {
	out := make(map[string][]waste.Relationship)
	for _, rel := range rels {
		out[rel.TargetResourceID] = append(out[rel.TargetResourceID], rel)
	}
	return out
}

func countRelationships(rels []waste.Relationship, relationshipType string) int {
	count := 0
	for _, rel := range rels {
		if rel.RelationshipType == relationshipType {
			count++
		}
	}
	return count
}

func isActiveStorageState(state string) bool {
	state = strings.ToUpper(strings.TrimSpace(state))
	return state == "" || state == "AVAILABLE"
}

func isActivePublicIPState(state string) bool {
	state = strings.ToUpper(strings.TrimSpace(state))
	return state == "" || state == "AVAILABLE" || state == "ASSIGNED" || state == "UNASSIGNED"
}

func ageDays(resource waste.Resource, now time.Time) int {
	if resource.TimeCreated == nil {
		return math.MaxInt / 2
	}
	return int(now.Sub(resource.TimeCreated.UTC()).Hours() / 24)
}

func estimateMonthlyWaste(cost waste.CostSignal) (*float64, bool) {
	if cost.HasLast7d {
		estimate := cost.Last7dCost / 7 * 30
		return &estimate, true
	}
	if cost.HasLast30d {
		estimate := cost.Last30dCost
		return &estimate, true
	}
	return nil, false
}

func observedCost(cost waste.CostSignal) float64 {
	if cost.HasLast7d {
		return cost.Last7dCost
	}
	if cost.HasLast30d {
		return cost.Last30dCost
	}
	return 0
}

func severity(estimate *float64, cfg waste.Config) string {
	if estimate == nil {
		return "low"
	}
	if *estimate >= cfg.HighMonthlyThreshold {
		return "high"
	}
	return "medium"
}

func currency(cost waste.CostSignal, cfg waste.Config) string {
	if strings.TrimSpace(cost.Currency) != "" {
		return strings.TrimSpace(cost.Currency)
	}
	if strings.TrimSpace(cfg.Currency) != "" {
		return strings.TrimSpace(cfg.Currency)
	}
	return "USD"
}

func baseEvidence(resource waste.Resource, now time.Time, cost waste.CostSignal, estimate *float64) map[string]any {
	evidence := map[string]any{
		"lifecycle_state": resource.LifecycleState,
		"age_days":        ageDays(resource, now),
	}
	if resource.AvailabilityDomain != "" {
		evidence["availability_domain"] = resource.AvailabilityDomain
	}
	if size, ok := resource.Raw["volume_size_gb"]; ok {
		evidence["volume_size_gb"] = size
	}
	if cost.HasLast7d {
		evidence["last_7d_cost"] = cost.Last7dCost
	}
	if cost.HasLast30d {
		evidence["last_30d_cost"] = cost.Last30dCost
	}
	if estimate != nil {
		evidence["estimated_monthly_waste"] = *estimate
	} else {
		evidence["cost_data_unavailable"] = true
	}
	return evidence
}

func hasExclusionTag(resource waste.Resource, cfg waste.Config) bool {
	if !cfg.EnableTagExclusions || len(resource.Tags) == 0 {
		return false
	}
	keys := make(map[string]struct{}, len(cfg.ExclusionTagKeys))
	for _, key := range cfg.ExclusionTagKeys {
		keys[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	for key, value := range resource.Tags {
		if !matchesExclusionTagKey(key, keys) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true", "yes", "1":
			return true
		}
	}
	return false
}

func matchesExclusionTagKey(key string, configured map[string]struct{}) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if _, ok := configured[normalized]; ok {
		return true
	}
	withoutPrefix := strings.TrimPrefix(normalized, "defined.")
	if _, ok := configured[withoutPrefix]; ok {
		return true
	}
	parts := strings.FieldsFunc(withoutPrefix, func(r rune) bool {
		return r == '.' || r == ':'
	})
	if len(parts) == 0 {
		return false
	}
	_, ok := configured[parts[len(parts)-1]]
	return ok
}
