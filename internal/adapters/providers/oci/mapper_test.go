package oci

import "testing"

func TestNormalizeOCIService(t *testing.T) {
	cases := map[string]struct {
		service     string
		description string
		sku         string
		want        string
	}{
		"compute":     {service: "Compute", want: "Compute"},
		"storage":     {service: "Object Storage", want: "Storage"},
		"networking":  {description: "VCN egress bandwidth", want: "Networking"},
		"database":    {description: "Autonomous Database", want: "Database"},
		"loadbalance": {description: "Load Balancer shape", want: "Load Balancer"},
		"monitoring":  {description: "Monitoring metric ingestion", want: "Monitoring"},
		"security":    {description: "Cloud Guard detector", want: "Security"},
		"kubernetes":  {description: "OKE cluster nodes", want: "Kubernetes"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := normalizeOCIService(tc.service, tc.description, tc.sku); got != tc.want {
				t.Fatalf("normalizeOCIService() = %q, want %q", got, tc.want)
			}
		})
	}
}
