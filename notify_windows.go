//go:build windows

package main

import (
	"github.com/gen2brain/beeep"
	"github.com/vivaui/viva/app"
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

func trayIcon() app.StatusIcon {
	const size = 32
	rgba := make([]byte, size*size*4)
	set := func(x, y int, r, g, b byte) {
		if x < 0 || x >= size || y < 0 || y >= size {
			return
		}
		i := (y*size + x) * 4
		rgba[i], rgba[i+1], rgba[i+2], rgba[i+3] = r, g, b, 255
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)-15.5, float64(y)-15.5
			if dx*dx+dy*dy <= 15.5*15.5 {
				set(x, y, 37, 99, 235)
			}
		}
	}
	for y := 8; y < 25; y++ {
		for x := 11; x < 15; x++ {
			set(x, y, 255, 255, 255)
		}
	}
	for y := 8; y < 17; y++ {
		for x := 15; x < 23; x++ {
			if y < 11 || y >= 14 || x >= 20 {
				set(x, y, 255, 255, 255)
			}
		}
	}
	return app.StatusIcon{Size: size, RGBA: rgba}
}
