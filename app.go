package main

import (
	"context"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx context.Context

	allowQuit atomic.Bool

	monitor *Monitor
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{monitor: NewMonitor()}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.monitor.Attach(ctx)
	setupTray(a)
	a.startAutoUpdateCheck()
}

// beforeClose 用于拦截用户点击关闭按钮的行为。
// 若当前正在监控，则弹出前端确认框：最小化到托盘 / 退出软件 / 取消。
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if a.allowQuit.Load() {
		return false
	}
	if a.monitor != nil {
		status := a.monitor.Status()
		if status.Running {
			runtime.EventsEmit(ctx, "app:close-requested")
			return true
		}
	}
	return false
}

// QuitApp 由前端在用户选择“退出软件”时调用。
// 该方法会放行 OnBeforeClose 并退出程序。
func (a *App) QuitApp() {
	a.allowQuit.Store(true)
	trayQuit()
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

func (a *App) StartMonitoring(channelKey string) error {
	return a.monitor.Start(channelKey)
}

func (a *App) StopMonitoring() {
	a.monitor.Stop()
}

func (a *App) GetStatus() MonitorStatus {
	return a.monitor.Status()
}

func (a *App) GetSettings() AppSettings {
	return a.monitor.GetSettings()
}

func (a *App) GetAppInfo() AppInfo {
	return AppInfo{Name: AppName, Author: AppAuthor, Version: AppVersion}
}
