package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"diasoft-diploma-api/internal/database"
	"diasoft-diploma-api/internal/kafka"
	"diasoft-diploma-api/internal/logger"
	"diasoft-diploma-api/internal/models"
)

type DiplomaHandler struct {
	db             *database.DB
	redis          *redis.Client
	kafka          *kafka.Producer
	uploadEnqueuer kafka.JobEnqueuer
	log            *logger.Logger
	jwtSecret      string
}

func NewDiplomaHandler(db *database.DB, rdb *redis.Client, kp *kafka.Producer, uploadEnqueuer kafka.JobEnqueuer, jwtSecret string, log *logger.Logger) *DiplomaHandler {
	return &DiplomaHandler{db: db, redis: rdb, kafka: kp, uploadEnqueuer: uploadEnqueuer, jwtSecret: jwtSecret, log: log}
}

type CreateDiplomaRequest struct {
	DiplomaNumber string                 `json:"diploma_number" binding:"required"`
	UniversityID  int64                  `json:"university_id" binding:"required"`
	Metadata      map[string]interface{} `json:"metadata"`
}

func (h *DiplomaHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	var req CreateDiplomaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	metadata, _ := json.Marshal(req.Metadata)
	var diplomaID int64
	err := h.db.QueryRow(ctx,
		"INSERT INTO diplomas (student_id, university_id, diploma_number, status, metadata) VALUES ($1, $2, $3, 'pending', $4) RETURNING id",
		userID, req.UniversityID, req.DiplomaNumber, metadata,
	).Scan(&diplomaID)
	if err != nil {
		h.log.Error("Failed to create diploma", "error", err)
		c.JSON(http.StatusConflict, gin.H{"error": "Diploma already exists"})
		return
	}

	event := models.DiplomaEvent{
		Type:      "diploma.created",
		DiplomaID: diplomaID,
		UserID:    userID,
		Data:      req.Metadata,
	}
	h.kafka.PublishEvent(ctx, "diploma-events", fmt.Sprintf("%d", diplomaID), event)

	c.JSON(http.StatusCreated, gin.H{"id": diplomaID, "status": "pending"})
}

func (h *DiplomaHandler) GetByID(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	cacheKey := "diploma:" + id
	if cached, err := h.redis.Get(ctx, cacheKey).Result(); err == nil {
		var diploma map[string]interface{}
		if err := json.Unmarshal([]byte(cached), &diploma); err == nil {
			c.JSON(http.StatusOK, diploma)
			return
		}
	}

	var studentID, universityID int64
	var diplomaNumber, status string
	var metadata []byte
	var createdAt time.Time
	err := h.db.QueryRow(ctx,
		"SELECT student_id, university_id, diploma_number, status, metadata, created_at FROM diplomas WHERE id = $1",
		id,
	).Scan(&studentID, &universityID, &diplomaNumber, &status, &metadata, &createdAt)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Diploma not found"})
		return
	}

	var metadataMap map[string]interface{}
	json.Unmarshal(metadata, &metadataMap)

	diploma := gin.H{
		"id":             id,
		"student_id":     studentID,
		"university_id":  universityID,
		"diploma_number": diplomaNumber,
		"status":         status,
		"metadata":       metadataMap,
		"created_at":     createdAt.Format(time.RFC3339),
	}

	data, _ := json.Marshal(diploma)
	h.redis.Set(ctx, cacheKey, data, 10*time.Minute)

	c.JSON(http.StatusOK, diploma)
}

