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

func (s *Server) handleFeedingSummary(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var total, dailyAvg float64
	var topFeed string
	var topCost float64

	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(cost), 0)
		FROM feeding_records
		WHERE DATE_TRUNC('month', feed_date) = DATE_TRUNC('month', CURRENT_DATE)
	`).Scan(&total)
	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(AVG(day_total), 0)
		FROM (
			SELECT feed_date, SUM(cost) AS day_total
			FROM feeding_records
			WHERE DATE_TRUNC('month', feed_date) = DATE_TRUNC('month', CURRENT_DATE)
			GROUP BY feed_date
		) t
	`).Scan(&dailyAvg)
	_ = s.db.QueryRow(ctx, `
		SELECT feed_type, SUM(cost) AS total
		FROM feeding_records
		GROUP BY feed_type
		ORDER BY total DESC
		LIMIT 1
	`).Scan(&topFeed, &topCost)

	respondJSON(w, http.StatusOK, map[string]any{
		"totalCost":  total,
		"dailyAverage": dailyAvg,
		"topFeed":    topFeed,
		"topCost":    topCost,
	})
}

func (s *Server) handleFeeding(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	page, pageSize := parsePagination(r)
	search := parseSearch(r)
	offset := (page - 1) * pageSize

	var total int64
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM feeding_records f
		LEFT JOIN animals a ON a.id = f.animal_id
		WHERE ($1 = '' OR f.feed_type ILIKE '%' || $1 || '%' OR f.supplier ILIKE '%' || $1 || '%' OR COALESCE(a.tag_id,'') ILIKE '%' || $1 || '%')
	`, search).Scan(&total)

	rows, err := s.db.Query(ctx, `
		SELECT f.id, f.feed_date, COALESCE(a.tag_id, ''), f.feed_type, f.quantity_value, f.quantity_unit, f.supplier, f.cost, COALESCE(f.notes,''),
		       f.ration_id, COALESCE(r.name, ''), f.plan_id
		FROM feeding_records f
		LEFT JOIN animals a ON a.id = f.animal_id
		LEFT JOIN feeding_rations r ON r.id = f.ration_id
		WHERE ($1 = '' OR f.feed_type ILIKE '%' || $1 || '%' OR f.supplier ILIKE '%' || $1 || '%' OR COALESCE(a.tag_id,'') ILIKE '%' || $1 || '%')
		ORDER BY f.feed_date DESC, f.id DESC
		LIMIT $2 OFFSET $3
	`, search, pageSize, offset)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load feeding records"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var d time.Time
		var animalTag, feedType, unit, supplier, notes, rationName string
		var qty, cost float64
		var rationID, planID *int64
		if err := rows.Scan(&id, &d, &animalTag, &feedType, &qty, &unit, &supplier, &cost, &notes, &rationID, &rationName, &planID); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse feeding records"})
			return
		}
		rationIDOut := int64(0)
		if rationID != nil {
			rationIDOut = *rationID
		}
		planIDOut := int64(0)
		if planID != nil {
			planIDOut = *planID
		}
		out = append(out, map[string]any{
			"id":         id,
			"date":       s.formatDateCompact(d),
			"dateRaw":    s.formatISODate(d),
			"animalTag":  animalTag,
			"feedType":   feedType,
			"quantity":   fmt.Sprintf("%s %s", trimZero(qty), unit),
			"quantityValue": qty,
			"quantityUnit":  unit,
			"supplier":   supplier,
			"cost":       formatKES(cost),
			"costRaw":    cost,
			"notes":      notes,
			"rationId":   rationIDOut,
			"rationName": rationName,
			"planId":     planIDOut,
		})
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"items":    out,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (s *Server) handleFeedingRations(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	page, pageSize := parsePagination(r)
	search := parseSearch(r)
	offset := (page - 1) * pageSize

	var total int64
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM feeding_rations
		WHERE ($1 = '' OR name ILIKE '%' || $1 || '%' OR species ILIKE '%' || $1 || '%' OR state ILIKE '%' || $1 || '%')
	`, search).Scan(&total)

	rows, err := s.db.Query(ctx, `
		SELECT id, name, species, state, COALESCE(notes,'')
		FROM feeding_rations
		WHERE ($1 = '' OR name ILIKE '%' || $1 || '%' OR species ILIKE '%' || $1 || '%' OR state ILIKE '%' || $1 || '%')
		ORDER BY name
		LIMIT $2 OFFSET $3
	`, search, pageSize, offset)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load rations"})
		return
	}
	defer rows.Close()

	rations := make([]map[string]any, 0)
	rationIDs := make([]int64, 0)
	for rows.Next() {
		var id int64
		var name, species, state, notes string
		if err := rows.Scan(&id, &name, &species, &state, &notes); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse rations"})
			return
		}
			rations = append(rations, map[string]any{
				"id":      id,
				"name":    name,
				"species": species,
				"state":   state,
				"notes":   notes,
				"items":   []map[string]any{},
			})
			rationIDs = append(rationIDs, id)
		}

	if len(rationIDs) > 0 {
		rows, err = s.db.Query(ctx, `
			SELECT ration_id, ingredient, quantity_value, quantity_unit
			FROM feeding_ration_items
			WHERE ration_id = ANY($1)
			ORDER BY id
		`, rationIDs)
		if err == nil {
			itemsByRation := map[int64][]map[string]any{}
			for rows.Next() {
				var rid int64
				var ingredient, unit string
				var qty float64
				if err := rows.Scan(&rid, &ingredient, &qty, &unit); err != nil {
					continue
				}
				itemsByRation[rid] = append(itemsByRation[rid], map[string]any{
					"ingredient": ingredient,
					"quantity":   qty,
					"unit":       unit,
				})
			}
			rows.Close()
			for i := range rations {
				rid, _ := rations[i]["id"].(int64)
				rations[i]["items"] = itemsByRation[rid]
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":    rations,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (s *Server) handleFeedingPlans(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	page, pageSize := parsePagination(r)
	search := parseSearch(r)
	offset := (page - 1) * pageSize

	var total int64
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM feeding_plans p
		LEFT JOIN animals a ON a.id = p.animal_id
		LEFT JOIN feeding_rations r ON r.id = p.ration_id
		WHERE ($1 = '' OR COALESCE(a.tag_id,'') ILIKE '%' || $1 || '%' OR COALESCE(r.name,'') ILIKE '%' || $1 || '%' OR p.animal_state ILIKE '%' || $1 || '%')
	`, search).Scan(&total)

	rows, err := s.db.Query(ctx, `
		SELECT p.id, COALESCE(a.tag_id,''), COALESCE(r.name,''), p.ration_id, p.animal_state, p.daily_quantity_value, p.daily_quantity_unit,
		       p.start_date, p.end_date, p.status, COALESCE(p.notes,'')
		FROM feeding_plans p
		LEFT JOIN animals a ON a.id = p.animal_id
		LEFT JOIN feeding_rations r ON r.id = p.ration_id
		WHERE ($1 = '' OR COALESCE(a.tag_id,'') ILIKE '%' || $1 || '%' OR COALESCE(r.name,'') ILIKE '%' || $1 || '%' OR p.animal_state ILIKE '%' || $1 || '%')
		ORDER BY p.start_date DESC, p.id DESC
		LIMIT $2 OFFSET $3
	`, search, pageSize, offset)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load feeding plans"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var animalTag, rationName, unit, state, status, notes string
		var qty float64
		var start time.Time
		var end *time.Time
		var rationID *int64
		if err := rows.Scan(&id, &animalTag, &rationName, &rationID, &state, &qty, &unit, &start, &end, &status, &notes); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse feeding plans"})
			return
		}
		rid := int64(0)
		if rationID != nil {
			rid = *rationID
		}
		endOut := ""
		if end != nil {
			endOut = s.formatISODate(*end)
		}
		out = append(out, map[string]any{
			"id":              id,
			"animalTag":       animalTag,
			"rationId":        rid,
			"rationName":      rationName,
			"state":           state,
			"dailyQuantity":   fmt.Sprintf("%s %s", trimZero(qty), unit),
			"dailyQuantityValue": qty,
			"dailyQuantityUnit":  unit,
			"startDate":       s.formatISODate(start),
			"endDate":         endOut,
			"status":          status,
			"notes":           notes,
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
