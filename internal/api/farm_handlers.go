package api

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var totalAnimals, sickAnimals, upcomingVaccines int64
	var monthlyGrossRevenue, monthlyNetRevenue, monthlyVATCollected float64

	_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM animals WHERE is_active = true`).Scan(&totalAnimals)
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM animals WHERE health_status <> 'healthy' AND is_active = true`).Scan(&sickAnimals)
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM health_records WHERE next_due >= CURRENT_DATE AND next_due <= CURRENT_DATE + INTERVAL '7 days'`).Scan(&upcomingVaccines)
	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(total_amount), 0), COALESCE(SUM(net_amount), 0), COALESCE(SUM(vat_amount), 0)
		FROM sales
		WHERE DATE_TRUNC('month', sale_date) = DATE_TRUNC('month', CURRENT_DATE)
	`).Scan(&monthlyGrossRevenue, &monthlyNetRevenue, &monthlyVATCollected)

	typeCounts := make([]map[string]any, 0)
	rows, err := s.db.Query(ctx, `
		SELECT type, COUNT(*)
		FROM animals
		WHERE is_active = true
		GROUP BY type
		ORDER BY type
	`)
	if err == nil {
		for rows.Next() {
			var typ string
			var count int64
			if err := rows.Scan(&typ, &count); err != nil {
				break
			}
			typeCounts = append(typeCounts, map[string]any{
				"type":  typ,
				"count": count,
			})
		}
		rows.Close()
	}

	typeAttention := make([]map[string]any, 0)
	rows, err = s.db.Query(ctx, `
		SELECT type, COUNT(*)
		FROM animals
		WHERE is_active = true AND health_status <> 'healthy'
		GROUP BY type
		ORDER BY type
	`)
	if err == nil {
		for rows.Next() {
			var typ string
			var count int64
			if err := rows.Scan(&typ, &count); err != nil {
				break
			}
			typeAttention = append(typeAttention, map[string]any{
				"type":  typ,
				"count": count,
			})
		}
		rows.Close()
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"totalAnimals":         totalAnimals,
		"sickAnimals":          sickAnimals,
		"upcomingVaccinations": upcomingVaccines,
		"monthlyRevenue":       monthlyGrossRevenue,
		"monthlyGrossRevenue":  monthlyGrossRevenue,
		"monthlyNetRevenue":    monthlyNetRevenue,
		"monthlyVATCollected":  monthlyVATCollected,
		"animalTypeCounts":     typeCounts,
		"animalTypeAttention":  typeAttention,
	})
}

