package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleEtimsReceipts(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := s.db.Query(ctx, `
		SELECT e.id, e.sale_id, e.invoice_number, e.status, e.submitted_at
		FROM etims_submissions e
		ORDER BY e.submitted_at DESC, e.id DESC
		LIMIT 100
	`)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load local receipts"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, saleID int64
		var invoiceNo, status string
		var generated time.Time
		if err := rows.Scan(&id, &saleID, &invoiceNo, &status, &generated); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse local receipts"})
			return
		}
		out = append(out, map[string]any{
			"id":            id,
			"saleId":        saleID,
			"invoiceNumber": invoiceNo,
			"status":        status,
			"generatedAt":   s.formatDateLong(generated),
		})
	}
	respondJSON(w, http.StatusOK, out)
}

func (s *Server) handleEtimsGenerateReceipt(w http.ResponseWriter, r *http.Request) {
	saleID, err := parsePathID(r, "saleId")
	if err != nil || saleID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sale id"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var saleDate time.Time
	var product, quantityUnit, buyer, buyerPIN, county, subcounty string
	var quantityValue, pricePerUnit, totalAmount, vatRate, vatAmount, netAmount float64
	var vatApplicable bool
	err = s.db.QueryRow(ctx, `
		SELECT sale_date, product, quantity_value, quantity_unit, buyer, buyer_pin, delivery_county, delivery_subcounty,
		       vat_applicable, vat_rate, vat_amount, net_amount, price_per_unit, total_amount
		FROM sales
		WHERE id = $1
	`, saleID).Scan(
		&saleDate, &product, &quantityValue, &quantityUnit, &buyer, &buyerPIN, &county, &subcounty,
		&vatApplicable, &vatRate, &vatAmount, &netAmount, &pricePerUnit, &totalAmount,
	)
	if err != nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "sale not found"})
		return
	}

	invoiceNumber := fmt.Sprintf("FP-%06d", saleID)
	taxableAmount := netAmount
	if !vatApplicable || vatRate <= 0 {
		vatRate = 0
		vatAmount = 0
		taxableAmount = totalAmount
	}

	payload := map[string]any{
		"invoiceNumber": invoiceNumber,
		"invoiceDate":   s.formatISODate(saleDate),
		"currency":      "KES",
		"supplierPin":   s.kraPIN,
		"buyerName":     buyer,
		"buyerPin":      buyerPIN,
		"county":        county,
		"subcounty":     subcounty,
		"items": []map[string]any{
			{
				"description":   product,
				"quantity":      quantityValue,
				"unit":          quantityUnit,
				"unitPrice":     pricePerUnit,
				"vatApplicable": vatApplicable,
				"taxRate":       vatRate,
				"total":         totalAmount,
			},
		},
		"summary": map[string]any{
			"taxableAmount": taxableAmount,
			"vatAmount":     vatAmount,
			"grossAmount":   totalAmount,
		},
		"environment": "local",
		"mode":        "receipt_only",
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to encode local receipt"})
		return
	}

	responseJSON, _ := json.Marshal(map[string]any{
		"message": "generated locally",
		"at":      s.now().Format(time.RFC3339),
	})

	_, err = s.db.Exec(ctx, `
		INSERT INTO etims_submissions(sale_id, invoice_number, status, payload, submitted_at, response)
		VALUES ($1, $2, 'local_generated', $3::jsonb, NOW(), $4::jsonb)
		ON CONFLICT (sale_id) DO UPDATE SET
			invoice_number = EXCLUDED.invoice_number,
			status = EXCLUDED.status,
			payload = EXCLUDED.payload,
			submitted_at = EXCLUDED.submitted_at,
			response = EXCLUDED.response
	`, saleID, invoiceNumber, string(payloadJSON), string(responseJSON))
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save local receipt"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"saleId":        saleID,
		"invoiceNumber": invoiceNumber,
		"status":        "local_generated",
		"payload":       payload,
		"notice":        "Tax receipt generated.",
	})
}

