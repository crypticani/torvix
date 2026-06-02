package oci

import "testing"

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
