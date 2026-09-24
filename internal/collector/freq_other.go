//go:build !linux && !windows

package collector

func newFrequencyReader() frequencyReader { return nil }
