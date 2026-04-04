package hasher

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

// DiplomaData holds normalized fields used for hashing
type DiplomaData struct {
	FullName      string
	DiplomaNumber string
	University    string
	Degree        string
	Date          string
}

// Generate produces a deterministic SHA256 hex hash for a diploma.
// Formula: SHA256(full_name|diploma_number|university|date|SECRET_KEY)
// Degree is intentionally excluded — it may vary in wording but doesn't
// change the identity of the diploma. Adjust if your rules differ.
func Generate(d DiplomaData) string {
	secret := os.Getenv("DIPLOMA_SECRET_KEY")
	if secret == "" {
		secret = "changeme" // fallback for local dev only
	}

	parts := []string{
		normalize(d.FullName),
		normalize(d.DiplomaNumber),
		normalize(d.University),
		normalize(d.Date),
		secret,
	}

	raw := strings.Join(parts, "|")
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum)
}

// normalize trims and lowercases a field before hashing
// so "  Иванов Иван  " and "иванов иван" produce the same hash
func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
