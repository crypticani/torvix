package oci

import (
	"compress/gzip"
	"io"
	"testing"
)

func newTestGzipWriter(t *testing.T, w io.Writer) *gzip.Writer {
	t.Helper()
	return gzip.NewWriter(w)
}
