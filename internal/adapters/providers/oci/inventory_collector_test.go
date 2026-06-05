package oci

import (
	"testing"

	"github.com/crypticani/torvix/internal/config"
)

func TestOCITagsIncludeDefinedNamespaceKeys(t *testing.T) {
	tags := ociTags(
		map[string]string{"keep": "false"},
		map[string]map[string]interface{}{
			"Operations": {"keep": true},
		},
	)

	for _, key := range []string{"defined.Operations.keep", "Operations.keep", "Operations:keep"} {
		if tags[key] != "true" {
			t.Fatalf("expected defined tag key %q to be true, got %q", key, tags[key])
		}
	}
	if tags["keep"] != "false" {
		t.Fatalf("expected freeform tag to be preserved, got %q", tags["keep"])
	}
}

func TestInventoryCollectorRootCompartmentPrefersTenancyOCID(t *testing.T) {
	const tenancyOCID = "ocid1.tenancy.oc1..aaaaexample"
	c := &InventoryCollector{
		cfg:       config.Provider{Account: "kocharsoft"},
		tenancyID: tenancyOCID,
	}

	if got := c.rootCompartmentID(); got != tenancyOCID {
		t.Fatalf("rootCompartmentID() = %q, want tenancy OCID %q", got, tenancyOCID)
	}
}

func TestInventoryCollectorRootCompartmentFallsBackToAccountOCID(t *testing.T) {
	const accountOCID = "ocid1.tenancy.oc1..bbbbexample"
	c := &InventoryCollector{cfg: config.Provider{Account: accountOCID}}

	if got := c.rootCompartmentID(); got != accountOCID {
		t.Fatalf("rootCompartmentID() = %q, want configured account OCID %q", got, accountOCID)
	}
}
