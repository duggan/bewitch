package alert

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/duggan/bewitch/internal/config"
)

// fakeMTA is a minimal SMTP server that records one message per connection.
// rejectRcpt makes it refuse recipients (a real MTA error, not "nothing listening").
func fakeMTA(t *testing.T, rejectRcpt bool) (addr string, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan string, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				w := func(s string) { c.Write([]byte(s + "\r\n")) }
				w("220 fake ESMTP")
				var msg strings.Builder
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
						w("250 fake")
					case strings.HasPrefix(cmd, "MAIL FROM"):
						msg.WriteString(strings.TrimSpace(line) + "\n")
						w("250 ok")
					case strings.HasPrefix(cmd, "RCPT TO"):
						if rejectRcpt {
							w("554 relay access denied")
							continue
						}
						w("250 ok")
					case cmd == "DATA":
						w("354 go")
						for {
							l, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if l == ".\r\n" {
								break
							}
							msg.WriteString(l)
						}
						got <- msg.String()
						w("250 queued")
					case cmd == "QUIT":
						w("221 bye")
						return
					default:
						w("250 ok")
					}
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), got
}

// fakeMailCmd puts a `mail` script on PATH that records that it ran.
func fakeMailCmd(t *testing.T) (marker string) {
	t.Helper()
	dir := t.TempDir()
	marker = filepath.Join(dir, "ran")
	script := "#!/bin/sh\ncat > /dev/null\necho \"$@\" > " + marker + "\n"
	if err := os.WriteFile(filepath.Join(dir, "mail"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return marker
}

func closedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func withLocalMTA(t *testing.T, addr string, nnp bool) {
	t.Helper()
	oldAddr, oldFn := localMTAAddr, noNewPrivsFn
	localMTAAddr = addr
	noNewPrivsFn = func() bool { return nnp }
	t.Cleanup(func() { localMTAAddr, noNewPrivsFn = oldAddr, oldFn })
}

func mailCmdNotifier() *EmailNotifier {
	return NewEmailNotifier(config.EmailDest{UseMailCmd: true, To: []string{"ops@example.com"}})
}

var testAlert = &Alert{RuleName: "disk-full", Severity: "critical", Message: "disk.used_pct 97 > 90"}

func TestMailCmdUnderNoNewPrivsUsesLocalMTA(t *testing.T) {
	addr, got := fakeMTA(t, false)
	withLocalMTA(t, addr, true)
	marker := fakeMailCmd(t)

	res := mailCmdNotifier().Send(testAlert)
	if res.Error != "" {
		t.Fatalf("send: %s", res.Error)
	}
	msg := <-got
	for _, want := range []string{"Subject: [bewitch] critical: disk-full", "To: ops@example.com", "disk.used_pct 97 > 90", "MAIL FROM:<bewitch@"} {
		if !strings.Contains(msg, want) {
			t.Errorf("delivered message missing %q:\n%s", want, msg)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("mail(1) also ran; local MTA delivery should replace it")
	}
}

func TestMailCmdUnderNoNewPrivsFallsBackWithoutMTA(t *testing.T) {
	withLocalMTA(t, closedAddr(t), true)
	marker := fakeMailCmd(t)

	if res := mailCmdNotifier().Send(testAlert); res.Error != "" {
		t.Fatalf("send: %s", res.Error)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("nothing listening on loopback:25 — mail(1) should have been used (msmtp-style relays)")
	}
}

func TestMailCmdWithoutNoNewPrivsUsesMailCmd(t *testing.T) {
	addr, got := fakeMTA(t, false)
	withLocalMTA(t, addr, false)
	marker := fakeMailCmd(t)

	if res := mailCmdNotifier().Send(testAlert); res.Error != "" {
		t.Fatalf("send: %s", res.Error)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("outside the sandbox use_mail_cmd must keep running mail(1)")
	}
	select {
	case m := <-got:
		t.Errorf("local MTA used outside the sandbox: %s", m)
	default:
	}
}

func TestMailCmdLocalMTARejectionIsAnError(t *testing.T) {
	addr, _ := fakeMTA(t, true)
	withLocalMTA(t, addr, true)
	marker := fakeMailCmd(t)

	res := mailCmdNotifier().Send(testAlert)
	if !strings.Contains(res.Error, "relay access denied") {
		t.Errorf("error = %q, want the MTA's rejection surfaced", res.Error)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a real MTA rejection must not silently fall back to mail(1)")
	}
}
