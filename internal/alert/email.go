package alert

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"

	"github.com/duggan/bewitch/internal/config"
)

// EmailNotifier delivers alerts via SMTP email.
type EmailNotifier struct {
	cfg         config.EmailDest
	logLocalMTA sync.Once
}

// use_mail_cmd under the hardened systemd unit.
//
// The packaged unit sets NoNewPrivileges (and RestrictSUIDSGID), so exec'ing
// mail(1) can't gain the setgid/setuid bits a local MTA's submission helper
// relies on: Postfix's sendmail hands off to setgid-postdrop, which then fails
// "mail_queue_enter: create file maildrop/…: Permission denied" and retries
// until our 10s timeout — every alert was silently dropped. Exim and classic
// sendmail are setuid/setgid the same way. Rather than weaken the sandbox, when
// NoNewPrivileges is in effect we hand the message to the local MTA over SMTP
// on loopback (an MTA that relays for local mail listens there by default).
// If nothing is listening, mail(1) is still tried, which covers relays like
// msmtp/ssmtp that need no privileges.
var (
	localMTAAddr = "127.0.0.1:25"
	noNewPrivsFn = noNewPrivs
)

var errNoLocalMTA = errors.New("no local MTA listening")

// notifyCmdTimeout bounds the mail(1) and command notifiers (a var for tests).
var notifyCmdTimeout = 10 * time.Second

// noNewPrivs reports whether this process runs with no_new_privs set.
func noNewPrivs() bool {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "NoNewPrivs:"); ok {
			return strings.TrimSpace(v) == "1"
		}
	}
	return false
}

func NewEmailNotifier(cfg config.EmailDest) *EmailNotifier {
	return &EmailNotifier{cfg: cfg}
}

// sanitizeHeader strips CR, LF, and other control characters so a value can be
// safely interpolated into an email Subject header without allowing header
// injection (RFC822 header smuggling via embedded newlines).
func sanitizeHeader(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func (n *EmailNotifier) Name() string   { return "email:" + strings.Join(n.cfg.To, ",") }
func (n *EmailNotifier) Method() string { return "email" }

func (n *EmailNotifier) Send(a *Alert) NotifyResult {
	result := NotifyResult{
		Method: "email",
		Dest:   strings.Join(n.cfg.To, ", "),
	}

	statusPrefix := ""
	statusLine := "FIRING"
	if a.Resolved {
		statusPrefix = "RESOLVED "
		statusLine = "RESOLVED"
	}
	// Sanitize the severity and rule name: both are caller-supplied (settable via
	// the alert-rule API / persisted rules) and a CR/LF here would inject extra
	// RFC822 headers (e.g. Bcc:) into the SMTP message or the mail-cmd subject.
	subject := fmt.Sprintf("[bewitch] %s%s: %s", statusPrefix, sanitizeHeader(a.Severity), sanitizeHeader(a.RuleName))
	body := fmt.Sprintf("%s\n\nRule: %s\nSeverity: %s\nStatus: %s\nTime: %s\n",
		a.Message,
		a.RuleName,
		a.Severity,
		statusLine,
		time.Now().UTC().Format(time.RFC3339),
	)

	if n.cfg.UseMailCmd {
		result.Body = body
		start := time.Now()
		if noNewPrivsFn() {
			err := n.sendLocalMTA(subject, body)
			if err == nil {
				n.logLocalMTA.Do(func() {
					log.Infof("email: use_mail_cmd: delivering via the local MTA at %s (NoNewPrivileges blocks the setgid/setuid helpers mail(1) relies on)", localMTAAddr)
				})
				result.Latency = time.Since(start)
				return result
			}
			if !errors.Is(err, errNoLocalMTA) {
				result.Latency = time.Since(start)
				result.Error = fmt.Sprintf("local MTA %s: %v", localMTAAddr, err)
				return result
			}
			// Nothing on loopback:25 — fall back to mail(1) (works for msmtp-style relays).
		}
		err := n.sendMailCmd(subject, body)
		result.Latency = time.Since(start)
		if err != nil {
			result.Error = fmt.Sprintf("mail cmd: %v", err)
		}
		return result
	}

	msg := fmt.Sprintf("Subject: %s\r\nFrom: %s\r\nTo: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		subject,
		n.cfg.From,
		strings.Join(n.cfg.To, ", "),
		body,
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

// sendLocalMTA submits the message to the local MTA over SMTP on loopback: no
// TLS or auth (it never leaves the host), relayed as local mail.
func (n *EmailNotifier) sendLocalMTA(subject, body string) error {
	conn, err := net.DialTimeout("tcp", localMTAAddr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("%w: %v", errNoLocalMTA, err)
	}
	c, err := smtp.NewClient(conn, "localhost")
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Close()

	from := n.cfg.From
	if from == "" {
		// mail(1) lets the MTA pick the sender; over SMTP we must name one.
		host, _ := os.Hostname()
		from = "bewitch@" + host
	}
	msg := fmt.Sprintf("Subject: %s\r\nFrom: %s\r\nTo: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		subject, from, strings.Join(n.cfg.To, ", "), body)
	return deliverMessage(c, from, n.cfg.To, msg)
}

func (n *EmailNotifier) sendMailCmd(subject, body string) error {
	ctx, cancel := context.WithTimeout(context.Background(), notifyCmdTimeout)
	defer cancel()

	args := []string{"-s", subject}
	if n.cfg.From != "" {
		args = append(args, "-r", n.cfg.From)
	}
	args = append(args, n.cfg.To...)

	cmd := exec.CommandContext(ctx, "mail", args...)
	contain(cmd)
	cmd.Stdin = bytes.NewReader([]byte(body))

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("mail command timed out (%s)", notifyCmdTimeout)
		}
		if stderr.Len() > 0 {
			return fmt.Errorf("%w: %s", err, stderr.String())
		}
		return err
	}
	return nil
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
	return deliverMessage(c, n.cfg.From, n.cfg.To, msg)
}

func deliverMessage(c *smtp.Client, from string, to []string, msg string) error {
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	for _, rcpt := range to {
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
