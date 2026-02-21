package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleExpensesSummary(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var total, dailyAvg float64
	var category string
	var categoryAmount float64

	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		FROM expenses
		WHERE DATE_TRUNC('month', expense_date) = DATE_TRUNC('month', CURRENT_DATE)
	`).Scan(&total)
	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(AVG(day_total), 0)
		FROM (
			SELECT expense_date, SUM(amount) AS day_total
			FROM expenses
			WHERE DATE_TRUNC('month', expense_date) = DATE_TRUNC('month', CURRENT_DATE)
			GROUP BY expense_date
		) t
	`).Scan(&dailyAvg)
	_ = s.db.QueryRow(ctx, `
		SELECT category, SUM(amount) AS total
		FROM expenses
		GROUP BY category
		ORDER BY total DESC
		LIMIT 1
	`).Scan(&category, &categoryAmount)

	respondJSON(w, http.StatusOK, map[string]any{
		"totalExpenses":   total,
		"dailyAverage":    dailyAvg,
		"largestCategory": category,
		"largestAmount":   categoryAmount,
	})
}

func (s *Server) handleExpenses(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	page, pageSize := parsePagination(r)
	search := parseSearch(r)
	offset := (page - 1) * pageSize

	var total int64
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM expenses
		WHERE ($1 = '' OR category ILIKE '%' || $1 || '%' OR item ILIKE '%' || $1 || '%' OR vendor ILIKE '%' || $1 || '%')
	`, search).Scan(&total)

	rows, err := s.db.Query(ctx, `
		SELECT id, expense_date, category, item, vendor, amount
		FROM expenses
		WHERE ($1 = '' OR category ILIKE '%' || $1 || '%' OR item ILIKE '%' || $1 || '%' OR vendor ILIKE '%' || $1 || '%')
		ORDER BY expense_date DESC, id DESC
		LIMIT $2 OFFSET $3
	`, search, pageSize, offset)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load expenses"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var d time.Time
		var category, item, vendor string
		var amount float64
		if err := rows.Scan(&id, &d, &category, &item, &vendor, &amount); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse expenses"})
			return
		}
		out = append(out, map[string]any{
			"id":        id,
			"date":      s.formatDateCompact(d),
			"dateRaw":   s.formatISODate(d),
			"category":  category,
			"item":      item,
			"vendor":    vendor,
			"amount":    formatKES(amount),
			"amountRaw": amount,
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"items":    out,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (s *Server) handleSalesSummary(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var total, netRevenue, vatCollected, dailyAvg float64
	var topProduct string
	var topAmount float64

	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(total_amount), 0), COALESCE(SUM(net_amount), 0), COALESCE(SUM(vat_amount), 0)
		FROM sales
		WHERE DATE_TRUNC('month', sale_date) = DATE_TRUNC('month', CURRENT_DATE)
	`).Scan(&total, &netRevenue, &vatCollected)
	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(AVG(day_total), 0)
		FROM (
			SELECT sale_date, SUM(total_amount) AS day_total
			FROM sales
			WHERE DATE_TRUNC('month', sale_date) = DATE_TRUNC('month', CURRENT_DATE)
			GROUP BY sale_date
		) t
	`).Scan(&dailyAvg)
	_ = s.db.QueryRow(ctx, `
		SELECT product, SUM(total_amount) AS total
		FROM sales
		GROUP BY product
		ORDER BY total DESC
		LIMIT 1
	`).Scan(&topProduct, &topAmount)

	respondJSON(w, http.StatusOK, map[string]any{
		"totalRevenue": total,
		"netRevenue":   netRevenue,
		"vatCollected": vatCollected,
		"dailyAverage": dailyAvg,
		"topProduct":   topProduct,
		"topAmount":    topAmount,
	})
}

func (s *Server) handleSales(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	page, pageSize := parsePagination(r)
	search := parseSearch(r)
	offset := (page - 1) * pageSize

	var totalRows int64
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM sales
		WHERE ($1 = '' OR product ILIKE '%' || $1 || '%' OR buyer ILIKE '%' || $1 || '%')
	`, search).Scan(&totalRows)

	rows, err := s.db.Query(ctx, `
		SELECT id, sale_date, product, quantity_value, quantity_unit, buyer, buyer_pin, delivery_county, delivery_subcounty,
		       vat_applicable, vat_rate, vat_amount, net_amount, price_per_unit, total_amount
		FROM sales
		WHERE ($1 = '' OR product ILIKE '%' || $1 || '%' OR buyer ILIKE '%' || $1 || '%')
		ORDER BY sale_date DESC, id DESC
		LIMIT $2 OFFSET $3
	`, search, pageSize, offset)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load sales"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var d time.Time
		var product, unit, buyer, buyerPIN, county, subcounty string
		var qty, price, total, vatRate, vatAmount, netAmount float64
		var vatApplicable bool
		if err := rows.Scan(
			&id, &d, &product, &qty, &unit, &buyer, &buyerPIN, &county, &subcounty,
			&vatApplicable, &vatRate, &vatAmount, &netAmount, &price, &total,
		); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse sales"})
			return
		}
		out = append(out, map[string]any{
			"id":                id,
			"date":              s.formatDateCompact(d),
			"dateRaw":           s.formatISODate(d),
			"product":           product,
			"quantity":          trimZero(qty) + " " + unit,
			"quantityValue":     qty,
			"quantityUnit":      unit,
			"buyer":             buyer,
			"buyerPIN":          buyerPIN,
			"deliveryCounty":    county,
			"deliverySubcounty": subcounty,
			"vatApplicable":     vatApplicable,
			"vatRate":           vatRate,
			"vatAmount":         vatAmount,
			"netAmount":         netAmount,
			"price":             formatKES(price),
			"pricePerUnit":      price,
			"total":             formatKES(total),
			"totalAmount":       total,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":    out,
		"total":    totalRows,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (s *Server) handleReportStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var grossRevenue, netRevenue, vatCollected, expense float64
	var animals int64
	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(total_amount),0), COALESCE(SUM(net_amount),0), COALESCE(SUM(vat_amount),0)
		FROM sales
		WHERE DATE_TRUNC('month', sale_date) = DATE_TRUNC('month', CURRENT_DATE)
	`).Scan(&grossRevenue, &netRevenue, &vatCollected)
	_ = s.db.QueryRow(ctx, `SELECT COALESCE(SUM(amount),0) FROM expenses WHERE DATE_TRUNC('month', expense_date) = DATE_TRUNC('month', CURRENT_DATE)`).Scan(&expense)
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM animals WHERE is_active = true`).Scan(&animals)

	profit := netRevenue - expense
	productivity := 0
	if netRevenue > 0 {
		productivity = int(math.Round((profit / netRevenue) * 100))
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"grossRevenue":     grossRevenue,
		"netRevenue":       netRevenue,
		"vatCollected":     vatCollected,
		"monthlyProfit":    profit,
		"totalAnimals":     animals,
		"operatingCosts":   expense,
		"productivityRate": productivity,
	})
}

