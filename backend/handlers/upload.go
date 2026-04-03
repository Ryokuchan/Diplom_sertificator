package handlers

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"diploma-verify/db"
)

func Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
		return
	}

	ext := filepath.Ext(file.Filename)
	if ext != ".csv" && ext != ".xlsx" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "only .csv and .xlsx supported"})
		return
	}

	jobID := uuid.NewString()
	savePath := filepath.Join("data", "uploads", jobID+ext)

	if err := os.MkdirAll(filepath.Dir(savePath), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot create upload dir"})
		return
	}

	if err := c.SaveUploadedFile(file, savePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot save file"})
		return
	}

	_, err = db.DB.Exec(
		`INSERT INTO upload_jobs (id, filename, status) VALUES (?, ?, 'pending')`,
		jobID, file.Filename,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	// enqueue job
	go processJob(jobID, savePath)

	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID})
}
