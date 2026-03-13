package alert

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/ross/bewitch/internal/config"
)

// EmailNotifier delivers alerts via SMTP email.
type EmailNotifier struct {
	cfg config.EmailDest
}

func NewEmailNotifier(cfg config.EmailDest) *EmailNotifier {
	return &EmailNotifier{cfg: cfg}
}

func (n *EmailNotifier) Name() string   { return "email:" + strings.Join(n.cfg.To, ",") }
func (n *EmailNotifier) Method() string { return "email" }

func (n *EmailNotifier) Send(a *Alert) NotifyResult {
	result := NotifyResult{
		Method: "email",
		Dest:   strings.Join(n.cfg.To, ", "),
	}

	subject := fmt.Sprintf("[bewitch] %s: %s", a.Severity, a.RuleName)
	msg := fmt.Sprintf("Subject: %s\r\nFrom: %s\r\nTo: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\n\nRule: %s\nSeverity: %s\nTime: %s\n",
		subject,
		n.cfg.From,
		strings.Join(n.cfg.To, ", "),
		a.Message,
		a.RuleName,
		a.Severity,
		time.Now().UTC().Format(time.RFC3339),
	)
	result.Body = msg

	port := n.cfg.GetSMTPPort()
	addr := fmt.Sprintf("%s:%d", n.cfg.SMTPHost, port)

	start := time.Now()
	err := n.sendMail(addr, port, msg)
	result.Latency = time.Since(start)

	if err != nil {
		result.Error = fmt.Sprintf("smtp: %v", err)
	}
	return result
}

func (n *EmailNotifier) sendMail(addr string, port int, msg string) error {
	// Port 465 uses implicit TLS (connect with TLS immediately).
	// Other ports use plain connection, optionally upgrading via STARTTLS.
	if port == 465 {
		return n.sendMailImplicitTLS(addr, msg)
	}
	return n.sendMailStartTLS(addr, msg)
}

func (n *EmailNotifier) sendMailStartTLS(addr string, msg string) error {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	host := n.cfg.SMTPHost
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	if n.cfg.IsStartTLS() {
		tlsCfg := &tls.Config{ServerName: host}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if n.cfg.Username != "" {
		auth := smtp.PlainAuth("", n.cfg.Username, n.cfg.Password, host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	return n.deliverMessage(c, msg)
}

func (n *EmailNotifier) sendMailImplicitTLS(addr string, msg string) error {
	tlsCfg := &tls.Config{ServerName: n.cfg.SMTPHost}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}

	c, err := smtp.NewClient(conn, n.cfg.SMTPHost)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	if n.cfg.Username != "" {
		auth := smtp.PlainAuth("", n.cfg.Username, n.cfg.Password, n.cfg.SMTPHost)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
	}

	return n.deliverMessage(c, msg)
}

func (n *EmailNotifier) deliverMessage(c *smtp.Client, msg string) error {
	if err := c.Mail(n.cfg.From); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	for _, rcpt := range n.cfg.To {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt %s: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}
	return c.Quit()
}
