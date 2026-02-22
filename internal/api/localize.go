package api

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var kePhoneRe = regexp.MustCompile(`^(?:\+254|254|0)(7\d{8}|1\d{8})$`)
var kraPINRe = regexp.MustCompile(`^[A-Z][0-9]{9}[A-Z]$`)
var animalTagRe = regexp.MustCompile(`^[A-Z0-9-]{2,24}$`)

var kenyaCounties = map[string]struct{}{
	"baringo": {}, "bomet": {}, "bungoma": {}, "busia": {}, "elgeyo marakwet": {}, "embu": {},
	"garissa": {}, "homa bay": {}, "isiolo": {}, "kajiado": {}, "kakamega": {}, "kericho": {},
	"kiambu": {}, "kilifi": {}, "kirinyaga": {}, "kisii": {}, "kisumu": {}, "kitui": {},
	"kwale": {}, "laikipia": {}, "lamu": {}, "machakos": {}, "makueni": {}, "mandera": {},
	"marsabit": {}, "meru": {}, "migori": {}, "mombasa": {}, "muranga": {}, "nairobi": {},
	"nakuru": {}, "nandi": {}, "narok": {}, "nyamira": {}, "nyandarua": {}, "nyeri": {},
	"samburu": {}, "siaya": {}, "taita taveta": {}, "tana river": {}, "tharaka nithi": {}, "trans nzoia": {},
	"turkana": {}, "uasin gishu": {}, "vihiga": {}, "wajir": {}, "west pokot": {},
}

func (s *Server) now() time.Time {
	if s.location == nil {
		return time.Now().UTC()
	}
	return time.Now().In(s.location)
}

func (s *Server) formatDate(d time.Time) string {
	if s.location == nil {
		return d.Format("02/01/2006")
	}
	return d.In(s.location).Format("02/01/2006")
}

func (s *Server) formatDateCompact(d time.Time) string {
	if s.location == nil {
		return d.Format("02 Jan")
	}
	return d.In(s.location).Format("02 Jan")
}

func (s *Server) formatDateLong(d time.Time) string {
	if s.location == nil {
		return d.Format("02 January 2006")
	}
	return d.In(s.location).Format("02 January 2006")
}

func (s *Server) formatISODate(d time.Time) string {
	if s.location == nil {
		return d.Format("2006-01-02")
	}
	return d.In(s.location).Format("2006-01-02")
}

func speciesProfile(species string) string {
	v := strings.ToLower(strings.TrimSpace(species))
	if v == "" {
		return "mammal"
	}
	switch v {
	case "poultry", "chicken", "hen", "rooster", "broiler", "layer", "duck", "turkey", "quail", "goose", "guinea fowl", "guineafowl":
		return "poultry"
	default:
		return "mammal"
	}
}

func formatKES(v float64) string {
	return "KSh " + trimZero(v)
}

func normalizeKenyaPhone(raw string) (string, bool) {
	v := strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	if v == "" {
		return "", true
	}
	if !kePhoneRe.MatchString(v) {
		return "", false
	}
	if strings.HasPrefix(v, "0") {
		return "+254" + v[1:], true
	}
	if strings.HasPrefix(v, "254") {
		return "+" + v, true
	}
	return v, true
}

func normalizeKRAPIN(raw string) (string, bool) {
	v := strings.ToUpper(strings.TrimSpace(raw))
	if v == "" {
		return "", true
	}
	if !kraPINRe.MatchString(v) {
		return "", false
	}
	return v, true
}

func normalizeCounty(raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", true
	}
	if _, ok := kenyaCounties[strings.ToLower(v)]; !ok {
		return "", false
	}
	return v, true
}

func normalizeAnimalTag(raw string) (string, bool) {
	v := strings.ToUpper(strings.TrimSpace(raw))
	if v == "" {
		return "", false
	}
	if !animalTagRe.MatchString(v) {
		return "", false
	}
	return v, true
}

func expectedTagPrefixByType(animalType string) (string, bool) {
	v := strings.ToLower(strings.TrimSpace(animalType))
	if v == "" {
		return "", false
	}
	switch v {
	case "cattle", "cow", "bull", "heifer":
		return "C-", true
	case "goat", "goats":
		return "G-", true
	case "sheep", "ram", "ewe":
		return "S-", true
	case "pig", "swine", "boar", "sow":
		return "P-", true
	case "horse", "mare", "stallion":
		return "H-", true
	case "donkey", "ass", "mule":
		return "DN-", true
	case "camel":
		return "CM-", true
	case "buffalo":
		return "BF-", true
	case "rabbit":
		return "RB-", true
	case "chicken", "hen", "rooster", "broiler", "layer":
		return "CH-", true
	case "duck":
		return "DK-", true
	case "goose":
		return "GS-", true
	case "turkey":
		return "TK-", true
	case "quail":
		return "Q-", true
	case "guinea fowl", "guineafowl":
		return "GF-", true
	default:
		return "", false
	}
}

func (s *Server) frontendURL(pathWithLeadingSlash string) string {
	base := strings.TrimSpace(s.frontendBaseURL)
	if base == "" {
		base = "http://localhost:5173"
	}
	path := strings.TrimSpace(pathWithLeadingSlash)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("%s%s", strings.TrimRight(base, "/"), path)
}