func (h *DiplomaHandler) List(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit > 100 || limit < 1 {
		limit = 20
	}
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	rows, err := h.db.Query(ctx,
		"SELECT id, diploma_number, status, created_at FROM diplomas WHERE student_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3",
		userID, limit, offset,
	)
	if err != nil {
		h.log.Error("Failed to fetch diplomas", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch diplomas"})
		return
	}
	defer rows.Close()

	diplomas := []gin.H{}
	for rows.Next() {
		var id int64
		var diplomaNumber, status string
		var createdAt time.Time
		if err := rows.Scan(&id, &diplomaNumber, &status, &createdAt); err != nil {
			h.log.Error("Failed to scan diploma row", "error", err)
			continue
		}
		diplomas = append(diplomas, gin.H{
			"id":             id,
			"diploma_number": diplomaNumber,
			"status":         status,
			"created_at":     createdAt.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		h.log.Error("Row iteration error in List", "error", err)
	}

	c.JSON(http.StatusOK, gin.H{"data": diplomas, "page": page, "limit": limit})
}

func (h *DiplomaHandler) Verify(c *gin.Context) {
	ctx := c.Request.Context()
	diplomaID := c.Param("id")
	verifierID := c.GetInt64("user_id")

	if c.GetString("role") == "university" {
		var ownerUni int64
		err := h.db.QueryRow(ctx, "SELECT university_id FROM diplomas WHERE id = $1", diplomaID).Scan(&ownerUni)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Diploma not found"})
			return
		}
		if ownerUni != verifierID {
			c.JSON(http.StatusForbidden, gin.H{"error": "Можно подтверждать только заявки своего вуза"})
			return
		}
	}

	_, err := h.db.Exec(ctx,
		"UPDATE diplomas SET status = 'verified', updated_at = NOW() WHERE id = $1",
		diplomaID,
	)
	if err != nil {
		h.log.Error("Failed to verify diploma", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify diploma"})
		return
	}

	h.db.Exec(ctx,
		"INSERT INTO verification_logs (diploma_id, verifier_id, action) VALUES ($1, $2, 'verified')",
		diplomaID, verifierID,
	)

	h.redis.Del(ctx, "diploma:"+diplomaID)
	h.redis.Del(ctx, "verify:"+diplomaID)

	id, _ := strconv.ParseInt(diplomaID, 10, 64)
	h.kafka.PublishEvent(ctx, "diploma-events", diplomaID, models.DiplomaEvent{
		Type:      "diploma.verified",
		DiplomaID: id,
		UserID:    verifierID,
	})

	c.JSON(http.StatusOK, gin.H{"message": "Diploma verified"})
}

func (h *DiplomaHandler) GetStudentDocuments(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	rows, err := h.db.Query(ctx,
		"SELECT diploma_number, status FROM diplomas WHERE student_id = $1 ORDER BY created_at DESC",
		userID,
	)
	if err != nil {
		h.log.Error("Failed to fetch student documents", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch documents"})
		return
	}
	defer rows.Close()

	docs := []gin.H{}
	for rows.Next() {
		var name, status string
		if err := rows.Scan(&name, &status); err != nil {
			h.log.Error("Failed to scan document row", "error", err)
			continue
		}
		docs = append(docs, gin.H{"name": name, "status": status})
	}
	if err := rows.Err(); err != nil {
		h.log.Error("Row iteration error in GetStudentDocuments", "error", err)
	}

	c.JSON(http.StatusOK, docs)
}

// optionalEmployerFromToken — если передан валидный JWT роли hr, возвращаем id для аудита проверок.
func (h *DiplomaHandler) optionalEmployerFromToken(c *gin.Context) (userID int64, ok bool) {
	if h.jwtSecret == "" {
		return 0, false
	}
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return 0, false
	}
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		return []byte(h.jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return 0, false
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, false
	}
	uid, ok := claims["user_id"].(float64)
	if !ok {
		return 0, false
	}
	role, _ := claims["role"].(string)
	if role != "hr" {
		return 0, false
	}
	return int64(uid), true
}

func (h *DiplomaHandler) logEmployerPublicCheck(ctx context.Context, verifierID, diplomaID int64) {
	_, err := h.db.Exec(ctx,
		`INSERT INTO verification_logs (diploma_id, verifier_id, action, metadata) VALUES ($1, $2, 'public_check', '{}')`,
		diplomaID, verifierID,
	)
	if err != nil {
		h.log.Error("Failed to log employer public verification", "error", err)
	}
}

func metadataStr(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func metadataYearInt(m map[string]interface{}) int {
	if m == nil {
		return 0
	}
	v, ok := m["year"]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		s := strings.TrimSpace(x)
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
		if len(s) >= 4 {
			if n, err := strconv.Atoi(s[:4]); err == nil {
				return n
			}
		}
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return 0
}

func (h *DiplomaHandler) VerifyPublic(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	verifierID, isEmployer := h.optionalEmployerFromToken(c)

	cacheKey := "verify:" + id
	if !isEmployer {
		if cached, err := h.redis.Get(ctx, cacheKey).Result(); err == nil {
			var result map[string]interface{}
			if err := json.Unmarshal([]byte(cached), &result); err == nil {
				c.JSON(http.StatusOK, result)
				return
			}
		}
	}

	var diplomaPK int64
	var studentID, universityID *int64
	var diplomaNumber, status string
	var metadata []byte
	var qrCode string

	err := h.db.QueryRow(ctx,
		`SELECT id, student_id, university_id, diploma_number, status, metadata, COALESCE(qr_code, '')
		FROM diplomas
		WHERE diploma_number = $1 OR qr_code LIKE '%' || $1 OR id::text = $1
		LIMIT 1`,
		id,
	).Scan(&diplomaPK, &studentID, &universityID, &diplomaNumber, &status, &metadata, &qrCode)

	if err != nil || status == "revoked" || status == "pending" {
		result := gin.H{"valid": false}
		resultJSON, _ := json.Marshal(result)
		if !isEmployer {
			h.redis.Set(ctx, cacheKey, resultJSON, 5*time.Minute)
		}
		c.JSON(http.StatusOK, result)
		return
	}

	var metadataMap map[string]interface{}
	json.Unmarshal(metadata, &metadataMap)

	name, university, specialty, year := "", "", "", ""
	if v := metadataMap["name"]; v != nil {
		name = fmt.Sprintf("%v", v)
	}
	if v := metadataMap["university"]; v != nil {
		university = fmt.Sprintf("%v", v)
	}
	if v := metadataMap["specialty"]; v != nil {
		specialty = fmt.Sprintf("%v", v)
	}
	if v := metadataMap["year"]; v != nil {
		year = fmt.Sprintf("%v", v)
	}

	result := gin.H{
		"valid":      true,
		"name":       name,
		"university": university,
		"specialty":  specialty,
		"year":       year,
		"hash":       qrCode,
	}

	if isEmployer {
		h.logEmployerPublicCheck(ctx, verifierID, diplomaPK)
	}

	resultJSON, _ := json.Marshal(result)
	if !isEmployer {
		h.redis.Set(ctx, cacheKey, resultJSON, 30*time.Minute)
	}

	c.JSON(http.StatusOK, result)
}

func (h *DiplomaHandler) UploadFile(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	ext := filepath.Ext(file.Filename)
	if ext != ".csv" && ext != ".xlsx" && ext != ".xls" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Only .csv and .xlsx files are supported"})
		return
	}

	jobID := uuid.NewString()
	uploadDir := filepath.Join("data", "uploads")
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		h.log.Error("Failed to create upload directory", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cannot create upload directory"})
		return
	}

	savePath := filepath.Join(uploadDir, jobID+ext)
	if err := c.SaveUploadedFile(file, savePath); err != nil {
		h.log.Error("Failed to save file", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Cannot save file"})
		return
	}

	_, err = h.db.Exec(ctx,
		`INSERT INTO upload_jobs (id, user_id, filename, status, progress) VALUES ($1, $2, $3, 'pending', 0)`,
		jobID, userID, file.Filename,
	)
	if err != nil {
		h.log.Error("Failed to create upload job", "error", err)
		os.Remove(savePath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create job"})
		return
	}

	h.log.Info("File uploaded", "filename", file.Filename, "jobId", jobID, "userId", userID)

	if h.uploadEnqueuer != nil {
		h.uploadEnqueuer.EnqueueJob(jobID, savePath, userID)
	} else {
		_ = h.kafka.PublishEvent(ctx, "diploma-events", jobID, models.DiplomaEvent{
			Type:   "file.uploaded",
			UserID: userID,
			Data:   map[string]interface{}{"job_id": jobID, "path": savePath},
		})
	}

	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID, "message": "File queued for processing"})
}

func diplomaSecretKey() string {
	if s := os.Getenv("DIPLOMA_SECRET_KEY"); s != "" {
		return s
	}
	return "DIPLOMA_SECRET_KEY_CHANGE_IN_PRODUCTION"
}

func universityManualHash(fullName, diplomaNumber, university, year string) string {
	raw := fullName + "|" + diplomaNumber + "|" + university + "|" + year + "|" + diplomaSecretKey()
	return fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
}

func universityManualQRLink(hash string) string {
	baseURL := os.Getenv("PUBLIC_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	return baseURL + "/api/v1/verify/" + hash
}

// UniversityManualDiplomaRequest — одна запись в реестр (как после обработки Excel), сразу verified.
type UniversityManualDiplomaRequest struct {
	DiplomaNumber string `json:"diploma_number" binding:"required"`
	FullName      string `json:"full_name" binding:"required"`
	Specialty     string `json:"specialty"`
	University    string `json:"university"`
	Year          string `json:"year"`
}

func (h *DiplomaHandler) UniversityManualCreate(c *gin.Context) {
	ctx := c.Request.Context()
	if c.GetString("role") != "university" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Только для учётной записи ВУЗа"})
		return
	}
	userID := c.GetInt64("user_id")

	var req UniversityManualDiplomaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	fullName := strings.TrimSpace(req.FullName)
	num := strings.TrimSpace(req.DiplomaNumber)
	univ := strings.TrimSpace(req.University)
	if univ == "" {
		univ = "—"
	}
	year := strings.TrimSpace(req.Year)
	spec := strings.TrimSpace(req.Specialty)

	hash := universityManualHash(fullName, num, univ, year)
	qrLink := universityManualQRLink(hash)

	metadata := map[string]interface{}{
		"name":       fullName,
		"specialty":  spec,
		"university": univ,
		"year":       year,
	}
	metadataJSON, _ := json.Marshal(metadata)

	var newID int64
	err := h.db.QueryRow(ctx,
		`INSERT INTO diplomas (student_id, university_id, diploma_number, status, metadata, qr_code)
		 VALUES ($1, $2, $3, 'verified', $4, $5)
		 ON CONFLICT (diploma_number) DO NOTHING
		 RETURNING id`,
		nil, userID, num, metadataJSON, qrLink,
	).Scan(&newID)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusConflict, gin.H{"error": "Запись с таким номером диплома уже есть"})
			return
		}
		h.log.Error("University manual diploma insert failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось сохранить запись"})
		return
	}

	h.kafka.PublishEvent(ctx, "diploma-events", fmt.Sprintf("%d", newID), models.DiplomaEvent{
		Type:      "diploma.created",
		DiplomaID: newID,
		UserID:    userID,
		Data:      metadata,
	})

	c.JSON(http.StatusCreated, gin.H{
		"id":         newID,
		"status":     "verified",
		"verify_url": qrLink,
		"hash":       hash,
	})
}

