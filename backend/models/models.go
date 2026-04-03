package models

type UploadJob struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	Status    string `json:"status"` // pending | processing | done | failed
	Error     string `json:"error,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Diploma struct {
	Hash         string `json:"hash"`
	FullName     string `json:"full_name"`
	DiplomaNumber string `json:"diploma_number"`
	University   string `json:"university"`
	Degree       string `json:"degree"`
	Date         string `json:"date"`
	UploadJobID  string `json:"upload_job_id"`
	CreatedAt    string `json:"created_at"`
}
