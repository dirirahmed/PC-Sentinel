package analyzer

import (
	"testing"

	"github.com/dirirahmed/pc-sentinel/internal/models"
)

func TestSummarizeUsesPeakColumns(t *testing.T) {
	pts := []models.HistoryPoint{
		{CPU: 10, CPUMax: 80, Memory: 40, GPU: f(20), GPUMax: f(90)},
		{CPU: 30, CPUMax: 35, Memory: 60},
	}
	s := Summarize(pts)
	if s.CPU.Avg != 20 || s.CPU.Peak != 80 || s.CPU.Current != 30 {
		t.Errorf("cpu stat = %+v", s.CPU)
	}
	if s.Memory.Avg != 50 || s.Memory.Peak != 60 {
		t.Errorf("memory stat = %+v", s.Memory)
	}
	// GPU is only present in one point; averages must ignore the gap.
	if !s.GPU.Available || s.GPU.Avg != 20 || s.GPU.Peak != 90 {
		t.Errorf("gpu stat = %+v", s.GPU)
	}
	if s.CPUTemp.Available {
		t.Error("cpu temperature should be unavailable")
	}
}

func TestSummarizeEmpty(t *testing.T) {
	if s := Summarize(nil); s.CPU.Available {
		t.Error("empty series must report unavailable stats")
	}
}
