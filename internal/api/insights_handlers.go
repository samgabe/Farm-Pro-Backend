package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"
)

type insightItem struct {
	Category string             `json:"category"`
	Title    string             `json:"title"`
	Detail   string             `json:"detail"`
	Severity string             `json:"severity"`
	Metrics  map[string]float64 `json:"metrics,omitempty"`
	Action   string             `json:"action,omitempty"`
}

type speciesProfitRow struct {
	Species string  `json:"species"`
	Revenue float64 `json:"revenue"`
	FeedCost float64 `json:"feedCost"`
	Profit  float64 `json:"profit"`
}

type feedConversionRow struct {
	Species      string  `json:"species"`
	FeedCost     float64 `json:"feedCost"`
	OutputUnit   string  `json:"outputUnit"`
	OutputAmount float64 `json:"outputAmount"`
	CostPerUnit  float64 `json:"costPerUnit"`
}

type healthMonthRow struct {
	Month string `json:"month"`
	Count int64  `json:"count"`
}

func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()

	var totalAnimals, activeAnimals, sickAnimals, attentionAnimals int64
	_ = s.db.QueryRow(ctx, `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE is_active) AS active,
			COUNT(*) FILTER (WHERE health_status = 'sick') AS sick,
			COUNT(*) FILTER (WHERE health_status = 'attention') AS attention
		FROM animals
	`).Scan(&totalAnimals, &activeAnimals, &sickAnimals, &attentionAnimals)

	var vaccinesDue7 int64
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM health_records
		WHERE next_due IS NOT NULL
			AND next_due >= CURRENT_DATE
			AND next_due <= CURRENT_DATE + INTERVAL '7 days'
	`).Scan(&vaccinesDue7)

	var breedingActive, breedingOnHeat, aiRecent30, expectedBirths30 int64
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM breeding_records
		WHERE status = 'active'
	`).Scan(&breedingActive)
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM breeding_records
		WHERE status = 'active' AND on_heat = true
	`).Scan(&breedingOnHeat)
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM breeding_records
		WHERE ai_date >= CURRENT_DATE - INTERVAL '30 days'
	`).Scan(&aiRecent30)
	_ = s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM breeding_records
		WHERE expected_birth_date >= CURRENT_DATE
			AND expected_birth_date <= CURRENT_DATE + INTERVAL '30 days'
	`).Scan(&expectedBirths30)

	var eggsSet90, chicksHatched90 int64
	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(eggs_set), 0), COALESCE(SUM(chicks_hatched), 0)
		FROM poultry_breeding_records
		WHERE egg_set_date >= CURRENT_DATE - INTERVAL '90 days'
	`).Scan(&eggsSet90, &chicksHatched90)

	var milk30, milkCow30, milkGoat30, eggs30, wool30, meat30, productionValue30 float64
	var productionLogs30 int64
	_ = s.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(milk_liters), 0),
			COALESCE(SUM(milk_cow_liters), 0),
			COALESCE(SUM(milk_goat_liters), 0),
			COALESCE(SUM(eggs_count), 0),
			COALESCE(SUM(wool_kg), 0),
			COALESCE(SUM(meat_kg), 0),
			COALESCE(SUM(total_value), 0),
			COUNT(*)
		FROM production_logs
		WHERE log_date >= CURRENT_DATE - INTERVAL '30 days'
	`).Scan(&milk30, &milkCow30, &milkGoat30, &eggs30, &wool30, &meat30, &productionValue30, &productionLogs30)

	var feedCost30, feedQty30 float64
	var feedRecords30 int64
	_ = s.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(cost), 0),
			COALESCE(SUM(quantity_value), 0),
			COUNT(*)
		FROM feeding_records
		WHERE feed_date >= CURRENT_DATE - INTERVAL '30 days'
	`).Scan(&feedCost30, &feedQty30, &feedRecords30)

	var topFeed string
	var topFeedCost float64
	_ = s.db.QueryRow(ctx, `
		SELECT feed_type, SUM(cost) AS total
		FROM feeding_records
		GROUP BY feed_type
		ORDER BY total DESC
		LIMIT 1
	`).Scan(&topFeed, &topFeedCost)

	var expensesMonth, salesMonth float64
	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0)
		FROM expenses
		WHERE DATE_TRUNC('month', expense_date) = DATE_TRUNC('month', CURRENT_DATE)
	`).Scan(&expensesMonth)
	_ = s.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(total_amount), 0)
		FROM sales
		WHERE DATE_TRUNC('month', sale_date) = DATE_TRUNC('month', CURRENT_DATE)
	`).Scan(&salesMonth)

	var topExpenseCategory string
	var topExpenseAmount float64
	_ = s.db.QueryRow(ctx, `
		SELECT category, SUM(amount) AS total
		FROM expenses
		WHERE DATE_TRUNC('month', expense_date) = DATE_TRUNC('month', CURRENT_DATE)
		GROUP BY category
		ORDER BY total DESC
		LIMIT 1
	`).Scan(&topExpenseCategory, &topExpenseAmount)

	var topSalesProduct string
	var topSalesAmount float64
	_ = s.db.QueryRow(ctx, `
		SELECT product, SUM(total_amount) AS total
		FROM sales
		WHERE DATE_TRUNC('month', sale_date) = DATE_TRUNC('month', CURRENT_DATE)
		GROUP BY product
		ORDER BY total DESC
		LIMIT 1
	`).Scan(&topSalesProduct, &topSalesAmount)

	// Feed costs by species (last 30 days, only where animal_id exists)
	feedCostBySpecies := make(map[string]float64)
	feedQtyBySpecies := make(map[string]float64)
	{
		rows, err := s.db.Query(ctx, `
			SELECT a.type, COALESCE(SUM(f.cost), 0), COALESCE(SUM(f.quantity_value), 0)
			FROM feeding_records f
			JOIN animals a ON a.id = f.animal_id
			WHERE f.feed_date >= CURRENT_DATE - INTERVAL '30 days'
			GROUP BY a.type
		`)
		if err == nil {
			for rows.Next() {
				var t string
				var cost, qty float64
				if err := rows.Scan(&t, &cost, &qty); err != nil {
					continue
				}
				key := normalizeSpeciesName(t)
				feedCostBySpecies[key] += cost
				feedQtyBySpecies[key] += qty
			}
			rows.Close()
		}
	}

	// Sales revenue by product (last 30 days)
	revenueByProduct := make(map[string]float64)
	{
		rows, err := s.db.Query(ctx, `
			SELECT product, COALESCE(SUM(total_amount), 0)
			FROM sales
			WHERE sale_date >= CURRENT_DATE - INTERVAL '30 days'
			GROUP BY product
		`)
		if err == nil {
			for rows.Next() {
				var prod string
				var total float64
				if err := rows.Scan(&prod, &total); err != nil {
					continue
				}
				revenueByProduct[strings.ToLower(strings.TrimSpace(prod))] += total
			}
			rows.Close()
		}
	}

	// Active animals by species
	activeBySpecies := make(map[string]int64)
	{
		rows, err := s.db.Query(ctx, `
			SELECT type, COUNT(*)
			FROM animals
			WHERE is_active
			GROUP BY type
		`)
		if err == nil {
			for rows.Next() {
				var t string
				var count int64
				if err := rows.Scan(&t, &count); err != nil {
					continue
				}
				activeBySpecies[normalizeSpeciesName(t)] += count
			}
			rows.Close()
		}
	}

	aiTotal := int64(0)
	aiSuccess := int64(0)
	_ = s.db.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE ai_date IS NOT NULL) AS total,
			COUNT(*) FILTER (WHERE ai_date IS NOT NULL AND (actual_birth_date IS NOT NULL OR COALESCE(offspring_count, 0) > 0 OR status = 'completed')) AS success
		FROM breeding_records
		WHERE ai_date >= CURRENT_DATE - INTERVAL '365 days'
	`).Scan(&aiTotal, &aiSuccess)

	healthIncidents := make([]healthMonthRow, 0)
	{
		rows, err := s.db.Query(ctx, `
			SELECT DATE_TRUNC('month', record_date)::date, COUNT(*)
			FROM health_records
			WHERE record_date >= DATE_TRUNC('month', CURRENT_DATE) - INTERVAL '5 months'
			GROUP BY DATE_TRUNC('month', record_date)
			ORDER BY DATE_TRUNC('month', record_date)
		`)
		if err == nil {
			for rows.Next() {
				var month time.Time
				var count int64
				if err := rows.Scan(&month, &count); err != nil {
					continue
				}
				healthIncidents = append(healthIncidents, healthMonthRow{
					Month: month.Format("Jan 2006"),
					Count: count,
				})
			}
			rows.Close()
		}
	}

	insights := make([]insightItem, 0)

	active := float64(activeAnimals)
	attentionRate := 0.0
	if active > 0 {
		attentionRate = float64(sickAnimals+attentionAnimals) / active
	}
	if activeAnimals > 0 {
		severity := "good"
		if attentionRate > 0.1 {
			severity = "alert"
		} else if attentionRate > 0.05 {
			severity = "warn"
		}
		insights = append(insights, insightItem{
			Category: "Animals",
			Title:    "Animals Needing Attention",
			Detail:   "Share of active animals flagged as sick or attention.",
			Severity: severity,
			Metrics: map[string]float64{
				"attention_rate": attentionRate * 100,
				"active_animals": float64(activeAnimals),
			},
			Action: "Schedule health checks or update treatment plans for flagged animals.",
		})
	}

	if vaccinesDue7 > 0 {
		insights = append(insights, insightItem{
			Category: "Health",
			Title:    "Vaccines Due Soon",
			Detail:   "Upcoming vaccinations in the next 7 days.",
			Severity: "warn",
			Metrics: map[string]float64{
				"vaccines_due": float64(vaccinesDue7),
			},
			Action: "Prepare doses and lock calendar slots with the vet team.",
		})
	}

	if breedingActive > 0 || breedingOnHeat > 0 || expectedBirths30 > 0 {
		insights = append(insights, insightItem{
			Category: "Breeding",
			Title:    "Breeding Pipeline",
			Detail:   "Active breeding and near-term expected births.",
			Severity: "info",
			Metrics: map[string]float64{
				"active":        float64(breedingActive),
				"on_heat":       float64(breedingOnHeat),
				"births_next30": float64(expectedBirths30),
				"ai_recent30":   float64(aiRecent30),
			},
			Action: "Align staffing and housing for upcoming births.",
		})
	}

	if eggsSet90 > 0 {
		hatchRate := float64(0)
		if eggsSet90 > 0 {
			hatchRate = float64(chicksHatched90) / float64(eggsSet90)
		}
		severity := "good"
		if hatchRate < 0.6 {
			severity = "alert"
		} else if hatchRate < 0.75 {
			severity = "warn"
		}
		insights = append(insights, insightItem{
			Category: "Breeding",
			Title:    "Poultry Hatch Rate",
			Detail:   "Hatch success rate over the last 90 days.",
			Severity: severity,
			Metrics: map[string]float64{
				"hatch_rate": hatchRate * 100,
				"eggs_set":   float64(eggsSet90),
			},
			Action: "Review incubation settings and egg handling protocol.",
		})
	}

	if productionLogs30 > 0 {
		logCoverage := float64(productionLogs30) / 30.0
		severity := "good"
		if logCoverage < 0.6 {
			severity = "alert"
		} else if logCoverage < 0.8 {
			severity = "warn"
		}
		insights = append(insights, insightItem{
			Category: "Production",
			Title:    "Production Log Coverage",
			Detail:   "Share of days with production logs in the last 30 days.",
			Severity: severity,
			Metrics: map[string]float64{
				"log_coverage": logCoverage * 100,
				"logs":         float64(productionLogs30),
			},
			Action: "Keep daily logs to improve insights accuracy.",
		})
	}

	if feedCost30 > 0 && productionValue30 > 0 {
		ratio := feedCost30 / productionValue30
		severity := "good"
		if ratio > 0.7 {
			severity = "alert"
		} else if ratio > 0.5 {
			severity = "warn"
		}
		insights = append(insights, insightItem{
			Category: "Feeding",
			Title:    "Feed Cost vs Output Value",
			Detail:   "Feed cost as a share of production value (last 30 days).",
			Severity: severity,
			Metrics: map[string]float64{
				"feed_cost_ratio": ratio * 100,
				"feed_cost":       feedCost30,
				"output_value":    productionValue30,
			},
			Action: "Tune rations or sourcing if feed cost share is rising.",
		})
	}

	if salesMonth > 0 || expensesMonth > 0 {
		profit := salesMonth - expensesMonth
		margin := 0.0
		if salesMonth > 0 {
			margin = profit / salesMonth
		}
		severity := "good"
		if profit < 0 {
			severity = "alert"
		} else if margin < 0.15 {
			severity = "warn"
		}
		insights = append(insights, insightItem{
			Category: "Finance",
			Title:    "Monthly Profitability",
			Detail:   "Sales minus expenses for the current month.",
			Severity: severity,
			Metrics: map[string]float64{
				"profit":     profit,
				"sales":      salesMonth,
				"expenses":   expensesMonth,
				"margin_pct": margin * 100,
			},
			Action: "Focus on high-margin products and reduce low ROI costs.",
		})
	}

	// Per-species profitability (estimated)
	speciesRevenue := estimateRevenueBySpecies(revenueByProduct, milkCow30, milkGoat30, activeBySpecies)
	speciesProfitability := make([]speciesProfitRow, 0, len(speciesRevenue))
	for species, revenue := range speciesRevenue {
		feedCost := feedCostBySpecies[species]
		speciesProfitability = append(speciesProfitability, speciesProfitRow{
			Species: species,
			Revenue: revenue,
			FeedCost: feedCost,
			Profit:  revenue - feedCost,
		})
	}
	sort.Slice(speciesProfitability, func(i, j int) bool {
		return speciesProfitability[i].Profit > speciesProfitability[j].Profit
	})

	if len(speciesProfitability) > 0 {
		top := speciesProfitability[0]
		insights = append(insights, insightItem{
			Category: "Finance",
			Title:    "Top Species by Profit (Est.)",
			Detail:   "Estimated profit from sales minus feed costs per species over the last 30 days.",
			Severity: "info",
			Metrics: map[string]float64{
				"profit": top.Profit,
				"revenue": top.Revenue,
				"feed_cost": top.FeedCost,
			},
			Action: "Validate the leader with accurate cost allocations and keep optimizing feed costs.",
		})
	}

	// Feed conversion per species
	feedConversion := buildFeedConversion(feedCostBySpecies, milkCow30, milkGoat30, eggs30, wool30, meat30)
	if len(feedConversion) > 0 {
		worst := feedConversion[0]
		for _, row := range feedConversion[1:] {
			if row.CostPerUnit > worst.CostPerUnit {
				worst = row
			}
		}
		insights = append(insights, insightItem{
			Category: "Feeding",
			Title:    "Highest Feed Cost per Unit",
			Detail:   "Estimated feed cost per output unit by species (last 30 days).",
			Severity: "warn",
			Metrics: map[string]float64{
				"cost_per_unit": worst.CostPerUnit,
				"feed_cost":     worst.FeedCost,
				"output":        worst.OutputAmount,
			},
			Action: "Review ration plan and sourcing for the highlighted species.",
		})
	}

	if aiTotal > 0 {
		rate := float64(aiSuccess) / float64(aiTotal)
		severity := "good"
		if rate < 0.5 {
			severity = "alert"
		} else if rate < 0.7 {
			severity = "warn"
		}
		insights = append(insights, insightItem{
			Category: "Breeding",
			Title:    "AI Success Rate",
			Detail:   "AI success rate over the last 12 months.",
			Severity: severity,
			Metrics: map[string]float64{
				"success_rate": rate * 100,
				"ai_total":     float64(aiTotal),
			},
			Action: "Review insemination timing, semen quality, and heat detection accuracy.",
		})
	}

	if len(healthIncidents) > 0 {
		last := healthIncidents[len(healthIncidents)-1]
		insights = append(insights, insightItem{
			Category: "Health",
			Title:    "Health Incidents (Last Month)",
			Detail:   "Recorded health activities in the most recent month.",
			Severity: "info",
			Metrics: map[string]float64{
				"incidents": float64(last.Count),
			},
			Action: "Monitor month-over-month changes to catch spikes early.",
		})
	}

	summary := map[string]any{
		"periodDays": 30,
		"animals": map[string]any{
			"total":        totalAnimals,
			"active":       activeAnimals,
			"sick":         sickAnimals,
			"attention":    attentionAnimals,
			"attentionPct": attentionRate * 100,
		},
		"health": map[string]any{
			"vaccinesDue7": vaccinesDue7,
		},
		"breeding": map[string]any{
			"active":         breedingActive,
			"onHeat":         breedingOnHeat,
			"aiRecent30":     aiRecent30,
			"expectedBirths": expectedBirths30,
		},
		"poultry": map[string]any{
			"eggsSet90":     eggsSet90,
			"chicksHatched": chicksHatched90,
			"hatchRatePct":  func() float64 { if eggsSet90 == 0 { return 0 }; return float64(chicksHatched90) / float64(eggsSet90) * 100 }(),
		},
		"production": map[string]any{
			"milk":       milk30,
			"milkCow":    milkCow30,
			"milkGoat":   milkGoat30,
			"eggs":       eggs30,
			"wool":       wool30,
			"meat":       meat30,
			"value":      productionValue30,
			"logCount":   productionLogs30,
		},
		"feeding": map[string]any{
			"cost":     feedCost30,
			"quantity": feedQty30,
			"records":  feedRecords30,
			"topFeed":  topFeed,
			"topCost":  topFeedCost,
		},
		"finance": map[string]any{
			"salesMonth":    salesMonth,
			"expensesMonth": expensesMonth,
			"profitMonth":   salesMonth - expensesMonth,
			"topExpense":    topExpenseCategory,
			"topExpenseAmt": topExpenseAmount,
			"topProduct":    topSalesProduct,
			"topProductAmt": topSalesAmount,
		},
		"ai": map[string]any{
			"total":       aiTotal,
			"success":     aiSuccess,
			"successRate": func() float64 { if aiTotal == 0 { return 0 }; return float64(aiSuccess) / float64(aiTotal) * 100 }(),
		},
		"speciesProfitability": speciesProfitability,
		"feedConversion":       feedConversion,
		"healthIncidents":      healthIncidents,
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"summary":  summary,
		"insights": insights,
	})
}

func normalizeSpeciesName(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(v, "cow") || strings.Contains(v, "cattle") || strings.Contains(v, "bull") || strings.Contains(v, "heifer"):
		return "Cow"
	case strings.Contains(v, "goat"):
		return "Goat"
	case strings.Contains(v, "sheep") || strings.Contains(v, "ram") || strings.Contains(v, "ewe"):
		return "Sheep"
	case strings.Contains(v, "pig") || strings.Contains(v, "swine") || strings.Contains(v, "boar") || strings.Contains(v, "sow"):
		return "Pig"
	case strings.Contains(v, "chicken") || strings.Contains(v, "hen") || strings.Contains(v, "rooster") || strings.Contains(v, "broiler") || strings.Contains(v, "layer") || strings.Contains(v, "duck") || strings.Contains(v, "goose") || strings.Contains(v, "turkey") || strings.Contains(v, "quail") || strings.Contains(v, "guinea"):
		return "Poultry"
	default:
		if v == "" {
			return "Other"
		}
		return strings.Title(v)
	}
}

func estimateRevenueBySpecies(revenueByProduct map[string]float64, milkCow, milkGoat float64, activeBySpecies map[string]int64) map[string]float64 {
	out := make(map[string]float64)
	milkRevenue := 0.0
	eggRevenue := 0.0
	woolRevenue := 0.0
	meatRevenue := 0.0
	for prod, total := range revenueByProduct {
		switch {
		case strings.Contains(prod, "milk"):
			milkRevenue += total
		case strings.Contains(prod, "egg"):
			eggRevenue += total
		case strings.Contains(prod, "wool"):
			woolRevenue += total
		case strings.Contains(prod, "meat") || strings.Contains(prod, "beef") || strings.Contains(prod, "pork") || strings.Contains(prod, "mutton") || strings.Contains(prod, "chevon") || strings.Contains(prod, "lamb") || strings.Contains(prod, "chicken") || strings.Contains(prod, "turkey") || strings.Contains(prod, "duck"):
			meatRevenue += total
		}
	}

	milkTotal := milkCow + milkGoat
	if milkTotal <= 0 {
		milkTotal = 0
	}
	if milkTotal > 0 && milkRevenue > 0 {
		out["Cow"] += milkRevenue * (milkCow / milkTotal)
		out["Goat"] += milkRevenue * (milkGoat / milkTotal)
	} else if milkRevenue > 0 {
		out["Cow"] += milkRevenue
	}

	if eggRevenue > 0 {
		out["Poultry"] += eggRevenue
	}
	if woolRevenue > 0 {
		out["Sheep"] += woolRevenue
	}

	if meatRevenue > 0 {
		mammals := []string{"Cow", "Goat", "Sheep", "Pig"}
		totalActive := int64(0)
		for _, m := range mammals {
			totalActive += activeBySpecies[m]
		}
		if totalActive == 0 {
			out["Cow"] += meatRevenue
		} else {
			for _, m := range mammals {
				out[m] += meatRevenue * (float64(activeBySpecies[m]) / float64(totalActive))
			}
		}
	}

	return out
}

func buildFeedConversion(feedCostBySpecies map[string]float64, milkCow, milkGoat, eggs, wool, meat float64) []feedConversionRow {
	out := make([]feedConversionRow, 0)
	if feedCostBySpecies["Cow"] > 0 && milkCow > 0 {
		out = append(out, feedConversionRow{
			Species:      "Cow",
			FeedCost:     feedCostBySpecies["Cow"],
			OutputUnit:   "L milk",
			OutputAmount: milkCow,
			CostPerUnit:  feedCostBySpecies["Cow"] / milkCow,
		})
	}
	if feedCostBySpecies["Goat"] > 0 && milkGoat > 0 {
		out = append(out, feedConversionRow{
			Species:      "Goat",
			FeedCost:     feedCostBySpecies["Goat"],
			OutputUnit:   "L milk",
			OutputAmount: milkGoat,
			CostPerUnit:  feedCostBySpecies["Goat"] / milkGoat,
		})
	}
	if feedCostBySpecies["Poultry"] > 0 && eggs > 0 {
		out = append(out, feedConversionRow{
			Species:      "Poultry",
			FeedCost:     feedCostBySpecies["Poultry"],
			OutputUnit:   "eggs",
			OutputAmount: eggs,
			CostPerUnit:  feedCostBySpecies["Poultry"] / eggs,
		})
	}
	if feedCostBySpecies["Sheep"] > 0 && wool > 0 {
		out = append(out, feedConversionRow{
			Species:      "Sheep",
			FeedCost:     feedCostBySpecies["Sheep"],
			OutputUnit:   "kg wool",
			OutputAmount: wool,
			CostPerUnit:  feedCostBySpecies["Sheep"] / wool,
		})
	}
	if feedCostBySpecies["Pig"] > 0 && meat > 0 {
		out = append(out, feedConversionRow{
			Species:      "Pig",
			FeedCost:     feedCostBySpecies["Pig"],
			OutputUnit:   "kg meat",
			OutputAmount: meat,
			CostPerUnit:  feedCostBySpecies["Pig"] / meat,
		})
	}
	return out
}
