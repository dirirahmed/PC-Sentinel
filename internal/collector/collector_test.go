package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/sensors"
)

func TestCounterRate(t *testing.T) {
	if got := counterRate(1000, 3000, 2); got != 1000 {
		t.Errorf("rate = %v, want 1000", got)
	}
	if got := counterRate(5000, 100, 2); got != 0 {
		t.Errorf("counter reset should yield 0, got %v", got)
	}
	if got := counterRate(0, 100, 0); got != 0 {
		t.Errorf("zero interval should yield 0, got %v", got)
	}
}

func TestBusyPercent(t *testing.T) {
	prev := cpu.TimesStat{User: 100, System: 50, Idle: 850}
	cur := cpu.TimesStat{User: 130, System: 70, Idle: 900} // 50 busy of 100 total
	if got := busyPercent(prev, cur); got != 50 {
		t.Errorf("busy = %v, want 50", got)
	}
	if got := busyPercent(cur, cur); got != 0 {
		t.Errorf("no elapsed time should give 0, got %v", got)
	}
	if got := busyPercent(cur, prev); got != 0 {
		t.Errorf("counters going backwards should give 0, got %v", got)
	}
}

func TestMemoryFromVirtual(t *testing.T) {
	m := memoryFromVirtual(&mem.VirtualMemoryStat{Total: 16 << 30, Available: 4 << 30})
	if m.UsedBytes != 12<<30 || m.UsedPercent != 75 {
		t.Errorf("got %+v", m)
	}
	// Defensive: inconsistent OS data must not underflow.
	if m := memoryFromVirtual(&mem.VirtualMemoryStat{Total: 1, Available: 2}); m.UsedBytes != 0 {
		t.Errorf("underflow: %+v", m)
	}
}

func TestAggregateEnginesMatchesTaskManagerMath(t *testing.T) {
	const luid = "luid_0x00000000_0x0000D1B5"
	values := map[string]float64{
		// Two processes share the 3D engine: summed to 55%.
		"pid_100_" + luid + "_phys_0_eng_0_engtype_3D":          40,
		"pid_200_" + luid + "_phys_0_eng_0_engtype_3D":          15,
		"pid_300_" + luid + "_phys_0_eng_2_engtype_VideoDecode": 12,
		"pid_300_" + luid + "_phys_0_eng_5_engtype_Copy":        3,
		// Second adapter.
		"pid_400_luid_0x00000000_0x0000AAAA_phys_0_eng_0_engtype_3D": 7,
		"garbage instance": 99,
	}
	got := aggregateEngines(values)
	a, ok := got["0x00000000_0x0000d1b5"]
	if !ok {
		t.Fatalf("adapter missing; got keys %v", got)
	}
	if a.usage != 55 {
		t.Errorf("usage = %v, want 55 (busiest engine)", a.usage)
	}
	if a.engines[0].Name != "3D" || a.engines[1].Name != "Video Decode" {
		t.Errorf("engines not sorted by load: %+v", a.engines)
	}
	if got["0x00000000_0x0000aaaa"].usage != 7 {
		t.Errorf("second adapter usage = %v", got["0x00000000_0x0000aaaa"].usage)
	}
	if len(got) != 2 {
		t.Errorf("unexpected adapters: %v", got)
	}
}

func TestAggregateEnginesClampsOverflow(t *testing.T) {
	values := map[string]float64{
		"pid_1_luid_0x0_0x1_phys_0_eng_0_engtype_3D": 80,
		"pid_2_luid_0x0_0x1_phys_0_eng_0_engtype_3D": 70,
	}
	if u := aggregateEngines(values)["0x0_0x1"].usage; u != 100 {
		t.Errorf("usage = %v, want clamped to 100", u)
	}
}

func TestAggregateAdapterMemory(t *testing.T) {
	got := aggregateAdapterMemory(map[string]float64{
		"luid_0x00000000_0x0000D1B5_phys_0": 2 << 30,
		"luid_0x00000000_0x0000D1B5_phys_1": 1 << 30,
		"not_a_luid":                        5,
	})
	if got["0x00000000_0x0000d1b5"] != 3<<30 || len(got) != 1 {
		t.Errorf("got %v", got)
	}
}

func TestFormatLUIDMatchesCounterNames(t *testing.T) {
	// DXGI gives the LUID as (HighPart, LowPart); PDH prints high first.
	if got := formatLUID(0, 0xD1B5); got != luidKey("0x00000000", "0x0000D1B5") {
		t.Errorf("formatLUID = %s", got)
	}
}

