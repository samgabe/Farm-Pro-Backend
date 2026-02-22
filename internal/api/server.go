package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	db              *pgxpool.Pool
	jwtSecret       []byte
	allowAnyOrigin  bool
	allowedOrigins  map[string]struct{}
	loginLimiter    *attemptLimiter
	registerLimiter *attemptLimiter
	mailer          *smtpMailer
	frontendBaseURL string
	kraPIN          string
	location        *time.Location
	mlBaseURL       string
}

type authContextKey string

const userIDContextKey authContextKey = "user_id"
const userRoleContextKey authContextKey = "user_role"
const userPermissionsContextKey authContextKey = "user_permissions"

func NewServer(db *pgxpool.Pool, jwtSecret string, corsAllowedOrigins []string, mailer *smtpMailer, frontendBaseURL string, appTimezone string, kraPIN string, mlBaseURL string) *Server {
	allowedOrigins := make(map[string]struct{}, len(corsAllowedOrigins))
	allowAnyOrigin := false
	for _, raw := range corsAllowedOrigins {
		origin := strings.TrimSpace(raw)
		if origin == "" {
			continue
		}
		if origin == "*" {
			allowAnyOrigin = true
			continue
		}
		allowedOrigins[origin] = struct{}{}
	}
	loc, err := time.LoadLocation(strings.TrimSpace(appTimezone))
	if err != nil {
		loc = time.UTC
	}

	return &Server{
		db:              db,
		jwtSecret:       []byte(jwtSecret),
		allowAnyOrigin:  allowAnyOrigin,
		allowedOrigins:  allowedOrigins,
		loginLimiter:    newAttemptLimiter(12, time.Minute),
		registerLimiter: newAttemptLimiter(5, 15*time.Minute),
		mailer:          mailer,
		frontendBaseURL: strings.TrimRight(strings.TrimSpace(frontendBaseURL), "/"),
		kraPIN:          strings.ToUpper(strings.TrimSpace(kraPIN)),
		location:        loc,
		mlBaseURL:       strings.TrimRight(strings.TrimSpace(mlBaseURL), "/"),
	}
}

