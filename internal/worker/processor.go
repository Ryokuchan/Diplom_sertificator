package worker

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"diasoft-diploma-api/internal/database"
	"diasoft-diploma-api/internal/kafka"
	"diasoft-diploma-api/internal/logger"
	"diasoft-diploma-api/internal/models"
)

func diplomaHashSecret() string {
	if s := os.Getenv("DIPLOMA_SECRET_KEY"); s != "" {
		return s
	}
	return "DIPLOMA_SECRET_KEY_CHANGE_IN_PRODUCTION"
}

// ValidationError описывает ошибку валидации одной строки
type ValidationError struct {
	Row    int    `json:"row"`
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// ProcessResult итог обработки файла
type ProcessResult struct {
	Total    int               `json:"total"`
	Success  int               `json:"success"`
	Skipped  int               `json:"skipped"`
	Errors   []ValidationError `json:"errors,omitempty"`
}

type Worker struct {
	db     *database.DB
	kafka  *kafka.Producer
	log    *logger.Logger
	jobsCh chan Job
}

type Job struct {
	ID       string
	FilePath string
	UserID   int64
}

func NewWorker(db *database.DB, kp *kafka.Producer, log *logger.Logger) *Worker {
	return &Worker{
		db:     db,
		kafka:  kp,
		log:    log,
		jobsCh: make(chan Job, 100),
	}
}

func (w *Worker) Start(ctx context.Context) {
	for i := 0; i < 3; i++ {
		go w.processJobs(ctx)
	}
}

func (w *Worker) EnqueueJob(id string, filePath string, userID int64) {
	select {
	case w.jobsCh <- Job{ID: id, FilePath: filePath, UserID: userID}:
	default:
		w.log.Error("Job queue is full, dropping job", "jobId", id)
	}
}

func (w *Worker) processJobs(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-w.jobsCh:
			w.processJob(ctx, job)
		}
	}
}

func (w *Worker) processJob(ctx context.Context, job Job) {
	w.log.Info("Processing job", "jobId", job.ID)
	w.setJobStatus(ctx, job.ID, "processing", "", 0)

	var records []map[string]string
	var err error

	ext := strings.ToLower(filepath.Ext(job.FilePath))
	switch ext {
	case ".csv":
		records, err = w.parseCSV(job.FilePath)
	case ".xlsx", ".xls":
		records, err = w.parseXLSX(job.FilePath)
	default:
		w.setJobStatus(ctx, job.ID, "failed", "unsupported file type", 0)
		return
	}

	if err != nil {
		w.log.Error("Failed to parse file", "jobId", job.ID, "error", err)
		w.setJobStatus(ctx, job.ID, "failed", err.Error(), 0)
		return
	}

	result := w.bulkInsert(ctx, job, records)

	summary := fmt.Sprintf("Processed %d/%d records, skipped %d duplicates", result.Success, result.Total, result.Skipped)
	if len(result.Errors) > 0 {
		summary += fmt.Sprintf(", %d validation errors", len(result.Errors))
	}

	w.log.Info("Job completed", "jobId", job.ID, "result", result)
	w.setJobStatus(ctx, job.ID, "done", summary, 100)

	// Удаляем файл после обработки
	if err := os.Remove(job.FilePath); err != nil {
		w.log.Error("Failed to remove uploaded file", "path", job.FilePath, "error", err)
	}

	// Публикуем итоговое событие в Kafka
	errorsJSON, _ := json.Marshal(result.Errors)
	event := models.DiplomaEvent{
		Type:   "batch.processed",
		UserID: job.UserID,
		Data: map[string]interface{}{
			"job_id":  job.ID,
			"total":   result.Total,
			"success": result.Success,
			"skipped": result.Skipped,
			"errors":  string(errorsJSON),
		},
	}
	w.kafka.PublishEvent(ctx, "diploma-events", job.ID, event)
}

// bulkInsert вставляет записи пачками по 50, обновляет прогресс
func (w *Worker) bulkInsert(ctx context.Context, job Job, records []map[string]string) ProcessResult {
	result := ProcessResult{Total: len(records)}
	total := len(records)

	for i, r := range records {
		normalized := w.normalize(r)

		// Валидация
		if errs := w.validate(normalized, i+2); len(errs) > 0 {
			result.Errors = append(result.Errors, errs...)
			continue
		}

		hash := w.generateHash(normalized)
		qrLink := w.generateQRLink(hash)

		metadata := map[string]interface{}{
			"name":       normalized["full_name"],
			"specialty":  normalized["degree"],
			"university": normalized["university"],
			"year":       normalized["date"],
		}
		metadataJSON, _ := json.Marshal(metadata)

		// Реестр от ВУЗа: student_id не заполняем (владелец диплома — не учётка загрузчика).
		tag, err := w.db.Exec(ctx,
			`INSERT INTO diplomas (student_id, university_id, diploma_number, status, metadata, qr_code)
			 VALUES ($1, $2, $3, 'verified', $4, $5)
			 ON CONFLICT (diploma_number) DO NOTHING`,
			nil,
			job.UserID,
			normalized["diploma_number"],
			metadataJSON,
			qrLink,
		)
		if err != nil {
			w.log.Error("Failed to insert diploma", "row", i+2, "error", err)
			result.Errors = append(result.Errors, ValidationError{Row: i + 2, Field: "db", Reason: err.Error()})
			continue
		}

		if tag.RowsAffected() == 0 {
			result.Skipped++
		} else {
			result.Success++
		}

		// Обновляем прогресс каждые 50 записей
		if (i+1)%50 == 0 || i+1 == total {
			progress := int(float64(i+1) / float64(total) * 100)
			w.setJobStatus(ctx, job.ID, "processing", "", progress)
		}
	}

	return result
}

