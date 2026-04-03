package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"diploma-verify/db"
	"diploma-verify/models"
)

func Verify(c *gin.Context) {
	hash := c.Param("hash")

	var d models.Diploma
	err := db.DB.QueryRow(
		`SELECT hash, full_name, diploma_number, university, degree, date FROM diplomas WHERE hash = ?`,
		hash,
	).Scan(&d.Hash, &d.FullName, &d.DiplomaNumber, &d.University, &d.Degree, &d.Date)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"valid": false, "message": "diploma not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"valid": true, "diploma": d})
}

func JobStatus(c *gin.Context) {
	jobID := c.Param("job_id")

	var status, errMsg sql.NullString
	err := db.DB.QueryRow(
		`SELECT status, error FROM upload_jobs WHERE id = ?`, jobID,
	).Scan(&status, &errMsg)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"job_id": jobID, "status": status.String, "error": errMsg.String})
}

func ListDiplomas(c *gin.Context) {
	rows, err := db.DB.Query(
		`SELECT hash, full_name, diploma_number, university, degree, date, upload_job_id, created_at FROM diplomas`,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	defer rows.Close()

	var diplomas []models.Diploma
	for rows.Next() {
		var d models.Diploma
		rows.Scan(&d.Hash, &d.FullName, &d.DiplomaNumber, &d.University, &d.Degree, &d.Date, &d.UploadJobID, &d.CreatedAt)
		diplomas = append(diplomas, d)
	}

	c.JSON(http.StatusOK, diplomas)
}
