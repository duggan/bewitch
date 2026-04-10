package collector

import (
	"fmt"

	"github.com/prometheus/procfs"
)

// newProcFS creates a procfs.FS using the default /proc mount point.
func newProcFS() (procfs.FS, error) {
	fs, err := procfs.NewDefaultFS()
	if err != nil {
		return procfs.FS{}, fmt.Errorf("creating procfs: %w", err)
	}
	return fs, nil
}
