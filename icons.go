package main

import (
	"os/exec"

	"github.com/vivaui/viva/ui"
)

const githubRepoURL = "https://github.com/Miles1994/ParkingMonitor"

// githubIcon is the GitHub mark from Simple Icons (CC0), 24x24 view box.
var githubIcon = ui.Icon{
	ViewBox: 24,
	Commands: []ui.PathCommand{
		ui.MoveTo(12, 0.297),
		ui.CubicTo(5.37, 0.297, 0, 5.67, 0, 12.297),
		ui.CubicTo(0, 17.6, 3.438, 22.097, 8.205, 23.682),
		ui.CubicTo(8.805, 23.795, 9.025, 23.424, 9.025, 23.105),
		ui.CubicTo(9.025, 22.82, 9.015, 22.065, 9.01, 21.065),
		ui.CubicTo(5.672, 21.789, 4.968, 19.455, 4.968, 19.455),
		ui.CubicTo(4.422, 18.07, 3.633, 17.7, 3.633, 17.7),
		ui.CubicTo(2.546, 16.956, 3.717, 16.971, 3.717, 16.971),
		ui.CubicTo(4.922, 17.055, 5.555, 18.207, 5.555, 18.207),
		ui.CubicTo(6.625, 20.042, 8.364, 19.512, 9.05, 19.205),
		ui.CubicTo(9.158, 18.429, 9.467, 17.9, 9.81, 17.6),
		ui.CubicTo(7.145, 17.3, 4.344, 16.268, 4.344, 11.67),
		ui.CubicTo(4.344, 10.36, 4.809, 9.29, 5.579, 8.45),
		ui.CubicTo(5.444, 8.147, 5.039, 6.927, 5.684, 5.274),
		ui.CubicTo(5.684, 5.274, 6.689, 4.952, 8.984, 6.504),
		ui.CubicTo(9.944, 6.237, 10.964, 6.105, 11.984, 6.099),
		ui.CubicTo(13.004, 6.105, 14.024, 6.237, 14.984, 6.504),
		ui.CubicTo(17.264, 4.952, 18.269, 5.274, 18.269, 5.274),
		ui.CubicTo(18.914, 6.927, 18.509, 8.147, 18.389, 8.45),
		ui.CubicTo(19.154, 9.29, 19.619, 10.36, 19.619, 11.67),
		ui.CubicTo(19.619, 16.28, 16.814, 17.295, 14.144, 17.59),
		ui.CubicTo(14.564, 17.95, 14.954, 18.686, 14.954, 19.81),
		ui.CubicTo(14.954, 21.416, 14.939, 22.706, 14.939, 23.096),
		ui.CubicTo(14.939, 23.411, 15.149, 23.786, 15.764, 23.666),
		ui.CubicTo(20.565, 22.092, 24, 17.592, 24, 12.297),
		ui.CubicTo(24, 5.67, 18.627, 0.297, 12, 0.297),
		ui.ClosePath(),
	},
}

func openURL(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