func (s *Server) Mux() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/forgot-password", s.handleForgotPassword)
	mux.HandleFunc("POST /api/auth/reset-password", s.handleResetPassword)
	mux.HandleFunc("POST /api/auth/verify-email", s.handleVerifyEmail)
	mux.Handle("GET /api/auth/me", s.authRequired(http.HandlerFunc(s.handleMe)))

	mux.Handle("GET /api/dashboard", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDashboard), "dashboard.read")))
	mux.Handle("GET /api/animals", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleAnimals), "animals.read")))
	mux.Handle("POST /api/animals", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleCreateAnimal), "animals.write")))
	mux.Handle("PUT /api/animals/{tagId}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUpdateAnimal), "animals.write")))
	mux.Handle("DELETE /api/animals/{tagId}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDeleteAnimal), "animals.write")))
	mux.Handle("GET /api/health/upcoming", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUpcomingVaccinations), "health.read")))
	mux.Handle("GET /api/health/records", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleHealthRecords), "health.read")))
	mux.Handle("POST /api/health/records", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleCreateHealthRecord), "health.write")))
	mux.Handle("PUT /api/health/records/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUpdateHealthRecord), "health.write")))
	mux.Handle("DELETE /api/health/records/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDeleteHealthRecord), "health.write")))
	mux.Handle("GET /api/breeding/active", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleBreedingActive), "breeding.read")))
	mux.Handle("GET /api/breeding/births", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleBreedingBirths), "breeding.read")))
	mux.Handle("GET /api/breeding/poultry/active", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleBreedingPoultryActive), "breeding.read")))
	mux.Handle("POST /api/breeding", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleCreateBreedingRecord), "breeding.write")))
	mux.Handle("PUT /api/breeding/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUpdateBreedingRecord), "breeding.write")))
	mux.Handle("POST /api/breeding/{id}/birth", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleRecordBirth), "breeding.write")))
	mux.Handle("DELETE /api/breeding/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDeleteBreedingRecord), "breeding.write")))
	mux.Handle("POST /api/breeding/poultry", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleCreatePoultryBreedingRecord), "breeding.write")))
	mux.Handle("PUT /api/breeding/poultry/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUpdatePoultryBreedingRecord), "breeding.write")))
	mux.Handle("DELETE /api/breeding/poultry/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDeletePoultryBreedingRecord), "breeding.write")))
	mux.Handle("GET /api/production/summary", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleProductionSummary), "production.read")))
	mux.Handle("GET /api/production/logs", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleProductionLogs), "production.read")))
	mux.Handle("POST /api/production/logs", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleCreateProductionLog), "production.create")))
	mux.Handle("PUT /api/production/logs/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUpdateProductionLog), "production.manage")))
	mux.Handle("DELETE /api/production/logs/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDeleteProductionLog), "production.manage")))
	mux.Handle("GET /api/expenses/summary", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleExpensesSummary), "expenses.read")))
	mux.Handle("GET /api/expenses", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleExpenses), "expenses.read")))
	mux.Handle("POST /api/expenses", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleCreateExpense), "expenses.write")))
	mux.Handle("PUT /api/expenses/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUpdateExpense), "expenses.write")))
	mux.Handle("DELETE /api/expenses/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDeleteExpense), "expenses.write")))
	mux.Handle("GET /api/feeding/summary", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleFeedingSummary), "feeding.read")))
	mux.Handle("GET /api/feeding", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleFeeding), "feeding.read")))
	mux.Handle("POST /api/feeding", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleCreateFeedingRecord), "feeding.write")))
	mux.Handle("PUT /api/feeding/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUpdateFeedingRecord), "feeding.write")))
	mux.Handle("DELETE /api/feeding/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDeleteFeedingRecord), "feeding.write")))
	mux.Handle("GET /api/feeding/rations", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleFeedingRations), "feeding.read")))
	mux.Handle("POST /api/feeding/rations", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleCreateFeedingRation), "feeding.write")))
	mux.Handle("PUT /api/feeding/rations/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUpdateFeedingRation), "feeding.write")))
	mux.Handle("DELETE /api/feeding/rations/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDeleteFeedingRation), "feeding.write")))
	mux.Handle("GET /api/feeding/plans", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleFeedingPlans), "feeding.read")))
	mux.Handle("POST /api/feeding/plans", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleCreateFeedingPlan), "feeding.write")))
	mux.Handle("PUT /api/feeding/plans/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUpdateFeedingPlan), "feeding.write")))
	mux.Handle("DELETE /api/feeding/plans/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDeleteFeedingPlan), "feeding.write")))
	mux.Handle("GET /api/insights", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleInsights), "dashboard.read")))
	mux.Handle("GET /api/ml/insights", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleMLInsights), "dashboard.read")))
	mux.Handle("POST /api/ml/train", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleMLTrain), "dashboard.read")))
	mux.Handle("GET /api/sales/summary", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleSalesSummary), "sales.read")))
	mux.Handle("GET /api/sales", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleSales), "sales.read")))
	mux.Handle("POST /api/sales", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleCreateSale), "sales.write")))
	mux.Handle("PUT /api/sales/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUpdateSale), "sales.write")))
	mux.Handle("DELETE /api/sales/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDeleteSale), "sales.write")))
	mux.Handle("GET /api/reports/stats", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleReportStats), "reports.read")))
	mux.Handle("GET /api/reports", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleReports), "reports.read")))
	mux.Handle("POST /api/reports/generate", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleGenerateReport), "reports.generate")))
	mux.Handle("GET /api/reports/{id}/download", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDownloadReport), "reports.read")))
	mux.Handle("GET /api/etims/receipts", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleEtimsReceipts), "etims.manage")))
	mux.Handle("POST /api/etims/receipts/generate/{saleId}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleEtimsGenerateReceipt), "etims.manage")))
	mux.Handle("GET /api/etims/receipts/{id}/download", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleEtimsDownloadReceipt), "etims.manage")))
	mux.Handle("GET /api/users/stats", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUserStats), "users.read")))
	mux.Handle("GET /api/users", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUsers), "users.read")))
	mux.Handle("POST /api/users", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleCreateUser), "users.manage")))
	mux.Handle("PUT /api/users/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleUpdateUser), "users.manage")))
	mux.Handle("DELETE /api/users/{id}", s.authRequired(s.permissionRequired(http.HandlerFunc(s.handleDeleteUser), "users.manage")))

	return s.withCORS(mux)
}
