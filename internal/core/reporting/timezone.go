package reporting

import (
	"time"
	_ "time/tzdata"
)

// LoadLocation resolves IANA timezone names using Go's embedded tzdata, so
// minimal containers do not need OS timezone files installed.
func LoadLocation(name string) (*time.Location, error) {
	return time.LoadLocation(name)
}
