package database

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	*pgxpool.Pool
}

func Connect(ctx context.Context, url string) (*DB, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}

	// Оптимизация пула соединений
	config.MaxConns = 50
	config.MinConns = 10
	config.MaxConnLifetime = 3600
	config.MaxConnIdleTime = 300

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}

	db := &DB{Pool: pool}
	if err := db.migrate(ctx); err != nil {
		return nil, err
	}

	return db, nil
}

func (db *DB) migrate(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id BIGSERIAL PRIMARY KEY,
		email VARCHAR(255) UNIQUE NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		role VARCHAR(50) NOT NULL DEFAULT 'student',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
	CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);

	CREATE TABLE IF NOT EXISTS diplomas (
		id BIGSERIAL PRIMARY KEY,
		student_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
		university_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
		diploma_number VARCHAR(100) UNIQUE NOT NULL,
		qr_code TEXT,
		status VARCHAR(50) DEFAULT 'pending',
		metadata JSONB DEFAULT '{}',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_diplomas_student ON diplomas(student_id);
	CREATE INDEX IF NOT EXISTS idx_diplomas_university ON diplomas(university_id);
	CREATE INDEX IF NOT EXISTS idx_diplomas_status ON diplomas(status);
	CREATE INDEX IF NOT EXISTS idx_diplomas_number ON diplomas(diploma_number);

	CREATE TABLE IF NOT EXISTS verification_logs (
		id BIGSERIAL PRIMARY KEY,
		diploma_id BIGINT REFERENCES diplomas(id) ON DELETE CASCADE,
		verifier_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
		action VARCHAR(50) NOT NULL,
		metadata JSONB DEFAULT '{}',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_logs_diploma ON verification_logs(diploma_id);
	CREATE INDEX IF NOT EXISTS idx_logs_created ON verification_logs(created_at);

	CREATE TABLE IF NOT EXISTS upload_jobs (
		id TEXT PRIMARY KEY,
		user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
		filename TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		error TEXT,
		progress INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_jobs_user ON upload_jobs(user_id);
	CREATE INDEX IF NOT EXISTS idx_jobs_status ON upload_jobs(status);

	CREATE TABLE IF NOT EXISTS university_applications (
		id BIGSERIAL PRIMARY KEY,
		email VARCHAR(255) NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		organization_name TEXT NOT NULL,
		notes TEXT,
		documents JSONB DEFAULT '[]',
		status VARCHAR(32) NOT NULL DEFAULT 'pending',
		reviewer_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
		review_note TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_uni_app_status ON university_applications(status);
	CREATE INDEX IF NOT EXISTS idx_uni_app_email ON university_applications(email);
	`
	if _, err := db.Exec(ctx, schema); err != nil {
		return err
	}
	alters := `
ALTER TABLE users ADD COLUMN IF NOT EXISTS passport_last_name TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS passport_first_name TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS passport_patronymic TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS claimed_diploma_number TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS claimed_university_full TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS claimed_specialty TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS claimed_graduation_year TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS identity_verified_at TIMESTAMPTZ;
`
	_, err := db.Exec(ctx, alters)
	return err
}