func (h *DiplomaHandler) GetUniversityPendingClaims(c *gin.Context) {
	ctx := c.Request.Context()
	if c.GetString("role") != "university" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Только для учётной записи ВУЗа"})
		return
	}
	userID := c.GetInt64("user_id")

	rows, err := h.db.Query(ctx,
		`SELECT d.id, d.diploma_number, d.metadata, COALESCE(u.email, '')
		 FROM diplomas d
		 LEFT JOIN users u ON u.id = d.student_id
		 WHERE d.university_id = $1 AND d.status = 'pending' AND d.student_id IS NOT NULL
		 ORDER BY d.created_at DESC LIMIT 200`,
		userID,
	)
	if err != nil {
		h.log.Error("GetUniversityPendingClaims failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить заявки"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var id int64
		var num, studentEmail string
		var meta []byte
		if err := rows.Scan(&id, &num, &meta, &studentEmail); err != nil {
			continue
		}
		var m map[string]interface{}
		_ = json.Unmarshal(meta, &m)
		out = append(out, gin.H{
			"id":             id,
			"diploma_number": num,
			"student_email":  studentEmail,
			"name":           metadataStr(m, "name"),
			"specialty":      metadataStr(m, "specialty"),
			"university":     metadataStr(m, "university"),
			"year":           metadataYearInt(m),
		})
	}
	if err := rows.Err(); err != nil {
		h.log.Error("GetUniversityPendingClaims rows", "error", err)
	}

	c.JSON(http.StatusOK, out)
}

