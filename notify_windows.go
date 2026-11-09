//go:build windows

package main

import (
	"github.com/gen2brain/beeep"
	"golang.org/x/sys/windows"
)

var (
	user32DLL   = windows.NewLazySystemDLL("user32.dll")
	kernel32DLL = windows.NewLazySystemDLL("kernel32.dll")

	procMessageBeep             = user32DLL.NewProc("MessageBeep")
	procSetThreadExecutionState = kernel32DLL.NewProc("SetThreadExecutionState")
)

func playAlertSound() {
	procMessageBeep.Call(0xFFFFFFFF)
}

func sendSystemNotification(title, message string) error {
	return beeep.Notify(title, message, "")
}

func keepAwake(enable bool) {
	const (
		esContinuous     = 0x80000000
		esSystemRequired = 0x00000001
	)
	if enable {
		procSetThreadExecutionState.Call(esContinuous | esSystemRequired)
	} else {
		procSetThreadExecutionState.Call(esContinuous)
	}
}
