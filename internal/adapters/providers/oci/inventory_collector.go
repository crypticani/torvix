package oci

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	ocicore "github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"

	"github.com/crypticani/torvix/internal/config"
	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/waste"
)

type InventoryCollector struct {
	cfg          config.Provider
	repo         waste.Repository
	logger       *slog.Logger
	identity     identity.IdentityClient
	compute      ocicore.ComputeClient
	blockstorage ocicore.BlockstorageClient
	network      ocicore.VirtualNetworkClient
	region       string
	tenancyID    string
}

func NewInventoryCollector(cfg config.Provider, repo waste.Repository, logger *slog.Logger) (*InventoryCollector, error) {
	provider, err := configurationProvider(cfg)
	if err != nil {
		return nil, err
	}
	identityClient, err := identity.NewIdentityClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("create OCI identity client: %w", err)
	}
	computeClient, err := ocicore.NewComputeClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("create OCI compute client: %w", err)
	}
	blockstorageClient, err := ocicore.NewBlockstorageClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("create OCI block storage client: %w", err)
	}
	networkClient, err := ocicore.NewVirtualNetworkClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("create OCI network client: %w", err)
	}
	region, _ := provider.Region()
	tenancyID, _ := provider.TenancyOCID()
	tenancyID = strings.TrimSpace(tenancyID)
	if logger == nil {
		logger = slog.Default()
	}
	return &InventoryCollector{
		cfg:          cfg,
		repo:         repo,
		logger:       logger,
		identity:     identityClient,
		compute:      computeClient,
		blockstorage: blockstorageClient,
		network:      networkClient,
		region:       region,
		tenancyID:    tenancyID,
	}, nil
}

func (c *InventoryCollector) Provider() domain.Provider {
	return domain.ProviderOCI
}

