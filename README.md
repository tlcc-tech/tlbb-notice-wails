# tlbb-notice（Wails）

一个极简桌面小工具：监控天龙公告列表，发现新公告后自动打开链接并发微信推送；提供【开始监控】【结束监控】按钮与日志输出界面。

## 环境要求

- Go（建议使用 Homebrew 安装的新版本 Go）
- Node.js 18+
- Wails CLI v2

如果在国内网络环境，建议配置 Go 代理：

```bash
go env -w GOPROXY=https://goproxy.cn,direct GOSUMDB=off
```

## 开发运行

在项目根目录执行：

```bash
wails dev
```

## 构建产物

### macOS (.app)

```bash
wails build
```

产物默认在 `build/bin/` 下。

### macOS 可选：打包 .dmg

Wails 默认产物是 `.app`，如需 `.dmg`，可在拿到 `.app` 后再用第三方工具打包（例如 `create-dmg` 或 `dmgbuild`）。

### Windows (.exe)

建议在 Windows 机器上执行：

```powershell
wails build
```

（macOS 上直接交叉编译 Windows 通常需要额外工具链与配置，不作为默认流程。）

## 网络慢：推荐用 GitHub Actions 打包

本项目已提供工作流：.github/workflows/build.yml

用法：

- 推送一个 tag（例如 v1.0.0）到 GitHub，会自动在 Windows/macOS runner 上构建并上传产物
- 或在 GitHub 的 Actions 页面手动触发（workflow_dispatch）

## 体积尽量小的建议

- 前端使用 vanilla，避免引入大型 UI 框架
- 已移除模板自带外置字体引用，减少静态资源
- 可选：使用 UPX 压缩 Windows 可执行文件（可能触发部分安全软件误报，请自行评估）
