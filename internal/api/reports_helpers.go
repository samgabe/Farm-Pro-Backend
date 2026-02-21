package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

type reportContent struct {
	ID          int64            `json:"id"`
	Title       string           `json:"title"`
	Category    string           `json:"category"`
	DateRange   string           `json:"dateRange"`
	Format      string           `json:"format"`
	GeneratedOn string           `json:"generatedOn"`
	Summary     map[string]any   `json:"summary"`
	Records     []map[string]any `json:"records"`
}

func normalizeReportType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "financial":
		return "Financial"
	case "health":
		return "Health"
	case "resources":
		return "Resources"
	case "sales":
		return "Sales"
	default:
		return "Financial"
	}
}

func normalizeDateRange(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "last 7 days":
		return "Last 7 days"
	case "last 30 days":
		return "Last 30 days"
	case "this month":
		return "This month"
	default:
		return "Last 7 days"
	}
}

func normalizeReportFormat(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "PDF":
		return "PDF"
	case "CSV":
		return "CSV"
	case "JSON":
		return "JSON"
	default:
		return "JSON"
	}
}

func parseReportMetadata(description string) (string, string) {
	dateRange := "Last 7 days"
	format := "JSON"
	desc := strings.TrimSpace(description)
	if desc == "" {
		return dateRange, format
	}

	lower := strings.ToLower(desc)
	if strings.Contains(lower, "| range=") {
		parts := strings.Split(desc, "|")
		for _, p := range parts {
			segment := strings.TrimSpace(p)
			switch {
			case strings.HasPrefix(strings.ToLower(segment), "range="):
				dateRange = normalizeDateRange(strings.TrimSpace(segment[len("range="):]))
			case strings.HasPrefix(strings.ToLower(segment), "format="):
				format = normalizeReportFormat(strings.TrimSpace(segment[len("format="):]))
			}
		}
		return dateRange, format
	}

	if i := strings.LastIndex(desc, "("); i >= 0 && strings.HasSuffix(desc, ")") && i < len(desc)-1 {
		format = normalizeReportFormat(desc[i+1 : len(desc)-1])
	}
	if i := strings.Index(strings.ToLower(desc), " for "); i >= 0 {
		rest := strings.TrimSpace(desc[i+5:])
		if j := strings.LastIndex(rest, " ("); j > 0 {
			rest = strings.TrimSpace(rest[:j])
		}
		dateRange = normalizeDateRange(rest)
	}

	return dateRange, format
}

func resolveDateRangeWindow(dateRange string, now time.Time) (time.Time, time.Time) {
	end := now
	switch normalizeDateRange(dateRange) {
	case "Last 30 days":
		return end.AddDate(0, 0, -29), end
	case "This month":
		return time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, end.Location()), end
	default:
		return end.AddDate(0, 0, -6), end
	}
}