func (c *InventoryCollector) Sync(ctx context.Context) (waste.InventoryResult, error) {
	started := time.Now()
	if c.repo == nil {
		return waste.InventoryResult{Provider: domain.ProviderOCI, Skipped: true, Message: "waste repository is not configured"}, nil
	}
	rootCompartmentID := c.rootCompartmentID()
	if rootCompartmentID == "" {
		return waste.InventoryResult{Provider: domain.ProviderOCI, Skipped: true, Message: "OCI tenancy/account OCID is not configured"}, nil
	}
	c.logger.Info("OCI waste inventory sync started", "provider", "oci", "root_compartment_id", rootCompartmentID)
	runID, err := c.repo.StartCloudInventoryRun(ctx, waste.InventoryRun{
		Provider: domain.ProviderOCI,
		Region:   c.region,
		ScopeID:  rootCompartmentID,
		Status:   "running",
		Metadata: map[string]any{"source": "oci_waste_inventory"},
	})
	if err != nil {
		return waste.InventoryResult{Provider: domain.ProviderOCI}, err
	}
	scopes, err := c.compartments(ctx, rootCompartmentID)
	if err != nil {
		_ = c.repo.CompleteCloudInventoryRun(ctx, runID, "failed", err.Error())
		return waste.InventoryResult{Provider: domain.ProviderOCI}, err
	}
	var resources []waste.Resource
	relationshipBatches := map[waste.RelationshipScope][]waste.Relationship{}
	var syncErrors []error
	for _, scope := range scopes {
		compartmentResources, ads, err := c.resourcesForCompartment(ctx, scope)
		if err != nil {
			c.logger.Warn("OCI waste inventory compartment sync failed", "scope_id", scope.id, "scope_name", scope.name, "error", err)
			syncErrors = append(syncErrors, fmt.Errorf("sync compartment resources %s: %w", scope.id, err))
			continue
		}
		resources = append(resources, compartmentResources...)
		blockRelationships, err := c.blockVolumeRelationships(ctx, scope)
		if err != nil {
			c.logger.Warn("OCI block volume relationship sync failed", "scope_id", scope.id, "scope_name", scope.name, "error", err)
			syncErrors = append(syncErrors, fmt.Errorf("sync block volume relationships %s: %w", scope.id, err))
		} else {
			relationshipBatches[waste.RelationshipScope{Provider: domain.ProviderOCI, Region: c.region, ScopeID: scope.id, RelationshipType: waste.RelationshipBlockVolumeAttachedToInstance}] = blockRelationships
		}
		publicIPResources, publicIPRelationships, err := c.publicIPs(ctx, scope)
		if err != nil {
			c.logger.Warn("OCI public IP inventory sync failed", "scope_id", scope.id, "scope_name", scope.name, "error", err)
			syncErrors = append(syncErrors, fmt.Errorf("sync public IP inventory %s: %w", scope.id, err))
		} else {
			resources = append(resources, publicIPResources...)
			relationshipBatches[waste.RelationshipScope{Provider: domain.ProviderOCI, Region: c.region, ScopeID: scope.id, RelationshipType: waste.RelationshipPublicIPAssignedToPrivateIP}] = publicIPRelationships
		}
		var bootRelationships []waste.Relationship
		bootComplete := true
		for ad := range ads {
			adRelationships, err := c.bootVolumeRelationships(ctx, scope, ad)
			if err != nil {
				c.logger.Warn("OCI boot volume relationship sync failed", "scope_id", scope.id, "scope_name", scope.name, "availability_domain", ad, "error", err)
				syncErrors = append(syncErrors, fmt.Errorf("sync boot volume relationships %s/%s: %w", scope.id, ad, err))
				bootComplete = false
				continue
			}
			bootRelationships = append(bootRelationships, adRelationships...)
		}
		if bootComplete {
			relationshipBatches[waste.RelationshipScope{Provider: domain.ProviderOCI, Region: c.region, ScopeID: scope.id, RelationshipType: waste.RelationshipBootVolumeAttachedToInstance}] = bootRelationships
		}
	}
	if len(syncErrors) > 0 {
		err := errors.Join(syncErrors...)
		_ = c.repo.CompleteCloudInventoryRun(ctx, runID, "failed", err.Error())
		c.logger.Warn("OCI waste inventory sync failed; skipping waste detection for partial run", "provider", "oci", "errors", len(syncErrors), "duration", time.Since(started).String())
		return waste.InventoryResult{Provider: domain.ProviderOCI, Message: "partial OCI inventory sync failed"}, err
	}
	for i := range resources {
		resources[i].LastSeenRunID = runID
	}
	if err := c.repo.UpsertCloudResources(ctx, resources); err != nil {
		_ = c.repo.CompleteCloudInventoryRun(ctx, runID, "failed", err.Error())
		return waste.InventoryResult{Provider: domain.ProviderOCI}, err
	}
	var relationshipCount int
	for scope, relationships := range relationshipBatches {
		if err := c.repo.ReplaceCloudRelationshipsScoped(ctx, scope, relationships); err != nil {
			_ = c.repo.CompleteCloudInventoryRun(ctx, runID, "failed", err.Error())
			return waste.InventoryResult{Provider: domain.ProviderOCI}, err
		}
		relationshipCount += len(relationships)
	}
	if _, err := c.repo.MarkMissingCloudResourcesInactive(ctx, domain.ProviderOCI, c.region, runID); err != nil {
		_ = c.repo.CompleteCloudInventoryRun(ctx, runID, "failed", err.Error())
		return waste.InventoryResult{Provider: domain.ProviderOCI}, err
	}
	if err := c.repo.CompleteCloudInventoryRun(ctx, runID, "success", ""); err != nil {
		return waste.InventoryResult{Provider: domain.ProviderOCI}, err
	}
	result := waste.InventoryResult{
		Provider:              domain.ProviderOCI,
		ResourcesScanned:      len(resources),
		RelationshipsScanned:  relationshipCount,
		ResourcesUpserted:     len(resources),
		RelationshipsUpserted: relationshipCount,
	}
	c.logger.Info("OCI waste inventory sync completed", "provider", "oci", "resources", len(resources), "relationships", relationshipCount, "duration", time.Since(started).String())
	return result, nil
}

type compartmentScope struct {
	id   string
	name string
}

func (c *InventoryCollector) rootCompartmentID() string {
	if strings.TrimSpace(c.tenancyID) != "" {
		return strings.TrimSpace(c.tenancyID)
	}
	account := strings.TrimSpace(c.cfg.Account)
	if strings.HasPrefix(account, "ocid1.tenancy.") {
		return account
	}
	return ""
}