func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	page, pageSize := parsePagination(r)
	offset := (page - 1) * pageSize
	var total int64
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM reports`).Scan(&total)

	rows, err := s.db.Query(ctx, `
		SELECT id, title, description, category, last_generated
		FROM reports
		ORDER BY last_generated DESC, id DESC
		LIMIT $1 OFFSET $2
	`, pageSize, offset)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load reports"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var title, description, category string
		var generated time.Time
		if err := rows.Scan(&id, &title, &description, &category, &generated); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse reports"})
			return
		}
		dateRange, format := parseReportMetadata(description)
		detail := fmt.Sprintf("Generated %s report for %s (%s)", strings.ToLower(normalizeReportType(category)), dateRange, format)
		out = append(out, map[string]any{
			"id":        id,
			"title":     title,
			"detail":    detail,
			"category":  category,
			"dateRange": dateRange,
			"format":    format,
			"generated": s.formatDateLong(generated),
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"items":    out,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (s *Server) handleUserStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var total, active, managers, vets int64
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&total)
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE status = 'active'`).Scan(&active)
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE r.name = 'manager'
	`).Scan(&managers)
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE r.name = 'veterinarian'
	`).Scan(&vets)

	respondJSON(w, http.StatusOK, map[string]any{
		"totalStaff":    total,
		"activeUsers":   active,
		"managers":      managers,
		"veterinarians": vets,
	})
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	page, pageSize := parsePagination(r)
	search := parseSearch(r)
	offset := (page - 1) * pageSize

	var total int64
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE ($1 = '' OR u.name ILIKE '%' || $1 || '%' OR u.email ILIKE '%' || $1 || '%' OR r.name ILIKE '%' || $1 || '%')
	`, search).Scan(&total)

	rows, err := s.db.Query(ctx, `
		SELECT u.id, u.name, r.name, u.email, u.phone, u.status
		FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE ($1 = '' OR u.name ILIKE '%' || $1 || '%' OR u.email ILIKE '%' || $1 || '%' OR r.name ILIKE '%' || $1 || '%')
		ORDER BY u.id
		LIMIT $2 OFFSET $3
	`, search, pageSize, offset)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load users"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var name, role, email, phone, status string
		if err := rows.Scan(&id, &name, &role, &email, &phone, &status); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse users"})
			return
		}
		initials := ""
		parts := strings.Fields(name)
		for _, p := range parts {
			initials += strings.ToUpper(p[:1])
			if len(initials) == 2 {
				break
			}
		}
		out = append(out, map[string]any{
			"id":       id,
			"initials": initials,
			"name":     name,
			"role":     capitalizeRole(role),
			"email":    email,
			"phone":    phone,
			"status":   status,
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"items":    out,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}
