package worker

import (
	"testing"
)

// Тестируем парсинг CSV с запятой
func TestParseCSV_Comma(t *testing.T) {
	w := &Worker{}
	records, err := w.parseCSV("../../testdata/sample.csv")
	if err != nil {
		t.Fatalf("parseCSV failed: %v", err)
	}
	if len(records) != 4 {
		t.Errorf("expected 4 records, got %d", len(records))
	}
	if records[0]["full_name"] != "Иванов Иван Иванович" {
		t.Errorf("unexpected full_name: %q", records[0]["full_name"])
	}
}

// Тестируем парсинг CSV с точкой с запятой и русскими заголовками
func TestParseCSV_Semicolon(t *testing.T) {
	w := &Worker{}
	records, err := w.parseCSV("../../testdata/sample_semicolon.csv")
	if err != nil {
		t.Fatalf("parseCSV semicolon failed: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records, got %d", len(records))
	}
}

// Тестируем нормализацию русских заголовков
func TestNormalize_RussianHeaders(t *testing.T) {
	w := &Worker{}
	row := map[string]string{
		"фио":            "Тестов Тест",
		"номер диплома":  "АА-001",
		"вуз":            "КазНУ",
		"специальность":  "Информатика",
		"год":            "2023",
	}
	result := w.normalize(row)
	if result["full_name"] != "Тестов Тест" {
		t.Errorf("full_name not normalized: %q", result["full_name"])
	}
	if result["diploma_number"] != "АА-001" {
		t.Errorf("diploma_number not normalized: %q", result["diploma_number"])
	}
}

// Тестируем валидацию
func TestValidate(t *testing.T) {
	w := &Worker{}

	// Пустое ФИО
	errs := w.validate(map[string]string{"full_name": "", "diploma_number": "АА-001"}, 2)
	if len(errs) == 0 {
		t.Error("expected validation error for empty full_name")
	}

	// Пустой номер диплома
	errs = w.validate(map[string]string{"full_name": "Тест", "diploma_number": ""}, 3)
	if len(errs) == 0 {
		t.Error("expected validation error for empty diploma_number")
	}

	// Корректная запись
	errs = w.validate(map[string]string{
		"full_name":      "Иванов Иван",
		"diploma_number": "ДВС-123456",
		"date":           "2023-06-15",
	}, 4)
	if len(errs) != 0 {
		t.Errorf("unexpected validation errors: %v", errs)
	}
}

// Тестируем форматы дат
func TestIsValidDate(t *testing.T) {
	valid := []string{"2023", "2023-06-15", "15.06.2023", "06/15/2023", "June 2023"}
	for _, d := range valid {
		if !isValidDate(d) {
			t.Errorf("expected %q to be valid date", d)
		}
	}

	invalid := []string{"не-дата", "abc", "32.13.2023"}
	for _, d := range invalid {
		if isValidDate(d) {
			t.Errorf("expected %q to be invalid date", d)
		}
	}
}

// Тестируем генерацию хэша — детерминированность
func TestGenerateHash_Deterministic(t *testing.T) {
	w := &Worker{}
	d := map[string]string{
		"full_name":      "Иванов Иван",
		"diploma_number": "ДВС-001",
		"university":     "КазНУ",
		"date":           "2023",
	}
	h1 := w.generateHash(d)
	h2 := w.generateHash(d)
	if h1 != h2 {
		t.Error("hash is not deterministic")
	}
	if len(h1) != 64 {
		t.Errorf("expected SHA-256 hex (64 chars), got %d", len(h1))
	}
}

// Тестируем что пустые строки пропускаются
func TestRowsToMaps_SkipsEmptyRows(t *testing.T) {
	rows := [][]string{
		{"full_name", "diploma_number"},
		{"Иванов", "АА-001"},
		{"", ""},
		{"Петров", "АА-002"},
	}
	records := rowsToMaps(rows)
	if len(records) != 2 {
		t.Errorf("expected 2 non-empty records, got %d", len(records))
	}
}
