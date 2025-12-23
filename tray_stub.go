//go:build !windows

package main

// setupTray 在非 Windows 平台下不启用系统托盘。
func setupTray(_ *App) {}

// trayQuit 在非 Windows 平台下无需处理。
func trayQuit() {}
