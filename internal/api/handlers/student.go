package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (h *UserHandler) GetStudentProfile(c *gin.Context) {
	if c.GetString("role") != "student" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Доступно только студентам"})
		return
	}
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	var email string
	var pl, pf, pp, cuniv, cspec, cyear string
	var cnum sql.NullString
	var identAt sql.NullTime
	var dipID sql.NullInt64
	var dipNum sql.NullString
	var meta []byte
	var dipStatus sql.NullString

	err := h.db.QueryRow(ctx, `
		SELECT u.email,
			COALESCE(u.passport_last_name, ''),
			COALESCE(u.passport_first_name, ''),
			COALESCE(u.passport_patronymic, ''),
			u.claimed_diploma_number,
			COALESCE(u.claimed_university_full, ''),
			COALESCE(u.claimed_specialty, ''),
			COALESCE(u.claimed_graduation_year, ''),
			u.identity_verified_at,
			d.id,
			d.diploma_number,
			d.metadata,
			d.status
		FROM users u
		LEFT JOIN LATERAL (
			SELECT id, diploma_number, metadata, status
			FROM diplomas
			WHERE student_id = u.id
			ORDER BY created_at DESC
			LIMIT 1
		) d ON true
		WHERE u.id = $1`,
		userID,
	).Scan(&email, &pl, &pf, &pp, &cnum, &cuniv, &cspec, &cyear, &identAt, &dipID, &dipNum, &meta, &dipStatus)

	if err != nil {
		h.log.Error("Failed to get student profile", "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Profile not found"})
		return
	}

	fullFromPassport := strings.TrimSpace(strings.Join([]string{pl, pf, pp}, " "))

	var name, specialty, university, diplomaNumber string
	var year interface{}
	source := "profile"

	if dipNum.Valid && strings.TrimSpace(dipNum.String) != "" {
		source = "registry"
		diplomaNumber = strings.TrimSpace(dipNum.String)
		var m map[string]interface{}
		_ = json.Unmarshal(meta, &m)
		if m != nil {
			if v, ok := m["name"].(string); ok {
				name = v
			}
			if v, ok := m["specialty"].(string); ok {
				specialty = v
			}
			if v, ok := m["university"].(string); ok {
				university = v
			}
			if y, ok := m["year"]; ok && y != nil {
				year = y
			}
		}
	} else {
		name = fullFromPassport
		specialty = cspec
		university = cuniv
		if cnum.Valid {
			diplomaNumber = strings.TrimSpace(cnum.String)
		}
		if cyear != "" {
			year = cyear
		} else {
			year = 0
		}
	}

	out := gin.H{
		"name":           name,
		"specialty":      specialty,
		"university":     university,
		"diplomaNumber":  diplomaNumber,
		"year":           year,
		"source":         source,
		"diploma_status": nil,
	}
	if dipStatus.Valid {
		out["diploma_status"] = dipStatus.String
	}
	if dipID.Valid {
		out["linked_diploma_id"] = dipID.Int64
	} else {
		out["linked_diploma_id"] = nil
	}
	if identAt.Valid {
		out["identity_verified_at"] = identAt.Time.Format("2006-01-02T15:04:05Z07:00")
	} else {
		out["identity_verified_at"] = nil
	}

	c.JSON(http.StatusOK, out)
}
