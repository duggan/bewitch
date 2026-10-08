package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/duggan/bewitch/internal/config"
)

// Responses shaped like the Proxmox VE API (every payload is wrapped in "data").
var pveFixtures = map[string]string{
	"/api2/json/nodes/pve/status": `{"data":{"cpu":0.0731,"loadavg":["0.42","0.30","0.25"],
		"memory":{"used":8589934592,"total":34359738368,"free":25769803776},
		"ksm":{"shared":1048576},"rootfs":{"used":10737418240,"total":107374182400},
		"pveversion":"pve-manager/9.1.4/abc","kversion":"Linux 6.17.4-2-pve"}}`,
	"/api2/json/cluster/resources?type=vm": `{"data":[
		{"vmid":100,"name":"homeassistant","type":"qemu","status":"running","cpu":0.05},
		{"vmid":101,"name":"plex","type":"lxc","status":"running","cpu":0.10,"lock":"backup"},
		{"vmid":102,"name":"old","type":"qemu","status":"stopped","cpu":0}]}`,
	"/api2/json/cluster/resources?type=storage": `{"data":[
		{"storage":"local","disk":5368709120,"maxdisk":107374182400},
		{"storage":"local-lvm","disk":214748364800,"maxdisk":858993459200}]}`,
	"/api2/json/cluster/status": `{"data":[{"type":"cluster","name":"lab","quorate":1},
		{"type":"node","name":"pve1","online":1},{"type":"node","name":"pve2","online":0}]}`,
}

func pveServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "PVEAPIToken=bewitch@pve!monitor=") {
			http.Error(w, "no token", http.StatusUnauthorized)
			return
		}
		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		body, ok := pveFixtures[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func loadPVEExample(t *testing.T) []config.CustomSourceConfig {
	t.Helper()
	srcs, _, err := config.LoadSources(nil, "../../examples/sources.d")
	if err != nil {
		t.Fatalf("shipped example fails validation: %v", err)
	}
	if len(srcs) != 4 {
		t.Fatalf("example defines %d sources, want 4", len(srcs))
	}
	return srcs
}

func serverFingerprint(srv *httptest.Server) string {
	sum := sha256.Sum256(srv.Certificate().Raw)
	return hex.EncodeToString(sum[:])
}

// TestPVEExampleEndToEnd drives the shipped examples/sources.d/proxmox.toml
// against PVE-shaped responses over TLS with a pinned fingerprint, checking the
// token header and every extracted value (incl. the gjson count/filter queries).
func TestPVEExampleEndToEnd(t *testing.T) {
	srv := pveServer(t)
	want := map[string]map[string]float64{
		"pve-node":    {"cpu": 0.0731, "load1": 0.42, "mem_used": 8589934592, "ksm_shared": 1048576, "rootfs_used": 10737418240},
		"pve-guests":  {"running": 2, "stopped": 1, "locked": 1},
		"pve-storage": {"local_lvm_used": 214748364800, "local_lvm_total": 858993459200},
		"pve-cluster": {"nodes_online": 1},
	}
	for _, cfg := range loadPVEExample(t) {
		cfg.BaseURL = srv.URL
		cfg.TLS.Fingerprint = serverFingerprint(srv)
		s, err := NewCustomSourceCollector(cfg, 30*time.Second).Collect()
		if err != nil {
			t.Fatalf("%s: %v", cfg.Name, err)
		}
		data := s.Data.(CustomSourceData)
		got := map[string]float64{}
		for _, m := range data.Metrics {
			got[m.Name] = m.Value
		}
		for name, v := range want[cfg.Name] {
			if got[name] != v {
				t.Errorf("%s.%s = %v, want %v", cfg.Name, name, got[name], v)
			}
		}
		if cfg.Name == "pve-cluster" {
			if len(data.Status) != 1 || data.Status[0].Value != "1" || data.Status[0].Badge != "ok" {
				t.Errorf("quorum status = %+v, want 1/ok", data.Status)
			}
		}
	}
}

func TestCustomSourceTLSModes(t *testing.T) {
	srv := pveServer(t)
	base := loadPVEExample(t)[0]
	base.BaseURL = srv.URL
	collect := func(tlsCfg config.CustomTLSConfig) error {
		cfg := base
		cfg.TLS = tlsCfg
		_, err := NewCustomSourceCollector(cfg, 30*time.Second).Collect()
		return err
	}

	// Wrong pin (the example's placeholder): refused, and the error names the
	// server's real fingerprint so the user can verify and pin it.
	err := collect(config.CustomTLSConfig{Fingerprint: strings.Repeat("00", 32)})
	if err == nil || !strings.Contains(err.Error(), "server presented sha256:"+serverFingerprint(srv)) {
		t.Errorf("wrong pin: err = %v, want mismatch naming the server fingerprint", err)
	}
	// Colon-separated upper-case pin (as Proxmox's UI shows it) is accepted.
	fp := strings.ToUpper(serverFingerprint(srv))
	var colon []string
	for i := 0; i < len(fp); i += 2 {
		colon = append(colon, fp[i:i+2])
	}
	if err := collect(config.CustomTLSConfig{Fingerprint: strings.Join(colon, ":")}); err != nil {
		t.Errorf("colon-format pin: %v", err)
	}
	// No [tls]: system roots reject the self-signed cert.
	if err := collect(config.CustomTLSConfig{}); err == nil {
		t.Error("self-signed cert accepted without any [tls] option")
	}
	// ca_file holding the server's certificate verifies the chain + hostname.
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o644)
	if err := collect(config.CustomTLSConfig{CAFile: caFile}); err != nil {
		t.Errorf("ca_file: %v", err)
	}
	if err := collect(config.CustomTLSConfig{InsecureSkipVerify: true}); err != nil {
		t.Errorf("insecure_skip_verify: %v", err)
	}
}