func (s *Server) handleEtimsDownloadReceipt(w http.ResponseWriter, r *http.Request) {
	receiptID, err := parsePathID(r, "id")
	if err != nil || receiptID <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid receipt id"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var invoiceNumber string
	var payloadRaw []byte
	err = s.db.QueryRow(ctx, `
		SELECT invoice_number, payload
		FROM etims_submissions
		WHERE id = $1
	`, receiptID).Scan(&invoiceNumber, &payloadRaw)
	if err != nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "receipt not found"})
		return
	}

	format := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "PDF"
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid receipt payload"})
		return
	}

	filenameBase := reportFilename("receipt-" + strings.ToLower(invoiceNumber))
	switch format {
	case "PDF":
		receiptReport := receiptPayloadAsReport(receiptID, invoiceNumber, payload)
		if err := writePDFReport(w, receiptReport); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to render receipt pdf"})
		}
		return
	case "CSV":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.csv\"", filenameBase))

		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"field", "value"})
		_ = cw.Write([]string{"invoiceNumber", asString(payload["invoiceNumber"])})
		_ = cw.Write([]string{"invoiceDate", asString(payload["invoiceDate"])})
		_ = cw.Write([]string{"currency", asString(payload["currency"])})
		_ = cw.Write([]string{"supplierPin", asString(payload["supplierPin"])})
		_ = cw.Write([]string{"buyerName", asString(payload["buyerName"])})
		_ = cw.Write([]string{"buyerPin", asString(payload["buyerPin"])})
		_ = cw.Write([]string{"county", asString(payload["county"])})
		_ = cw.Write([]string{"subcounty", asString(payload["subcounty"])})
		_ = cw.Write([]string{})
		_ = cw.Write([]string{"description", "quantity", "unit", "unitPrice", "taxRate", "total"})

		if items, ok := payload["items"].([]any); ok {
			for _, it := range items {
				row, _ := it.(map[string]any)
				_ = cw.Write([]string{
					asString(row["description"]),
					asString(row["quantity"]),
					asString(row["unit"]),
					asString(row["unitPrice"]),
					asString(row["taxRate"]),
					asString(row["total"]),
				})
			}
		}
		cw.Flush()
		return
	case "JSON":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.json\"", filenameBase))
		out, _ := json.MarshalIndent(payload, "", "  ")
		_, _ = w.Write(out)
		return
	default:
		receiptReport := receiptPayloadAsReport(receiptID, invoiceNumber, payload)
		if err := writePDFReport(w, receiptReport); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to render receipt pdf"})
		}
		return
	}
}

func receiptPayloadAsReport(receiptID int64, invoiceNumber string, payload map[string]any) reportContent {
	summary := map[string]any{
		"invoiceNumber": asString(payload["invoiceNumber"]),
		"invoiceDate":   asString(payload["invoiceDate"]),
		"currency":      asString(payload["currency"]),
		"supplierPin":   asString(payload["supplierPin"]),
		"buyerName":     asString(payload["buyerName"]),
		"buyerPin":      asString(payload["buyerPin"]),
		"county":        asString(payload["county"]),
		"subcounty":     asString(payload["subcounty"]),
	}
	if totals, ok := payload["summary"].(map[string]any); ok {
		for k, v := range totals {
			summary[k] = v
		}
	}

	records := make([]map[string]any, 0)
	if items, ok := payload["items"].([]any); ok {
		for _, it := range items {
			if row, ok := it.(map[string]any); ok {
				records = append(records, map[string]any{
					"description":   asString(row["description"]),
					"quantity":      asString(row["quantity"]),
					"unit":          asString(row["unit"]),
					"unitPrice":     asString(row["unitPrice"]),
					"vatApplicable": asString(row["vatApplicable"]),
					"taxRate":       asString(row["taxRate"]),
					"total":         asString(row["total"]),
				})
			}
		}
	}

	return reportContent{
		ID:          receiptID,
		Title:       "Tax Receipt " + invoiceNumber,
		Category:    "Financial",
		DateRange:   "Single receipt",
		Format:      "PDF",
		GeneratedOn: time.Now().Format("2006-01-02"),
		Summary:     summary,
		Records:     records,
	}
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}
