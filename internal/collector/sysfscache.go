package collector

import (
	"os"
	"strings"
	"time"
)

const sysfsRefreshInterval = 60 * time.Second

// sysfsCache tracks when sysfs paths were last discovered so collectors
// can periodically re-scan without doing it on every collection cycle.
type sysfsCache struct {
	lastDiscovery time.Time
}

// needsRefresh returns true if the cache has never been populated (count == 0)
// or if the refresh interval has elapsed since the last discovery.
func (c *sysfsCache) needsRefresh(count int) bool {
	return count == 0 || time.Since(c.lastDiscovery) > sysfsRefreshInterval
}

// markRefreshed records the current time as the last discovery time.
func (c *sysfsCache) markRefreshed() {
	c.lastDiscovery = time.Now()
}

// readString reads a sysfs file and returns its contents trimmed of whitespace.
func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// readStringFile reads a sysfs file and returns its raw contents as a string.
func readStringFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
