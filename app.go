package main

import (
	"context"
)

// App struct
type App struct {
	ctx context.Context

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
	a.startAutoUpdateCheck()
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

func (a *App) GetAppInfo() AppInfo {
	return AppInfo{Name: AppName, Author: AppAuthor, Version: AppVersion}
}
