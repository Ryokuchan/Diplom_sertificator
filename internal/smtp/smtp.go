package smtp

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"os"
	"strings"
)

// SMTPService отправляет письма через SMTP.
// Если переменные окружения не заданы — сервис отключён (Enabled() == false).
type SMTPService struct {
	host, port, user, password, from string
	enabled                           bool
}

func NewSMTPService() *SMTPService {
	host := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	port := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	user := strings.TrimSpace(os.Getenv("SMTP_USER"))
	password := strings.TrimSpace(os.Getenv("SMTP_PASSWORD"))
	from := strings.TrimSpace(os.Getenv("SMTP_FROM"))

	if port == "" {
		port = "587"
	}
	enabled := host != "" && user != "" && password != "" && from != ""
	return &SMTPService{
		host:     host,
		port:     port,
		user:     user,
		password: password,
		from:     from,
		enabled:  enabled,
	}
}

func (s *SMTPService) Enabled() bool { return s.enabled }

// Send отправляет письмо. Поддерживает STARTTLS (587) и TLS (465).
func (s *SMTPService) Send(to, subject, body string) error {
	if !s.enabled {
		return fmt.Errorf("SMTP не настроен")
	}

	msg := buildMessage(s.from, to, subject, body)
	addr := s.host + ":" + s.port

	// Порт 465 — implicit TLS
	if s.port == "465" {
		return s.sendTLS(addr, to, msg)
	}

	// Порт 587 / 25 — STARTTLS через smtp.SendMail
	auth := smtp.PlainAuth("", s.user, s.password, s.host)
	return smtp.SendMail(addr, auth, s.from, []string{to}, msg)
}

func (s *SMTPService) sendTLS(addr, to string, msg []byte) error {
	tlsCfg := &tls.Config{ServerName: s.host}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Quit()

	auth := smtp.PlainAuth("", s.user, s.password, s.host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(s.from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	_, err = w.Write(msg)
	if err != nil {
		return err
	}
	return w.Close()
}

func buildMessage(from, to, subject, body string) []byte {
	return []byte(
		"From: DiplomaVerify <" + from + ">\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=UTF-8\r\n" +
			"\r\n" +
			body + "\r\n",
	)
}
