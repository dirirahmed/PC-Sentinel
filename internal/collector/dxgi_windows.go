package collector

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DXGI adapter enumeration gives vendor-neutral adapter names, dedicated
// VRAM size and the LUID needed to join with PDH GPU counters. It is called
// through raw COM vtables to avoid a cgo or COM-library dependency.

var (
	moddxgi                = windows.NewLazySystemDLL("dxgi.dll")
	procCreateDXGIFactory1 = moddxgi.NewProc("CreateDXGIFactory1")

	iidIDXGIFactory1 = windows.GUID{Data1: 0x770aae78, Data2: 0xf26f, Data3: 0x4dba,
		Data4: [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}
)

const (
	// vtable slots: IUnknown(3) + IDXGIObject(4) + IDXGIFactory(5) -> EnumAdapters1.
	vtblRelease       = 2
	vtblEnumAdapters1 = 12
	// IUnknown(3) + IDXGIObject(4) + IDXGIAdapter(3) -> GetDesc1.
	vtblGetDesc1 = 10

	dxgiErrorNotFound       = 0x887A0002
	dxgiAdapterFlagRemote   = 0x1
	dxgiAdapterFlagSoftware = 0x2
)

type dxgiAdapterDesc1 struct {
	Description           [128]uint16
	VendorID              uint32
	DeviceID              uint32
	SubSysID              uint32
	Revision              uint32
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	LuidLow               uint32
	LuidHigh              int32
	Flags                 uint32
}

type dxgiAdapter struct {
	name          string
	vendorID      uint32
	dedicatedVRAM uint64
	luid          string
}

func comCall(obj unsafe.Pointer, slot uintptr, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

func enumerateDXGIAdapters() ([]dxgiAdapter, error) {
	if err := procCreateDXGIFactory1.Find(); err != nil {
		return nil, err
	}
	var factory unsafe.Pointer
	hr, _, _ := procCreateDXGIFactory1.Call(uintptr(unsafe.Pointer(&iidIDXGIFactory1)), uintptr(unsafe.Pointer(&factory)))
	if int32(hr) < 0 || factory == nil {
		return nil, fmt.Errorf("CreateDXGIFactory1 failed: HRESULT 0x%08X", uint32(hr))
	}
	defer comCall(factory, vtblRelease)

	var adapters []dxgiAdapter
	for i := uintptr(0); i < 16; i++ {
		var adapter unsafe.Pointer
		hr := comCall(factory, vtblEnumAdapters1, i, uintptr(unsafe.Pointer(&adapter)))
		if uint32(hr) == dxgiErrorNotFound {
			break
		}
		if int32(hr) < 0 || adapter == nil {
			return adapters, fmt.Errorf("EnumAdapters1(%d) failed: HRESULT 0x%08X", i, uint32(hr))
		}
		var desc dxgiAdapterDesc1
		hr = comCall(adapter, vtblGetDesc1, uintptr(unsafe.Pointer(&desc)))
		comCall(adapter, vtblRelease)
		if int32(hr) < 0 || desc.Flags&(dxgiAdapterFlagSoftware|dxgiAdapterFlagRemote) != 0 {
			continue
		}
		adapters = append(adapters, dxgiAdapter{
			name:          windows.UTF16ToString(desc.Description[:]),
			vendorID:      desc.VendorID,
			dedicatedVRAM: uint64(desc.DedicatedVideoMemory),
			luid:          formatLUID(desc.LuidHigh, desc.LuidLow),
		})
	}
	return adapters, nil
}
