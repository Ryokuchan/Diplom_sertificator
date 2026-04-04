package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"diasoft-diploma-api/internal/database"
	"diasoft-diploma-api/internal/logger"
	"diasoft-diploma-api/internal/studentverify"
)

type UserHandler struct {
	db    *database.DB
	redis *redis.Client
	log   *logger.Logger
}

func NewUserHandler(db *database.DB, rdb *redis.Client, log *logger.Logger) *UserHandler {
	return &UserHandler{db: db, redis: rdb, log: log}
}

func (h *UserHandler) GetProfile(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	var email, role string
	var createdAt time.Time
	var pl, pf, pp, cuniv, cspec, cyear string
	var cnum sql.NullString
	var identAt sql.NullTime
	err := h.db.QueryRow(ctx, `
		SELECT email, role, created_at,
			COALESCE(passport_last_name, ''),
			COALESCE(passport_first_name, ''),
			COALESCE(passport_patronymic, ''),
			claimed_diploma_number,
			COALESCE(claimed_university_full, ''),
			COALESCE(claimed_specialty, ''),
			COALESCE(claimed_graduation_year, ''),
			identity_verified_at
		FROM users WHERE id = $1`,
		userID,
	).Scan(&email, &role, &createdAt, &pl, &pf, &pp, &cnum, &cuniv, &cspec, &cyear, &identAt)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	out := gin.H{
		"id":                      userID,
		"email":                   email,
		"role":                    role,
		"created_at":              createdAt.Format(time.RFC3339),
		"passport_last_name":      pl,
		"passport_first_name":     pf,
		"passport_patronymic":     pp,
		"claimed_university_full": cuniv,
		"claimed_specialty":       cspec,
		"claimed_graduation_year": cyear,
	}
	if cnum.Valid && strings.TrimSpace(cnum.String) != "" {
		out["claimed_diploma_number"] = strings.TrimSpace(cnum.String)
	} else {
		out["claimed_diploma_number"] = nil
	}
	if identAt.Valid {
		out["identity_verified_at"] = identAt.Time.Format(time.RFC3339)
	} else {
		out["identity_verified_at"] = nil
	}

	c.JSON(http.StatusOK, out)
}

// ListUniversitiesForStudent — выпадающий список вузов при подаче заявки студентом.
func (h *UserHandler) ListUniversitiesForStudent(c *gin.Context) {
	if c.GetString("role") != "student" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Доступно только студентам"})
		return
	}
	ctx := c.Request.Context()
	rows, err := h.db.Query(ctx,
		`SELECT id, email FROM users WHERE role = 'university' ORDER BY email ASC LIMIT 500`,
	)
	if err != nil {
		h.log.Error("ListUniversitiesForStudent failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось загрузить список вузов"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			continue
		}
		out = append(out, gin.H{"id": id, "email": email})
	}
	if err := rows.Err(); err != nil {
		h.log.Error("ListUniversitiesForStudent rows", "error", err)
	}

	c.JSON(http.StatusOK, out)
}

func (h *UserHandler) UpdateProfile(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetInt64("user_id")

	var req struct {
		Email                 string `json:"email" binding:"omitempty,email"`
		PassportLastName      string `json:"passport_last_name"`
		PassportFirstName     string `json:"passport_first_name"`
		PassportPatronymic    string `json:"passport_patronymic"`
		ClaimedDiplomaNumber  string `json:"claimed_diploma_number"`
		ClaimedUniversityFull string `json:"claimed_university_full"`
		ClaimedSpecialty      string `json:"claimed_specialty"`
		ClaimedGraduationYear string `json:"claimed_graduation_year"`
		RegistryDiplomaID     int64  `json:"registry_diploma_id"` // id строки в реестре (из кабинета вуза), 0 = не указывать
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var role string
	if err := h.db.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, userID).Scan(&role); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	if role == "student" {
		pl := strings.TrimSpace(req.PassportLastName)
		pf := strings.TrimSpace(req.PassportFirstName)
		pp := strings.TrimSpace(req.PassportPatronymic)
		cnum := strings.TrimSpace(req.ClaimedDiplomaNumber)
		cuniv := strings.TrimSpace(req.ClaimedUniversityFull)
		cspec := strings.TrimSpace(req.ClaimedSpecialty)
		cyear := strings.TrimSpace(req.ClaimedGraduationYear)
		emailPatch := strings.TrimSpace(req.Email)

		var cnumArg, cyearArg interface{}
		if cnum != "" {
			cnumArg = cnum
		}
		if cyear != "" {
			cyearArg = cyear
		}

		_, err := h.db.Exec(ctx, `
			UPDATE users SET
				email = CASE WHEN $1 <> '' THEN $1 ELSE email END,
				passport_last_name = $2,
				passport_first_name = $3,
				passport_patronymic = $4,
				claimed_diploma_number = $5,
				claimed_university_full = $6,
				claimed_specialty = $7,
				claimed_graduation_year = $8,
				updated_at = NOW()
			WHERE id = $9`,
			emailPatch, pl, pf, pp, cnumArg, cuniv, cspec, cyearArg, userID,
		)
		if err != nil {
			h.log.Error("Failed to update student profile", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Не удалось сохранить профиль"})
			return
		}

		linked, dipID, err := studentverify.TryAutoLink(ctx, h.db, h.redis, userID, req.RegistryDiplomaID)
		if err != nil {
			h.log.Error("TryAutoLink", "error", err)
		}

		out := gin.H{"message": "Профиль сохранён"}
		if linked {
			out["auto_linked"] = true
			out["diploma_id"] = dipID
			out["detail"] = "Данные совпали с реестром вуза (загруженной таблицей). Диплом привязан к аккаунту."
		}
		c.JSON(http.StatusOK, out)
		return
	}

	if strings.TrimSpace(req.Email) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Укажите email"})
		return
	}

	_, err := h.db.Exec(ctx,
		"UPDATE users SET email = $1, updated_at = NOW() WHERE id = $2",
		strings.TrimSpace(req.Email), userID,
	)
	if err != nil {
		h.log.Error("Failed to update profile", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Profile updated"})
}
