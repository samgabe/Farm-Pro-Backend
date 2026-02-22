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

	normalizedTag, ok := normalizeAnimalTag(in.TagID)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "tagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
		return
	}
	in.TagID = normalizedTag
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
	if prefix, ok := expectedTagPrefixByType(in.Type); ok {
		if !strings.HasPrefix(in.TagID, prefix) {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("tagId must start with %s for %s", prefix, in.Type)})
			return
		}
	}

	birthDate, err := optionalDate(in.BirthDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "birthDate must be YYYY-MM-DD"})
		return
	}
	if birthDate != nil && birthDate.After(time.Now()) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "birthDate cannot be in the future"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err = s.db.Exec(ctx, `
		INSERT INTO animals(tag_id, type, breed, birth_date, weight_kg, health_status, status, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, in.TagID, in.Type, in.Breed, birthDate, in.WeightKg, in.HealthStatus, in.Status, in.Status == "active")
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
	tagID, ok := normalizeAnimalTag(r.PathValue("tagId"))
	if !ok {
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
	if prefix, ok := expectedTagPrefixByType(in.Type); ok {
		if !strings.HasPrefix(tagID, prefix) {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("tagId must start with %s for %s", prefix, in.Type)})
			return
		}
	}
	birthDate, err := optionalDate(in.BirthDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "birthDate must be YYYY-MM-DD"})
		return
	}
	if birthDate != nil && birthDate.After(time.Now()) {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "birthDate cannot be in the future"})
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
	tagID, ok := normalizeAnimalTag(r.PathValue("tagId"))
	if !ok {
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

	tagID, ok := normalizeAnimalTag(in.AnimalTagID)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animalTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
		return
	}
	in.AnimalTagID = tagID
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
	tagID, ok := normalizeAnimalTag(in.AnimalTagID)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animalTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
		return
	}
	in.AnimalTagID = tagID
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
		HeatDate          string `json:"heatDate"`
		AIDate            string `json:"aiDate"`
		OnHeat            *bool  `json:"onHeat"`
		AISireSource      string `json:"aiSireSource"`
		AISireName        string `json:"aiSireName"`
		AISireCode        string `json:"aiSireCode"`
		ExpectedBirthDate string `json:"expectedBirthDate"`
		Notes             string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	motherTag, ok := normalizeAnimalTag(in.MotherTagID)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "motherTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
		return
	}
	in.MotherTagID = motherTag
	in.Species = strings.TrimSpace(in.Species)
	in.Notes = strings.TrimSpace(in.Notes)
	in.AISireSource = strings.TrimSpace(in.AISireSource)
	in.AISireName = strings.TrimSpace(in.AISireName)
	in.AISireCode = strings.TrimSpace(in.AISireCode)
	if in.BreedingDate == "" {
		in.BreedingDate = time.Now().Format("2006-01-02")
	}
	if in.MotherTagID == "" || in.Species == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "motherTagId and species are required"})
		return
	}
	if speciesProfile(in.Species) == "poultry" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "poultry records must use the poultry breeding endpoint"})
		return
	}

	breedDate, err := time.Parse("2006-01-02", in.BreedingDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "breedingDate must be YYYY-MM-DD"})
		return
	}
	heatDate, err := optionalDate(in.HeatDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "heatDate must be YYYY-MM-DD"})
		return
	}
	aiDate, err := optionalDate(in.AIDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "aiDate must be YYYY-MM-DD"})
		return
	}
	expectedDate, err := optionalDate(in.ExpectedBirthDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "expectedBirthDate must be YYYY-MM-DD"})
		return
	}
	onHeat := false
	if in.OnHeat != nil {
		onHeat = *in.OnHeat
	}
	aiSource, ok := normalizeAISource(in.AISireSource)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "aiSireSource must be internal, external, or semen_batch"})
		return
	}
	hasAIFields := aiDate != nil || in.AISireName != "" || in.AISireCode != ""
	if hasAIFields && aiSource == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "aiSireSource is required when AI details are provided"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var motherID int64
	if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1 AND is_active = true`, in.MotherTagID).Scan(&motherID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "mother animal not found"})
		return
	}
	var fatherID *int64
	if strings.TrimSpace(in.FatherTagID) != "" {
		fatherTag, ok := normalizeAnimalTag(in.FatherTagID)
		if !ok {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "fatherTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
			return
		}
		if motherTag == fatherTag {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "motherTagId and fatherTagId must be different"})
			return
		}
		var father int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1 AND is_active = true`, fatherTag).Scan(&father); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "father animal not found"})
			return
		}
		fatherID = &father
	}
	if aiSource == "internal" || aiSource == "" {
		if fatherID == nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "fatherTagId is required for natural or internal AI breeding"})
			return
		}
	}
	var aiSourcePtr *string
	if aiSource != "" {
		aiSourcePtr = &aiSource
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO breeding_records(mother_animal_id, father_animal_id, species, breeding_date, heat_date, ai_date, on_heat,
			ai_sire_source, ai_sire_name, ai_sire_code, expected_birth_date, status, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'active', $12)
	`, motherID, fatherID, in.Species, breedDate, heatDate, aiDate, onHeat, aiSourcePtr, in.AISireName, in.AISireCode, expectedDate, in.Notes)
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
		HeatDate          string `json:"heatDate"`
		AIDate            string `json:"aiDate"`
		OnHeat            *bool  `json:"onHeat"`
		AISireSource      string `json:"aiSireSource"`
		AISireName        string `json:"aiSireName"`
		AISireCode        string `json:"aiSireCode"`
		ExpectedBirthDate string `json:"expectedBirthDate"`
		Status            string `json:"status"`
		Notes             string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	motherTag, ok := normalizeAnimalTag(in.MotherTagID)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "motherTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
		return
	}
	in.MotherTagID = motherTag
	in.Species = strings.TrimSpace(in.Species)
	in.AISireSource = strings.TrimSpace(in.AISireSource)
	in.AISireName = strings.TrimSpace(in.AISireName)
	in.AISireCode = strings.TrimSpace(in.AISireCode)
	if in.Status == "" {
		in.Status = "active"
	}
	if in.MotherTagID == "" || in.Species == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "motherTagId and species are required"})
		return
	}
	if speciesProfile(in.Species) == "poultry" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "poultry records must use the poultry breeding endpoint"})
		return
	}
	breedingDate, err := time.Parse("2006-01-02", in.BreedingDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "breedingDate must be YYYY-MM-DD"})
		return
	}
	heatDate, err := optionalDate(in.HeatDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "heatDate must be YYYY-MM-DD"})
		return
	}
	aiDate, err := optionalDate(in.AIDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "aiDate must be YYYY-MM-DD"})
		return
	}
	expectedDate, err := optionalDate(in.ExpectedBirthDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "expectedBirthDate must be YYYY-MM-DD"})
		return
	}
	onHeat := false
	if in.OnHeat != nil {
		onHeat = *in.OnHeat
	}
	aiSource, ok := normalizeAISource(in.AISireSource)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "aiSireSource must be internal, external, or semen_batch"})
		return
	}
	hasAIFields := aiDate != nil || in.AISireName != "" || in.AISireCode != ""
	if hasAIFields && aiSource == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "aiSireSource is required when AI details are provided"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var motherID int64
	if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1`, in.MotherTagID).Scan(&motherID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "mother animal not found"})
		return
	}
	var fatherID *int64
	if strings.TrimSpace(in.FatherTagID) != "" {
		fatherTag, ok := normalizeAnimalTag(in.FatherTagID)
		if !ok {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "fatherTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
			return
		}
		if motherTag == fatherTag {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "motherTagId and fatherTagId must be different"})
			return
		}
		var father int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1`, fatherTag).Scan(&father); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "father animal not found"})
			return
		}
		fatherID = &father
	}
	if aiSource == "internal" || aiSource == "" {
		if fatherID == nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "fatherTagId is required for natural or internal AI breeding"})
			return
		}
	}
	var aiSourcePtr *string
	if aiSource != "" {
		aiSourcePtr = &aiSource
	}
	res, err := s.db.Exec(ctx, `
		UPDATE breeding_records
		SET mother_animal_id = $1, father_animal_id = $2, species = $3, breeding_date = $4, heat_date = $5, ai_date = $6, on_heat = $7,
			ai_sire_source = $8, ai_sire_name = $9, ai_sire_code = $10, expected_birth_date = $11, status = $12, notes = $13
		WHERE id = $14
	`, motherID, fatherID, in.Species, breedingDate, heatDate, aiDate, onHeat, aiSourcePtr, in.AISireName, in.AISireCode, expectedDate, in.Status, strings.TrimSpace(in.Notes), recordID)
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

