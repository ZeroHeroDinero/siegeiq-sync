// security.go - read-only system security posture check.
//
// TRUST BOUNDARY: same as the rest of Sync. Every read here is a Windows
// registry value or a WMI class that a signed-in user can already read
// without an admin prompt - Secure Boot state, TPM presence, VBS/HVCI
// status, and the OS build number. Nothing here touches the game process,
// memory, or network traffic, and nothing here gates or blocks the
// replay-watching loop in main.go - a failed or "unknown" read just gets
// logged and reported, the same as any other diagnostic. This is a report,
// not a requirement.
//
// DEPENDENCY NOTE: this adds one dependency beyond the systray + x/sys pair
// documented at the top of main.go - github.com/StackExchange/wmi (and its
// own dependency, github.com/go-ole/go-ole), needed to query Win32_Tpm and
// Win32_DeviceGuard. There is no clean registry-only path to TPM spec
// version or Device Guard status, so this is the smallest addition that
// covers those two checks. The main.go header comment says "never weaken"
// the single-dependency claim, so that comment has been updated alongside
// this file rather than left stale - see main.go.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/StackExchange/wmi"
	"golang.org/x/sys/windows/registry"
)

const unknown = "unknown"

// securityPosture is the JSON shape returned by checkSecurityPosture and
// printed by -security-check. Fields that can genuinely fail to read (a
// missing registry key, a missing WMI namespace on an older Windows build)
// are typed as any so they can hold either a real value or the string
// "unknown" - a failed read is never guessed into a false.
type securityPosture struct {
	SecureBoot       any    `json:"secure_boot"`
	TPM20            any    `json:"tpm_2_0"`
	VBSRunning       any    `json:"vbs_running"`
	HVCIRunning      any    `json:"hvci_running"`
	IOMMU            string `json:"iommu"`
	WindowsBuild     any    `json:"windows_build"`
	Meets25H2OrLater any    `json:"meets_25h2_or_later"`
}

// checkSecurityPosture runs all five checks and returns the combined result.
// Each check is independent - one failing never stops the others from
// running, and none of them require elevation.
func checkSecurityPosture() securityPosture {
	vbs, hvci, iommu := checkDeviceGuard()
	build, meets := checkWindowsBuild()
	return securityPosture{
		SecureBoot:       checkSecureBoot(),
		TPM20:            checkTPM20(),
		VBSRunning:       vbs,
		HVCIRunning:      hvci,
		IOMMU:            iommu,
		WindowsBuild:     build,
		Meets25H2OrLater: meets,
	}
}

// runSecurityCheckOnce runs the check in the background and writes one
// summary line to sync.log. Called once from runSync() at launch (see
// main.go) - it never blocks the tray or the watch loop, and there is
// nothing for a caller to handle: a bad read shows up as "unknown" in the
// logged JSON, not an error return.
func runSecurityCheckOnce() {
	defer func() {
		if r := recover(); r != nil {
			// This check must never be able to take the whole app down -
			// a WMI/COM failure on some odd machine is a logged "unknown",
			// never a crash of the replay-watching loop.
			logf("security check: recovered from panic: %v", r)
		}
	}()

	b, err := json.Marshal(checkSecurityPosture())
	if err != nil {
		logf("security check: could not marshal result: %v", err)
		return
	}
	logf("security posture: %s", string(b))
}

// printSecurityCheckAndExit backs the -security-check CLI flag: run once,
// print the JSON to stdout, and exit. No tray, no watch loop.
func printSecurityCheckAndExit() {
	b, err := json.MarshalIndent(checkSecurityPosture(), "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "security check failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(b))
	os.Exit(0)
}

