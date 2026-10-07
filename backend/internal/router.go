package internal

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/cors"

	"examshield/internal/auth"
	"examshield/internal/config"
	"examshield/internal/handlers"
	appmw "examshield/internal/middleware"
	"examshield/internal/models"
	"examshield/internal/repositories"
	"examshield/internal/services"
	"examshield/internal/websocket"
	"examshield/internal/webui"

	"gorm.io/gorm"
)

// NewRouter wires every dependency and returns the fully configured
// HTTP handler for the application.
func NewRouter(cfg *config.Config, gdb *gorm.DB, logger *slog.Logger) http.Handler {
	tm := auth.NewTokenManager(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	hub := websocket.NewHub(logger)

	userRepo := repositories.NewUserRepository(gdb)
	examRepo := repositories.NewExamRepository(gdb)
	monRepo := repositories.NewMonitoringRepository(gdb)

	auditService := services.NewAuditService(monRepo, logger)
	authService := services.NewAuthService(userRepo, tm, auditService)
	servePort, err := strconv.Atoi(cfg.HTTPPort)
	if err != nil {
		servePort = 8080
	}
	examService := services.NewExamService(examRepo, monRepo, hub, logger, servePort)
	monitoringService := services.NewMonitoringService(monRepo, hub, cfg.AIServiceURL, logger)
	reportService := services.NewReportService(examRepo, monRepo)

	authHandler := handlers.NewAuthHandler(authService)
	userHandler := handlers.NewUserHandler(userRepo)
	examHandler := handlers.NewExamHandler(examService, auditService)
	monitoringHandler := handlers.NewMonitoringHandler(monitoringService)
	dashboardHandler := handlers.NewDashboardHandler(examService, reportService)

	r := mux.NewRouter()
	r.Use(appmw.SecurityHeaders)
	r.Use(appmw.RequestLogger(logger))

	loginLimiter := appmw.NewRateLimiter(10, time.Minute)

	api := r.PathPrefix("/api/v1").Subrouter()

	// --- Public auth routes ---
	api.Handle("/auth/login", loginLimiter.Middleware(http.HandlerFunc(authHandler.Login))).Methods(http.MethodPost)
	api.HandleFunc("/auth/refresh", authHandler.Refresh).Methods(http.MethodPost)

	// --- Authenticated routes ---
	authed := api.PathPrefix("").Subrouter()
	authed.Use(appmw.Authenticate(tm))

	authed.HandleFunc("/auth/logout", authHandler.Logout).Methods(http.MethodPost)
	authed.HandleFunc("/dashboard", dashboardHandler.Summary).Methods(http.MethodGet)

	// User management: SUPER_ADMIN and ADMIN only.
	adminOnly := appmw.RequireRole(models.RoleSuperAdmin, models.RoleAdmin)
	authed.Handle("/auth/register", adminOnly(http.HandlerFunc(authHandler.Register))).Methods(http.MethodPost)
	authed.Handle("/users", adminOnly(http.HandlerFunc(userHandler.List))).Methods(http.MethodGet)
	authed.Handle("/users/{id}/suspend", adminOnly(http.HandlerFunc(userHandler.Suspend))).Methods(http.MethodPost)
	authed.Handle("/users/{id}/reinstate", adminOnly(http.HandlerFunc(userHandler.Reinstate))).Methods(http.MethodPost)

	// Exams: creation/editing by ADMIN + LECTURER; SUPER_ADMIN can view everything.
	staffOnly := appmw.RequireRole(models.RoleSuperAdmin, models.RoleAdmin, models.RoleLecturer)
	authed.Handle("/exams", staffOnly(http.HandlerFunc(examHandler.Create))).Methods(http.MethodPost)
	authed.Handle("/exams", staffOnly(http.HandlerFunc(examHandler.List))).Methods(http.MethodGet)
	authed.Handle("/exams/{id}", staffOnly(http.HandlerFunc(examHandler.Get))).Methods(http.MethodGet)
	authed.Handle("/exams/{id}", staffOnly(http.HandlerFunc(examHandler.Update))).Methods(http.MethodPut)
	authed.Handle("/exams/{id}", adminOnly(http.HandlerFunc(examHandler.Delete))).Methods(http.MethodDelete)

	authed.Handle("/exams/{id}/questions", staffOnly(http.HandlerFunc(examHandler.AddQuestion))).Methods(http.MethodPost)
	authed.Handle("/exams/{id}/questions", staffOnly(http.HandlerFunc(examHandler.ListQuestions))).Methods(http.MethodGet)
	authed.Handle("/exams/{id}/questions/{questionId}", staffOnly(http.HandlerFunc(examHandler.DeleteQuestion))).Methods(http.MethodDelete)
	authed.Handle("/exams/{id}/questions/upload-pdf", staffOnly(http.HandlerFunc(examHandler.UploadQuestionsPDF))).Methods(http.MethodPost)

	authed.Handle("/exams/{id}/schedule", staffOnly(http.HandlerFunc(examHandler.Schedule))).Methods(http.MethodPost)
	authed.Handle("/exams/{id}/publish", staffOnly(http.HandlerFunc(examHandler.Publish))).Methods(http.MethodPost)
	authed.Handle("/exams/{id}/start", staffOnly(http.HandlerFunc(examHandler.Start))).Methods(http.MethodPost)
	authed.Handle("/exams/{id}/end", staffOnly(http.HandlerFunc(examHandler.End))).Methods(http.MethodPost)

	authed.Handle("/exams/{id}/monitoring", staffOnly(http.HandlerFunc(monitoringHandler.ListEvents))).Methods(http.MethodGet)
	authed.Handle("/exams/{id}/events", staffOnly(http.HandlerFunc(monitoringHandler.ListEvents))).Methods(http.MethodGet)
	authed.Handle("/exams/{id}/events/{eventId}/review", staffOnly(http.HandlerFunc(monitoringHandler.ReviewEvent))).Methods(http.MethodPost)
	authed.Handle("/exams/{id}/report", staffOnly(http.HandlerFunc(dashboardHandler.ExamReport))).Methods(http.MethodGet)

	// Student-facing exam-taking routes.
	studentOnly := appmw.RequireRole(models.RoleStudent)
	// Returns the currently ACTIVE exam for a student (or {"exam": null}).
	authed.Handle("/exams/my-active", studentOnly(http.HandlerFunc(examHandler.GetMyActiveExam))).Methods(http.MethodGet)
	authed.Handle("/exams/{id}/start-attempt", studentOnly(http.HandlerFunc(examHandler.StartAttempt))).Methods(http.MethodPost)
	authed.Handle("/exams/attempts/{attemptId}/submit", studentOnly(http.HandlerFunc(examHandler.SubmitExam))).Methods(http.MethodPost)
	authed.HandleFunc("/answers", examHandler.SubmitAnswer).Methods(http.MethodPost)

	// Face verification / AI monitoring ingestion — Go relays to Python.
	authed.HandleFunc("/face/verify", monitoringHandler.VerifyFace).Methods(http.MethodPost)
	authed.HandleFunc("/monitoring/analyze-frame", monitoringHandler.AnalyzeFrame).Methods(http.MethodPost)

	// --- WebSocket monitoring (token passed as query param) ---
	r.HandleFunc("/ws/exams/{id}/monitor", websocket.ServeMonitoring(hub, tm, cfg.CORSAllowedOrigins))

	// --- Embedded frontend (SPA) ---
	// Mounted last, as a catch-all: this is what makes LAN/offline
	// mode actually usable — the same process and port that answers
	// /api/v1/* and /ws/* also serves the built React app, so a
	// student visiting http://<lan-ip>:<port> in a browser gets the
	// full UI, not a bare 404.
	r.PathPrefix("/").Handler(webui.Handler("/api/v1", "/ws"))

	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   cfg.CORSAllowedOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
	})

	return corsHandler.Handler(r)
}
