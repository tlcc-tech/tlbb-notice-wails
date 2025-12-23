package main

// 由构建时通过 -ldflags "-X main.AppVersion=..." 注入
// 本地未注入时显示 dev
var AppVersion = "dev"

const (
	AppName   = "怀旧天龙公告检测"
	AppAuthor = "怀旧天龙CC科技"
	UpdateRepoOwner = "tlcc-tech"
	UpdateRepoName  = "tlbb-notice-wails"
)

type AppInfo struct {
	Name    string `json:"name"`
	Author  string `json:"author"`
	Version string `json:"version"`
}