// checkSecureBoot reads
// HKLM\SYSTEM\CurrentControlSet\Control\SecureBoot\State\UEFISecureBootEnabled.
// Machines with no UEFI Secure Boot support (legacy BIOS, some VMs) simply
// don't have this key - that is reported as "unknown", not false, since the
// read never happened rather than confirming Secure Boot is off.
func checkSecureBoot() any {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Control\SecureBoot\State`, registry.QUERY_VALUE)
	if err != nil {
		return unknown
	}
	defer k.Close()

	val, _, err := k.GetIntegerValue("UEFISecureBootEnabled")
	if err != nil {
		return unknown
	}
	return val == 1
}

// win32Tpm mirrors the WMI class root\CIMV2\Security\MicrosoftTpm:Win32_Tpm,
// trimmed to the two fields this check needs.
type win32Tpm struct {
	IsEnabled_InitialValue bool
	SpecVersion            string
}

// checkTPM20 queries Win32_Tpm for a present, enabled TPM reporting spec
// version 2.0. The MicrosoftTpm namespace does not exist on a machine with
// no TPM (or no TPM driver loaded) - that query error is "unknown", not
// false, same reasoning as checkSecureBoot.
func checkTPM20() (result any) {
	defer func() {
		if r := recover(); r != nil {
			logf("security check: TPM query panicked, treating as unknown: %v", r)
			result = unknown
		}
	}()

	var dst []win32Tpm
	q := "SELECT IsEnabled_InitialValue, SpecVersion FROM Win32_Tpm"
	if err := wmi.QueryNamespace(q, &dst, `root\CIMV2\Security\MicrosoftTpm`); err != nil || len(dst) == 0 {
		return unknown
	}
	t := dst[0]
	return t.IsEnabled_InitialValue && strings.HasPrefix(t.SpecVersion, "2.0")
}

// win32DeviceGuard mirrors the WMI class
// root\Microsoft\Windows\DeviceGuard:Win32_DeviceGuard, trimmed to the two
// fields the VBS/HVCI checks need.
// COM/WMI surfaces these two properties as signed 32-bit integers (VT_I4),
// not unsigned - declaring them uint32 makes the wmi library panic trying to
// convert a signed reflect.Value with .Uint(). Confirmed on a live Windows
// 11 machine (build 26100): "panic: reflect: call of reflect.Value.Uint on
// int32 Value". int32 is what actually comes back; the values themselves
// (0/1/2 and a small array of small integers) are never negative in
// practice.
type win32DeviceGuard struct {
	VirtualizationBasedSecurityStatus int32
	SecurityServicesRunning           []int32
}

// checkDeviceGuard queries Win32_DeviceGuard once and derives VBS, HVCI, and
// the IOMMU inference from that single result, since all three read from the
// same WMI object. The DeviceGuard namespace does not exist on Windows
// builds that predate Device Guard, so a query failure here is reported as
// "unknown" across all three return values, never guessed as false.
//
// IOMMU has no direct WMI/registry signal, so it is inferred rather than
// measured: VBS requires an IOMMU to actually run, so "VBS running" implies
// "IOMMU present and active". That is why it comes back as the string
// "inferred_from_vbs:<value>" instead of a bare bool - so nothing reading
// the JSON mistakes it for an independent measurement.
func checkDeviceGuard() (vbsRunning any, hvciRunning any, iommu string) {
	defer func() {
		if r := recover(); r != nil {
			logf("security check: Device Guard query panicked, treating as unknown: %v", r)
			vbsRunning, hvciRunning, iommu = unknown, unknown, "inferred_from_vbs:unknown"
		}
	}()

	var dst []win32DeviceGuard
	q := "SELECT VirtualizationBasedSecurityStatus, SecurityServicesRunning FROM Win32_DeviceGuard"
	if err := wmi.QueryNamespace(q, &dst, `root\Microsoft\Windows\DeviceGuard`); err != nil || len(dst) == 0 {
		return unknown, unknown, "inferred_from_vbs:unknown"
	}

	d := dst[0]
	vbs := d.VirtualizationBasedSecurityStatus == 2

	hvci := false
	for _, svc := range d.SecurityServicesRunning {
		if svc == 2 {
			hvci = true
			break
		}
	}

	return vbs, hvci, fmt.Sprintf("inferred_from_vbs:%v", vbs)
}

// checkWindowsBuild reads CurrentBuildNumber and compares it numerically
// against 26200, the first build number in the 25H2 family. Comparing
// numerically instead of string-matching "25H2" means this keeps working,
// unchanged, on every future Windows version.
func checkWindowsBuild() (build any, meets any) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return unknown, unknown
	}
	defer k.Close()

	s, _, err := k.GetStringValue("CurrentBuildNumber")
	if err != nil {
		return unknown, unknown
	}

	n, err := strconv.Atoi(s)
	if err != nil {
		return unknown, unknown
	}

	return n, n >= 26200
}
