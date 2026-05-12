package oci

import "strings"

func normalizeOCIService(service, description, sku string) string {
	value := strings.ToLower(strings.Join([]string{service, description, sku}, " "))
	switch {
	case containsAny(value, "oke", "kubernetes", "container engine"):
		return "Kubernetes"
	case containsAny(value, "load balancer", "loadbalancer", "network load balancer"):
		return "Load Balancer"
	case containsAny(value, "database", "autonomous", "mysql", "heatwave", "exadata"):
		return "Database"
	case containsAny(value, "object storage", "block volume", "boot volume", "file storage", "archive storage", "storage gateway"):
		return "Storage"
	case containsAny(value, "vcn", "network", "bandwidth", "fastconnect", "nat gateway", "public ip", "private ip", "dns", "traffic"):
		return "Networking"
	case containsAny(value, "compute", "instance", "gpu", "bare metal"):
		return "Compute"
	case containsAny(value, "monitoring", "logging", "alarm", "events", "notifications", "observability"):
		return "Monitoring"
	case containsAny(value, "vault", "key management", "security", "cloud guard", "waf", "bastion", "certificate"):
		return "Security"
	default:
		if strings.TrimSpace(service) != "" {
			return strings.TrimSpace(service)
		}
		return "OCI Other"
	}
}

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