func (c *InventoryCollector) compartments(ctx context.Context, rootCompartmentID string) ([]compartmentScope, error) {
	scopes := []compartmentScope{{id: rootCompartmentID, name: "root"}}
	var page *string
	for {
		resp, err := c.identity.ListCompartments(ctx, identity.ListCompartmentsRequest{
			CompartmentId:          common.String(rootCompartmentID),
			CompartmentIdInSubtree: common.Bool(true),
			AccessLevel:            identity.ListCompartmentsAccessLevelAccessible,
			LifecycleState:         identity.CompartmentLifecycleStateActive,
			Page:                   page,
			RequestMetadata:        requestMetadata(),
		})
		if err != nil {
			return scopes, fmt.Errorf("list OCI compartments: %w", err)
		}
		for _, compartment := range resp.Items {
			if compartment.Id == nil {
				continue
			}
			scopes = append(scopes, compartmentScope{id: *compartment.Id, name: stringValue(compartment.Name)})
		}
		if resp.OpcNextPage == nil {
			break
		}
		page = resp.OpcNextPage
	}
	return scopes, nil
}

func (c *InventoryCollector) resourcesForCompartment(ctx context.Context, scope compartmentScope) ([]waste.Resource, map[string]struct{}, error) {
	var resources []waste.Resource
	ads := map[string]struct{}{}
	instances, err := c.instances(ctx, scope)
	if err != nil {
		return nil, ads, err
	}
	resources = append(resources, instances...)
	for _, resource := range instances {
		if resource.AvailabilityDomain != "" {
			ads[resource.AvailabilityDomain] = struct{}{}
		}
	}
	volumes, err := c.blockVolumes(ctx, scope)
	if err != nil {
		return nil, ads, err
	}
	resources = append(resources, volumes...)
	for _, resource := range volumes {
		if resource.AvailabilityDomain != "" {
			ads[resource.AvailabilityDomain] = struct{}{}
		}
	}
	bootVolumes, err := c.bootVolumes(ctx, scope)
	if err != nil {
		return nil, ads, err
	}
	resources = append(resources, bootVolumes...)
	for _, resource := range bootVolumes {
		if resource.AvailabilityDomain != "" {
			ads[resource.AvailabilityDomain] = struct{}{}
		}
	}
	return resources, ads, nil
}

func (c *InventoryCollector) instances(ctx context.Context, scope compartmentScope) ([]waste.Resource, error) {
	var resources []waste.Resource
	var page *string
	for {
		resp, err := c.compute.ListInstances(ctx, ocicore.ListInstancesRequest{CompartmentId: common.String(scope.id), Page: page, RequestMetadata: requestMetadata()})
		if err != nil {
			return resources, err
		}
		for _, item := range resp.Items {
			if item.Id == nil {
				continue
			}
			resources = append(resources, waste.Resource{
				Provider:           domain.ProviderOCI,
				ResourceID:         *item.Id,
				ResourceName:       stringValue(item.DisplayName),
				ResourceType:       waste.ResourceComputeInstance,
				Region:             firstNonEmpty(stringValue(item.Region), c.region),
				ScopeID:            scope.id,
				ScopeName:          scope.name,
				LifecycleState:     string(item.LifecycleState),
				AvailabilityDomain: stringValue(item.AvailabilityDomain),
				TimeCreated:        sdkTime(item.TimeCreated),
				Tags:               ociTags(item.FreeformTags, item.DefinedTags),
				Raw: map[string]any{
					"shape": stringValue(item.Shape),
				},
			})
		}
		if resp.OpcNextPage == nil {
			break
		}
		page = resp.OpcNextPage
	}
	return resources, nil
}

