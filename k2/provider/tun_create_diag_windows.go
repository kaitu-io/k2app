//go:build windows

package provider

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows side of DIAG: wintun-create-fail — see tun_create_diag.go for why.
// Everything here goes through public SetupAPI / CfgMgr calls, deliberately
// not through sing-tun's internal wintun package (memory-loaded, unexported),
// so a sing-tun upgrade cannot silently break it.

func startTUNCreateDiag(name string) func(error) {
	return runTUNCreateDiag(name, enumerateWintunDevices, tunCreateSampleInterval, func() []any {
		return []any{"osBuild", windowsBuildString()}
	})
}

var (
	// GUID_DEVCLASS_NET {4d36e972-e325-11ce-bfc1-08002be10318}
	guidDevClassNet = windows.GUID{Data1: 0x4d36e972, Data2: 0xe325, Data3: 0x11ce,
		Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18}}

	// DEVPKEY_Wintun_Name, from wintun api/adapter.c.
	devpkeyWintunName = windows.DEVPROPKEY{
		FmtID: windows.DEVPROPGUID{Data1: 0x3361c968, Data2: 0x2f2e, Data3: 0x4660,
			Data4: [8]byte{0xb4, 0x7e, 0x69, 0x9c, 0xdc, 0x4c, 0x32, 0xb9}},
		PID: windows.DEVPROPID_FIRST_USABLE + 1,
	}

	// DEVPKEY_Device_ProblemStatus {4340a6c5-93fa-4706-972c-7b648008a5a7}, 12 —
	// the NTSTATUS wintun feeds to RtlNtStatusToDosError on the failure path.
	devpkeyProblemStatus = windows.DEVPROPKEY{
		FmtID: windows.DEVPROPGUID{Data1: 0x4340a6c5, Data2: 0x93fa, Data3: 0x4706,
			Data4: [8]byte{0x97, 0x2c, 0x7b, 0x64, 0x80, 0x08, 0xa5, 0xa7}},
		PID: 12,
	}

	// x/sys's SetupDiGetDeviceProperty decodes only strings; ProblemStatus is
	// DEVPROP_TYPE_NTSTATUS, so it is read raw.
	procSetupDiGetDevicePropertyW = windows.NewLazySystemDLL("setupapi.dll").NewProc("SetupDiGetDevicePropertyW")
)

// wintunEnumerator mirrors WINTUN_ENUMERATOR in wintun api/adapter.h:
// ROOT\Wintun on Windows 7 (6.1), SWD\Wintun on everything newer. Only the
// one wintun actually uses is queried — the other can make SetupAPI error,
// which would put permanent noise in sampleErr.
func wintunEnumerator() string {
	if v := windows.RtlGetVersion(); v.MajorVersion == 6 && v.MinorVersion == 1 {
		return `ROOT\Wintun`
	}
	return `SWD\Wintun`
}

// maxWintunEnum bounds one sample; phantom adapters can pile up.
const maxWintunEnum = 64

func enumerateWintunDevices() ([]wintunDevice, error) {
	enum := wintunEnumerator()
	// Flags 0 (not DIGCF_PRESENT), same as wintun itself: phantom adapters
	// are worth seeing too; Present marks the difference.
	set, err := windows.SetupDiGetClassDevsEx(&guidDevClassNet, enum, 0, 0, 0, "")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", enum, err)
	}
	defer set.Close()
	var out []wintunDevice
	for i := 0; i < maxWintunEnum; i++ {
		data, err := set.EnumDeviceInfo(i)
		if errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
			break
		}
		if err != nil {
			continue
		}
		out = append(out, readWintunDevice(set, data))
	}
	return out, nil
}

func readWintunDevice(set windows.DevInfo, data *windows.DevInfoData) wintunDevice {
	var d wintunDevice
	d.InstanceID, _ = set.DeviceInstanceID(data)
	if v, err := windows.SetupDiGetDeviceProperty(set, data, &devpkeyWintunName); err == nil {
		d.Name, _ = v.(string)
	}
	var status, problem uint32
	if windows.CM_Get_DevNode_Status(&status, &problem, data.DevInst, 0) == nil {
		d.Present = true
		d.Status = status
		d.ProblemCode = problem
		d.ProblemStatus = deviceProblemStatus(set, data)
	}
	return d
}

func deviceProblemStatus(set windows.DevInfo, data *windows.DevInfoData) uint32 {
	var propType windows.DEVPROPTYPE
	var value, required uint32
	r, _, _ := procSetupDiGetDevicePropertyW.Call(
		uintptr(set),
		uintptr(unsafe.Pointer(data)),
		uintptr(unsafe.Pointer(&devpkeyProblemStatus)),
		uintptr(unsafe.Pointer(&propType)),
		uintptr(unsafe.Pointer(&value)),
		unsafe.Sizeof(value),
		uintptr(unsafe.Pointer(&required)),
		0,
	)
	if r == 0 || propType != windows.DEVPROP_TYPE_NTSTATUS {
		return 0
	}
	return value
}

func windowsBuildString() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}
