package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"diasoft-diploma-api/internal/database"
	"diasoft-diploma-api/internal/logger"
)

type AuthHandler struct {
	db        *database.DB
	jwtSecret string
	log       *logger.Logger
}

func NewAuthHandler(db *database.DB, secret string, log *logger.Logger) *AuthHandler {
	return &AuthHandler{db: db, jwtSecret: secret, log: log}
}

type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Role     string `json:"role" binding:"required,oneof=student hr"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role" binding:"omitempty"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		h.log.Error("Failed to hash password", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}

	ctx := context.Background()
	var userID int64
	err = h.db.QueryRow(ctx,
		"INSERT INTO users (email, password_hash, role) VALUES ($1, $2, $3) RETURNING id",
		req.Email, string(hash), req.Role,
	).Scan(&userID)
	if err != nil {
		h.log.Error("Failed to register user", "error", err)
		c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
		return
	}

	token, err := h.generateToken(userID, req.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}
	refreshToken, err := h.generateRefreshToken(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"token":         token,
		"refresh_token": refreshToken,
		"user_id":       userID,
		"role":          req.Role,
		"email":         req.Email,
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := context.Background()
	var userID int64
	var passwordHash, role string
	err := h.db.QueryRow(ctx,
		"SELECT id, password_hash, role FROM users WHERE email = $1",
		req.Email,
	).Scan(&userID, &passwordHash, &role)

	if err != nil || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	token, err := h.generateToken(userID, role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}
	refreshToken, err := h.generateRefreshToken(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":         token,
		"refresh_token": refreshToken,
		"user_id":       userID,
		"role":          role,
		"email":         req.Email,
	})
}

func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token, err := jwt.Parse(req.RefreshToken, func(token *jwt.Token) (interface{}, error) {
		return []byte(h.jwtSecret), nil
	})
	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid refresh token"})
		return
	}

	claims := token.Claims.(jwt.MapClaims)
	if claims["type"] != "refresh" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token type"})
		return
	}

	userIDFloat, ok := claims["user_id"].(float64)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
		return
	}
	userID := int64(userIDFloat)

	ctx := context.Background()
	var role string
	if err := h.db.QueryRow(ctx, "SELECT role FROM users WHERE id = $1", userID).Scan(&role); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	newToken, err := h.generateToken(userID, role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": newToken})
}

func (h *AuthHandler) generateToken(userID int64, role string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"role":    role,
		"exp":     time.Now().Add(15 * time.Minute).Unix(),
	})
	return token.SignedString([]byte(h.jwtSecret))
}

func (h *AuthHandler) generateRefreshToken(userID int64) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"type":    "refresh",
		"exp":     time.Now().Add(7 * 24 * time.Hour).Unix(),
	})
	return token.SignedString([]byte(h.jwtSecret))
}