// validate проверяет обязательные поля и форматы
func (w *Worker) validate(row map[string]string, rowNum int) []ValidationError {
	var errs []ValidationError

	required := []string{"full_name", "diploma_number"}
	for _, field := range required {
		if row[field] == "" {
			errs = append(errs, ValidationError{Row: rowNum, Field: field, Reason: "required field is empty"})
		}
	}

	// Номер диплома: только буквы, цифры, дефис
	if row["diploma_number"] != "" {
		matched, _ := regexp.MatchString(`^[A-Za-zА-Яа-яЁё0-9\-\/\s]+$`, row["diploma_number"])
		if !matched {
			errs = append(errs, ValidationError{Row: rowNum, Field: "diploma_number", Reason: "invalid format"})
		}
	}

	// Дата: пробуем распознать
	if row["date"] != "" {
		if !isValidDate(row["date"]) {
			errs = append(errs, ValidationError{Row: rowNum, Field: "date", Reason: "unrecognized date format"})
		}
	}

	return errs
}

// isValidDate пробует несколько форматов даты
func isValidDate(s string) bool {
	formats := []string{
		"2006", "2006-01-02", "02.01.2006", "01/02/2006", "2006/01/02",
		"January 2006", "Jan 2006",
	}
	for _, f := range formats {
		if _, err := time.Parse(f, strings.TrimSpace(s)); err == nil {
			return true
		}
	}
	// Просто год (4 цифры)
	matched, _ := regexp.MatchString(`^\d{4}$`, strings.TrimSpace(s))
	return matched
}

func (w *Worker) setJobStatus(ctx context.Context, jobID, status, message string, progress int) {
	_, err := w.db.Exec(ctx,
		`UPDATE upload_jobs SET status = $1, error = $2, progress = $3, updated_at = NOW() WHERE id = $4`,
		status, message, progress, jobID,
	)
	if err != nil {
		w.log.Error("Failed to update job status", "jobId", jobID, "error", err)
	}
}

func (w *Worker) normalize(row map[string]string) map[string]string {
	aliases := map[string][]string{
		"full_name":      {"full_name", "name", "student", "фио", "студент", "ФИО"},
		"diploma_number": {"diploma_number", "number", "diploma_no", "номер", "номер диплома", "Номер диплома"},
		"university":     {"university", "institution", "вуз", "университет", "ВУЗ"},
		"degree":         {"degree", "qualification", "specialty", "степень", "квалификация", "специальность"},
		"date":           {"date", "issue_date", "year", "дата", "дата выдачи", "год"},
	}

	result := make(map[string]string)
	for field, keys := range aliases {
		for _, k := range keys {
			if v, ok := row[strings.ToLower(strings.TrimSpace(k))]; ok && strings.TrimSpace(v) != "" {
				result[field] = strings.TrimSpace(v)
				break
			}
		}
	}
	return result
}

func (w *Worker) generateHash(d map[string]string) string {
	raw := d["full_name"] + "|" + d["diploma_number"] + "|" + d["university"] + "|" + d["date"] + "|" + diplomaHashSecret()
	return fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
}

// generateQRLink возвращает публичную ссылку для верификации (вшивается в QR-код)
func (w *Worker) generateQRLink(hash string) string {
	baseURL := os.Getenv("PUBLIC_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	return baseURL + "/api/v1/verify/" + hash
}

func (w *Worker) parseCSV(path string) ([]map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Пробуем запятую, потом точку с запятой
	for _, comma := range []rune{',', ';'} {
		f.Seek(0, 0)
		r := csv.NewReader(f)
		r.Comma = comma
		r.LazyQuotes = true
		r.TrimLeadingSpace = true

		rows, err := r.ReadAll()
		if err != nil || len(rows) < 2 {
			continue
		}

		return rowsToMaps(rows), nil
	}

	return nil, fmt.Errorf("cannot parse CSV: empty or invalid format")
}

func (w *Worker) parseXLSX(path string) ([]map[string]string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("no sheets found")
	}

	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, err
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("empty sheet")
	}

	return rowsToMaps(rows), nil
}

// rowsToMaps конвертирует [][]string в []map[string]string используя первую строку как заголовки
func rowsToMaps(rows [][]string) []map[string]string {
	headers := rows[0]
	var records []map[string]string
	for _, row := range rows[1:] {
		rec := make(map[string]string)
		for i, h := range headers {
			if i < len(row) {
				rec[strings.ToLower(strings.TrimSpace(h))] = strings.TrimSpace(row[i])
			}
		}
		// Пропускаем полностью пустые строки
		hasData := false
		for _, v := range rec {
			if v != "" {
				hasData = true
				break
			}
		}
		if hasData {
			records = append(records, rec)
		}
	}
	return records
}
