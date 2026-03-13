//go:build !linux

package collector

import "fmt"

func satSMARTInfo(devPath string) (*SMARTInfo, error) {
	return nil, fmt.Errorf("SAT passthrough not supported on this OS")
}
