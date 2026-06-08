package unit

import (
	"os"
	"strings"
	"testing"

	"github.com/crypticani/torvix/internal/version"
)

func TestVersionIsConsistentAcrossPublicAPIDocs(t *testing.T) {
	for _, path := range []string{
		"../../cmd/torvix/main.go",
		"../../docs/docs.go",
		"../../docs/swagger.json",
		"../../docs/swagger.yaml",
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(b), version.Version) {
			t.Fatalf("%s does not contain runtime version %s", path, version.Version)
		}
	}
}
