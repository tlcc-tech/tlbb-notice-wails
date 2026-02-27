package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const settingsDirName = "tlbb-notice-wails"
const settingsFileName = "settings.json"

type persistedSettings struct {
	ChannelKey string `json:"channelKey"`

	LastAnnounceKey   string `json:"lastAnnounceKey"`
	LastAnnounceTitle string `json:"lastAnnounceTitle"`

	LastActivityKey   string `json:"lastActivityKey"`
	LastActivityTitle string `json:"lastActivityTitle"`
	LastActivityLink  string `json:"lastActivityLink"`
	ActivitySeenKeys  []string `json:"activitySeenKeys,omitempty"`

	LastForumKey   string `json:"lastForumKey"`
	LastForumTitle string `json:"lastForumTitle"`
	LastForumLink  string `json:"lastForumLink"`

	UpdatedAt string `json:"updatedAt"`
}

func settingsFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", errors.New("无法获取用户配置目录")
	}
	return filepath.Join(dir, settingsDirName, settingsFileName), nil
}

func loadSettings() (persistedSettings, error) {
	path, err := settingsFilePath()
	if err != nil {
		return persistedSettings{}, err
	}

	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return persistedSettings{}, nil
		}
		return persistedSettings{}, err
	}

	var s persistedSettings
	if err := json.Unmarshal(b, &s); err != nil {
		return persistedSettings{}, err
	}
	return s, nil
}

func saveSettings(s persistedSettings) error {
	path, err := settingsFilePath()
	if err != nil {
		return err
	}

	s.UpdatedAt = time.Now().Format(time.RFC3339)

	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