func (s *Server) buildReportContent(ctx context.Context, id int64, title string, category string, dateRange string, generated time.Time, format string) (reportContent, error) {
	start, end := resolveDateRangeWindow(dateRange, time.Now())
	c := reportContent{
		ID:          id,
		Title:       title,
		Category:    normalizeReportType(category),
		DateRange:   normalizeDateRange(dateRange),
		Format:      normalizeReportFormat(format),
		GeneratedOn: s.formatISODate(generated),
		Summary:     map[string]any{},
		Records:     make([]map[string]any, 0),
	}

	switch c.Category {
	case "Health":
		var healthy, attention, sick int64
		_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM animals WHERE is_active = true AND health_status = 'healthy'`).Scan(&healthy)
		_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM animals WHERE is_active = true AND health_status = 'attention'`).Scan(&attention)
		_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM animals WHERE is_active = true AND health_status = 'sick'`).Scan(&sick)
		c.Summary["healthy"] = healthy
		c.Summary["attention"] = attention
		c.Summary["sick"] = sick

		rows, err := s.db.Query(ctx, `
			SELECT h.record_date, a.tag_id, h.action, h.treatment, h.veterinarian
			FROM health_records h
			JOIN animals a ON a.id = h.animal_id
			WHERE h.record_date BETWEEN $1 AND $2
			ORDER BY h.record_date DESC, h.id DESC
			LIMIT 250
		`, start.Format("2006-01-02"), end.Format("2006-01-02"))
		if err != nil {
			return c, err
		}
		defer rows.Close()
		for rows.Next() {
			var d time.Time
			var tagID, action, treatment, vet string
			if err := rows.Scan(&d, &tagID, &action, &treatment, &vet); err != nil {
				return c, err
			}
			c.Records = append(c.Records, map[string]any{
				"date":         s.formatISODate(d),
				"animalTagId":  tagID,
				"action":       action,
				"treatment":    treatment,
				"veterinarian": vet,
			})
		}

	case "Resources":
		var milk, wool, value float64
		var eggs int64
		_ = s.db.QueryRow(ctx, `
			SELECT COALESCE(SUM(milk_liters), 0), COALESCE(SUM(eggs_count), 0), COALESCE(SUM(wool_kg), 0), COALESCE(SUM(total_value), 0)
			FROM production_logs
			WHERE log_date BETWEEN $1 AND $2
		`, start.Format("2006-01-02"), end.Format("2006-01-02")).Scan(&milk, &eggs, &wool, &value)
		c.Summary["milkLiters"] = milk
		c.Summary["eggsCount"] = eggs
		c.Summary["woolKg"] = wool
		c.Summary["totalValue"] = value

		rows, err := s.db.Query(ctx, `
			SELECT log_date, milk_liters, eggs_count, wool_kg, total_value
			FROM production_logs
			WHERE log_date BETWEEN $1 AND $2
			ORDER BY log_date DESC
			LIMIT 250
		`, start.Format("2006-01-02"), end.Format("2006-01-02"))
		if err != nil {
			return c, err
		}
		defer rows.Close()
		for rows.Next() {
			var d time.Time
			var milkLiters, woolKg, totalValue float64
			var eggsCount int64
			if err := rows.Scan(&d, &milkLiters, &eggsCount, &woolKg, &totalValue); err != nil {
				return c, err
			}
			c.Records = append(c.Records, map[string]any{
				"date":       s.formatISODate(d),
				"milkLiters": milkLiters,
				"eggsCount":  eggsCount,
				"woolKg":     woolKg,
				"totalValue": totalValue,
			})
		}

	case "Sales":
		var grossRevenue, netRevenue, vatCollected float64
		var transactions int64
		_ = s.db.QueryRow(ctx, `
			SELECT COALESCE(SUM(total_amount), 0), COALESCE(SUM(net_amount), 0), COALESCE(SUM(vat_amount), 0), COUNT(*)
			FROM sales
			WHERE sale_date BETWEEN $1 AND $2
		`, start.Format("2006-01-02"), end.Format("2006-01-02")).Scan(&grossRevenue, &netRevenue, &vatCollected, &transactions)
		c.Summary["totalRevenue"] = grossRevenue
		c.Summary["grossRevenue"] = grossRevenue
		c.Summary["netRevenue"] = netRevenue
		c.Summary["vatCollected"] = vatCollected
		c.Summary["transactions"] = transactions

		rows, err := s.db.Query(ctx, `
			SELECT sale_date, product, quantity_value, quantity_unit, buyer, buyer_pin, vat_applicable, vat_rate, vat_amount, net_amount, price_per_unit, total_amount
			FROM sales
			WHERE sale_date BETWEEN $1 AND $2
			ORDER BY sale_date DESC, id DESC
			LIMIT 250
		`, start.Format("2006-01-02"), end.Format("2006-01-02"))
		if err != nil {
			return c, err
		}
		defer rows.Close()
		for rows.Next() {
			var d time.Time
			var product, unit, buyer, buyerPIN string
			var qty, price, total, vatRate, vatAmount, netAmount float64
			var vatApplicable bool
			if err := rows.Scan(&d, &product, &qty, &unit, &buyer, &buyerPIN, &vatApplicable, &vatRate, &vatAmount, &netAmount, &price, &total); err != nil {
				return c, err
			}
			c.Records = append(c.Records, map[string]any{
				"date":          s.formatISODate(d),
				"product":       product,
				"quantityValue": qty,
				"quantityUnit":  unit,
				"buyer":         buyer,
				"buyerPIN":      buyerPIN,
				"vatApplicable": vatApplicable,
				"vatRate":       vatRate,
				"vatAmount":     vatAmount,
				"netAmount":     netAmount,
				"pricePerUnit":  price,
				"totalAmount":   total,
			})
		}

	default:
		var grossRevenue, netRevenue, vatCollected, expense float64
		_ = s.db.QueryRow(ctx, `
			SELECT COALESCE(SUM(total_amount), 0), COALESCE(SUM(net_amount), 0), COALESCE(SUM(vat_amount), 0)
			FROM sales
			WHERE sale_date BETWEEN $1 AND $2
		`, start.Format("2006-01-02"), end.Format("2006-01-02")).Scan(&grossRevenue, &netRevenue, &vatCollected)
		_ = s.db.QueryRow(ctx, `
			SELECT COALESCE(SUM(amount), 0)
			FROM expenses
			WHERE expense_date BETWEEN $1 AND $2
		`, start.Format("2006-01-02"), end.Format("2006-01-02")).Scan(&expense)
		c.Summary["totalRevenue"] = grossRevenue
		c.Summary["grossRevenue"] = grossRevenue
		c.Summary["netRevenue"] = netRevenue
		c.Summary["vatCollected"] = vatCollected
		c.Summary["totalExpenses"] = expense
		c.Summary["profit"] = netRevenue - expense

		rows, err := s.db.Query(ctx, `
			SELECT entry_date, entry_type, item, amount
			FROM (
				SELECT sale_date AS entry_date, 'sale' AS entry_type, product AS item, total_amount AS amount
				FROM sales
				WHERE sale_date BETWEEN $1 AND $2
				UNION ALL
				SELECT expense_date AS entry_date, 'expense' AS entry_type, item, amount * -1
				FROM expenses
				WHERE expense_date BETWEEN $1 AND $2
			) t
			ORDER BY entry_date DESC
			LIMIT 250
		`, start.Format("2006-01-02"), end.Format("2006-01-02"))
		if err != nil {
			return c, err
		}
		defer rows.Close()
		for rows.Next() {
			var d time.Time
			var kind, item string
			var amount float64
			if err := rows.Scan(&d, &kind, &item, &amount); err != nil {
				return c, err
			}
			c.Records = append(c.Records, map[string]any{
				"date":   s.formatISODate(d),
				"type":   kind,
				"item":   item,
				"amount": amount,
			})
		}
	}

	return c, nil
}

func writePDFReport(w http.ResponseWriter, report reportContent) error {
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.pdf\"", reportFilename(report.Title)))

	summaryKeys := make([]string, 0, len(report.Summary))
	for k := range report.Summary {
		summaryKeys = append(summaryKeys, k)
	}
	sort.Strings(summaryKeys)
	summaryLines := make([]string, 0, len(summaryKeys))
	for _, k := range orderedReportSummaryKeys(report.Category, summaryKeys) {
		summaryLines = append(summaryLines, fmt.Sprintf("%s: %s", prettyMetricLabel(k), formatSummaryValue(k, report.Summary[k])))
	}
	if len(summaryLines) == 0 {
		summaryLines = append(summaryLines, "No summary metrics available.")
	}

	recordHeaders, recordRows, overflowNote := buildRecordHighlights(report)
	pdf := buildStyledPDF(report, summaryLines, recordHeaders, recordRows, overflowNote)
	_, err := w.Write(pdf)
	return err
}

func buildStyledPDF(report reportContent, summaryLines []string, recordHeaders []string, recordRows [][]string, overflowNote string) []byte {
	accentR, accentG, accentB := categoryTheme(report.Category)

	var stream bytes.Buffer
	stream.WriteString("0.95 0.94 0.91 rg 0 0 595 842 re f\n")
	stream.WriteString("0.91 0.90 0.86 rg 0 0 595 220 re f\n")
	stream.WriteString(fmt.Sprintf("%.2f %.2f %.2f rg 0 758 595 84 re f\n", accentR, accentG, accentB))
	stream.WriteString(fmt.Sprintf("%.2f %.2f %.2f rg 0 742 595 16 re f\n", accentR*0.85, accentG*0.85, accentB*0.85))

	stream.WriteString("1 1 1 rg BT /F2 24 Tf 48 806 Td (FarmPro Report) Tj ET\n")
	stream.WriteString("0.92 0.96 0.94 rg BT /F1 10 Tf 48 786 Td (Operational export summary) Tj ET\n")
	stream.WriteString("1 1 1 rg BT /F1 10 Tf 435 806 Td (")
	stream.WriteString(pdfEscape(fmt.Sprintf("Report #%d", report.ID)))
	stream.WriteString(") Tj ET\n")

	stream.WriteString("0.98 0.98 0.97 rg 36 610 523 132 re f\n")
	stream.WriteString("0.86 0.84 0.79 RG 1 w 36 610 523 132 re S\n")
	stream.WriteString(fmt.Sprintf("%.2f %.2f %.2f rg 36 610 6 132 re f\n", accentR, accentG, accentB))
	stream.WriteString("0.18 0.20 0.18 rg BT /F2 13 Tf 50 722 Td (Report Snapshot) Tj ET\n")

	metaLines := []string{
		fmt.Sprintf("Title: %s", report.Title),
		fmt.Sprintf("Category: %s", report.Category),
		fmt.Sprintf("Date Range: %s", report.DateRange),
		fmt.Sprintf("Generated On: %s", report.GeneratedOn),
	}
	y := 703
	for _, line := range metaLines {
		for _, wrapped := range wrapPDFText(line, 78) {
			stream.WriteString(fmt.Sprintf("0.20 0.22 0.20 rg BT /F1 10 Tf 50 %d Td (%s) Tj ET\n", y, pdfEscape(wrapped)))
			y -= 14
			if y < 622 {
				break
			}
		}
	}

	stream.WriteString("0.99 0.98 0.96 rg 36 466 523 128 re f\n")
	stream.WriteString("0.86 0.84 0.79 RG 1 w 36 466 523 128 re S\n")
	stream.WriteString(fmt.Sprintf("%.2f %.2f %.2f rg 36 586 523 8 re f\n", accentR, accentG, accentB))
	stream.WriteString("0.18 0.20 0.18 rg BT /F2 13 Tf 50 574 Td (Summary Metrics) Tj ET\n")

	cardW := 161
	cardX := []int{50, 216, 382}
	cardY := []int{538, 500}
	cardIdx := 0
	for _, line := range summaryLines {
		if cardIdx >= 6 {
			break
		}
		key, value := parseMetricLine(line)
		x := cardX[cardIdx%3]
		yy := cardY[cardIdx/3]
		stream.WriteString("0.97 0.96 0.93 rg ")
		stream.WriteString(fmt.Sprintf("%d %d %d 30 re f\n", x, yy, cardW))
		stream.WriteString("0.86 0.84 0.79 RG 0.8 w ")
		stream.WriteString(fmt.Sprintf("%d %d %d 30 re S\n", x, yy, cardW))
		stream.WriteString("0.30 0.32 0.30 rg BT /F1 8 Tf ")
		stream.WriteString(fmt.Sprintf("%d %d Td (%s) Tj ET\n", x+8, yy+18, pdfEscape(shortenPDFText(key, 26))))
		stream.WriteString(fmt.Sprintf("%.2f %.2f %.2f rg BT /F2 10 Tf ", accentR*0.9, accentG*0.9, accentB*0.9))
		stream.WriteString(fmt.Sprintf("%d %d Td (%s) Tj ET\n", x+8, yy+7, pdfEscape(shortenPDFText(value, 24))))
		cardIdx++
	}
	if cardIdx == 0 {
		stream.WriteString("0.30 0.32 0.30 rg BT /F1 10 Tf 52 526 Td (No summary metrics available.) Tj ET\n")
	}

	stream.WriteString("0.99 0.98 0.96 rg 36 48 523 402 re f\n")
	stream.WriteString("0.86 0.84 0.79 RG 1 w 36 48 523 402 re S\n")
	stream.WriteString(fmt.Sprintf("%.2f %.2f %.2f rg 36 442 523 8 re f\n", accentR, accentG, accentB))
	stream.WriteString("0.18 0.20 0.18 rg BT /F2 13 Tf 50 430 Td (Record Highlights) Tj ET\n")

	if len(recordHeaders) == 0 {
		recordHeaders = []string{"#", "Details"}
	}
	columnWidths := highlightColumnWidths(report.Category, len(recordHeaders))
	headerY := 406
	stream.WriteString(fmt.Sprintf("0.94 0.94 0.92 rg 50 %d 496 20 re f\n", headerY))
	stream.WriteString("0.30 0.32 0.30 rg ")
	x := 58
	for i, h := range recordHeaders {
		stream.WriteString(fmt.Sprintf("BT /F2 8 Tf %d %d Td (%s) Tj ET\n", x, headerY+6, pdfEscape(shortenPDFText(h, 22))))
		if i < len(columnWidths) {
			x += columnWidths[i]
		}
	}

	y = 390
	rowH := 22
	for rowIdx, row := range recordRows {
		if y < 70 {
			break
		}
		if rowIdx%2 == 0 {
			stream.WriteString("0.98 0.97 0.95 rg ")
		} else {
			stream.WriteString("0.96 0.95 0.92 rg ")
		}
		stream.WriteString(fmt.Sprintf("50 %d 496 %d re f\n", y-4, rowH))
		stream.WriteString("0.88 0.86 0.82 RG 0.3 w ")
		stream.WriteString(fmt.Sprintf("50 %d 496 %d re S\n", y-4, rowH))

		x = 58
		for i, cell := range row {
			width := 92
			if i < len(columnWidths) {
				width = columnWidths[i]
			}
			maxChars := intMax(6, (width-10)/4)
			stream.WriteString("0.24 0.26 0.24 rg BT /F1 7 Tf ")
			stream.WriteString(fmt.Sprintf("%d %d Td (%s) Tj ET\n", x, y+5, pdfEscape(shortenPDFText(cell, maxChars))))
			x += width
		}
		y -= rowH
	}
	if overflowNote != "" {
		stream.WriteString("0.30 0.32 0.30 rg BT /F1 8 Tf 58 60 Td (")
		stream.WriteString(pdfEscape(overflowNote))
		stream.WriteString(") Tj ET\n")
	}

	stream.WriteString(fmt.Sprintf("%.2f %.2f %.2f rg 0 0 595 28 re f\n", accentR*0.9, accentG*0.9, accentB*0.9))
	stream.WriteString("0.94 0.96 0.94 rg BT /F1 9 Tf 48 11 Td (Generated by FarmPro Analytics Engine) Tj ET\n")
	stream.WriteString("0.94 0.96 0.94 rg BT /F1 9 Tf 430 11 Td (")
	stream.WriteString(pdfEscape(report.GeneratedOn))
	stream.WriteString(") Tj ET\n")

	streamStr := stream.String()

	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources << /Font << /F1 5 0 R /F2 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(streamStr), streamStr),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>",
	}

	var body bytes.Buffer
	offsets := make([]int, len(objects)+1)
	body.WriteString("%PDF-1.4\n")
	for i, obj := range objects {
		offsets[i+1] = body.Len()
		body.WriteString(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", i+1, obj))
	}

	xrefStart := body.Len()
	body.WriteString(fmt.Sprintf("xref\n0 %d\n", len(objects)+1))
	body.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		body.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[i]))
	}
	body.WriteString("trailer\n")
	body.WriteString(fmt.Sprintf("<< /Size %d /Root 1 0 R >>\n", len(objects)+1))
	body.WriteString("startxref\n")
	body.WriteString(fmt.Sprintf("%d\n", xrefStart))
	body.WriteString("%%EOF")
	return body.Bytes()
}

func wrapPDFText(input string, max int) []string {
	v := strings.TrimSpace(input)
	if v == "" {
		return []string{""}
	}
	words := strings.Fields(v)
	if len(words) == 0 {
		return []string{""}
	}
	lines := make([]string, 0)
	current := words[0]
	for i := 1; i < len(words); i++ {
		next := words[i]
		if len(current)+1+len(next) <= max {
			current += " " + next
			continue
		}
		lines = append(lines, current)
		current = next
	}
	lines = append(lines, current)
	return lines
}

func categoryTheme(category string) (float64, float64, float64) {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "health":
		return 0.66, 0.27, 0.24
	case "resources":
		return 0.31, 0.40, 0.18
	case "sales":
		return 0.12, 0.34, 0.48
	default:
		return 0.14, 0.45, 0.30
	}
}

func parseMetricLine(line string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
	if len(parts) != 2 {
		return "metric", strings.TrimSpace(line)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func shortenPDFText(s string, max int) string {
	v := strings.TrimSpace(s)
	if max <= 3 || len(v) <= max {
		return v
	}
	return v[:max-3] + "..."
}

func orderedReportSummaryKeys(category string, summaryKeys []string) []string {
	priorityByCategory := map[string][]string{
		"financial": {"grossRevenue", "netRevenue", "profit", "totalExpenses", "totalRevenue", "vatCollected"},
		"sales":     {"grossRevenue", "netRevenue", "totalRevenue", "vatCollected", "transactions"},
		"resources": {"totalValue", "milkLiters", "eggsCount", "woolKg"},
		"health":    {"healthy", "attention", "sick"},
	}
	priority := priorityByCategory[strings.ToLower(strings.TrimSpace(category))]
	if len(priority) == 0 {
		return summaryKeys
	}

	seen := make(map[string]struct{}, len(summaryKeys))
	out := make([]string, 0, len(summaryKeys))
	for _, k := range summaryKeys {
		seen[k] = struct{}{}
	}
	for _, k := range priority {
		if _, ok := seen[k]; ok {
			out = append(out, k)
			delete(seen, k)
		}
	}
	for _, k := range summaryKeys {
		if _, ok := seen[k]; ok {
			out = append(out, k)
			delete(seen, k)
		}
	}
	return out
}

func prettyMetricLabel(raw string) string {
	known := map[string]string{
		"grossRevenue":  "Gross Revenue",
		"netRevenue":    "Net Revenue",
		"profit":        "Profit",
		"totalExpenses": "Total Expenses",
		"totalRevenue":  "Total Revenue",
		"vatCollected":  "VAT Collected",
		"transactions":  "Transactions",
		"milkLiters":    "Milk (Liters)",
		"eggsCount":     "Eggs Count",
		"woolKg":        "Wool (Kg)",
		"totalValue":    "Total Value",
		"healthy":       "Healthy Animals",
		"attention":     "Needs Attention",
		"sick":          "Sick Animals",
	}
	if v, ok := known[raw]; ok {
		return v
	}
	if raw == "" {
		return "Metric"
	}
	return raw
}

func formatSummaryValue(key string, value any) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "grossrevenue", "netrevenue", "profit", "totalexpenses", "totalrevenue", "vatcollected", "totalvalue":
		return formatAnyCurrency(value)
	case "transactions", "healthy", "attention", "sick", "eggscount":
		if n, ok := asFloat64(value); ok {
			return fmt.Sprintf("%d", int64(math.Round(n)))
		}
		return fmt.Sprint(value)
	case "milkliters", "woolkg":
		return formatAnyNumber(value)
	default:
		if n, ok := asFloat64(value); ok {
			return trimZero(n)
		}
		return fmt.Sprint(value)
	}
}

func buildRecordHighlights(report reportContent) ([]string, [][]string, string) {
	rows := make([][]string, 0)
	if len(report.Records) == 0 {
		return []string{"#", "Details"}, [][]string{{"1", "No records available in this range."}}, ""
	}

	headersByCategory := map[string][]string{
		"financial": {"#", "Date", "Type", "Item", "Amount"},
		"sales":     {"#", "Date", "Product", "Qty", "Buyer", "Total"},
		"resources": {"#", "Date", "Milk (L)", "Eggs", "Wool (Kg)", "Value"},
		"health":    {"#", "Date", "Tag", "Action", "Treatment", "Vet"},
	}
	category := strings.ToLower(strings.TrimSpace(report.Category))
	headers := headersByCategory[category]
	if len(headers) == 0 {
		headers = []string{"#", "Details"}
	}

	maxRows := len(report.Records)
	if maxRows > 12 {
		maxRows = 12
	}
	for i := 0; i < maxRows; i++ {
		r := report.Records[i]
		switch category {
		case "financial":
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1),
				fmt.Sprint(r["date"]),
				titleWord(fmt.Sprint(r["type"])),
				fmt.Sprint(r["item"]),
				formatAnyCurrency(r["amount"]),
			})
		case "sales":
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1),
				fmt.Sprint(r["date"]),
				fmt.Sprint(r["product"]),
				strings.TrimSpace(fmt.Sprintf("%s %s", formatAnyNumber(r["quantityValue"]), fmt.Sprint(r["quantityUnit"]))),
				fmt.Sprint(r["buyer"]),
				formatAnyCurrency(r["totalAmount"]),
			})
		case "resources":
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1),
				fmt.Sprint(r["date"]),
				formatAnyNumber(r["milkLiters"]),
				formatAnyNumber(r["eggsCount"]),
				formatAnyNumber(r["woolKg"]),
				formatAnyCurrency(r["totalValue"]),
			})
		case "health":
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1),
				fmt.Sprint(r["date"]),
				fmt.Sprint(r["animalTagId"]),
				fmt.Sprint(r["action"]),
				fmt.Sprint(r["treatment"]),
				fmt.Sprint(r["veterinarian"]),
			})
		default:
			rows = append(rows, []string{
				fmt.Sprintf("%d", i+1),
				formatRecordHighlight(report.Category, r),
			})
		}
	}

	overflow := ""
	if len(report.Records) > maxRows {
		overflow = fmt.Sprintf("%d additional records not shown.", len(report.Records)-maxRows)
	}
	return headers, rows, overflow
}

func highlightColumnWidths(category string, headerCount int) []int {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "financial":
		return []int{26, 82, 62, 218, 96}
	case "sales":
		return []int{24, 70, 118, 72, 134, 78}
	case "resources":
		return []int{24, 70, 88, 64, 88, 86}
	case "health":
		return []int{24, 66, 64, 88, 142, 112}
	default:
		if headerCount <= 2 {
			return []int{24, 472}
		}
		widths := make([]int, headerCount)
		widths[0] = 24
		remaining := 472
		if headerCount > 1 {
			each := remaining / (headerCount - 1)
			for i := 1; i < headerCount; i++ {
				widths[i] = each
			}
		}
		return widths
	}
}

func formatRecordHighlight(category string, record map[string]any) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "financial":
		date := fmt.Sprint(record["date"])
		item := fmt.Sprint(record["item"])
		kind := titleWord(fmt.Sprint(record["type"]))
		amount := formatAnyCurrency(record["amount"])
		return fmt.Sprintf("%s | %s | %s | %s", date, kind, item, amount)
	case "sales":
		date := fmt.Sprint(record["date"])
		product := fmt.Sprint(record["product"])
		buyer := fmt.Sprint(record["buyer"])
		qty := strings.TrimSpace(fmt.Sprintf("%s %s", formatAnyNumber(record["quantityValue"]), fmt.Sprint(record["quantityUnit"])))
		total := formatAnyCurrency(record["totalAmount"])
		return fmt.Sprintf("%s | %s | %s | %s | %s", date, product, qty, buyer, total)
	case "resources":
		date := fmt.Sprint(record["date"])
		milk := formatAnyNumber(record["milkLiters"])
		eggs := formatAnyNumber(record["eggsCount"])
		wool := formatAnyNumber(record["woolKg"])
		total := formatAnyCurrency(record["totalValue"])
		return fmt.Sprintf("%s | Milk %s L | Eggs %s | Wool %s Kg | %s", date, milk, eggs, wool, total)
	case "health":
		date := fmt.Sprint(record["date"])
		tag := fmt.Sprint(record["animalTagId"])
		action := fmt.Sprint(record["action"])
		treatment := fmt.Sprint(record["treatment"])
		vet := fmt.Sprint(record["veterinarian"])
		return fmt.Sprintf("%s | Tag %s | %s | %s | Vet: %s", date, tag, action, treatment, vet)
	default:
		keys := make([]string, 0, len(record))
		for k := range record {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%v", k, record[k]))
		}
		return strings.Join(parts, " | ")
	}
}

func titleWord(v string) string {
	s := strings.ToLower(strings.TrimSpace(v))
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func asFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

func formatAnyCurrency(v any) string {
	if n, ok := asFloat64(v); ok {
		return formatKES(n)
	}
	return fmt.Sprint(v)
}

func formatAnyNumber(v any) string {
	if n, ok := asFloat64(v); ok {
		return trimZero(n)
	}
	return fmt.Sprint(v)
}

func intMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func pdfEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "(", "\\(")
	s = strings.ReplaceAll(s, ")", "\\)")
	return s
}

func writeCSVReport(w http.ResponseWriter, report reportContent) error {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.csv\"", reportFilename(report.Title)))

	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"report_id", fmt.Sprintf("%d", report.ID)}); err != nil {
		return err
	}
	if err := cw.Write([]string{"title", report.Title}); err != nil {
		return err
	}
	if err := cw.Write([]string{"category", report.Category}); err != nil {
		return err
	}
	if err := cw.Write([]string{"date_range", report.DateRange}); err != nil {
		return err
	}
	if err := cw.Write([]string{"generated_on", report.GeneratedOn}); err != nil {
		return err
	}
	if err := cw.Write([]string{}); err != nil {
		return err
	}

	if err := cw.Write([]string{"summary_key", "summary_value"}); err != nil {
		return err
	}
	keys := make([]string, 0, len(report.Summary))
	for k := range report.Summary {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := cw.Write([]string{k, fmt.Sprint(report.Summary[k])}); err != nil {
			return err
		}
	}
	if err := cw.Write([]string{}); err != nil {
		return err
	}

	if len(report.Records) == 0 {
		if err := cw.Write([]string{"records", "none"}); err != nil {
			return err
		}
		cw.Flush()
		return cw.Error()
	}

	recordKeys := make([]string, 0, len(report.Records[0]))
	for k := range report.Records[0] {
		recordKeys = append(recordKeys, k)
	}
	sort.Strings(recordKeys)
	if err := cw.Write(recordKeys); err != nil {
		return err
	}
	for _, record := range report.Records {
		row := make([]string, 0, len(recordKeys))
		for _, k := range recordKeys {
			row = append(row, fmt.Sprint(record[k]))
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func reportFilename(title string) string {
	base := strings.ToLower(strings.TrimSpace(title))
	base = strings.ReplaceAll(base, " ", "-")
	base = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, base)
	if base == "" {
		return "report"
	}
	return base
}
