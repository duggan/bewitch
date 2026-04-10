package format

import "fmt"

const (
	kb = 1024
	mb = kb * 1024
	gb = mb * 1024
	tb = gb * 1024
)

// Bytes formats a byte count as a compact human-readable string (e.g. "1.5G", "512M").
func Bytes(b uint64) string {
	switch {
	case b >= tb:
		return fmt.Sprintf("%.1fT", float64(b)/float64(tb))
	case b >= gb:
		return fmt.Sprintf("%.1fG", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1fM", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1fK", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// BytesLong formats a byte count as a human-readable string with spaced units
// (e.g. "1.5 GB", "512.0 MB").
func BytesLong(b int64) string {
	switch {
	case b >= int64(tb):
		return fmt.Sprintf("%.1f TB", float64(b)/float64(tb))
	case b >= int64(gb):
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= int64(mb):
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= int64(kb):
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
