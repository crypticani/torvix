package oci

import (
	"io"
	"strings"
	"testing"
)

func TestParserParsesOCIProprietaryCostReport(t *testing.T) {
	input := strings.Join([]string{
		"lineItem/intervalUsageStart,lineItem/intervalUsageEnd,product/service,product/description,cost/productSku,product/region,product/resourceId,usage/billedQuantity,cost/myCost,cost/currencyCode,cost/billingUnitReadable,tags/Env",
		"2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,Compute,VM.Standard.E4.Flex,cpu-hours,us-ashburn-1,ocid1.instance.oc1..abc,12,34.5,USD,HOURS,prod",
	}, "\n")

	parser := NewParser()
	records, err := parser.Parse(io.NopCloser(strings.NewReader(input)), "reports/oci.csv", "ocid1.tenancy.oc1..example")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Service != "Compute" {
		t.Fatalf("expected Compute service, got %q", records[0].Service)
	}
	if records[0].Tags["tags/env"] != "prod" {
		t.Fatalf("expected tag value prod, got %q", records[0].Tags["tags/env"])
	}
}

func TestParserHandlesGzipAutomatically(t *testing.T) {
	data := gzipString(t, strings.Join([]string{
		"lineItem/intervalUsageStart,product/service,usage/billedQuantity,cost/myCost",
		"2026-05-01T00:00:00Z,Object Storage,1,2.5",
	}, "\n"))

	parser := NewParser()
	records, err := parser.Parse(io.NopCloser(strings.NewReader(data)), "reports/oci.csv.gz", "")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Category != "Storage" {
		t.Fatalf("expected Storage category, got %q", records[0].Category)
	}
}
