package studentverify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"diasoft-diploma-api/internal/database"
)

func normalizeSpaces(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func normalizeKey(s string) string {
	s = normalizeSpaces(s)
	s = strings.ReplaceAll(s, "ё", "е")
	s = strings.ReplaceAll(s, "Ё", "е")
	return strings.ToLower(s)
}

func passportFullName(last, first, patr string) string {
	return normalizeSpaces(strings.Join([]string{last, first, patr}, " "))
}

func stringsMatch(a, b string) bool {
	if normalizeSpaces(a) == "" || normalizeSpaces(b) == "" {
		return false
	}
	return normalizeKey(a) == normalizeKey(b)
}

// Учреждение: допускаем частичное совпадение (разный порядок слов / сокращения).
func institutionMatch(registry, claimed string) bool {
	cr, cc := normalizeKey(registry), normalizeKey(claimed)
	if cr == "" || cc == "" {
		return false
	}
	if cr == cc {
		return true
	}
	if len(cc) >= 8 && (strings.Contains(cr, cc) || strings.Contains(cc, cr)) {
		return true
	}
	return false
}

func specialtyMatch(registry, claimed string) bool {
	if stringsMatch(registry, claimed) {
		return true
	}
	cr, cc := normalizeKey(registry), normalizeKey(claimed)
	if cr == "" || cc == "" {
		return false
	}
	if len(cc) >= 6 && (strings.Contains(cr, cc) || strings.Contains(cc, cr)) {
		return true
	}
	return false
}

func metaYear(m map[string]interface{}) string {
	v, ok := m["year"]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return fmt.Sprintf("%.0f", x)
	case json.Number:
		return string(x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func yearMatch(metaYearStr, claimedYear string) bool {
	cy := strings.TrimSpace(claimedYear)
	if cy == "" {
		return true
	}
	my := strings.TrimSpace(metaYearStr)
	if my == "" {
		return true
	}
	// сравниваем по 4 цифрам года, если есть
	extract := func(s string) string {
		for i := 0; i <= len(s)-4; i++ {
			chunk := s[i : i+4]
			if _, err := strconv.Atoi(chunk); err == nil {
				return chunk
			}
		}
		return normalizeKey(s)
	}
	return extract(my) == extract(cy)
}

// TryAutoLink связывает студента с записью реестра ВУЗа (Excel), если ФИО, вуз, специальность и номер совпадают.
// registryDiplomaID > 0 — взять строку реестра по id (как в кабинете вуза); иначе поиск по номеру диплома.
func TryAutoLink(ctx context.Context, db *database.DB, rdb *redis.Client, userID int64, registryDiplomaID int64) (linked bool, diplomaID int64, err error) {
	var role string
	var last, first, pat, claimNum, claimUniv, claimSpec, claimYear string
	err = db.QueryRow(ctx, `
		SELECT role,
			COALESCE(passport_last_name, ''),
			COALESCE(passport_first_name, ''),
			COALESCE(passport_patronymic, ''),
			COALESCE(claimed_diploma_number, ''),
			COALESCE(claimed_university_full, ''),
			COALESCE(claimed_specialty, ''),
			COALESCE(claimed_graduation_year, '')
		FROM users WHERE id = $1
	`, userID).Scan(&role, &last, &first, &pat, &claimNum, &claimUniv, &claimSpec, &claimYear)
	if err != nil {
		return false, 0, err
	}
	if role != "student" {
		return false, 0, nil
	}

	num := normalizeSpaces(claimNum)
	if num == "" && registryDiplomaID <= 0 {
		return false, 0, nil
	}

	full := passportFullName(last, first, pat)
	if full == "" {
		return false, 0, nil
	}
	if normalizeSpaces(claimUniv) == "" || normalizeSpaces(claimSpec) == "" {
		return false, 0, nil
	}

	var id int64
	var metaBytes []byte

	if registryDiplomaID > 0 {
		var rowDiplomaNum string
		err = db.QueryRow(ctx, `
			SELECT id, metadata, TRIM(diploma_number) FROM diplomas
			WHERE id = $1 AND student_id IS NULL AND status = 'verified'
		`, registryDiplomaID).Scan(&id, &metaBytes, &rowDiplomaNum)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, 0, nil
			}
			return false, 0, err
		}
		if num != "" && normalizeSpaces(rowDiplomaNum) != num && strings.ReplaceAll(rowDiplomaNum, " ", "") != strings.ReplaceAll(num, " ", "") {
			return false, 0, nil
		}
	} else {
		err = db.QueryRow(ctx, `
			SELECT id, metadata FROM diplomas
			WHERE student_id IS NULL AND status = 'verified' AND TRIM(diploma_number) = $1
			LIMIT 1
		`, num).Scan(&id, &metaBytes)
		if errors.Is(err, pgx.ErrNoRows) {
			compact := strings.ReplaceAll(num, " ", "")
			if compact != "" && compact != num {
				err = db.QueryRow(ctx, `
					SELECT id, metadata FROM diplomas
					WHERE student_id IS NULL AND status = 'verified'
					  AND REPLACE(TRIM(diploma_number), ' ', '') = $1
					LIMIT 1
				`, compact).Scan(&id, &metaBytes)
			}
		}
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, 0, nil
			}
			return false, 0, err
		}
	}

	var meta map[string]interface{}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return false, 0, nil
	}
	regName, _ := meta["name"].(string)
	regUniv, _ := meta["university"].(string)
	regSpec, _ := meta["specialty"].(string)
	if !stringsMatch(regName, full) {
		return false, 0, nil
	}
	if !institutionMatch(regUniv, claimUniv) {
		return false, 0, nil
	}
	if !specialtyMatch(regSpec, claimSpec) {
		return false, 0, nil
	}
	if !yearMatch(metaYear(meta), claimYear) {
		return false, 0, nil
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE diplomas SET student_id = $1, updated_at = NOW()
		WHERE id = $2 AND student_id IS NULL AND status = 'verified'
	`, userID, id)
	if err != nil {
		return false, 0, err
	}
	if tag.RowsAffected() == 0 {
		return false, 0, nil
	}

	_, err = tx.Exec(ctx, `
		UPDATE users SET identity_verified_at = NOW(), updated_at = NOW() WHERE id = $1
	`, userID)
	if err != nil {
		return false, 0, err
	}

	_, _ = tx.Exec(ctx,
		`INSERT INTO verification_logs (diploma_id, verifier_id, action, metadata) VALUES ($1, NULL, $2, $3)`,
		id, "auto_registry_match", []byte(`{"source":"excel_registry"}`),
	)

	if err := tx.Commit(ctx); err != nil {
		return false, 0, err
	}

	if rdb != nil {
		_ = rdb.Del(ctx, "diploma:"+strconv.FormatInt(id, 10)).Err()
	}

	return true, id, nil
}