func (c *InventoryCollector) blockVolumes(ctx context.Context, scope compartmentScope) ([]waste.Resource, error) {
	var resources []waste.Resource
	var page *string
	for {
		resp, err := c.blockstorage.ListVolumes(ctx, ocicore.ListVolumesRequest{CompartmentId: common.String(scope.id), Page: page, RequestMetadata: requestMetadata()})
		if err != nil {
			return resources, err
		}
		for _, item := range resp.Items {
			if item.Id == nil {
				continue
			}
			resources = append(resources, waste.Resource{
				Provider:           domain.ProviderOCI,
				ResourceID:         *item.Id,
				ResourceName:       stringValue(item.DisplayName),
				ResourceType:       waste.ResourceBlockVolume,
				Region:             c.region,
				ScopeID:            scope.id,
				ScopeName:          scope.name,
				LifecycleState:     string(item.LifecycleState),
				AvailabilityDomain: stringValue(item.AvailabilityDomain),
				TimeCreated:        sdkTime(item.TimeCreated),
				Tags:               ociTags(item.FreeformTags, item.DefinedTags),
				Raw: map[string]any{
					"volume_size_gb": int64Value(item.SizeInGBs),
					"vpus_per_gb":    int64Value(item.VpusPerGB),
				},
			})
		}
		if resp.OpcNextPage == nil {
			break
		}
		page = resp.OpcNextPage
	}
	return resources, nil
}

func (c *InventoryCollector) bootVolumes(ctx context.Context, scope compartmentScope) ([]waste.Resource, error) {
	var resources []waste.Resource
	var page *string
	for {
		resp, err := c.blockstorage.ListBootVolumes(ctx, ocicore.ListBootVolumesRequest{CompartmentId: common.String(scope.id), Page: page, RequestMetadata: requestMetadata()})
		if err != nil {
			return resources, err
		}
		for _, item := range resp.Items {
			if item.Id == nil {
				continue
			}
			resources = append(resources, waste.Resource{
				Provider:           domain.ProviderOCI,
				ResourceID:         *item.Id,
				ResourceName:       stringValue(item.DisplayName),
				ResourceType:       waste.ResourceBootVolume,
				Region:             c.region,
				ScopeID:            scope.id,
				ScopeName:          scope.name,
				LifecycleState:     string(item.LifecycleState),
				AvailabilityDomain: stringValue(item.AvailabilityDomain),
				TimeCreated:        sdkTime(item.TimeCreated),
				Tags:               ociTags(item.FreeformTags, item.DefinedTags),
				Raw: map[string]any{
					"volume_size_gb": int64Value(item.SizeInGBs),
					"vpus_per_gb":    int64Value(item.VpusPerGB),
				},
			})
		}
		if resp.OpcNextPage == nil {
			break
		}
		page = resp.OpcNextPage
	}
	return resources, nil
}

func (c *InventoryCollector) blockVolumeRelationships(ctx context.Context, scope compartmentScope) ([]waste.Relationship, error) {
	var relationships []waste.Relationship
	var page *string
	for {
		resp, err := c.compute.ListVolumeAttachments(ctx, ocicore.ListVolumeAttachmentsRequest{CompartmentId: common.String(scope.id), Page: page, RequestMetadata: requestMetadata()})
		if err != nil {
			return relationships, err
		}
		for _, item := range resp.Items {
			if item.GetLifecycleState() != ocicore.VolumeAttachmentLifecycleStateAttached || item.GetVolumeId() == nil || item.GetInstanceId() == nil {
				continue
			}
			relationships = append(relationships, waste.Relationship{
				Provider:         domain.ProviderOCI,
				SourceResourceID: *item.GetVolumeId(),
				TargetResourceID: *item.GetInstanceId(),
				RelationshipType: waste.RelationshipBlockVolumeAttachedToInstance,
				Region:           c.region,
				ScopeID:          scope.id,
				DetectedAt:       time.Now().UTC(),
				Raw: map[string]any{
					"attachment_id":       stringValue(item.GetId()),
					"availability_domain": stringValue(item.GetAvailabilityDomain()),
				},
			})
		}
		if resp.OpcNextPage == nil {
			break
		}
		page = resp.OpcNextPage
	}
	return relationships, nil
}

