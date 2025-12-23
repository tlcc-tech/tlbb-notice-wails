package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

type releaseAsset struct {
	Name               string
	BrowserDownloadURL string
}

func (a *App) startAutoUpdateCheck() {
	// 每次启动检查一次，不阻塞 UI
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := a.checkAndUpdate(ctx); err != nil {
			a.emitLog("WARN", "更新检查失败: "+err.Error())
		}
	}()
}

func (a *App) checkAndUpdate(ctx context.Context) error {
	current := normalizeVersion(AppVersion)
	if current == "" || current == "dev" {
		a.emitLog("INFO", "当前为开发版，跳过自动更新")
		return nil
	}

	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return err
	}

	latest := normalizeVersion(rel.TagName)
	if latest == "" {
		return errors.New("无法解析远程版本")
	}

	cmp, err := compareSemver(latest, current)
	if err != nil {
		return err
	}
	if cmp <= 0 {
		a.emitLog("INFO", "当前已是最新版本: "+current)
		return nil
	}

	a.emitLog("INFO", fmt.Sprintf("发现新版本: %s -> %s，开始下载更新...", current, latest))

	// 目前优先实现 Windows 的自动下载并自更新
	if runtime.GOOS != "windows" {
		a.emitLog("INFO", "非 Windows 平台暂不自动安装更新，将打开下载页")
		if a.ctx != nil && rel.HTMLURL != "" {
			wailsRuntime.BrowserOpenURL(a.ctx, rel.HTMLURL)
		}
		return nil
	}

	asset, err := pickWindowsAsset(rel)
	if err != nil {
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exePath, _ = filepath.Abs(exePath)
	exeDir := filepath.Dir(exePath)

	newPath := filepath.Join(exeDir, ".update-new.exe")
	if err := downloadFile(ctx, asset.BrowserDownloadURL, newPath, func(percent int, downloaded int64, total int64) {
		if total > 0 {
			a.emitLog("INFO", fmt.Sprintf("下载进度：%d%%（%s/%s）", percent, humanBytes(downloaded), humanBytes(total)))
			return
		}
		a.emitLog("INFO", fmt.Sprintf("下载中：%s", humanBytes(downloaded)))
	}); err != nil {
		return err
	}

	a.emitLog("INFO", "更新已下载，准备替换并重启...")

	// PowerShell：等待当前进程退出 -> 覆盖 exe -> 重新启动
	pid := os.Getpid()
	script := fmt.Sprintf(`$pid=%d; $src=%q; $dst=%q; Wait-Process -Id $pid -ErrorAction SilentlyContinue; Start-Sleep -Milliseconds 300; Move-Item -Force $src $dst; Start-Process -FilePath $dst`, pid, newPath, exePath)
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-Command", script)
	if err := cmd.Start(); err != nil {
		return err
	}

	if a.ctx != nil {
		wailsRuntime.Quit(a.ctx)
	}
	return nil
}

func (a *App) emitLog(level string, msg string) {
	if a.ctx == nil {
		return
	}
	line := time.Now().Format("2006-01-02 15:04:05") + " [" + level + "] " + msg
	wailsRuntime.EventsEmit(a.ctx, "log", line)
}

func fetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", UpdateRepoOwner, UpdateRepoName)
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "tlbb-notice-updater")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func pickWindowsAsset(rel *githubRelease) (*releaseAsset, error) {
	for _, a := range rel.Assets {
		name := strings.ToLower(a.Name)
		if strings.HasSuffix(name, "windows-amd64.exe") {
			return &releaseAsset{Name: a.Name, BrowserDownloadURL: a.BrowserDownloadURL}, nil
		}
	}
	return nil, errors.New("未找到 windows-amd64.exe 更新包，请确认 Release 资产已上传")
}

func downloadFile(ctx context.Context, url string, dst string, onProgress func(percent int, downloaded int64, total int64)) error {
	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "tlbb-notice-updater")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("下载失败 %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	if total <= 0 {
		// GitHub 可能返回 chunked，无法得知总大小
		total = -1
	}

	var downloaded int64
	lastPercent := -1
	lastLogAt := time.Time{}
	lastLoggedBytes := int64(0)

	buf := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			downloaded += int64(n)
		}

		now := time.Now()
		if onProgress != nil {
			if total > 0 {
				percent := int(math.Floor(float64(downloaded) * 100 / float64(total)))
				if percent > 100 {
					percent = 100
				}
				// 节流：百分比变化 或 超过 2 秒未输出
				if percent != lastPercent && (lastLogAt.IsZero() || now.Sub(lastLogAt) >= 500*time.Millisecond) {
					onProgress(percent, downloaded, total)
					lastPercent = percent
					lastLogAt = now
				}
			} else {
				// 无总大小：每增加 1MB 或超过 2 秒输出一次
				if downloaded-lastLoggedBytes >= 1*1024*1024 || lastLogAt.IsZero() || now.Sub(lastLogAt) >= 2*time.Second {
					onProgress(-1, downloaded, -1)
					lastLoggedBytes = downloaded
					lastLogAt = now
				}
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	if onProgress != nil {
		if total > 0 {
			onProgress(100, downloaded, total)
		} else {
			onProgress(-1, downloaded, -1)
		}
	}
	return nil
}

func humanBytes(n int64) string {
	if n < 0 {
		return "未知"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := float64(unit), 0
	val := float64(n)
	for val >= div*unit && exp < 4 {
		div *= unit
		exp++
	}
	pre := []string{"KB", "MB", "GB", "TB", "PB"}[exp]
	return fmt.Sprintf("%.1f%s", val/div, pre)
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
}

func compareSemver(a string, b string) (int, error) {
	pa, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	pb, err := parseSemver(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		if pa[i] > pb[i] {
			return 1, nil
		}
		if pa[i] < pb[i] {
			return -1, nil
		}
	}
	return 0, nil
}

func parseSemver(v string) ([3]int, error) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		return out, fmt.Errorf("版本号格式不正确: %s", v)
	}
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return out, fmt.Errorf("版本号格式不正确: %s", v)
		}
		out[i] = n
	}
	return out, nil
}