func TestParseNvidiaSMI(t *testing.T) {
	out := "NVIDIA GeForce RTX 3070, 61, 23, 1024, 8192\nNVIDIA T400, [N/A], 0, 10, 2048\nbad line\n"
	gpus := parseNvidiaSMI(out)
	if len(gpus) != 2 {
		t.Fatalf("got %d gpus", len(gpus))
	}
	if gpus[0].name != "NVIDIA GeForce RTX 3070" || *gpus[0].temperature != 61 || *gpus[0].memTotalMiB != 8192 {
		t.Errorf("first gpu = %+v", gpus[0])
	}
	if gpus[1].temperature != nil {
		t.Error("[N/A] must parse as unavailable")
	}
	if m := matchNvidia("nvidia t400", 2, gpus); m == nil || m.name != "NVIDIA T400" {
		t.Error("name match should be case-insensitive")
	}
	if m := matchNvidia("Something else", 2, gpus); m != nil {
		t.Error("ambiguous match must return nil")
	}
}

func TestPickCPUTemperature(t *testing.T) {
	temps := []sensors.TemperatureStat{
		{SensorKey: "acpitz", Temperature: 27.8},
		{SensorKey: "nvme_composite", Temperature: 40},
		{SensorKey: "coretemp_core_0", Temperature: 55},
		{SensorKey: "coretemp_package_id_0", Temperature: 58},
	}
	v, src, ok := pickCPUTemperature(temps)
	if !ok || v != 58 || src != "coretemp package" {
		t.Errorf("got %v %q %v", v, src, ok)
	}
	// Windows only exposes ACPI thermal zones; label them honestly.
	v, src, ok = pickCPUTemperature([]sensors.TemperatureStat{{SensorKey: `ACPI\ThermalZone\TZ00_0`, Temperature: 45}})
	if !ok || v != 45 || src == "" {
		t.Errorf("thermal zone: %v %q %v", v, src, ok)
	}
	if _, _, ok := pickCPUTemperature([]sensors.TemperatureStat{{SensorKey: "coretemp_package", Temperature: -273}}); ok {
		t.Error("implausible reading must be rejected")
	}
	if _, _, ok := pickCPUTemperature(nil); ok {
		t.Error("no sensors must report unavailable")
	}
}

func TestCPUTempReaderBacksOffAfterFailure(t *testing.T) {
	calls := 0
	r := &cpuTempReader{read: func(context.Context) ([]sensors.TemperatureStat, error) {
		calls++
		return nil, errors.New("access denied")
	}}
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		if v, _ := r.reading(ctx, t0.Add(time.Duration(i)*2*time.Second)); v != nil {
			t.Fatal("expected unavailable")
		}
	}
	if calls != 1 {
		t.Errorf("sensor queried %d times during back-off, want 1", calls)
	}
}

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestAggregateNetworkPerInterface(t *testing.T) {
	prev := map[string]net.IOCountersStat{
		"eth0": {Name: "eth0", BytesRecv: 1000, BytesSent: 500},
		"wlan": {Name: "wlan", BytesRecv: 9000, BytesSent: 9000},
	}
	cur := map[string]net.IOCountersStat{
		"eth0": {Name: "eth0", BytesRecv: 3000, BytesSent: 1500},
		"wlan": {Name: "wlan", BytesRecv: 100, BytesSent: 100}, // adapter reset
		"new0": {Name: "new0", BytesRecv: 50, BytesSent: 50},   // no baseline yet
	}
	s := aggregateNetwork(prev, cur, 2)
	if s.RxBytesPerSec != 1000 || s.TxBytesPerSec != 500 {
		t.Errorf("rates = %v/%v, want 1000/500", s.RxBytesPerSec, s.TxBytesPerSec)
	}
	if s.TotalRxBytes != 3150 || s.Interfaces != 3 {
		t.Errorf("totals = %+v", s)
	}
}

func TestIsLoopback(t *testing.T) {
	for name, want := range map[string]bool{"lo": true, "Loopback Pseudo-Interface 1": true, "Ethernet": false, "eth0": false} {
		if isLoopback(name) != want {
			t.Errorf("isLoopback(%q) != %v", name, want)
		}
	}
}

