package aws

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/crypticani/torvix/internal/domain"
)

const recordTypeCURLineItem = "cur_line_item"

type curSource struct {
	Bucket       string
	Key          string
	ETag         string
	Format       string
	LastModified time.Time
	Size         int64
}

type curParseResult struct {
	Records       []domain.RawBillingRecord
	RowsParsed    int
	RowsSkipped   int
	MalformedRows int
}

func parseCURCSV(reader io.Reader, source curSource) (curParseResult, error) {
	result := curParseResult{Records: make([]domain.RawBillingRecord, 0)}
	parsed, err := parseCURCSVStream(reader, source, func(record domain.RawBillingRecord) error {
		result.Records = append(result.Records, record)
		return nil
	})
	parsed.Records = result.Records
	return parsed, err
}

func parseCURCSVStream(reader io.Reader, source curSource, handle func(domain.RawBillingRecord) error) (curParseResult, error) {
	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1
	csvReader.ReuseRecord = true

	header, err := csvReader.Read()
	if err != nil {
		if err == io.EOF {
			return curParseResult{}, nil
		}
		return curParseResult{}, fmt.Errorf("read AWS CUR CSV header: %w", err)
	}
	columns := mapCURColumns(header)
	if err := requireCURColumns(columns); err != nil {
		return curParseResult{}, err
	}

	result := curParseResult{}
	lineNumber := int64(1)
	for {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		lineNumber++
		if err != nil {
			result.RowsSkipped++
			result.MalformedRows++
			continue
		}
		result.RowsParsed++
		record, ok := mapCURRow(row, columns, source, lineNumber)
		if !ok {
			result.RowsSkipped++
			result.MalformedRows++
			continue
		}
		if err := handle(record); err != nil {
			return result, err
		}
	}
	return result, nil
}

func requireCURColumns(columns map[string]int) error {
	for _, required := range []string{"usage_start", "account_id", "cost"} {
		if _, ok := columns[required]; !ok {
			return fmt.Errorf("AWS CUR file missing required column group %q", required)
		}
	}
	return nil
}

func mapCURColumns(header []string) map[string]int {
	out := make(map[string]int, len(header))
	for i, name := range header {
		canonical := canonicalCURColumn(name)
		if canonical != "" {
			if _, exists := out[canonical]; !exists {
				out[canonical] = i
			}
		}
		tagKey, ok := curTagKey(name)
		if ok {
			out["tag:"+tagKey] = i
		}
	}
	return out
}

func canonicalCURColumn(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, "/", "_")
	normalized = strings.ReplaceAll(normalized, ":", "_")
	normalized = strings.ReplaceAll(normalized, "-", "_")
	switch normalized {
	case "line_item_usage_start_date", "lineitem_usagestartdate", "usage_start_date":
		return "usage_start"
	case "line_item_usage_end_date", "lineitem_usageenddate", "usage_end_date":
		return "usage_end"
	case "line_item_usage_account_id", "lineitem_usageaccountid":
		return "account_id"
	case "product_product_name", "product_productname":
		return "product_name"
	case "line_item_product_code", "lineitem_productcode":
		return "product_code"
	case "product_region":
		return "region"
	case "product_availability_zone", "product_availabilityzone", "line_item_availability_zone", "lineitem_availabilityzone":
		return "availability_zone"
	case "line_item_unblended_cost", "lineitem_unblendedcost":
		return "cost"
	case "pricing_public_on_demand_cost", "pricing_publicondemandcost":
		return "public_on_demand_cost"
	case "reservation_effective_cost", "reservation_effectivecost":
		return "reservation_effective_cost"
	case "savings_plan_savings_plan_effective_cost", "savingsplan_savingsplaneffectivecost":
		return "savings_plan_effective_cost"
	case "pricing_currency":
		return "currency"
	case "line_item_usage_amount", "lineitem_usageamount":
		return "usage_amount"
	case "pricing_unit":
		return "usage_unit"
	case "line_item_resource_id", "lineitem_resourceid":
		return "resource_id"
	case "line_item_usage_type", "lineitem_usagetype":
		return "usage_type"
	case "line_item_operation", "lineitem_operation":
		return "operation"
	case "line_item_line_item_type", "lineitem_lineitemtype":
		return "line_item_type"
	case "bill_bill_type", "bill_billtype":
		return "bill_type"
	case "product_product_family", "product_productfamily":
		return "product_family"
	case "pricing_term":
		return "pricing_term"
	default:
		return ""
	}
}

