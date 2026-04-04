package database

import (
	"context"

	"golang.org/x/crypto/bcrypt"

	"diasoft-diploma-api/internal/logger"
)

// EnsureAdmin создаёт первого администратора, если в БД ещё никого с role=admin и заданы ADMIN_EMAIL / ADMIN_PASSWORD.
func EnsureAdmin(ctx context.Context, db *DB, email, password string, log *logger.Logger) error {
	if email == "" || password == "" {
		return nil
	}
	var n int64
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx,
		`INSERT INTO users (email, password_hash, role) VALUES ($1, $2, 'admin')`,
		email, string(hash),
	)
	if err != nil {
		return err
	}
	if log != nil {
		log.Info("Created initial admin user from ADMIN_EMAIL", "email", email)
	}
	return nil
}