func (s *Server) handleCreatePoultryBreedingRecord(w http.ResponseWriter, r *http.Request) {
	var in struct {
		HenTagID       string `json:"motherTagId"`
		RoosterTagID   string `json:"fatherTagId"`
		Species        string `json:"species"`
		EggSetDate     string `json:"eggSetDate"`
		HatchDate      string `json:"hatchDate"`
		EggsSet        *int   `json:"eggsSet"`
		ChicksHatched  *int   `json:"chicksHatched"`
		Status         string `json:"status"`
		Notes          string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	henTag, ok := normalizeAnimalTag(in.HenTagID)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "motherTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
		return
	}
	in.HenTagID = henTag
	in.Species = strings.TrimSpace(in.Species)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Species == "" {
		in.Species = "Poultry"
	}
	if speciesProfile(in.Species) != "poultry" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "species must be a poultry type"})
		return
	}
	if strings.TrimSpace(in.EggSetDate) == "" {
		in.EggSetDate = time.Now().Format("2006-01-02")
	}
	eggSetDate, err := time.Parse("2006-01-02", in.EggSetDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "eggSetDate must be YYYY-MM-DD"})
		return
	}
	hatchDate, err := optionalDate(in.HatchDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "hatchDate must be YYYY-MM-DD"})
		return
	}
	eggsSet := 0
	if in.EggsSet != nil {
		eggsSet = *in.EggsSet
	}
	if eggsSet < 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "eggsSet must be 0 or greater"})
		return
	}
	var chicksHatched *int
	if in.ChicksHatched != nil {
		if *in.ChicksHatched < 0 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "chicksHatched must be 0 or greater"})
			return
		}
		chicksHatched = in.ChicksHatched
	}
	if in.Status == "" {
		in.Status = "active"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var henID int64
	if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1 AND is_active = true`, in.HenTagID).Scan(&henID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "hen animal not found"})
		return
	}
	var roosterID *int64
	if strings.TrimSpace(in.RoosterTagID) != "" {
		roosterTag, ok := normalizeAnimalTag(in.RoosterTagID)
		if !ok {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "fatherTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
			return
		}
		if roosterTag == henTag {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "motherTagId and fatherTagId must be different"})
			return
		}
		var rooster int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1 AND is_active = true`, roosterTag).Scan(&rooster); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "rooster animal not found"})
			return
		}
		roosterID = &rooster
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO poultry_breeding_records(hen_animal_id, rooster_animal_id, species, egg_set_date, hatch_date, eggs_set, chicks_hatched, status, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, henID, roosterID, in.Species, eggSetDate, hatchDate, eggsSet, chicksHatched, in.Status, in.Notes)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create poultry breeding record"})
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleUpdatePoultryBreedingRecord(w http.ResponseWriter, r *http.Request) {
	recordID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid record id"})
		return
	}
	var in struct {
		HenTagID       string `json:"motherTagId"`
		RoosterTagID   string `json:"fatherTagId"`
		Species        string `json:"species"`
		EggSetDate     string `json:"eggSetDate"`
		HatchDate      string `json:"hatchDate"`
		EggsSet        *int   `json:"eggsSet"`
		ChicksHatched  *int   `json:"chicksHatched"`
		Status         string `json:"status"`
		Notes          string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	henTag, ok := normalizeAnimalTag(in.HenTagID)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "motherTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
		return
	}
	in.HenTagID = henTag
	in.Species = strings.TrimSpace(in.Species)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Species == "" {
		in.Species = "Poultry"
	}
	if speciesProfile(in.Species) != "poultry" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "species must be a poultry type"})
		return
	}
	eggSetDate, err := time.Parse("2006-01-02", in.EggSetDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "eggSetDate must be YYYY-MM-DD"})
		return
	}
	hatchDate, err := optionalDate(in.HatchDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "hatchDate must be YYYY-MM-DD"})
		return
	}
	eggsSet := 0
	if in.EggsSet != nil {
		eggsSet = *in.EggsSet
	}
	if eggsSet < 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "eggsSet must be 0 or greater"})
		return
	}
	var chicksHatched *int
	if in.ChicksHatched != nil {
		if *in.ChicksHatched < 0 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "chicksHatched must be 0 or greater"})
			return
		}
		chicksHatched = in.ChicksHatched
	}
	if in.Status == "" {
		in.Status = "active"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var henID int64
	if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1`, in.HenTagID).Scan(&henID); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "hen animal not found"})
		return
	}
	var roosterID *int64
	if strings.TrimSpace(in.RoosterTagID) != "" {
		roosterTag, ok := normalizeAnimalTag(in.RoosterTagID)
		if !ok {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "fatherTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
			return
		}
		if roosterTag == henTag {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "motherTagId and fatherTagId must be different"})
			return
		}
		var rooster int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1`, roosterTag).Scan(&rooster); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "rooster animal not found"})
			return
		}
		roosterID = &rooster
	}

	res, err := s.db.Exec(ctx, `
		UPDATE poultry_breeding_records
		SET hen_animal_id = $1, rooster_animal_id = $2, species = $3, egg_set_date = $4, hatch_date = $5, eggs_set = $6, chicks_hatched = $7, status = $8, notes = $9
		WHERE id = $10
	`, henID, roosterID, in.Species, eggSetDate, hatchDate, eggsSet, chicksHatched, in.Status, in.Notes, recordID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update poultry breeding record"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "record not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeletePoultryBreedingRecord(w http.ResponseWriter, r *http.Request) {
	recordID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid record id"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `DELETE FROM poultry_breeding_records WHERE id = $1`, recordID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete poultry breeding record"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "record not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRecordBirth(w http.ResponseWriter, r *http.Request) {
	recordID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid record id"})
		return
	}

	var in struct {
		ActualBirthDate string `json:"actualBirthDate"`
		OffspringCount  *int   `json:"offspringCount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.ActualBirthDate = strings.TrimSpace(in.ActualBirthDate)
	if in.ActualBirthDate == "" {
		in.ActualBirthDate = time.Now().Format("2006-01-02")
	}
	actualBirthDate, err := time.Parse("2006-01-02", in.ActualBirthDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "actualBirthDate must be YYYY-MM-DD"})
		return
	}

	offspringCount := 0
	if in.OffspringCount != nil {
		offspringCount = *in.OffspringCount
	}
	if offspringCount < 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "offspringCount cannot be negative"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	res, err := s.db.Exec(ctx, `
		UPDATE breeding_records
		SET actual_birth_date = $1, offspring_count = $2, status = 'completed'
		WHERE id = $3
	`, actualBirthDate, offspringCount, recordID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record birth"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "record not found"})
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateProductionLog(w http.ResponseWriter, r *http.Request) {
	const (
		defaultMilkRate     = 60.0
		defaultMilkCowRate  = 60.0
		defaultMilkGoatRate = 80.0
		defaultEggRate      = 15.0
		defaultWoolRate     = 500.0
		defaultMeatRate     = 450.0
	)

	var in struct {
		Date                string   `json:"date"`
		MilkLiters          *float64 `json:"milkLiters"`
		MilkCowLiters       *float64 `json:"milkCowLiters"`
		MilkGoatLiters      *float64 `json:"milkGoatLiters"`
		EggsCount           *int     `json:"eggsCount"`
		WoolKg              *float64 `json:"woolKg"`
		MeatKg              *float64 `json:"meatKg"`
		MilkRate            *float64 `json:"milkRate"`
		MilkCowRate         *float64 `json:"milkCowRate"`
		MilkGoatRate        *float64 `json:"milkGoatRate"`
		EggRate             *float64 `json:"eggRate"`
		WoolRate            *float64 `json:"woolRate"`
		MeatRate            *float64 `json:"meatRate"`
		TotalValue          *float64 `json:"totalValue"`
		ManualTotalOverride bool     `json:"manualTotalOverride"`
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
	milkCow := 0.0
	milkGoat := 0.0
	eggs := 0
	wool := 0.0
	meat := 0.0
	milkRate := defaultMilkRate
	milkCowRate := defaultMilkCowRate
	milkGoatRate := defaultMilkGoatRate
	eggRate := defaultEggRate
	woolRate := defaultWoolRate
	meatRate := defaultMeatRate
	totalValue := 0.0
	if in.MilkLiters != nil {
		milk = *in.MilkLiters
	}
	if in.MilkCowLiters != nil {
		milkCow = *in.MilkCowLiters
	}
	if in.MilkGoatLiters != nil {
		milkGoat = *in.MilkGoatLiters
	}
	if in.EggsCount != nil {
		eggs = *in.EggsCount
	}
	if in.WoolKg != nil {
		wool = *in.WoolKg
	}
	if in.MeatKg != nil {
		meat = *in.MeatKg
	}
	if in.MilkRate != nil {
		milkRate = *in.MilkRate
	}
	if in.MilkCowRate != nil {
		milkCowRate = *in.MilkCowRate
	}
	if in.MilkGoatRate != nil {
		milkGoatRate = *in.MilkGoatRate
	}
	if in.EggRate != nil {
		eggRate = *in.EggRate
	}
	if in.WoolRate != nil {
		woolRate = *in.WoolRate
	}
	if in.MeatRate != nil {
		meatRate = *in.MeatRate
	}

	milkTotalFromVariants := milkCow + milkGoat
	if milkTotalFromVariants > 0 {
		milk = milkTotalFromVariants
	}

	if milk < 0 || milkCow < 0 || milkGoat < 0 || eggs < 0 || wool < 0 || meat < 0 || milkRate < 0 || milkCowRate < 0 || milkGoatRate < 0 || eggRate < 0 || woolRate < 0 || meatRate < 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "production values cannot be negative"})
		return
	}
	if in.ManualTotalOverride {
		if in.TotalValue == nil || *in.TotalValue < 0 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "manual totalValue must be provided and non-negative"})
			return
		}
		totalValue = *in.TotalValue
	} else {
		if milkTotalFromVariants > 0 {
			totalValue = milkCow*milkCowRate + milkGoat*milkGoatRate
		} else {
			totalValue = milk * milkRate
		}
		totalValue += float64(eggs)*eggRate + wool*woolRate + meat*meatRate
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err = s.db.Exec(ctx, `
		INSERT INTO production_logs(log_date, milk_liters, milk_cow_liters, milk_goat_liters, eggs_count, wool_kg, meat_kg, total_value)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (log_date) DO UPDATE
		SET milk_liters = EXCLUDED.milk_liters,
			milk_cow_liters = EXCLUDED.milk_cow_liters,
			milk_goat_liters = EXCLUDED.milk_goat_liters,
			eggs_count = EXCLUDED.eggs_count,
			wool_kg = EXCLUDED.wool_kg,
			meat_kg = EXCLUDED.meat_kg,
			total_value = EXCLUDED.total_value
	`, d, milk, milkCow, milkGoat, eggs, wool, meat, totalValue)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create production log"})
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleUpdateProductionLog(w http.ResponseWriter, r *http.Request) {
	const (
		defaultMilkRate     = 60.0
		defaultMilkCowRate  = 60.0
		defaultMilkGoatRate = 80.0
		defaultEggRate      = 15.0
		defaultWoolRate     = 500.0
		defaultMeatRate     = 450.0
	)

	logID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid log id"})
		return
	}
	var in struct {
		Date                string   `json:"date"`
		MilkLiters          float64  `json:"milkLiters"`
		MilkCowLiters       float64  `json:"milkCowLiters"`
		MilkGoatLiters      float64  `json:"milkGoatLiters"`
		EggsCount           int      `json:"eggsCount"`
		WoolKg              float64  `json:"woolKg"`
		MeatKg              float64  `json:"meatKg"`
		MilkRate            *float64 `json:"milkRate"`
		MilkCowRate         *float64 `json:"milkCowRate"`
		MilkGoatRate        *float64 `json:"milkGoatRate"`
		EggRate             *float64 `json:"eggRate"`
		WoolRate            *float64 `json:"woolRate"`
		MeatRate            *float64 `json:"meatRate"`
		TotalValue          float64  `json:"totalValue"`
		ManualTotalOverride bool     `json:"manualTotalOverride"`
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
	milkRate := defaultMilkRate
	milkCowRate := defaultMilkCowRate
	milkGoatRate := defaultMilkGoatRate
	eggRate := defaultEggRate
	woolRate := defaultWoolRate
	meatRate := defaultMeatRate
	if in.MilkRate != nil {
		milkRate = *in.MilkRate
	}
	if in.MilkCowRate != nil {
		milkCowRate = *in.MilkCowRate
	}
	if in.MilkGoatRate != nil {
		milkGoatRate = *in.MilkGoatRate
	}
	if in.EggRate != nil {
		eggRate = *in.EggRate
	}
	if in.WoolRate != nil {
		woolRate = *in.WoolRate
	}
	if in.MeatRate != nil {
		meatRate = *in.MeatRate
	}

	milkTotalFromVariants := in.MilkCowLiters + in.MilkGoatLiters
	if milkTotalFromVariants > 0 {
		in.MilkLiters = milkTotalFromVariants
	}

	if in.MilkLiters < 0 || in.MilkCowLiters < 0 || in.MilkGoatLiters < 0 || in.EggsCount < 0 || in.WoolKg < 0 || in.MeatKg < 0 || milkRate < 0 || milkCowRate < 0 || milkGoatRate < 0 || eggRate < 0 || woolRate < 0 || meatRate < 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "production values cannot be negative"})
		return
	}
	totalValue := in.TotalValue
	if in.ManualTotalOverride {
		if in.TotalValue < 0 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "manual totalValue must be non-negative"})
			return
		}
	} else {
		if milkTotalFromVariants > 0 {
			totalValue = in.MilkCowLiters*milkCowRate + in.MilkGoatLiters*milkGoatRate
		} else {
			totalValue = in.MilkLiters * milkRate
		}
		totalValue += float64(in.EggsCount)*eggRate + in.WoolKg*woolRate + in.MeatKg*meatRate
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `
		UPDATE production_logs
		SET log_date = $1, milk_liters = $2, milk_cow_liters = $3, milk_goat_liters = $4,
			eggs_count = $5, wool_kg = $6, meat_kg = $7, total_value = $8
		WHERE id = $9
	`, d, in.MilkLiters, in.MilkCowLiters, in.MilkGoatLiters, in.EggsCount, in.WoolKg, in.MeatKg, totalValue, logID)
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

func (s *Server) handleCreateFeedingRecord(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Date         string  `json:"date"`
		AnimalTagID  string  `json:"animalTagId"`
		RationID     *int64  `json:"rationId"`
		PlanID       *int64  `json:"planId"`
		FeedType     string  `json:"feedType"`
		QuantityValue float64 `json:"quantityValue"`
		QuantityUnit string  `json:"quantityUnit"`
		Supplier     string  `json:"supplier"`
		Cost         float64 `json:"cost"`
		Notes        string  `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.FeedType = strings.TrimSpace(in.FeedType)
	in.QuantityUnit = strings.TrimSpace(in.QuantityUnit)
	in.Supplier = strings.TrimSpace(in.Supplier)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Date == "" {
		in.Date = time.Now().Format("2006-01-02")
	}
	if in.QuantityUnit == "" {
		in.QuantityUnit = "kg"
	}
	if in.FeedType == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "feedType is required"})
		return
	}
	if in.QuantityValue < 0 || in.Cost < 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "quantityValue and cost must be non-negative"})
		return
	}

	feedDate, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}

	var animalID *int64
	if strings.TrimSpace(in.AnimalTagID) != "" {
		tagID, ok := normalizeAnimalTag(in.AnimalTagID)
		if !ok {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animalTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
			return
		}
		in.AnimalTagID = tagID
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1 AND is_active = true`, in.AnimalTagID).Scan(&id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animal not found"})
			return
		}
		animalID = &id
	}

	var rationID *int64
	if in.RationID != nil && *in.RationID > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM feeding_rations WHERE id = $1`, *in.RationID).Scan(&id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "ration not found"})
			return
		}
		rationID = &id
	}

	var planID *int64
	if in.PlanID != nil && *in.PlanID > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM feeding_plans WHERE id = $1`, *in.PlanID).Scan(&id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "feeding plan not found"})
			return
		}
		planID = &id
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err = s.db.Exec(ctx, `
		INSERT INTO feeding_records(feed_date, animal_id, ration_id, plan_id, feed_type, quantity_value, quantity_unit, supplier, cost, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, feedDate, animalID, rationID, planID, in.FeedType, in.QuantityValue, in.QuantityUnit, in.Supplier, in.Cost, in.Notes)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create feeding record"})
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleUpdateFeedingRecord(w http.ResponseWriter, r *http.Request) {
	recordID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid record id"})
		return
	}

	var in struct {
		Date         string  `json:"date"`
		AnimalTagID  string  `json:"animalTagId"`
		RationID     *int64  `json:"rationId"`
		PlanID       *int64  `json:"planId"`
		FeedType     string  `json:"feedType"`
		QuantityValue float64 `json:"quantityValue"`
		QuantityUnit string  `json:"quantityUnit"`
		Supplier     string  `json:"supplier"`
		Cost         float64 `json:"cost"`
		Notes        string  `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.FeedType = strings.TrimSpace(in.FeedType)
	in.QuantityUnit = strings.TrimSpace(in.QuantityUnit)
	in.Supplier = strings.TrimSpace(in.Supplier)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.QuantityUnit == "" {
		in.QuantityUnit = "kg"
	}
	if in.FeedType == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "feedType is required"})
		return
	}
	if in.QuantityValue < 0 || in.Cost < 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "quantityValue and cost must be non-negative"})
		return
	}
	feedDate, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "date must be YYYY-MM-DD"})
		return
	}

	var animalID *int64
	if strings.TrimSpace(in.AnimalTagID) != "" {
		tagID, ok := normalizeAnimalTag(in.AnimalTagID)
		if !ok {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animalTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
			return
		}
		in.AnimalTagID = tagID
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1`, in.AnimalTagID).Scan(&id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animal not found"})
			return
		}
		animalID = &id
	}

	var rationID *int64
	if in.RationID != nil && *in.RationID > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM feeding_rations WHERE id = $1`, *in.RationID).Scan(&id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "ration not found"})
			return
		}
		rationID = &id
	}

	var planID *int64
	if in.PlanID != nil && *in.PlanID > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM feeding_plans WHERE id = $1`, *in.PlanID).Scan(&id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "feeding plan not found"})
			return
		}
		planID = &id
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `
		UPDATE feeding_records
		SET feed_date = $1, animal_id = $2, ration_id = $3, plan_id = $4, feed_type = $5, quantity_value = $6, quantity_unit = $7, supplier = $8, cost = $9, notes = $10
		WHERE id = $11
	`, feedDate, animalID, rationID, planID, in.FeedType, in.QuantityValue, in.QuantityUnit, in.Supplier, in.Cost, in.Notes, recordID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update feeding record"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "record not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteFeedingRecord(w http.ResponseWriter, r *http.Request) {
	recordID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid record id"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `DELETE FROM feeding_records WHERE id = $1`, recordID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete feeding record"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "record not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateFeedingRation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		Species string `json:"species"`
		State   string `json:"state"`
		Notes   string `json:"notes"`
		Items   []struct {
			Ingredient string  `json:"ingredient"`
			Quantity   float64 `json:"quantity"`
			Unit       string  `json:"unit"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Species = strings.TrimSpace(in.Species)
	in.State = strings.TrimSpace(in.State)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Name == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create ration"})
		return
	}
	defer tx.Rollback(ctx)

	var rationID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO feeding_rations(name, species, state, notes)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, in.Name, in.Species, in.State, in.Notes).Scan(&rationID); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create ration"})
		return
	}

	for _, item := range in.Items {
		ingredient := strings.TrimSpace(item.Ingredient)
		unit := strings.TrimSpace(item.Unit)
		if ingredient == "" {
			continue
		}
		if unit == "" {
			unit = "kg"
		}
		if item.Quantity < 0 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "ration item quantity must be non-negative"})
			return
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO feeding_ration_items(ration_id, ingredient, quantity_value, quantity_unit)
			VALUES ($1, $2, $3, $4)
		`, rationID, ingredient, item.Quantity, unit); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create ration items"})
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create ration"})
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleUpdateFeedingRation(w http.ResponseWriter, r *http.Request) {
	rationID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ration id"})
		return
	}

	var in struct {
		Name    string `json:"name"`
		Species string `json:"species"`
		State   string `json:"state"`
		Notes   string `json:"notes"`
		Items   []struct {
			Ingredient string  `json:"ingredient"`
			Quantity   float64 `json:"quantity"`
			Unit       string  `json:"unit"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Species = strings.TrimSpace(in.Species)
	in.State = strings.TrimSpace(in.State)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Name == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update ration"})
		return
	}
	defer tx.Rollback(ctx)

	res, err := tx.Exec(ctx, `
		UPDATE feeding_rations
		SET name = $1, species = $2, state = $3, notes = $4
		WHERE id = $5
	`, in.Name, in.Species, in.State, in.Notes, rationID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update ration"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "ration not found"})
		return
	}

	if _, err := tx.Exec(ctx, `DELETE FROM feeding_ration_items WHERE ration_id = $1`, rationID); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update ration items"})
		return
	}
	for _, item := range in.Items {
		ingredient := strings.TrimSpace(item.Ingredient)
		unit := strings.TrimSpace(item.Unit)
		if ingredient == "" {
			continue
		}
		if unit == "" {
			unit = "kg"
		}
		if item.Quantity < 0 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "ration item quantity must be non-negative"})
			return
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO feeding_ration_items(ration_id, ingredient, quantity_value, quantity_unit)
			VALUES ($1, $2, $3, $4)
		`, rationID, ingredient, item.Quantity, unit); err != nil {
			respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update ration items"})
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update ration"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteFeedingRation(w http.ResponseWriter, r *http.Request) {
	rationID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ration id"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `DELETE FROM feeding_rations WHERE id = $1`, rationID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete ration"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "ration not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateFeedingPlan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AnimalTagID       string  `json:"animalTagId"`
		RationID          *int64  `json:"rationId"`
		AnimalState       string  `json:"state"`
		DailyQuantityValue float64 `json:"dailyQuantityValue"`
		DailyQuantityUnit string  `json:"dailyQuantityUnit"`
		StartDate         string  `json:"startDate"`
		EndDate           string  `json:"endDate"`
		Status            string  `json:"status"`
		Notes             string  `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	in.AnimalState = strings.TrimSpace(in.AnimalState)
	in.DailyQuantityUnit = strings.TrimSpace(in.DailyQuantityUnit)
	in.Status = strings.TrimSpace(in.Status)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.DailyQuantityUnit == "" {
		in.DailyQuantityUnit = "kg"
	}
	if in.StartDate == "" {
		in.StartDate = time.Now().Format("2006-01-02")
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.DailyQuantityValue < 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "dailyQuantityValue must be non-negative"})
		return
	}
	if in.Status != "active" && in.Status != "paused" && in.Status != "completed" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be active, paused, or completed"})
		return
	}
	startDate, err := time.Parse("2006-01-02", in.StartDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "startDate must be YYYY-MM-DD"})
		return
	}
	endDate, err := optionalDate(in.EndDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "endDate must be YYYY-MM-DD"})
		return
	}

	var animalID *int64
	if strings.TrimSpace(in.AnimalTagID) != "" {
		tagID, ok := normalizeAnimalTag(in.AnimalTagID)
		if !ok {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animalTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
			return
		}
		in.AnimalTagID = tagID
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1 AND is_active = true`, in.AnimalTagID).Scan(&id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animal not found"})
			return
		}
		animalID = &id
	}

	var rationID *int64
	if in.RationID != nil && *in.RationID > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM feeding_rations WHERE id = $1`, *in.RationID).Scan(&id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "ration not found"})
			return
		}
		rationID = &id
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err = s.db.Exec(ctx, `
		INSERT INTO feeding_plans(animal_id, ration_id, animal_state, daily_quantity_value, daily_quantity_unit, start_date, end_date, status, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, animalID, rationID, in.AnimalState, in.DailyQuantityValue, in.DailyQuantityUnit, startDate, endDate, in.Status, in.Notes)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create feeding plan"})
		return
	}
	respondJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleUpdateFeedingPlan(w http.ResponseWriter, r *http.Request) {
	planID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid plan id"})
		return
	}

	var in struct {
		AnimalTagID       string  `json:"animalTagId"`
		RationID          *int64  `json:"rationId"`
		AnimalState       string  `json:"state"`
		DailyQuantityValue float64 `json:"dailyQuantityValue"`
		DailyQuantityUnit string  `json:"dailyQuantityUnit"`
		StartDate         string  `json:"startDate"`
		EndDate           string  `json:"endDate"`
		Status            string  `json:"status"`
		Notes             string  `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	in.AnimalState = strings.TrimSpace(in.AnimalState)
	in.DailyQuantityUnit = strings.TrimSpace(in.DailyQuantityUnit)
	in.Status = strings.TrimSpace(in.Status)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.DailyQuantityUnit == "" {
		in.DailyQuantityUnit = "kg"
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.DailyQuantityValue < 0 {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "dailyQuantityValue must be non-negative"})
		return
	}
	if in.Status != "active" && in.Status != "paused" && in.Status != "completed" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be active, paused, or completed"})
		return
	}
	startDate, err := time.Parse("2006-01-02", in.StartDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "startDate must be YYYY-MM-DD"})
		return
	}
	endDate, err := optionalDate(in.EndDate)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "endDate must be YYYY-MM-DD"})
		return
	}

	var animalID *int64
	if strings.TrimSpace(in.AnimalTagID) != "" {
		tagID, ok := normalizeAnimalTag(in.AnimalTagID)
		if !ok {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animalTagId must be 2-24 chars (A-Z, 0-9, hyphen)"})
			return
		}
		in.AnimalTagID = tagID
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM animals WHERE tag_id = $1`, in.AnimalTagID).Scan(&id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "animal not found"})
			return
		}
		animalID = &id
	}

	var rationID *int64
	if in.RationID != nil && *in.RationID > 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var id int64
		if err := s.db.QueryRow(ctx, `SELECT id FROM feeding_rations WHERE id = $1`, *in.RationID).Scan(&id); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "ration not found"})
			return
		}
		rationID = &id
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `
		UPDATE feeding_plans
		SET animal_id = $1, ration_id = $2, animal_state = $3, daily_quantity_value = $4, daily_quantity_unit = $5, start_date = $6, end_date = $7, status = $8, notes = $9
		WHERE id = $10
	`, animalID, rationID, in.AnimalState, in.DailyQuantityValue, in.DailyQuantityUnit, startDate, endDate, in.Status, in.Notes, planID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update feeding plan"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteFeedingPlan(w http.ResponseWriter, r *http.Request) {
	planID, err := parsePathID(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid plan id"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `DELETE FROM feeding_plans WHERE id = $1`, planID)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete feeding plan"})
		return
	}
	if res.RowsAffected() == 0 {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "plan not found"})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"ok": true})
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
		Date              string   `json:"date"`
		Product           string   `json:"product"`
		QuantityValue     float64  `json:"quantityValue"`
		QuantityUnit      string   `json:"quantityUnit"`
		Buyer             string   `json:"buyer"`
		BuyerPIN          string   `json:"buyerPIN"`
		DeliveryCounty    string   `json:"deliveryCounty"`
		DeliverySubcounty string   `json:"deliverySubcounty"`
		VATApplicable     bool     `json:"vatApplicable"`
		VATRate           *float64 `json:"vatRate"`
		PricePerUnit      float64  `json:"pricePerUnit"`
		TotalAmount       *float64 `json:"totalAmount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}

	in.Product = strings.TrimSpace(in.Product)
	in.QuantityUnit = strings.TrimSpace(in.QuantityUnit)
	in.Buyer = strings.TrimSpace(in.Buyer)
	in.BuyerPIN = strings.TrimSpace(in.BuyerPIN)
	in.DeliveryCounty = strings.TrimSpace(in.DeliveryCounty)
	in.DeliverySubcounty = strings.TrimSpace(in.DeliverySubcounty)
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

	buyerPIN, ok := normalizeKRAPIN(in.BuyerPIN)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "buyer PIN must be valid KRA format (e.g. A012345678Z)"})
		return
	}
	county, ok := normalizeCounty(in.DeliveryCounty)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "delivery county must be a valid Kenya county"})
		return
	}
	if in.DeliverySubcounty != "" && county == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "delivery county is required when subcounty is provided"})
		return
	}

	total := in.QuantityValue * in.PricePerUnit
	if in.TotalAmount != nil && *in.TotalAmount > 0 {
		total = *in.TotalAmount
	}
	vatRate := 0.0
	if in.VATApplicable {
		vatRate = 0.16
		if in.VATRate != nil {
			vatRate = *in.VATRate
		}
		if vatRate < 0 || vatRate > 1 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "vatRate must be between 0 and 1"})
			return
		}
	}
	netAmount := total
	vatAmount := 0.0
	if in.VATApplicable && vatRate > 0 {
		netAmount = total / (1 + vatRate)
		vatAmount = total - netAmount
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err = s.db.Exec(ctx, `
		INSERT INTO sales(
			sale_date, product, quantity_value, quantity_unit, buyer, buyer_pin,
			delivery_county, delivery_subcounty, vat_applicable, vat_rate, vat_amount, net_amount,
			price_per_unit, total_amount
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, d, in.Product, in.QuantityValue, in.QuantityUnit, in.Buyer, buyerPIN, county, in.DeliverySubcounty, in.VATApplicable, vatRate, vatAmount, netAmount, in.PricePerUnit, total)
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
		Date              string   `json:"date"`
		Product           string   `json:"product"`
		QuantityValue     float64  `json:"quantityValue"`
		QuantityUnit      string   `json:"quantityUnit"`
		Buyer             string   `json:"buyer"`
		BuyerPIN          string   `json:"buyerPIN"`
		DeliveryCounty    string   `json:"deliveryCounty"`
		DeliverySubcounty string   `json:"deliverySubcounty"`
		VATApplicable     bool     `json:"vatApplicable"`
		VATRate           *float64 `json:"vatRate"`
		PricePerUnit      float64  `json:"pricePerUnit"`
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
	buyerPIN, ok := normalizeKRAPIN(in.BuyerPIN)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "buyer PIN must be valid KRA format (e.g. A012345678Z)"})
		return
	}
	county, ok := normalizeCounty(in.DeliveryCounty)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "delivery county must be a valid Kenya county"})
		return
	}
	subcounty := strings.TrimSpace(in.DeliverySubcounty)
	if subcounty != "" && county == "" {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "delivery county is required when subcounty is provided"})
		return
	}
	total := in.QuantityValue * in.PricePerUnit
	vatRate := 0.0
	if in.VATApplicable {
		vatRate = 0.16
		if in.VATRate != nil {
			vatRate = *in.VATRate
		}
		if vatRate < 0 || vatRate > 1 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "vatRate must be between 0 and 1"})
			return
		}
	}
	netAmount := total
	vatAmount := 0.0
	if in.VATApplicable && vatRate > 0 {
		netAmount = total / (1 + vatRate)
		vatAmount = total - netAmount
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.db.Exec(ctx, `
		UPDATE sales
		SET sale_date = $1, product = $2, quantity_value = $3, quantity_unit = $4, buyer = $5, buyer_pin = $6,
			delivery_county = $7, delivery_subcounty = $8, vat_applicable = $9, vat_rate = $10, vat_amount = $11, net_amount = $12,
			price_per_unit = $13, total_amount = $14
		WHERE id = $15
	`, d, strings.TrimSpace(in.Product), in.QuantityValue, strings.TrimSpace(in.QuantityUnit), strings.TrimSpace(in.Buyer), buyerPIN, county, subcounty, in.VATApplicable, vatRate, vatAmount, netAmount, in.PricePerUnit, total, saleID)
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
	phone, ok := normalizeKenyaPhone(in.Phone)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "phone must be a valid Kenya number (e.g. +2547XXXXXXXX)"})
		return
	}
	in.Phone = phone

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "password processing failed"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	roleID, roleName, err := s.resolveRole(ctx, in.Role)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role"})
		return
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO users(name, email, password_hash, role_id, role, phone, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, in.Name, in.Email, string(hash), roleID, roleName, in.Phone, in.Status)
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
	phone, ok := normalizeKenyaPhone(in.Phone)
	if !ok {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "phone must be a valid Kenya number (e.g. +2547XXXXXXXX)"})
		return
	}
	in.Phone = phone

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	roleID, roleName, err := s.resolveRole(ctx, in.Role)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role"})
		return
	}

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
			SET name = $1, email = $2, role_id = $3, role = $4, phone = $5, status = $6, password_hash = $7
			WHERE id = $8
		`, in.Name, in.Email, roleID, roleName, in.Phone, in.Status, string(hash), userID)
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
		SET name = $1, email = $2, role_id = $3, role = $4, phone = $5, status = $6
		WHERE id = $7
	`, in.Name, in.Email, roleID, roleName, in.Phone, in.Status, userID)
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

func normalizeAISource(input string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(input))
	if v == "" {
		return "", true
	}
	switch v {
	case "internal":
		return "internal", true
	case "external":
		return "external", true
	case "semen", "semen_batch", "semenbatch":
		return "semen_batch", true
	default:
		return "", false
	}
}
