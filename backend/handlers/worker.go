package handlers

import (
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"diploma-verify/db"
)

const secretKey = "DIPLOMA_SECRET_KEY"

// processJob runs in a goroutine — acts as the worker
func processJob(jobID, filePath string) {
	setJobStatus(jobID, "processing", "")

	var records []map[string]string
	var err error

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".csv":
		records, err = parseCSV(filePath)
	case ".xlsx":
		records, err = parseXLSX(filePath)
	default:
		setJobStatus(jobID, "failed", "unsupported file type")
		return
	}

	if err != nil {
		setJobStatus(jobID, "failed", err.Error())
		return
	}

	for _, r := range records {
		normalized := normalize(r)
		hash := generateHash(normalized)

		_, dbErr := db.DB.Exec(
			`INSERT OR IGNORE INTO diplomas (hash, full_name, diploma_number, university, degree, date, upload_job_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			hash,
			normalized["full_name"],
			normalized["diploma_number"],
			normalized["university"],
			normalized["degree"],
			normalized["date"],
			jobID,
		)
		if dbErr != nil {
			log.Printf("insert error for job %s: %v", jobID, dbErr)
		}
	}

	setJobStatus(jobID, "done", "")
}

func setJobStatus(jobID, status, errMsg string) {
	db.DB.Exec(
		`UPDATE upload_jobs SET status = ?, error = ?, updated_at = ? WHERE id = ?`,
		status, errMsg, time.Now().UTC(), jobID,
	)
}

// normalize maps flexible column names to standard fields
func normalize(row map[string]string) map[string]string {
	aliases := map[string][]string{
		"full_name":      {"full_name", "name", "student", "фио", "студент"},
		"diploma_number": {"diploma_number", "number", "diploma_no", "номер", "номер диплома"},
		"university":     {"university", "institution", "вуз", "университет"},
		"degree":         {"degree", "qualification", "степень", "квалификация"},
		"date":           {"date", "issue_date", "дата", "дата выдачи"},
	}

	result := make(map[string]string)
	for field, keys := range aliases {
		for _, k := range keys {
			if v, ok := row[strings.ToLower(strings.TrimSpace(k))]; ok {
				result[field] = strings.TrimSpace(v)
				break
			}
		}
		if result[field] == "" {
			result[field] = ""
		}
	}
	return result
}

func generateHash(d map[string]string) string {
	raw := d["full_name"] + d["diploma_number"] + d["university"] + d["date"] + secretKey
	return fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
}

func parseCSV(path string) ([]map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	rows, err := r.ReadAll()
	if err != nil {
		// retry with semicolon delimiter
		f.Seek(0, 0)
		r2 := csv.NewReader(f)
		r2.Comma = ';'
		r2.LazyQuotes = true
		rows, err = r2.ReadAll()
		if err != nil {
			return nil, err
		}
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("empty file")
	}

	headers := rows[0]
	var records []map[string]string
	for _, row := range rows[1:] {
		rec := make(map[string]string)
		for i, h := range headers {
			if i < len(row) {
				rec[strings.ToLower(strings.TrimSpace(h))] = row[i]
			}
		}
		records = append(records, rec)
	}
	return records, nil
}
