package oci

import (
	"bufio"
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/crypticani/cloudpulse/internal/domain"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) Parse(r io.ReadCloser, objectName string, accountID string) ([]domain.RawBillingRecord, error) {
	reader, err := maybeGzipReader(r, objectName)
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	defer reader.Close()

	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1
	csvReader.ReuseRecord = true
	csvReader.TrimLeadingSpace = true

	header, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("read csv header: %w", err)
	}
	index := buildHeaderIndex(header)

	var records []domain.RawBillingRecord
	for {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read csv row: %w", err)
		}
		record, ok := parseRow(index, row, objectName, accountID)
		if !ok {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func maybeGzipReader(r io.ReadCloser, objectName string) (io.ReadCloser, error) {
	br := bufio.NewReader(r)
	if strings.HasSuffix(strings.ToLower(objectName), ".gz") {
		gr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("open gzip stream: %w", err)
		}
		return combinedReadCloser{Reader: gr, closers: []io.Closer{gr, r}}, nil
	}
	magic, err := br.Peek(2)
	if err == nil && len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("open gzip stream: %w", err)
		}
		return combinedReadCloser{Reader: gr, closers: []io.Closer{gr, r}}, nil
	}
	return combinedReadCloser{Reader: br, closers: []io.Closer{r}}, nil
}

type combinedReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (c combinedReadCloser) Close() error {
	var firstErr error
	for _, closer := range c.closers {
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func buildHeaderIndex(header []string) map[string]int {
	index := make(map[string]int, len(header))
	for i, column := range header {
		index[normalizeHeader(column)] = i
	}
	return index
}

func normalizeHeader(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

func parseRow(index map[string]int, row []string, objectName, configuredAccount string) (domain.RawBillingRecord, bool) {
	serviceRaw := lookup(index, row, "product/service", "service", "product/servicename")
	description := lookup(index, row, "product/description", "description")
	sku := lookup(index, row, "cost/productsku", "cost/skuunitdescription", "product/sku")

	usageStart, ok := parseTimeField(lookup(index, row,
		"lineitem/intervalusagestart",
		"lineitem/usagestart",
		"usagestarttime",
		"usage/intervalstart",
	))
	if !ok {
		return domain.RawBillingRecord{}, false
	}
	usageEnd, _ := parseTimeField(lookup(index, row,
		"lineitem/intervalusageend",
		"lineitem/usageend",
		"usageendtime",
		"usage/intervalend",
	))
	if usageEnd.IsZero() {
		usageEnd = usageStart
	}

	cost, _ := parseFloatField(lookup(index, row,
		"cost/mycost",
		"cost/attributedcost",
		"cost",
		"lineitem/cost",
	))
	usageAmount, _ := parseFloatField(lookup(index, row,
		"usage/billedquantity",
		"usage/attributedusage",
		"usage/quantity",
		"quantity",
	))

	accountID := configuredAccount
	if accountID == "" {
		accountID = lookup(index, row, "lineitem/tenantid", "tenantid", "cost/subscriptionid")
	}
	category := normalizeOCIService(serviceRaw, description, sku)

	return domain.RawBillingRecord{
		Provider:    domain.ProviderOCI,
		AccountID:   accountID,
		UsageStart:  usageStart,
		UsageEnd:    usageEnd,
		Service:     defaultString(serviceRaw, category),
		Category:    category,
		SKU:         sku,
		Region:      lookup(index, row, "product/region", "region"),
		ResourceID:  lookup(index, row, "product/resourceid", "resourceid"),
		Currency:    defaultString(lookup(index, row, "cost/currencycode", "currencycode", "currency"), "USD"),
		Cost:        cost,
		UsageAmount: usageAmount,
		UsageUnit:   lookup(index, row, "cost/billingunitreadable", "usage/unit", "unit"),
		Tags: extractTags(index, row, map[string]string{
			"oci_raw_service":         serviceRaw,
			"oci_compartment_id":      lookup(index, row, "product/compartmentid"),
			"oci_compartment_name":    lookup(index, row, "product/compartmentname"),
			"oci_availability_domain": lookup(index, row, "product/availabilitydomain"),
			"oci_description":         description,
			"oci_overage_flag":        lookup(index, row, "cost/overageflag"),
		}),
		Meter: defaultString(sku, description),
		RawData: map[string]any{
			"description":       description,
			"sku":               sku,
			"availability_zone": lookup(index, row, "product/availabilitydomain"),
			"source_type":       "oci_object_storage_report",
		},
		SourceObject: objectName,
	}, true
}

func lookup(index map[string]int, row []string, keys ...string) string {
	for _, key := range keys {
		if i, ok := index[normalizeHeader(key)]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
	}
	return ""
}

func parseTimeField(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func parseFloatField(v string) (float64, bool) {
	v = strings.TrimSpace(strings.ReplaceAll(v, ",", ""))
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func extractTags(index map[string]int, row []string, base map[string]string) map[string]string {
	tags := make(map[string]string, len(base))
	for k, v := range base {
		if strings.TrimSpace(v) != "" {
			tags[k] = v
		}
	}
	for key, idx := range index {
		if idx >= len(row) {
			continue
		}
		if strings.HasPrefix(key, "tags/") || strings.HasPrefix(key, "tag/") {
			value := strings.TrimSpace(row[idx])
			if value != "" {
				tags[key] = value
			}
		}
	}
	return tags
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
