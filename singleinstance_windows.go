//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const instanceMutexName = "Local\\ParkingMonitorSingleInstance"

var (
	procCreateMutexW        = kernel32DLL.NewProc("CreateMutexW")
	procFindWindowW         = user32DLL.NewProc("FindWindowW")
	procShowWindow          = user32DLL.NewProc("ShowWindow")
	procIsIconic            = user32DLL.NewProc("IsIconic")
	procSetForegroundWindow = user32DLL.NewProc("SetForegroundWindow")

	singleInstanceMutex windows.Handle
)

func ensureSingleInstance() bool {
	name, err := syscall.UTF16PtrFromString(instanceMutexName)
	if err != nil {
		return true
	}
	handle, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return true
	}
	singleInstanceMutex = windows.Handle(handle)

	if errno, ok := callErr.(syscall.Errno); ok && errno == 183 {
		activateExistingWindow()
		return false
	}
	return true
}

func activateExistingWindow() {
	title, err := syscall.UTF16PtrFromString("停车时长监控")
	if err != nil {
		return
	}
	for i := 0; i < 20; i++ {
		hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
		if hwnd != 0 {
			if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
				procShowWindow.Call(hwnd, 9)
			} else {
				procShowWindow.Call(hwnd, 5)
			}
			procSetForegroundWindow.Call(hwnd)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}
