package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func (s *Server) handleCreateAnimal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TagID        string   `json:"tagId"`
		Type         string   `json:"type"`
		Breed        string   `json:"breed"`
		BirthDate    string   `json:"birthDate"`
		WeightKg     *float64 `json:"weightKg"`
		HealthStatus string   `json:"healthStatus"`
		Status       string   `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.TagID = strings.ToUpper(strings.TrimSpace(in.TagID))
	in.Type = strings.TrimSpace(in.Type)
	in.Breed = strings.TrimSpace(in.Breed)
	if in.HealthStatus == "" {
		in.HealthStatus = "healthy"
	}
	if in.Status == "" {
		in.Status = "active"
	}

	if in.TagID == "" || in.Type == "" || in.Breed == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "tagId, type, and breed are required"})
		return
	}

	birthDate, err := optionalDate(in.BirthDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "birthDate must be YYYY-MM-DD"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err = s.db.Exec(ctx, `
		INSERT INTO animals(tag_id, type, breed, birth_date, weight_kg, health_status, status, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, in.TagID, in.Type, in.Breed, birthDate, in.WeightKg, in.HealthStatus, in.Status, in.Status != "sold")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			respondJSON(w, http.StatusConflict, map[string]string{"error": "tag ID already exists"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create animal"})
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleUpdateAnimal(w http.ResponseWriter, r *http.Request) {
	tagID := strings.ToUpper(strings.TrimSpace(r.PathValue("tagId")))
	if tagID == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tag ID"})
		return
	}

	var in struct {
		Type         string   `json:"type"`
		Breed        string   `json:"breed"`
		BirthDate    string   `json:"birthDate"`
		WeightKg     *float64 `json:"weightKg"`
		HealthStatus string   `json:"healthStatus"`
		Status       string   `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.Type = strings.TrimSpace(in.Type)
	in.Breed = strings.TrimSpace(in.Breed)
	if in.HealthStatus == "" {
		in.HealthStatus = "healthy"
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Type == "" || in.Breed == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "type and breed are required"})
		return
	}
	birthDate, err := optionalDate(in.BirthDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "birthDate must be YYYY-MM-DD"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `
		UPDATE animals
		SET type = $1, breed = $2, birth_date = $3, weight_kg = $4, health_status = $5, status = $6, is_active = $7
		WHERE tag_id = $8
	`, in.Type, in.Breed, birthDate, in.WeightKg, in.HealthStatus, in.Status, in.Status == "active", tagID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update animal"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "animal not found"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteAnimal(w http.ResponseWriter, r *http.Request) {
	tagID := strings.ToUpper(strings.TrimSpace(r.PathValue("tagId")))
	if tagID == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tag ID"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `
		UPDATE animals
		SET is_active = false, status = 'inactive'
		WHERE tag_id = $1
	`, tagID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete animal"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "animal not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateHealthRecord(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AnimalTagID  string `json:"animalTagId"`
		Action       string `json:"action"`
		Treatment    string `json:"treatment"`
		RecordDate   string `json:"recordDate"`
		Veterinarian string `json:"veterinarian"`
		NextDue      string `json:"nextDue"`
		Notes        string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.AnimalTagID = strings.ToUpper(strings.TrimSpace(in.AnimalTagID))
	in.Action = strings.TrimSpace(in.Action)
	in.Treatment = strings.TrimSpace(in.Treatment)
	in.Veterinarian = strings.TrimSpace(in.Veterinarian)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.RecordDate == "" {
		in.RecordDate = time.Now().Format("2006-01-02")
	}

	if in.AnimalTagID == "" || in.Action == "" || in.Treatment == "" || in.Veterinarian == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animalTagId, action, treatment, and veterinarian are required"})
		return
	}

	recordDate, err := time.Parse("2006-01-02", in.RecordDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "recordDate must be YYYY-MM-DD"})
		return
	}
	nextDue, err := optionalDate(in.NextDue)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "nextDue must be YYYY-MM-DD"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var animalID int64
	if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1 AND is_active = true`, in.AnimalTagID).Scan(&animalID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animal not found"})
		return
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO health_records(animal_id, action, treatment, record_date, veterinarian, next_due, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, animalID, in.Action, in.Treatment, recordDate, in.Veterinarian, nextDue, in.Notes)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create health record"})
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleUpdateHealthRecord(w http.ResponseWriter, r *http.Request) {
	recordID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid record id"})
		return
	}

	var in struct {
		AnimalTagID  string `json:"animalTagId"`
		Action       string `json:"action"`
		Treatment    string `json:"treatment"`
		RecordDate   string `json:"recordDate"`
		Veterinarian string `json:"veterinarian"`
		NextDue      string `json:"nextDue"`
		Notes        string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	in.AnimalTagID = strings.ToUpper(strings.TrimSpace(in.AnimalTagID))
	in.Action = strings.TrimSpace(in.Action)
	in.Treatment = strings.TrimSpace(in.Treatment)
	in.Veterinarian = strings.TrimSpace(in.Veterinarian)
	if in.AnimalTagID == "" || in.Action == "" || in.Treatment == "" || in.Veterinarian == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animalTagId, action, treatment, and veterinarian are required"})
		return
	}
	recordDate, err := time.Parse("2006-01-02", in.RecordDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "recordDate must be YYYY-MM-DD"})
		return
	}
	nextDue, err := optionalDate(in.NextDue)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "nextDue must be YYYY-MM-DD"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var animalID int64
	if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1`, in.AnimalTagID).Scan(&animalID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animal not found"})
		return
	}

	res, err := s.db.Exec(ctx, `
		UPDATE health_records
		SET animal_id = $1, action = $2, treatment = $3, record_date = $4, veterinarian = $5, next_due = $6, notes = $7
		WHERE id = $8
	`, animalID, in.Action, in.Treatment, recordDate, in.Veterinarian, nextDue, strings.TrimSpace(in.Notes), recordID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update health record"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "record not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteHealthRecord(w http.ResponseWriter, r *http.Request) {
	recordID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid record id"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `DELETE FROM health_records WHERE id = $1`, recordID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete health record"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "record not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateBreedingRecord(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MotherTagID       string `json:"motherTagId"`
		FatherTagID       string `json:"fatherTagId"`
		Species           string `json:"species"`
		BreedingDate      string `json:"breedingDate"`
		ExpectedBirthDate string `json:"expectedBirthDate"`
		Notes             string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.MotherTagID = strings.ToUpper(strings.TrimSpace(in.MotherTagID))
	in.FatherTagID = strings.ToUpper(strings.TrimSpace(in.FatherTagID))
	in.Species = strings.TrimSpace(in.Species)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.BreedingDate == "" {
		in.BreedingDate = time.Now().Format("2006-01-02")
	}
	if in.MotherTagID == "" || in.FatherTagID == "" || in.Species == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "motherTagId, fatherTagId, and species are required"})
		return
	}

	breedDate, err := time.Parse("2006-01-02", in.BreedingDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "breedingDate must be YYYY-MM-DD"})
		return
	}
	expectedDate, err := optionalDate(in.ExpectedBirthDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "expectedBirthDate must be YYYY-MM-DD"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var motherID, fatherID int64
	if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1 AND is_active = true`, in.MotherTagID).Scan(&motherID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "mother animal not found"})
		return
	}
	if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1 AND is_active = true`, in.FatherTagID).Scan(&fatherID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "father animal not found"})
		return
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO breeding_records(mother_animal_id, father_animal_id, species, breeding_date, expected_birth_date, status, notes)
		VALUES ($1, $2, $3, $4, $5, 'active', $6)
	`, motherID, fatherID, in.Species, breedDate, expectedDate, in.Notes)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create breeding record"})
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleUpdateBreedingRecord(w http.ResponseWriter, r *http.Request) {
	recordID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid record id"})
		return
	}

	var in struct {
		MotherTagID       string `json:"motherTagId"`
		FatherTagID       string `json:"fatherTagId"`
		Species           string `json:"species"`
		BreedingDate      string `json:"breedingDate"`
		ExpectedBirthDate string `json:"expectedBirthDate"`
		Status            string `json:"status"`
		Notes             string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	in.MotherTagID = strings.ToUpper(strings.TrimSpace(in.MotherTagID))
	in.FatherTagID = strings.ToUpper(strings.TrimSpace(in.FatherTagID))
	in.Species = strings.TrimSpace(in.Species)
	if in.Status == "" {
		in.Status = "active"
	}
	if in.MotherTagID == "" || in.FatherTagID == "" || in.Species == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "motherTagId, fatherTagId, and species are required"})
		return
	}
	breedingDate, err := time.Parse("2006-01-02", in.BreedingDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "breedingDate must be YYYY-MM-DD"})
		return
	}
	expectedDate, err := optionalDate(in.ExpectedBirthDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "expectedBirthDate must be YYYY-MM-DD"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var motherID, fatherID int64
	if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1`, in.MotherTagID).Scan(&motherID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "mother animal not found"})
		return
	}
	if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1`, in.FatherTagID).Scan(&fatherID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "father animal not found"})
		return
	}
	res, err := s.db.Exec(ctx, `
		UPDATE breeding_records
		SET mother_animal_id = $1, father_animal_id = $2, species = $3, breeding_date = $4, expected_birth_date = $5, status = $6, notes = $7
		WHERE id = $8
	`, motherID, fatherID, in.Species, breedingDate, expectedDate, in.Status, strings.TrimSpace(in.Notes), recordID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update breeding record"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "record not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteBreedingRecord(w http.ResponseWriter, r *http.Request) {
	recordID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid record id"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `DELETE FROM breeding_records WHERE id = $1`, recordID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete breeding record"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "record not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateProductionLog(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Date      string   `json:"date"`
		MilkLiters *float64 `json:"milkLiters"`
		EggsCount *float64 `json:"eggsCount"`
		WoolKg    *float64 `json:"woolKg"`
		TotalValue *float64 `json:"totalValue"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if strings.TrimSpace(in.Date) == "" {
		in.Date = time.Now().Format("2006-01-02")
	}
	d, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}
	milk := 0.0
	eggs := 0.0
	wool := 0.0
	totalValue := 0.0
	if in.MilkLiters != nil {
		milk = *in.MilkLiters
	}
	if in.EggsCount != nil {
		eggs = *in.EggsCount
	}
	if in.WoolKg != nil {
		wool = *in.WoolKg
	}
	if in.TotalValue != nil {
		totalValue = *in.TotalValue
	}
	if milk < 0 || eggs < 0 || wool < 0 || totalValue < 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "production values cannot be negative"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err = s.db.Exec(ctx, `
		INSERT INTO production_logs(log_date, milk_liters, eggs_count, wool_kg, total_value)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (log_date) DO UPDATE
		SET milk_liters = EXCLUDED.milk_liters,
			eggs_count = EXCLUDED.eggs_count,
			wool_kg = EXCLUDED.wool_kg,
			total_value = EXCLUDED.total_value
	`, d, milk, int(eggs), wool, totalValue)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create production log"})
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleUpdateProductionLog(w http.ResponseWriter, r *http.Request) {
	logID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid log id"})
		return
	}
	var in struct {
		Date       string  `json:"date"`
		MilkLiters float64 `json:"milkLiters"`
		EggsCount  float64 `json:"eggsCount"`
		WoolKg     float64 `json:"woolKg"`
		TotalValue float64 `json:"totalValue"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	d, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}
	if in.MilkLiters < 0 || in.EggsCount < 0 || in.WoolKg < 0 || in.TotalValue < 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "production values cannot be negative"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `
		UPDATE production_logs
		SET log_date = $1, milk_liters = $2, eggs_count = $3, wool_kg = $4, total_value = $5
		WHERE id = $6
	`, d, in.MilkLiters, int(in.EggsCount), in.WoolKg, in.TotalValue, logID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update production log"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "log not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteProductionLog(w http.ResponseWriter, r *http.Request) {
	logID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid log id"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `DELETE FROM production_logs WHERE id = $1`, logID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete production log"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "log not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateExpense(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Date     string  `json:"date"`
		Category string  `json:"category"`
		Item     string  `json:"item"`
		Vendor   string  `json:"vendor"`
		Amount   float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.Category = strings.TrimSpace(in.Category)
	in.Item = strings.TrimSpace(in.Item)
	in.Vendor = strings.TrimSpace(in.Vendor)
	if in.Date == "" {
		in.Date = time.Now().Format("2006-01-02")
	}
	if in.Category == "" || in.Item == "" || in.Vendor == "" || in.Amount <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "date, category, item, vendor and positive amount are required"})
		return
	}

	d, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err = s.db.Exec(ctx, `
		INSERT INTO expenses(expense_date, category, item, vendor, amount)
		VALUES ($1, $2, $3, $4, $5)
	`, d, in.Category, in.Item, in.Vendor, in.Amount)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create expense"})
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleUpdateExpense(w http.ResponseWriter, r *http.Request) {
	expenseID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid expense id"})
		return
	}
	var in struct {
		Date     string  `json:"date"`
		Category string  `json:"category"`
		Item     string  `json:"item"`
		Vendor   string  `json:"vendor"`
		Amount   float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	d, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}
	if strings.TrimSpace(in.Category) == "" || strings.TrimSpace(in.Item) == "" || strings.TrimSpace(in.Vendor) == "" || in.Amount <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "category, item, vendor and amount are required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `
		UPDATE expenses
		SET expense_date = $1, category = $2, item = $3, vendor = $4, amount = $5
		WHERE id = $6
	`, d, strings.TrimSpace(in.Category), strings.TrimSpace(in.Item), strings.TrimSpace(in.Vendor), in.Amount, expenseID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update expense"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "expense not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteExpense(w http.ResponseWriter, r *http.Request) {
	expenseID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid expense id"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `DELETE FROM expenses WHERE id = $1`, expenseID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete expense"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "expense not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateSale(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Date          string   `json:"date"`
		Product       string   `json:"product"`
		QuantityValue float64  `json:"quantityValue"`
		QuantityUnit  string   `json:"quantityUnit"`
		Buyer         string   `json:"buyer"`
		PricePerUnit  float64  `json:"pricePerUnit"`
		TotalAmount   *float64 `json:"totalAmount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.Product = strings.TrimSpace(in.Product)
	in.QuantityUnit = strings.TrimSpace(in.QuantityUnit)
	in.Buyer = strings.TrimSpace(in.Buyer)
	if in.Date == "" {
		in.Date = time.Now().Format("2006-01-02")
	}
	if in.Product == "" || in.QuantityUnit == "" || in.Buyer == "" || in.QuantityValue <= 0 || in.PricePerUnit <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "date, product, quantity, unit, buyer and price are required"})
		return
	}

	d, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}

	total := in.QuantityValue * in.PricePerUnit
	if in.TotalAmount != nil && *in.TotalAmount > 0 {
		total = *in.TotalAmount
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err = s.db.Exec(ctx, `
		INSERT INTO sales(sale_date, product, quantity_value, quantity_unit, buyer, price_per_unit, total_amount)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, d, in.Product, in.QuantityValue, in.QuantityUnit, in.Buyer, in.PricePerUnit, total)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create sale"})
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleUpdateSale(w http.ResponseWriter, r *http.Request) {
	saleID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sale id"})
		return
	}
	var in struct {
		Date          string  `json:"date"`
		Product       string  `json:"product"`
		QuantityValue float64 `json:"quantityValue"`
		QuantityUnit  string  `json:"quantityUnit"`
		Buyer         string  `json:"buyer"`
		PricePerUnit  float64 `json:"pricePerUnit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	d, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}
	if strings.TrimSpace(in.Product) == "" || strings.TrimSpace(in.QuantityUnit) == "" || strings.TrimSpace(in.Buyer) == "" || in.QuantityValue <= 0 || in.PricePerUnit <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "product, quantity, unit, buyer and price are required"})
		return
	}
	total := in.QuantityValue * in.PricePerUnit
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `
		UPDATE sales
		SET sale_date = $1, product = $2, quantity_value = $3, quantity_unit = $4, buyer = $5, price_per_unit = $6, total_amount = $7
		WHERE id = $8
	`, d, strings.TrimSpace(in.Product), in.QuantityValue, strings.TrimSpace(in.QuantityUnit), strings.TrimSpace(in.Buyer), in.PricePerUnit, total, saleID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update sale"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "sale not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteSale(w http.ResponseWriter, r *http.Request) {
	saleID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid sale id"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `DELETE FROM sales WHERE id = $1`, saleID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete sale"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "sale not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Role     string `json:"role"`
		Phone    string `json:"phone"`
		Status   string `json:"status"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Role = strings.ToLower(strings.TrimSpace(in.Role))
	in.Phone = strings.TrimSpace(in.Phone)
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Password == "" {
		in.Password = "password"
	}

	allowedRoles := map[string]bool{
		"owner": true, "manager": true, "worker": true, "veterinarian": true,
	}
	allowedStatus := map[string]bool{"active": true, "inactive": true}
	if in.Name == "" || in.Email == "" || !allowedRoles[in.Role] || !allowedStatus[in.Status] || len(in.Password) < 6 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "name, email, valid role/status and password(min 6) are required"})
		return
	}
	if !emailRe.MatchString(in.Email) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid email format"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "password processing failed"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err = s.db.Exec(ctx, `
		INSERT INTO users(name, email, password_hash, role, phone, status)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, in.Name, in.Email, string(hash), in.Role, in.Phone, in.Status)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
			respondJSON(w, http.StatusConflict, map[string]string{"error": "email already registered"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user"})
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	userID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Role     string `json:"role"`
		Phone    string `json:"phone"`
		Status   string `json:"status"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Role = strings.ToLower(strings.TrimSpace(in.Role))
	in.Phone = strings.TrimSpace(in.Phone)
	allowedRoles := map[string]bool{
		"owner": true, "manager": true, "worker": true, "veterinarian": true,
	}
	allowedStatus := map[string]bool{"active": true, "inactive": true}
	if in.Name == "" || in.Email == "" || !allowedRoles[in.Role] || !allowedStatus[in.Status] {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "name, email, valid role and status are required"})
		return
	}
	if !emailRe.MatchString(in.Email) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid email format"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if strings.TrimSpace(in.Password) != "" {
		if len(in.Password) < 6 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 6 characters"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "password processing failed"})
			return
		}
		res, err := s.db.Exec(ctx, `
			UPDATE users
			SET name = $1, email = $2, role = $3, phone = $4, status = $5, password_hash = $6
			WHERE id = $7
		`, in.Name, in.Email, in.Role, in.Phone, in.Status, string(hash), userID)
		if err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update user"})
			return
		}
		if res.RowsAffected() == 0 {
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	res, err := s.db.Exec(ctx, `
		UPDATE users
		SET name = $1, email = $2, role = $3, phone = $4, status = $5
		WHERE id = $6
	`, in.Name, in.Email, in.Role, in.Phone, in.Status, userID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update user"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	userID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}
	authID, _ := r.Context().Value(userIDContextKey).(int64)
	if authID == userID {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "you cannot delete your own account"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete user"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleGenerateReport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DateRange  string `json:"dateRange"`
		ReportType string `json:"reportType"`
		Format     string `json:"format"`
		Title      string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.DateRange = strings.TrimSpace(in.DateRange)
	in.ReportType = strings.TrimSpace(in.ReportType)
	in.Format = strings.TrimSpace(in.Format)
	in.Title = strings.TrimSpace(in.Title)

	in.DateRange = normalizeDateRange(in.DateRange)
	in.ReportType = normalizeReportType(in.ReportType)
	in.Format = normalizeReportFormat(in.Format)
	if in.Title == "" {
		in.Title = fmt.Sprintf("%s Report (%s)", in.ReportType, in.DateRange)
	}

	description := fmt.Sprintf("Generated %s report | range=%s | format=%s", strings.ToLower(in.ReportType), in.DateRange, in.Format)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var id int64
	err := s.db.QueryRow(ctx, `
		INSERT INTO reports(title, description, category, last_generated)
		VALUES ($1, $2, $3, CURRENT_DATE)
		RETURNING id
	`, in.Title, description, in.ReportType).Scan(&id)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate report"})
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"ok": true,
		"report": map[string]any{
			"id":          id,
			"title":       in.Title,
			"description": description,
			"category":    in.ReportType,
			"format":      in.Format,
		},
	})
}

func (s *Server) handleDownloadReport(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid report id"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var title, description, category string
	var generated time.Time
	err = s.db.QueryRow(ctx, `
		SELECT title, description, category, last_generated
		FROM reports
		WHERE id = $1
	`, id).Scan(&title, &description, &category, &generated)
	if err != nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "report not found"})
		return
	}

	dateRange, storedFormat := parseReportMetadata(description)
	requestedFormat := normalizeReportFormat(r.URL.Query().Get("format"))
	if strings.TrimSpace(r.URL.Query().Get("format")) == "" {
		requestedFormat = storedFormat
	}

	report, err := s.buildReportContent(ctx, id, title, category, dateRange, generated, requestedFormat)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to build report"})
		return
	}

	if report.Format == "CSV" {
		if err := writeCSVReport(w, report); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write csv report"})
		}
		return
	}
	if report.Format == "PDF" {
		if err := writePDFReport(w, report); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write pdf report"})
		}
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.json\"", reportFilename(title)))
	respondJSON(w, http.StatusOK, report)
}

func optionalDate(input string) (*time.Time, error) {
	v := strings.TrimSpace(input)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func parsePathID(r *http.Request, field string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(r.PathValue(field)), 10, 64)
}