func (s *Server) handleAnimals(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	page, pageSize := parsePagination(r)
	search := parseSearch(r)
	offset := (page - 1) * pageSize

	var total int64
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM animals
		WHERE is_active = true
			AND ($1 = '' OR tag_id ILIKE '%' || $1 || '%' OR type ILIKE '%' || $1 || '%' OR breed ILIKE '%' || $1 || '%')
	`, search).Scan(&total)

	rows, err := s.db.Query(ctx, `
		SELECT id, tag_id, type, breed, birth_date,
			CASE
				WHEN birth_date IS NULL THEN 'N/A'
				WHEN CURRENT_DATE - birth_date < 30 THEN (CURRENT_DATE - birth_date)::int::text || ' days'
				WHEN AGE(CURRENT_DATE, birth_date) < INTERVAL '1 year' THEN EXTRACT(MONTH FROM AGE(CURRENT_DATE, birth_date))::int::text || ' months'
				ELSE EXTRACT(YEAR FROM AGE(CURRENT_DATE, birth_date))::int::text || ' years'
			END AS age,
			COALESCE(weight_kg::text || ' kg', 'N/A') AS weight,
			health_status, status
		FROM animals
		WHERE is_active = true
			AND ($1 = '' OR tag_id ILIKE '%' || $1 || '%' OR type ILIKE '%' || $1 || '%' OR breed ILIKE '%' || $1 || '%')
		ORDER BY tag_id
		LIMIT $2 OFFSET $3
	`, search, pageSize, offset)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load animals"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var birthDate *time.Time
		var tagID, typ, breed, age, weight, health, status string
		if err := rows.Scan(&id, &tagID, &typ, &breed, &birthDate, &age, &weight, &health, &status); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse animals"})
			return
		}
		birthDateRaw := ""
		if birthDate != nil {
			birthDateRaw = s.formatISODate(*birthDate)
		}
		out = append(out, map[string]any{
			"id":        id,
			"tagId":     tagID,
			"type":      typ,
			"breed":     breed,
			"birthDate": birthDateRaw,
			"age":       age,
			"weight":    weight,
			"health":    health,
			"status":    status,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":    out,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

func (s *Server) handleUpcomingVaccinations(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := s.db.Query(ctx, `
		SELECT a.tag_id, a.type, h.treatment, h.next_due,
			(h.next_due - CURRENT_DATE)::int
		FROM health_records h
		JOIN animals a ON a.id = h.animal_id
		WHERE h.next_due >= CURRENT_DATE
		ORDER BY h.next_due
		LIMIT 20
	`)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load upcoming vaccinations"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id, animal, treatment string
		var due time.Time
		var days int
		if err := rows.Scan(&id, &animal, &treatment, &due, &days); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse upcoming vaccinations"})
			return
		}
		out = append(out, map[string]any{
			"id":        id,
			"animal":    animal,
			"treatment": treatment,
			"dueDate":   s.formatDate(due),
			"remaining": fmt.Sprintf("%d days remaining", days),
		})
	}

	respondJSON(w, http.StatusOK, out)
}

func (s *Server) handleHealthRecords(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := s.db.Query(ctx, `
		SELECT h.id, a.tag_id, h.action, h.treatment, h.record_date, h.veterinarian, h.next_due
		FROM health_records h
		JOIN animals a ON a.id = h.animal_id
		ORDER BY h.record_date DESC
	`)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load health records"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var animalID, action, treatment, vet string
		var date time.Time
		var nextDue *time.Time
		if err := rows.Scan(&id, &animalID, &action, &treatment, &date, &vet, &nextDue); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse health records"})
			return
		}
		next := "N/A"
		if nextDue != nil {
			next = s.formatDate(*nextDue)
		}
		out = append(out, map[string]any{
			"id":        id,
			"animalId":  animalID,
			"action":    action,
			"treatment": treatment,
			"date":      s.formatDate(date),
			"vet":       vet,
			"nextDue":   next,
		})
	}

	respondJSON(w, http.StatusOK, out)
}

func (s *Server) handleBreedingActive(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := s.db.Query(ctx, `
		SELECT b.id, m.tag_id, COALESCE(f.tag_id, ''), b.species, b.breeding_date, b.heat_date, b.ai_date, b.on_heat,
			b.ai_sire_source, COALESCE(b.ai_sire_name, ''), COALESCE(b.ai_sire_code, ''), b.expected_birth_date, COALESCE(b.notes, '')
		FROM breeding_records b
		JOIN animals m ON m.id = b.mother_animal_id
		LEFT JOIN animals f ON f.id = b.father_animal_id
		WHERE b.status = 'active'
		ORDER BY COALESCE(b.expected_birth_date, b.breeding_date) DESC
	`)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load breeding records"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var mother, father, species, notes string
		var aiSource, aiName, aiCode string
		var breedDate time.Time
		var heatDate, aiDate, expected *time.Time
		var onHeat bool
		if err := rows.Scan(&id, &mother, &father, &species, &breedDate, &heatDate, &aiDate, &onHeat, &aiSource, &aiName, &aiCode, &expected, &notes); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse breeding records"})
			return
		}

		progress := 0
		daysRemaining := 0
		if expected != nil {
			totalDays := int(expected.Sub(breedDate).Hours() / 24)
			elapsed := int(time.Since(breedDate).Hours() / 24)
			if totalDays > 0 {
				progress = int(math.Max(0, math.Min(100, float64(elapsed*100)/float64(totalDays))))
			}
			daysRemaining = int(time.Until(*expected).Hours() / 24)
			if daysRemaining < 0 {
				daysRemaining = 0
			}
		}

		heatOut := ""
		if heatDate != nil {
			heatOut = s.formatDate(*heatDate)
		}
		aiOut := ""
		if aiDate != nil {
			aiOut = s.formatDate(*aiDate)
		}
		expectedOut := ""
		if expected != nil {
			expectedOut = s.formatDate(*expected)
		}
		remainingOut := "N/A"
		if expected != nil {
			remainingOut = fmt.Sprintf("%d days", daysRemaining)
		}

		out = append(out, map[string]any{
			"id":        id,
			"mother":    mother,
			"father":    father,
			"animal":    species,
			"breedDate": s.formatDate(breedDate),
			"heatDate":  heatOut,
			"aiDate":    aiOut,
			"onHeat":    onHeat,
			"aiSource":  aiSource,
			"aiName":    aiName,
			"aiCode":    aiCode,
			"expected":  expectedOut,
			"days":      remainingOut,
			"progress":  progress,
			"notes":     notes,
		})
	}

	respondJSON(w, http.StatusOK, out)
}

func (s *Server) handleBreedingPoultryActive(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := s.db.Query(ctx, `
		SELECT p.id, h.tag_id, COALESCE(r.tag_id, ''), p.species, p.egg_set_date, p.hatch_date,
			p.eggs_set, COALESCE(p.chicks_hatched, 0), p.status, COALESCE(p.notes, '')
		FROM poultry_breeding_records p
		JOIN animals h ON h.id = p.hen_animal_id
		LEFT JOIN animals r ON r.id = p.rooster_animal_id
		WHERE p.status = 'active'
		ORDER BY p.egg_set_date DESC
	`)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load poultry breeding records"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var hen, rooster, species, status, notes string
		var eggSet time.Time
		var hatch *time.Time
		var eggsSet, chicksHatched int
		if err := rows.Scan(&id, &hen, &rooster, &species, &eggSet, &hatch, &eggsSet, &chicksHatched, &status, &notes); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse poultry breeding records"})
			return
		}
		hatchOut := ""
		if hatch != nil {
			hatchOut = s.formatDate(*hatch)
		}
		out = append(out, map[string]any{
			"id":            id,
			"hen":           hen,
			"rooster":       rooster,
			"species":       species,
			"eggSetDate":    s.formatDate(eggSet),
			"hatchDate":     hatchOut,
			"eggsSet":       eggsSet,
			"chicksHatched": chicksHatched,
			"status":        status,
			"notes":         notes,
		})
	}

	respondJSON(w, http.StatusOK, out)
}

func (s *Server) handleBreedingBirths(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	rows, err := s.db.Query(ctx, `
		SELECT m.tag_id, b.species, b.actual_birth_date, COALESCE(b.offspring_count, 0), m.health_status
		FROM breeding_records b
		JOIN animals m ON m.id = b.mother_animal_id
		WHERE b.actual_birth_date IS NOT NULL
		ORDER BY b.actual_birth_date DESC
		LIMIT 20
	`)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load recent births"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var mother, typ, health string
		var birth time.Time
		var offspring int
		if err := rows.Scan(&mother, &typ, &birth, &offspring, &health); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse recent births"})
			return
		}
		out = append(out, map[string]any{
			"mother":    mother,
			"type":      typ,
			"date":      s.formatDate(birth),
			"offspring": offspring,
			"health":    health,
		})
	}

	respondJSON(w, http.StatusOK, out)
}

func (s *Server) handleProductionSummary(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var milk, eggs, wool, meat, value float64
	var milkCow, milkGoat float64
	err := s.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(milk_liters),0),
			COALESCE(SUM(milk_cow_liters),0),
			COALESCE(SUM(milk_goat_liters),0),
			COALESCE(SUM(eggs_count),0),
			COALESCE(SUM(wool_kg),0),
			COALESCE(SUM(meat_kg),0),
			COALESCE(SUM(total_value),0)
		FROM production_logs
		WHERE log_date >= CURRENT_DATE - INTERVAL '6 days'
	`).Scan(&milk, &milkCow, &milkGoat, &eggs, &wool, &meat, &value)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load production summary"})
		return
	}

	var previousValue float64
	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(total_value),0)
		FROM production_logs
		WHERE log_date >= CURRENT_DATE - INTERVAL '13 days'
			AND log_date < CURRENT_DATE - INTERVAL '6 days'
	`).Scan(&previousValue)

	productivityChange := 0.0
	if previousValue > 0 {
		productivityChange = ((value - previousValue) / previousValue) * 100
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"weeklyMilk":         milk,
		"weeklyMilkTotal":    milk,
		"weeklyMilkCow":      milkCow,
		"weeklyMilkGoat":     milkGoat,
		"weeklyEggs":         eggs,
		"weeklyWool":         wool,
		"weeklyMeat":         meat,
		"weeklyValue":        value,
		"productivityChange": productivityChange,
	})
}

func (s *Server) handleProductionLogs(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	page, pageSize := parsePagination(r)
	offset := (page - 1) * pageSize
	var total int64
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM production_logs`).Scan(&total)

	rows, err := s.db.Query(ctx, `
		SELECT id, log_date, milk_liters, milk_cow_liters, milk_goat_liters, eggs_count, wool_kg, meat_kg, total_value
		FROM production_logs
		ORDER BY log_date DESC
		LIMIT $1 OFFSET $2
	`, pageSize, offset)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load production logs"})
		return
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var d time.Time
		var milk, milkCow, milkGoat, eggs, wool, meat, total float64
		if err := rows.Scan(&id, &d, &milk, &milkCow, &milkGoat, &eggs, &wool, &meat, &total); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse production logs"})
			return
		}
		out = append(out, map[string]any{
			"id":         id,
			"date":       s.formatDateCompact(d),
			"dateRaw":    s.formatISODate(d),
			"milk":       fmt.Sprintf("%.0f L", milk),
			"milkValue":  milk,
			"milkCow":    fmt.Sprintf("%.0f L", milkCow),
			"milkCowValue": milkCow,
			"milkGoat":     fmt.Sprintf("%.0f L", milkGoat),
			"milkGoatValue": milkGoat,
			"eggs":       fmt.Sprintf("%.0f units", eggs),
			"eggsValue":  eggs,
			"wool":       fmt.Sprintf("%.0f kg", wool),
			"woolValue":  wool,
			"meat":       fmt.Sprintf("%.0f kg", meat),
			"meatValue":  meat,
			"total":      formatKES(total),
			"totalValue": total,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":    out,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}
