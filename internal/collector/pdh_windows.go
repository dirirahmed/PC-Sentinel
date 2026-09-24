package collector

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Minimal wrapper over the Performance Data Helper API. PDH exposes the same
// counters Task Manager uses, including vendor-neutral GPU engine counters
// (Windows 10 1709+), so no vendor SDK is required.

var (
	modpdh                           = windows.NewLazySystemDLL("pdh.dll")
	procPdhOpenQueryW                = modpdh.NewProc("PdhOpenQueryW")
	procPdhAddEnglishCounterW        = modpdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQueryData          = modpdh.NewProc("PdhCollectQueryData")
	procPdhGetFormattedCounterArrayW = modpdh.NewProc("PdhGetFormattedCounterArrayW")
	procPdhCloseQuery                = modpdh.NewProc("PdhCloseQuery")
)

const (
	pdhFmtDouble   = 0x00000200
	pdhFmtNoCap100 = 0x00008000
	pdhMoreData    = 0x800007D2
	pdhNoData      = 0x800007D5
	pdhCstatusNew  = 0x00000001
)

// pdhFmtCounterValueItemDouble mirrors PDH_FMT_COUNTERVALUE_ITEM_W with a
// double payload. The union inside PDH_FMT_COUNTERVALUE is 8-byte aligned,
// hence the explicit padding after CStatus.
type pdhFmtCounterValueItemDouble struct {
	Name    *uint16
	CStatus uint32
	_       uint32
	Value   float64
}

type pdhQuery struct {
	handle   uintptr
	counters map[string]uintptr
}

// openPDHQuery adds every path it can. Counters that don't exist on this
// machine (e.g. GPU counters on old drivers) are skipped, not fatal.
func openPDHQuery(paths ...string) (*pdhQuery, error) {
	if err := procPdhOpenQueryW.Find(); err != nil {
		return nil, err
	}
	q := &pdhQuery{counters: map[string]uintptr{}}
	if r, _, _ := procPdhOpenQueryW.Call(0, 0, uintptr(unsafe.Pointer(&q.handle))); r != 0 {
		return nil, fmt.Errorf("PdhOpenQuery: 0x%08X", uint32(r))
	}
	for _, p := range paths {
		ptrPath, err := windows.UTF16PtrFromString(p)
		if err != nil {
			continue
		}
		var counter uintptr
		r, _, _ := procPdhAddEnglishCounterW.Call(q.handle, uintptr(unsafe.Pointer(ptrPath)), 0, uintptr(unsafe.Pointer(&counter)))
		if r == 0 {
			q.counters[p] = counter
		}
	}
	if len(q.counters) == 0 {
		q.close()
		return nil, fmt.Errorf("none of the requested performance counters are available")
	}
	// Rate counters need two samples; prime the first one now.
	procPdhCollectQueryData.Call(q.handle)
	return q, nil
}

func (q *pdhQuery) has(path string) bool {
	_, ok := q.counters[path]
	return ok
}

func (q *pdhQuery) collect() error {
	if r, _, _ := procPdhCollectQueryData.Call(q.handle); r != 0 {
		return fmt.Errorf("PdhCollectQueryData: 0x%08X", uint32(r))
	}
	return nil
}

// values returns instance name -> value for a (possibly wildcard) counter.
func (q *pdhQuery) values(path string) (map[string]float64, error) {
	counter, ok := q.counters[path]
	if !ok {
		return nil, fmt.Errorf("counter %s not available", path)
	}
	const format = pdhFmtDouble | pdhFmtNoCap100
	var size, count uint32
	r, _, _ := procPdhGetFormattedCounterArrayW.Call(counter, format, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if uint32(r) == pdhNoData {
		return map[string]float64{}, nil
	}
	if uint32(r) != pdhMoreData || size == 0 {
		return nil, fmt.Errorf("PdhGetFormattedCounterArray size query: 0x%08X", uint32(r))
	}
	// The buffer holds the item array followed by the instance name strings
	// the items point into; allocate it as uint64s to guarantee alignment.
	buf := make([]uint64, (size+7)/8)
	r, _, _ = procPdhGetFormattedCounterArrayW.Call(counter, format, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 {
		return nil, fmt.Errorf("PdhGetFormattedCounterArray: 0x%08X", uint32(r))
	}
	items := unsafe.Slice((*pdhFmtCounterValueItemDouble)(unsafe.Pointer(&buf[0])), count)
	out := make(map[string]float64, count)
	for _, it := range items {
		if it.CStatus > pdhCstatusNew || it.Name == nil {
			continue
		}
		out[windows.UTF16PtrToString(it.Name)] += it.Value
	}
	return out, nil
}

func (q *pdhQuery) close() {
	if q != nil && q.handle != 0 {
		procPdhCloseQuery.Call(q.handle)
		q.handle = 0
	}
}