func TestFilterPartitions(t *testing.T) {
	parts := []disk.PartitionStat{
		{Mountpoint: "/", Device: "/dev/sda1", Fstype: "ext4", Opts: []string{"rw"}},
		{Mountpoint: "/home", Device: "/dev/sda1", Fstype: "ext4", Opts: []string{"rw"}}, // bind mount
		{Mountpoint: "/snap/core/1", Device: "/dev/loop0", Fstype: "squashfs", Opts: []string{"ro"}},
		{Mountpoint: "/media/cd", Device: "/dev/sr0", Fstype: "iso9660"},
		{Mountpoint: "", Device: "x"},
	}
	got := filterPartitions(parts)
	if len(got) != 1 || got[0].Mountpoint != "/" {
		t.Errorf("got %+v", got)
	}
}

func TestBuildProcessList(t *testing.T) {
	a := procKey{pid: 10, created: 1}
	recycled := procKey{pid: 11, created: 99}
	raw := []rawProcess{
		{key: a, name: "busy.exe", cpuSeconds: 12, hasCPU: true, rss: 512 << 20},
		{key: recycled, name: "new.exe", cpuSeconds: 1, hasCPU: true},
		{key: procKey{pid: 4}, name: "System", hasCPU: false},
	}
	prev := map[procKey]float64{a: 10, {pid: 11, created: 5}: 0.5}
	list := buildProcessList(raw, prev, 1, 4, 1<<30)
	if list[0].CPUPercent == nil || *list[0].CPUPercent != 50 { // 2 CPU-seconds in 1s over 4 cores
		t.Errorf("busy.exe cpu = %v", list[0].CPUPercent)
	}
	if list[0].MemoryPct != 50 {
		t.Errorf("memory pct = %v", list[0].MemoryPct)
	}
	if list[1].CPUPercent != nil {
		t.Error("a recycled PID must not inherit the old process's CPU baseline")
	}
	if list[2].CPUPercent != nil {
		t.Error("process without readable CPU times must report nil")
	}
}

func TestGuardRecoversPanics(t *testing.T) {
	err := guard(nil, "boom", func() error { panic("driver exploded") })
	if err == nil {
		t.Fatal("expected error from panic")
	}
}

// TestCollectOnThisMachine exercises the real collectors. Whatever hardware
// the test runs on, a snapshot must come back with sane values and a status
// entry per collector; missing sensors are reported, never fatal.
func TestCollectOnThisMachine(t *testing.T) {
	ctx := context.Background()
	c := New(ctx, nil)
	defer c.Close()
	c.Collect(ctx)
	time.Sleep(300 * time.Millisecond)
	s := c.Collect(ctx)

	if s.CPU.UsagePercent < 0 || s.CPU.UsagePercent > 100 {
		t.Errorf("cpu usage out of range: %v", s.CPU.UsagePercent)
	}
	if s.CPU.LogicalCores < 1 {
		t.Errorf("logical cores = %d", s.CPU.LogicalCores)
	}
	if s.Memory.TotalBytes == 0 || s.Memory.UsedPercent <= 0 || s.Memory.UsedPercent > 100 {
		t.Errorf("memory = %+v", s.Memory)
	}
	if !s.GPU.Available && s.GPU.Reason == "" {
		t.Error("unavailable GPU must explain why")
	}
	if s.GPU.Adapters == nil || s.Disks == nil {
		t.Error("slices must be non-nil so JSON renders [] not null")
	}
	names := map[string]bool{}
	for _, st := range s.Collectors {
		names[st.Name] = true
	}
	for _, n := range []string{"cpu", "memory", "disk", "network", "gpu", "uptime"} {
		if !names[n] {
			t.Errorf("missing collector status %q", n)
		}
	}
	if s.CollectionMillis <= 0 {
		t.Error("collection duration not recorded")
	}
}

func TestProcessSamplerOnThisMachine(t *testing.T) {
	s := NewProcessSampler()
	procs, err := s.Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) == 0 {
		t.Fatal("no processes listed")
	}
	withCPU := 0
	for _, p := range procs {
		if p.CPUPercent != nil {
			withCPU++
			if *p.CPUPercent < 0 || *p.CPUPercent > 100 {
				t.Errorf("%s cpu out of range: %v", p.Name, *p.CPUPercent)
			}
		}
	}
	if withCPU == 0 {
		t.Error("warm-up sample should give CPU values for at least some processes")
	}
	// A second call inside the TTL is served from cache.
	again, _ := s.Sample(context.Background())
	if &again[0] != &procs[0] {
		t.Error("expected cached result")
	}
}