func (c *InventoryCollector) bootVolumeRelationships(ctx context.Context, scope compartmentScope, availabilityDomain string) ([]waste.Relationship, error) {
	var relationships []waste.Relationship
	var page *string
	for {
		resp, err := c.compute.ListBootVolumeAttachments(ctx, ocicore.ListBootVolumeAttachmentsRequest{CompartmentId: common.String(scope.id), AvailabilityDomain: common.String(availabilityDomain), Page: page, RequestMetadata: requestMetadata()})
		if err != nil {
			return relationships, err
		}
		for _, item := range resp.Items {
			if item.LifecycleState != ocicore.BootVolumeAttachmentLifecycleStateAttached || item.BootVolumeId == nil || item.InstanceId == nil {
				continue
			}
			relationships = append(relationships, waste.Relationship{
				Provider:         domain.ProviderOCI,
				SourceResourceID: *item.BootVolumeId,
				TargetResourceID: *item.InstanceId,
				RelationshipType: waste.RelationshipBootVolumeAttachedToInstance,
				Region:           c.region,
				ScopeID:          scope.id,
				DetectedAt:       time.Now().UTC(),
				Raw: map[string]any{
					"attachment_id":       stringValue(item.Id),
					"availability_domain": availabilityDomain,
				},
			})
		}
		if resp.OpcNextPage == nil {
			break
		}
		page = resp.OpcNextPage
	}
	return relationships, nil
}

func (c *InventoryCollector) publicIPs(ctx context.Context, scope compartmentScope) ([]waste.Resource, []waste.Relationship, error) {
	var resources []waste.Resource
	var relationships []waste.Relationship
	var page *string
	for {
		resp, err := c.network.ListPublicIps(ctx, ocicore.ListPublicIpsRequest{
			CompartmentId:   common.String(scope.id),
			Scope:           ocicore.ListPublicIpsScopeRegion,
			Lifetime:        ocicore.ListPublicIpsLifetimeReserved,
			Page:            page,
			RequestMetadata: requestMetadata(),
		})
		if err != nil {
			return resources, relationships, err
		}
		for _, item := range resp.Items {
			if item.Id == nil {
				continue
			}
			resourceID := *item.Id
			resources = append(resources, waste.Resource{
				Provider:           domain.ProviderOCI,
				ResourceID:         resourceID,
				ResourceName:       stringValue(item.DisplayName),
				ResourceType:       waste.ResourceReservedPublicIP,
				Region:             c.region,
				ScopeID:            scope.id,
				ScopeName:          scope.name,
				LifecycleState:     string(item.LifecycleState),
				AvailabilityDomain: stringValue(item.AvailabilityDomain),
				TimeCreated:        sdkTime(item.TimeCreated),
				Tags:               ociTags(item.FreeformTags, item.DefinedTags),
				Raw: map[string]any{
					"ip_address":           stringValue(item.IpAddress),
					"assigned_entity_type": string(item.AssignedEntityType),
				},
			})
			target := firstNonEmpty(stringValue(item.PrivateIpId), stringValue(item.AssignedEntityId))
			if target != "" {
				relationships = append(relationships, waste.Relationship{
					Provider:         domain.ProviderOCI,
					SourceResourceID: resourceID,
					TargetResourceID: target,
					RelationshipType: waste.RelationshipPublicIPAssignedToPrivateIP,
					Region:           c.region,
					ScopeID:          scope.id,
					DetectedAt:       time.Now().UTC(),
					Raw:              map[string]any{"assigned_entity_type": string(item.AssignedEntityType)},
				})
			}
		}
		if resp.OpcNextPage == nil {
			break
		}
		page = resp.OpcNextPage
	}
	return resources, relationships, nil
}

func requestMetadata() common.RequestMetadata {
	retryPolicy := boundedRetryPolicy()
	return common.RequestMetadata{RetryPolicy: &retryPolicy}
}

func sdkTime(value *common.SDKTime) *time.Time {
	if value == nil {
		return nil
	}
	t := value.Time.UTC()
	return &t
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func ociTags(freeform map[string]string, defined map[string]map[string]interface{}) map[string]string {
	tags := make(map[string]string, len(freeform)+len(defined))
	for key, value := range freeform {
		tags[key] = value
	}
	for namespace, values := range defined {
		for key, value := range values {
			tagKey := "defined." + namespace + "." + key
			tags[tagKey] = fmt.Sprint(value)
			tags[namespace+"."+key] = fmt.Sprint(value)
			tags[namespace+":"+key] = fmt.Sprint(value)
		}
	}
	return tags
}
