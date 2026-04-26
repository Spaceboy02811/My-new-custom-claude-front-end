package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	ntdll            = syscall.NewLazyDLL("ntdll.dll")
	createProcessW   = kernel32.NewProc("CreateProcessW")
	virtualProtectEx = kernel32.NewProc("VirtualProtectEx")
	writeProcessMem  = kernel32.NewProc("WriteProcessMemory")
	resumeThread     = kernel32.NewProc("ResumeThread")
	waitForSingle    = kernel32.NewProc("WaitForSingleObject")
	closeHandle      = kernel32.NewProc("CloseHandle")
	getProcAddress   = kernel32.NewProc("GetProcAddress")
	getModuleHandle  = kernel32.NewProc("GetModuleHandleW")
	rtlGetVersion    = ntdll.NewProc("RtlGetVersion")
)

// x64 shellcode that replaces RtlGetVersion.
// On entry, RCX = pointer to RTL_OSVERSIONINFOW:
//
//	+0x00 dwOSVersionInfoSize (caller sets, we leave it)
//	+0x04 dwMajorVersion  -> 10
//	+0x08 dwMinorVersion  -> 0
//	+0x0C dwBuildNumber   -> 19041 (0x4A51)
//	+0x10 dwPlatformId    -> 2
//
// Returns STATUS_SUCCESS (0) in EAX.
var patch = []byte{
	0xC7, 0x41, 0x04, 0x0A, 0x00, 0x00, 0x00, // mov dword [rcx+4],  10
	0xC7, 0x41, 0x08, 0x00, 0x00, 0x00, 0x00, // mov dword [rcx+8],  0
	0xC7, 0x41, 0x0C, 0x51, 0x4A, 0x00, 0x00, // mov dword [rcx+12], 19041
	0xC7, 0x41, 0x10, 0x02, 0x00, 0x00, 0x00, // mov dword [rcx+16], 2
	0x33, 0xC0, // xor eax, eax  (STATUS_SUCCESS)
	0xC3,       // ret
}

type startupInfo struct {
	Cb              uint32
	_               *uint16
	Desktop         *uint16
	Title           *uint16
	X, Y            uint32
	XSize, YSize    uint32
	XCountChars     uint32
	YCountChars     uint32
	FillAttribute   uint32
	Flags           uint32
	ShowWindow      uint16
	_               uint16
	_               *byte
	StdInput        syscall.Handle
	StdOutput       syscall.Handle
	StdErr          syscall.Handle
}

type processInfo struct {
	Process   syscall.Handle
	Thread    syscall.Handle
	ProcessId uint32
	ThreadId  uint32
}

const (
	createSuspended      = 0x00000004
	pageExecuteReadWrite = 0x40
	infinite             = 0xFFFFFFFF
)

func main() {
	// Resolve installer path relative to this exe
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot find own path: %v\n", err)
		os.Exit(1)
	}
	installer := filepath.Join(filepath.Dir(self), "Claude Setup.exe")
	if _, err := os.Stat(installer); err != nil {
		fmt.Fprintf(os.Stderr, "Claude Setup.exe not found next to launcher: %v\n", err)
		os.Exit(1)
	}

	installerPtr, _ := syscall.UTF16PtrFromString(installer)

	si := startupInfo{Cb: uint32(unsafe.Sizeof(startupInfo{}))}
	pi := processInfo{}

	// Launch installer suspended so we can patch before it reads the OS version
	r, _, e := createProcessW.Call(
		uintptr(unsafe.Pointer(installerPtr)),
		0, 0, 0, 0,
		createSuspended,
		0, 0,
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if r == 0 {
		fmt.Fprintf(os.Stderr, "CreateProcess failed: %v\n", e)
		os.Exit(1)
	}

	fmt.Println("Installer started (suspended). Patching version check...")

	// ntdll is loaded at the same address in all processes within a session.
	// Get RtlGetVersion address from our own process.
	target := rtlGetVersion.Addr()
	if target == 0 {
		fmt.Fprintln(os.Stderr, "Could not resolve RtlGetVersion")
		resumeThread.Call(uintptr(pi.Thread))
		os.Exit(1)
	}

	// Make the target page writable in the child process
	var oldProt uint32
	virtualProtectEx.Call(
		uintptr(pi.Process),
		target,
		uintptr(len(patch)),
		pageExecuteReadWrite,
		uintptr(unsafe.Pointer(&oldProt)),
	)

	// Write our patch
	var written uintptr
	writeProcessMem.Call(
		uintptr(pi.Process),
		target,
		uintptr(unsafe.Pointer(&patch[0])),
		uintptr(len(patch)),
		uintptr(unsafe.Pointer(&written)),
	)

	// Restore original page protection
	virtualProtectEx.Call(
		uintptr(pi.Process),
		target,
		uintptr(len(patch)),
		uintptr(oldProt),
		uintptr(unsafe.Pointer(&oldProt)),
	)

	fmt.Printf("Patched %d bytes at 0x%X. Resuming installer...\n", written, target)

	// Let the installer run
	resumeThread.Call(uintptr(pi.Thread))

	// Wait for it to finish
	waitForSingle.Call(uintptr(pi.Process), infinite)

	closeHandle.Call(uintptr(pi.Process))
	closeHandle.Call(uintptr(pi.Thread))

	fmt.Println("Installer finished.")
}