func (h *DiplomaHandler) GetUniversityRecords(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	rows, err := h.db.Query(ctx,
		`SELECT d.id, d.diploma_number, d.status, d.metadata, d.created_at
		FROM diplomas d WHERE d.university_id = $1
		ORDER BY d.created_at DESC LIMIT 100`,
		userID,
	)
	if err != nil {
		h.log.Error("Failed to fetch university records", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch records"})
		return
	}
	defer rows.Close()

	records := []gin.H{}
	for rows.Next() {
		var id int64
		var diplomaNumber, status string
		var createdAt time.Time
		var metadata []byte
		if err := rows.Scan(&id, &diplomaNumber, &status, &metadata, &createdAt); err != nil {
			h.log.Error("Failed to scan record row", "error", err)
			continue
		}

		var metadataMap map[string]interface{}
		json.Unmarshal(metadata, &metadataMap)

		name := metadataStr(metadataMap, "name")
		specialty := metadataStr(metadataMap, "specialty")
		year := metadataYearInt(metadataMap)

		records = append(records, gin.H{
			"id":            id,
			"name":          name,
			"specialty":     specialty,
			"year":          year,
			"diplomaNumber": diplomaNumber,
			"status":        status,
			"created_at":    createdAt.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		h.log.Error("Row iteration error in GetUniversityRecords", "error", err)
	}

	c.JSON(http.StatusOK, records)
}

func (h *DiplomaHandler) GetProcessingQueue(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	rows, err := h.db.Query(ctx,
		`SELECT id, filename, status, progress, COALESCE(error, ''), created_at, updated_at
		 FROM upload_jobs WHERE user_id = $1 ORDER BY created_at DESC LIMIT 50`,
		userID,
	)
	if err != nil {
		h.log.Error("Failed to get processing queue", "error", err)
		c.JSON(http.StatusOK, []gin.H{})
		return
	}
	defer rows.Close()

	jobs := []gin.H{}
	for rows.Next() {
		var id, filename, status, errMsg string
		var progress int
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &filename, &status, &progress, &errMsg, &createdAt, &updatedAt); err != nil {
			h.log.Error("Failed to scan job row", "error", err)
			continue
		}
		jobs = append(jobs, gin.H{
			"job_id":     id,
			"filename":   filename,
			"status":     status,
			"progress":   progress,
			"error":      errMsg,
			"created_at": createdAt.Format(time.RFC3339),
			"updated_at": updatedAt.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		h.log.Error("Row iteration error in GetProcessingQueue", "error", err)
	}

	c.JSON(http.StatusOK, jobs)
}

func (h *DiplomaHandler) Revoke(c *gin.Context) {
	ctx := c.Request.Context()
	diplomaID := c.Param("id")
	revokerID := c.GetInt64("user_id")

	result, err := h.db.Exec(ctx,
		`UPDATE diplomas SET status = 'revoked', updated_at = NOW() WHERE id = $1 AND university_id = $2`,
		diplomaID, revokerID,
	)
	if err != nil {
		h.log.Error("Failed to revoke diploma", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to revoke diploma"})
		return
	}
	if result.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Diploma not found or access denied"})
		return
	}

	h.db.Exec(ctx,
		"INSERT INTO verification_logs (diploma_id, verifier_id, action) VALUES ($1, $2, 'revoked')",
		diplomaID, revokerID,
	)

	h.redis.Del(ctx, "diploma:"+diplomaID)
	h.redis.Del(ctx, "verify:"+diplomaID)

	id, _ := strconv.ParseInt(diplomaID, 10, 64)
	h.kafka.PublishEvent(ctx, "diploma-events", diplomaID, models.DiplomaEvent{
		Type:      "diploma.revoked",
		DiplomaID: id,
		UserID:    revokerID,
	})

	c.JSON(http.StatusOK, gin.H{"message": "Diploma revoked"})
}

func (h *DiplomaHandler) GetEmployerHistory(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	rows, err := h.db.Query(ctx,
		`SELECT vl.created_at, d.diploma_number, d.metadata, d.status
		FROM verification_logs vl
		JOIN diplomas d ON d.id = vl.diploma_id
		WHERE vl.verifier_id = $1
		ORDER BY vl.created_at DESC LIMIT 50`,
		userID,
	)
	if err != nil {
		h.log.Error("Failed to fetch employer history", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch history"})
		return
	}
	defer rows.Close()

	history := []gin.H{}
	for rows.Next() {
		var date time.Time
		var diplomaNumber, status string
		var metadata []byte
		if err := rows.Scan(&date, &diplomaNumber, &metadata, &status); err != nil {
			h.log.Error("Failed to scan history row", "error", err)
			continue
		}

		var metadataMap map[string]interface{}
		json.Unmarshal(metadata, &metadataMap)

		name := ""
		if v, ok := metadataMap["name"].(string); ok {
			name = v
		}

		history = append(history, gin.H{
			"date":      date.Format(time.RFC3339),
			"diplomaId": diplomaNumber,
			"name":      name,
			"result":    status == "verified",
		})
	}
	if err := rows.Err(); err != nil {
		h.log.Error("Row iteration error in GetEmployerHistory", "error", err)
	}

	c.JSON(http.StatusOK, history)
}
