package api

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	db        *pgxpool.Pool
	jwtSecret []byte
}

type authContextKey string

const userIDContextKey authContextKey = "user_id"
const userRoleContextKey authContextKey = "user_role"

func NewServer(db *pgxpool.Pool, jwtSecret string) *Server {
	return &Server{db: db, jwtSecret: []byte(jwtSecret)}
}

func (s *Server) Mux() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.Handle("GET /api/auth/me", s.authRequired(http.HandlerFunc(s.handleMe)))

	mux.Handle("GET /api/dashboard", s.authRequired(http.HandlerFunc(s.handleDashboard)))
	mux.Handle("GET /api/animals", s.authRequired(http.HandlerFunc(s.handleAnimals)))
	mux.Handle("POST /api/animals", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleCreateAnimal), "owner", "manager")))
	mux.Handle("PUT /api/animals/{tagId}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleUpdateAnimal), "owner", "manager")))
	mux.Handle("DELETE /api/animals/{tagId}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleDeleteAnimal), "owner", "manager")))
	mux.Handle("GET /api/health/upcoming", s.authRequired(http.HandlerFunc(s.handleUpcomingVaccinations)))
	mux.Handle("GET /api/health/records", s.authRequired(http.HandlerFunc(s.handleHealthRecords)))
	mux.Handle("POST /api/health/records", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleCreateHealthRecord), "owner", "manager", "veterinarian")))
	mux.Handle("PUT /api/health/records/{id}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleUpdateHealthRecord), "owner", "manager", "veterinarian")))
	mux.Handle("DELETE /api/health/records/{id}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleDeleteHealthRecord), "owner", "manager", "veterinarian")))
	mux.Handle("GET /api/breeding/active", s.authRequired(http.HandlerFunc(s.handleBreedingActive)))
	mux.Handle("GET /api/breeding/births", s.authRequired(http.HandlerFunc(s.handleBreedingBirths)))
	mux.Handle("POST /api/breeding", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleCreateBreedingRecord), "owner", "manager")))
	mux.Handle("PUT /api/breeding/{id}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleUpdateBreedingRecord), "owner", "manager")))
	mux.Handle("DELETE /api/breeding/{id}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleDeleteBreedingRecord), "owner", "manager")))
	mux.Handle("GET /api/production/summary", s.authRequired(http.HandlerFunc(s.handleProductionSummary)))
	mux.Handle("GET /api/production/logs", s.authRequired(http.HandlerFunc(s.handleProductionLogs)))
	mux.Handle("POST /api/production/logs", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleCreateProductionLog), "owner", "manager", "worker")))
	mux.Handle("PUT /api/production/logs/{id}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleUpdateProductionLog), "owner", "manager")))
	mux.Handle("DELETE /api/production/logs/{id}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleDeleteProductionLog), "owner", "manager")))
	mux.Handle("GET /api/expenses/summary", s.authRequired(http.HandlerFunc(s.handleExpensesSummary)))
	mux.Handle("GET /api/expenses", s.authRequired(http.HandlerFunc(s.handleExpenses)))
	mux.Handle("POST /api/expenses", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleCreateExpense), "owner", "manager")))
	mux.Handle("PUT /api/expenses/{id}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleUpdateExpense), "owner", "manager")))
	mux.Handle("DELETE /api/expenses/{id}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleDeleteExpense), "owner", "manager")))
	mux.Handle("GET /api/sales/summary", s.authRequired(http.HandlerFunc(s.handleSalesSummary)))
	mux.Handle("GET /api/sales", s.authRequired(http.HandlerFunc(s.handleSales)))
	mux.Handle("POST /api/sales", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleCreateSale), "owner", "manager")))
	mux.Handle("PUT /api/sales/{id}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleUpdateSale), "owner", "manager")))
	mux.Handle("DELETE /api/sales/{id}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleDeleteSale), "owner", "manager")))
	mux.Handle("GET /api/reports/stats", s.authRequired(http.HandlerFunc(s.handleReportStats)))
	mux.Handle("GET /api/reports", s.authRequired(http.HandlerFunc(s.handleReports)))
	mux.Handle("POST /api/reports/generate", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleGenerateReport), "owner", "manager")))
	mux.Handle("GET /api/reports/{id}/download", s.authRequired(http.HandlerFunc(s.handleDownloadReport)))
	mux.Handle("GET /api/users/stats", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleUserStats), "owner", "manager")))
	mux.Handle("GET /api/users", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleUsers), "owner", "manager")))
	mux.Handle("POST /api/users", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleCreateUser), "owner")))
	mux.Handle("PUT /api/users/{id}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleUpdateUser), "owner")))
	mux.Handle("DELETE /api/users/{id}", s.authRequired(s.roleRequired(http.HandlerFunc(s.handleDeleteUser), "owner")))

	return withCORS(mux)
}
