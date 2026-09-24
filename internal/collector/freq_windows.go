package collector

import "sync"

const processorPerformanceCounter = `\Processor Information(_Total)\% Processor Performance`

// pdhFrequencyReader mirrors Task Manager's "Speed": the base clock scaled by
// the % Processor Performance counter, which exceeds 100 while boosting.
type pdhFrequencyReader struct {
	once  sync.Once
	query *pdhQuery
}

func newFrequencyReader() frequencyReader { return &pdhFrequencyReader{} }

func (r *pdhFrequencyReader) currentMHz(base *float64) *float64 {
	if base == nil {
		return nil
	}
	r.once.Do(func() {
		r.query, _ = openPDHQuery(processorPerformanceCounter)
	})
	if r.query == nil || r.query.collect() != nil {
		return nil
	}
	vals, err := r.query.values(processorPerformanceCounter)
	if err != nil {
		return nil
	}
	pct, ok := vals["_Total"]
	if !ok && len(vals) == 1 {
		for _, v := range vals {
			pct, ok = v, true
		}
	}
	if !ok || pct <= 0 {
		return nil
	}
	return ptr(*base * pct / 100)
}
