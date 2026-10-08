//go:build !linux

package collector

import "errors"

type perfCounter struct{}

func openPerfCounter(perfRAPLEvent) (*perfCounter, error) {
	return nil, errors.New("perf events are Linux-only")
}

func (p *perfCounter) joules() (float64, error) { return 0, errors.New("perf events are Linux-only") }

func (p *perfCounter) close() {}