func mapCURRow(row []string, columns map[string]int, source curSource, lineNumber int64) (domain.RawBillingRecord, bool) {
	usageStart, ok := parseCURTime(curValue(row, columns, "usage_start"))
	if !ok {
		return domain.RawBillingRecord{}, false
	}
	usageEnd, ok := parseCURTime(curValue(row, columns, "usage_end"))
	if !ok {
		usageEnd = usageStart.AddDate(0, 0, 1)
	}
	cost, ok := parseCURFloat(firstCURValue(row, columns, "cost", "public_on_demand_cost", "reservation_effective_cost", "savings_plan_effective_cost"))
	if !ok {
		return domain.RawBillingRecord{}, false
	}
	accountID := strings.TrimSpace(curValue(row, columns, "account_id"))
	if accountID == "" {
		return domain.RawBillingRecord{}, false
	}
	service := firstCURValue(row, columns, "product_name", "product_code")
	if strings.TrimSpace(service) == "" {
		service = "unknown"
	}
	region := normalizeRegion(firstCURValue(row, columns, "region"))
	if region == "global" {
		if derived := regionFromAvailabilityZone(curValue(row, columns, "availability_zone")); derived != "" {
			region = derived
		}
	}
	currency := firstCURValue(row, columns, "currency")
	if strings.TrimSpace(currency) == "" {
		currency = "USD"
	}
	usageAmount, _ := parseCURFloat(curValue(row, columns, "usage_amount"))
	usageUnit := firstCURValue(row, columns, "usage_unit", "usage_type")
	tags := curTags(row, columns)
	project := firstNonEmpty(tags["Project"], tags["project"])
	raw := curRawMetadata(row, columns, source)
	hash := curRecordHash(source, row, columns, lineNumber, usageStart, usageEnd, accountID, region, service)

	record := domain.RawBillingRecord{
		Provider:         domain.ProviderAWS,
		AccountID:        accountID,
		UsageStart:       usageStart,
		UsageEnd:         usageEnd,
		Service:          service,
		Category:         firstNonEmpty(curValue(row, columns, "product_family"), service),
		Region:           region,
		BillingScopeType: "linked_account",
		BillingScopeID:   accountID,
		BillingScopeName: accountID,
		ResourceID:       curValue(row, columns, "resource_id"),
		Currency:         currency,
		Cost:             cost,
		UsageAmount:      usageAmount,
		UsageUnit:        usageUnit,
		Tags:             tags,
		Meter:            firstCURValue(row, columns, "usage_type", "operation"),
		RecordType:       recordTypeCURLineItem,
		SourceFileKey:    source.Key,
		SourceFileETag:   source.ETag,
		SourceLineNumber: lineNumber,
		SourceRecordHash: hash,
		RawData:          raw,
		SourceObject:     source.Key,
	}
	if project != "" {
		record.ProjectSource = "tag"
		record.ProjectName = project
		record.ProjectID = project
	}
	// TODO(aws-cur-v2): enrich resource inventory for ENI, RDS subnet group, NAT Gateway,
	// ELB, tag sync, Cost Categories, and manual project mappings before making VPC
	// attribution part of the primary AWS drilldown.
	if vpcID := firstNonEmpty(tags["VpcId"], tags["VPC"], tags["vpc"], tags["vpc_id"]); vpcID != "" {
		record.NetworkScopeType = "vpc"
		record.NetworkScopeID = vpcID
		record.NetworkScopeName = firstNonEmpty(tags["VpcName"], tags["Name"])
	}
	return record, true
}

func curValue(row []string, columns map[string]int, key string) string {
	index, ok := columns[key]
	if !ok || index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func firstCURValue(row []string, columns map[string]int, keys ...string) string {
	for _, key := range keys {
		if value := curValue(row, columns, key); value != "" {
			return value
		}
	}
	return ""
}

func parseCURTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", time.DateOnly} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func parseCURFloat(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	return parsed, err == nil
}

func curTagKey(name string) (string, bool) {
	trimmed := strings.TrimSpace(name)
	lower := strings.ToLower(trimmed)
	var raw string
	switch {
	case strings.HasPrefix(lower, "resource_tags_user_"):
		raw = trimmed[len("resource_tags_user_"):]
	case strings.HasPrefix(lower, "resourcetags/user:"):
		raw = trimmed[len("resourceTags/user:"):]
	case strings.HasPrefix(lower, "resource_tags_aws_"):
		raw = "aws:" + trimmed[len("resource_tags_aws_"):]
	case strings.HasPrefix(lower, "resourcetags/aws:"):
		raw = "aws:" + trimmed[len("resourceTags/aws:"):]
	default:
		return "", false
	}
	return normalizeTagKey(raw), true
}

func normalizeTagKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	parts := strings.FieldsFunc(key, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, "")
}

func curTags(row []string, columns map[string]int) map[string]string {
	tags := map[string]string{}
	for key := range columns {
		if !strings.HasPrefix(key, "tag:") {
			continue
		}
		tagKey := strings.TrimPrefix(key, "tag:")
		if tagKey == "" {
			continue
		}
		if value := curValue(row, columns, key); value != "" {
			tags[tagKey] = value
		}
	}
	return tags
}

func curRawMetadata(row []string, columns map[string]int, source curSource) map[string]any {
	raw := map[string]any{
		"record_type":     recordTypeCURLineItem,
		"export_file_key": source.Key,
		"export_format":   source.Format,
	}
	for _, key := range []string{"bill_type", "line_item_type", "usage_type", "operation", "availability_zone", "product_code", "product_family", "pricing_term"} {
		if value := curValue(row, columns, key); value != "" {
			raw[strings.ReplaceAll(key, "-", "_")] = value
		}
	}
	if value := curValue(row, columns, "usage_type"); value != "" {
		raw["line_item_usage_type"] = value
	}
	if value := curValue(row, columns, "operation"); value != "" {
		raw["line_item_operation"] = value
	}
	if value := curValue(row, columns, "line_item_type"); value != "" {
		raw["line_item_line_item_type"] = value
	}
	if value := curValue(row, columns, "product_family"); value != "" {
		raw["product_product_family"] = value
	}
	return raw
}

func curRecordHash(source curSource, row []string, columns map[string]int, lineNumber int64, usageStart, usageEnd time.Time, accountID, region, service string) string {
	parts := []string{
		strconv.FormatInt(lineNumber, 10),
		usageStart.Format(time.RFC3339),
		usageEnd.Format(time.RFC3339),
		accountID,
		region,
		service,
		curValue(row, columns, "resource_id"),
		curValue(row, columns, "usage_type"),
		curValue(row, columns, "operation"),
		curValue(row, columns, "line_item_type"),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func regionFromAvailabilityZone(az string) string {
	az = strings.TrimSpace(az)
	if len(az) < 2 {
		return ""
	}
	last := az[len(az)-1]
	if last >= 'a' && last <= 'z' {
		return az[:len(az)-1]
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
