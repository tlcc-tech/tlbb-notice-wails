#!/usr/bin/env bash
set -euo pipefail

# Release version
VERSION="1.0.9"

# Build macOS universal .app
wails build -platform darwin/universal -clean -ldflags "-X main.AppVersion=${VERSION}"

# Optional: create .dmg (requires: brew install create-dmg)
APP_NAME="怀旧天龙公告检测"
APP_PATH="build/bin/${APP_NAME}.app"
DMG_PATH="build/bin/${APP_NAME}.dmg"

if command -v create-dmg >/dev/null 2>&1; then
  rm -rf build/dmg
  mkdir -p build/dmg
  cp -R "${APP_PATH}" build/dmg/
  create-dmg --volname "${APP_NAME}" \
    --volicon "${APP_PATH}/Contents/Resources/iconfile.icns" \
    --app-drop-link 300 180 \
    "${DMG_PATH}" \
    build/dmg
  echo "DMG created: ${DMG_PATH}"
else
  echo "create-dmg not found; skipped dmg packaging. Install with: brew install create-dmg"
fi

echo "APP built: ${APP_PATH}"
