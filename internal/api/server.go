package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"diasoft-diploma-api/internal/api/handlers"
	"diasoft-diploma-api/internal/api/middleware"
	"diasoft-diploma-api/internal/config"
	"diasoft-diploma-api/internal/database"
	"diasoft-diploma-api/internal/kafka"
	"diasoft-diploma-api/internal/logger"
)

type Server struct {
	config         *config.Config
	db             *database.DB
	redis          *redis.Client
	kafka          *kafka.Producer
	uploadEnqueuer kafka.JobEnqueuer // постановка Excel/CSV в воркер (без ожидания Kafka)
	router         *gin.Engine
	server         *http.Server
	log            *logger.Logger
}

func NewServer(cfg *config.Config, db *database.DB, rdb *redis.Client, kp *kafka.Producer, uploadEnqueuer kafka.JobEnqueuer, log *logger.Logger) *Server {
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	s := &Server{
		config:         cfg,
		db:             db,
		redis:          rdb,
		kafka:          kp,
		uploadEnqueuer: uploadEnqueuer,
		router:         gin.New(),
		log:            log,
	}

	s.router.Use(gin.Recovery())
	s.router.Use(middleware.Logger(log))
	s.router.Use(middleware.CORS())
	s.router.Use(middleware.RateLimiter(rdb))
	s.router.MaxMultipartMemory = 32 << 20 // 32 MB макс размер загружаемого файла

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// Основной UI — те же пути, что в fronted/ (относительные diplom.css / diplom.js)
	s.router.StaticFile("/diplom.css", "./web/site/diplom.css")
	s.router.StaticFile("/diplom.js", "./web/site/diplom.js")
	s.router.Static("/site", "./web/site")
	s.router.StaticFile("/", "./web/site/diplom.html")
	s.router.StaticFile("/favicon.ico", "./web/site/favicon.ico")
	// Старая витрина (при необходимости): /static/...
	s.router.Static("/static", "./web/static")

	authHandler := handlers.NewAuthHandler(s.db, s.config.JWTSecret, s.log)
	userHandler := handlers.NewUserHandler(s.db, s.redis, s.log)
	diplomaHandler := handlers.NewDiplomaHandler(s.db, s.redis, s.kafka, s.uploadEnqueuer, s.config.JWTSecret, s.log)
	shareHandler := handlers.NewShareHandler(s.db, s.redis, s.log)
	batchHandler := handlers.NewBatchHandler(s.db, s.redis, s.log)
	wsHandler := handlers.NewWSHandler(s.db, s.redis, s.log)
	uniAppHandler := handlers.NewUniversityApplicationHandler(s.db, s.log)

	s.router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	s.router.GET("/metrics", gin.WrapH(promhttp.Handler()))
	// Публичная страница диплома по одноразовой ссылке (QR студента)
	s.router.GET("/d/:token", shareHandler.ViewSharedDiplomaHTML)

	api := s.router.Group("/api/v1")
	{
		// Auth endpoints
		api.POST("/auth/register", authHandler.Register)
		api.POST("/auth/login", authHandler.Login)
		api.POST("/auth/refresh", authHandler.RefreshToken)
		api.POST("/auth/university/apply", uniAppHandler.Apply)

		// Public verification endpoint
		api.GET("/verify/:id", diplomaHandler.VerifyPublic)
		api.POST("/verify/batch", batchHandler.VerifyBatch)

		// Public shared diploma access
		api.GET("/shared/:token", shareHandler.AccessSharedDiploma)

		protected := api.Group("")
		protected.Use(middleware.AuthMiddleware(s.config.JWTSecret))
		{
			// User endpoints
			protected.GET("/users/me", userHandler.GetProfile)
			protected.PUT("/users/me", userHandler.UpdateProfile)
			
			// Student endpoints
			protected.GET("/student/profile", userHandler.GetStudentProfile)
			protected.GET("/student/documents", diplomaHandler.GetStudentDocuments)
			protected.GET("/student/universities", userHandler.ListUniversitiesForStudent)
			
			// University endpoints
			protected.POST("/university/upload", diplomaHandler.UploadFile)
			protected.POST("/university/diplomas", diplomaHandler.UniversityManualCreate)
			protected.GET("/university/records", diplomaHandler.GetUniversityRecords)
			protected.GET("/university/pending", diplomaHandler.GetUniversityPendingClaims)
			protected.GET("/university/queue", diplomaHandler.GetProcessingQueue)
			
			// Employer endpoints
			protected.GET("/employer/history", diplomaHandler.GetEmployerHistory)
			
			// Diploma endpoints
			protected.POST("/diplomas", diplomaHandler.Create)
			protected.GET("/diplomas/:id", diplomaHandler.GetByID)
			protected.GET("/diplomas", diplomaHandler.List)
			protected.PUT("/diplomas/:id/verify", diplomaHandler.Verify)
			protected.PUT("/diplomas/:id/revoke", diplomaHandler.Revoke)

			// Share links
			protected.POST("/diplomas/:id/share", shareHandler.CreateShareLink)
			protected.DELETE("/diplomas/:id/share/:token", shareHandler.RevokeShareLink)

			// Job report
			protected.GET("/university/jobs/:id/report", batchHandler.GetJobReport)

			// Admin — заявки ВУЗов
			protected.GET("/admin/university-applications", uniAppHandler.List)
			protected.POST("/admin/university-applications/:id/approve", uniAppHandler.Approve)
			protected.POST("/admin/university-applications/:id/reject", uniAppHandler.Reject)
			protected.GET("/admin/university-applications/:id/file", uniAppHandler.DownloadFile)

			// WebSocket
			protected.GET("/ws/jobs/:id", wsHandler.JobStatus)
		}
	}
}

func (s *Server) Run(addr string) error {
	s.server = &http.Server{
		Addr:           addr,
		Handler:        s.router,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
