package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	announceListURL = "http://tlhj.changyou.com/tlhj/newslist/announce/announce.shtml"
	activityJSONURL = "https://event.changyou.com/cycms/tlhj/banner/main1.json"
	minIntervalSec  = 300
	maxIntervalSec  = 600

	wechatPushURL = "http://push.ijingniu.cn/push"
)

type MonitorStatus struct {
	Running           bool   `json:"running"`
	LastTitle         string `json:"lastTitle"`
	LastActivityTitle string `json:"lastActivityTitle"`
	LastActivityLink  string `json:"lastActivityLink"`
	LastChecked       string `json:"lastChecked"`
}

type latestItem struct {
	Key   string
	Title string
	Link  string
}

type checker interface {
	Name() string
	PushHead() string
	FetchLatest(ctx context.Context, client *http.Client) (latestItem, error)
}

type announcementChecker struct{}

func (announcementChecker) Name() string    { return "公告" }
func (announcementChecker) PushHead() string { return "天龙发公告了" }

func (announcementChecker) FetchLatest(ctx context.Context, client *http.Client) (latestItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, announceListURL, nil)
	if err != nil {
		return latestItem{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return latestItem{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return latestItem{}, err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return latestItem{}, err
	}

	// 公告标题选择器（与原 JS 保持一致）
	sel := doc.Find(".news_list_sc .news_list li a .news_txt h6.textcont").First()
	if sel.Length() == 0 {
		return latestItem{}, nil
	}

	title := strings.TrimSpace(sel.Text())
	link := ""

	// 从 h6 往上找 a，取 href
	if a := sel.ParentsFiltered("a").First(); a.Length() > 0 {
		if href, ok := a.Attr("href"); ok {
			href = strings.TrimSpace(href)
			if href != "" {
				base, baseErr := url.Parse(announceListURL)
				ref, refErr := url.Parse(href)
				if baseErr == nil && refErr == nil {
					link = base.ResolveReference(ref).String()
				}
			}
		}
	}

	key := strings.TrimSpace(link)
	if key == "" {
		key = strings.TrimSpace(title)
	}
	return latestItem{Key: key, Title: title, Link: link}, nil
}

type activityChecker struct{}

func (activityChecker) Name() string    { return "活动" }
func (activityChecker) PushHead() string { return "天龙有新活动了" }

func (activityChecker) FetchLatest(ctx context.Context, client *http.Client) (latestItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, activityJSONURL, nil)
	if err != nil {
		return latestItem{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return latestItem{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return latestItem{}, errors.New("HTTP " + resp.Status + ": " + strings.TrimSpace(string(b)))
	}

	var items []struct {
		Title      string `json:"title"`
		HrefStatus int    `json:"href_status"`
		HrefURL    string `json:"href_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return latestItem{}, err
	}
	if len(items) == 0 {
		return latestItem{}, nil
	}

	first := items[0]
	_ = first.HrefStatus
	title := strings.TrimSpace(first.Title)
	link := strings.TrimSpace(first.HrefURL)
	key := strings.TrimSpace(link)
	if key == "" {
		key = strings.TrimSpace(title)
	}
	return latestItem{Key: key, Title: title, Link: link}, nil
}

type Monitor struct {
	mu sync.Mutex

	appCtx context.Context

	running bool
	cancel  context.CancelFunc

	channelKey   string
	lastKey      string
	lastTitle    string
	lastActKey   string
	lastActTitle string
	lastActLink  string
	lastChecked  time.Time

	httpClient *http.Client
	rng        *rand.Rand
}

func NewMonitor() *Monitor {
	return &Monitor{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (m *Monitor) Attach(appCtx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appCtx = appCtx

	// 读取本地持久化设置：ChannelKey + 上次已读公告/活动，用于跨重启去重与自动回填。
	if s, err := loadSettings(); err == nil {
		m.channelKey = strings.TrimSpace(s.ChannelKey)
		m.lastKey = strings.TrimSpace(s.LastAnnounceKey)
		m.lastTitle = strings.TrimSpace(s.LastAnnounceTitle)
		m.lastActKey = strings.TrimSpace(s.LastActivityKey)
		m.lastActTitle = strings.TrimSpace(s.LastActivityTitle)
		m.lastActLink = strings.TrimSpace(s.LastActivityLink)

		// 兼容旧数据：若只有 title 没有 key，则用 title 作为 key。
		if m.lastKey == "" {
			m.lastKey = m.lastTitle
		}
		if m.lastActKey == "" {
			m.lastActKey = m.lastActTitle
		}
	}
}

type AppSettings struct {
	ChannelKey string `json:"channelKey"`
}

func (m *Monitor) GetSettings() AppSettings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return AppSettings{ChannelKey: m.channelKey}
}

func (m *Monitor) Status() MonitorStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := MonitorStatus{
		Running:           m.running,
		LastTitle:         m.lastTitle,
		LastActivityTitle: m.lastActTitle,
		LastActivityLink:  m.lastActLink,
	}
	if !m.lastChecked.IsZero() {
		status.LastChecked = m.lastChecked.Format(time.RFC3339)
	}
	return status
}

func (m *Monitor) Start(channelKey string) error {
	channelKey = strings.TrimSpace(channelKey)

	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return errors.New("监控已在运行")
	}
	m.running = true
	m.channelKey = channelKey
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	appCtx := m.appCtx
	// 持久化 ChannelKey（允许为空，表示禁用推送）
	_ = saveSettings(persistedSettings{
		ChannelKey:         m.channelKey,
		LastAnnounceKey:    m.lastKey,
		LastAnnounceTitle:  m.lastTitle,
		LastActivityKey:    m.lastActKey,
		LastActivityTitle:  m.lastActTitle,
		LastActivityLink:   m.lastActLink,
	})
	m.mu.Unlock()

	m.emitLog(appCtx, "INFO", "监控已启动")
	if channelKey == "" {
		m.emitLog(appCtx, "WARN", "未填写 ChannelKey：将跳过微信推送，仅打开链接")
	}

	go func() {
		defer func() {
			m.mu.Lock()
			m.running = false
			m.cancel = nil
			m.mu.Unlock()
			m.emitLog(appCtx, "INFO", "监控已停止")
		}()

		for {
			if err := m.checkOnce(ctx, appCtx); err != nil {
				m.emitLog(appCtx, "ERROR", "检查失败: "+err.Error())
			}

			nextSec := m.randomIntervalSec()
			m.emitLog(appCtx, "INFO", "下次检查将在 "+(time.Duration(nextSec)*time.Second).String()+" 后")

			t := time.NewTimer(time.Duration(nextSec) * time.Second)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
	}()

	return nil
}

func (m *Monitor) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.running = false
	m.cancel = nil
	appCtx := m.appCtx
	m.mu.Unlock()

	if cancel != nil {
		m.emitLog(appCtx, "INFO", "收到停止请求")
		cancel()
	}
}

func (m *Monitor) randomIntervalSec() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	if maxIntervalSec <= minIntervalSec {
		return minIntervalSec
	}
	return m.rng.Intn(maxIntervalSec-minIntervalSec+1) + minIntervalSec
}

func (m *Monitor) persistSnapshot() {
	m.mu.Lock()
	s := persistedSettings{
		ChannelKey:         m.channelKey,
		LastAnnounceKey:    m.lastKey,
		LastAnnounceTitle:  m.lastTitle,
		LastActivityKey:    m.lastActKey,
		LastActivityTitle:  m.lastActTitle,
		LastActivityLink:   m.lastActLink,
	}
	m.mu.Unlock()
	_ = saveSettings(s)
}

func (m *Monitor) checkOnce(ctx context.Context, appCtx context.Context) error {
	checks := []checker{announcementChecker{}, activityChecker{}}

	now := time.Now()

	m.mu.Lock()
	m.lastChecked = now
	channelKey := m.channelKey
	prevAnnKey := m.lastKey
	prevActKey := m.lastActKey
	m.mu.Unlock()

	allFailed := true
	for _, c := range checks {
		item, err := c.FetchLatest(ctx, m.httpClient)
		if err != nil {
			m.emitLog(appCtx, "ERROR", c.Name()+"检查失败: "+err.Error())
			continue
		}
		allFailed = false
		if strings.TrimSpace(item.Key) == "" {
			m.emitLog(appCtx, "WARN", "未找到最新"+c.Name()+"标题")
			continue
		}

		switch c.Name() {
		case "公告":
			if strings.TrimSpace(prevAnnKey) == "" {
				m.mu.Lock()
				m.lastKey = item.Key
				m.lastTitle = item.Title
				m.mu.Unlock()
				m.persistSnapshot()
				m.emitLog(appCtx, "INFO", "已获取当前最新公告(基线): "+item.Title)
				prevAnnKey = item.Key
				continue
			}
			if item.Key == prevAnnKey {
				m.emitLog(appCtx, "INFO", "公告未发生变化: "+item.Title)
				continue
			}

			m.emitLog(appCtx, "INFO", "检测到新公告: "+item.Title)
			m.mu.Lock()
			m.lastKey = item.Key
			m.lastTitle = item.Title
			m.mu.Unlock()
			m.persistSnapshot()

			if strings.TrimSpace(item.Link) != "" {
				runtime.BrowserOpenURL(appCtx, item.Link)
				m.emitLog(appCtx, "INFO", "已打开公告链接: "+item.Link)
			} else {
				m.emitLog(appCtx, "WARN", "未解析到公告链接")
			}

			if strings.TrimSpace(channelKey) == "" {
				m.emitLog(appCtx, "INFO", "未配置 ChannelKey，已跳过微信推送")
			} else {
				if err := m.sendWechatPush(ctx, channelKey, c.PushHead(), item.Title); err != nil {
					m.emitLog(appCtx, "ERROR", "微信推送失败: "+err.Error())
				} else {
					m.emitLog(appCtx, "INFO", "微信推送发送成功")
				}
			}
			prevAnnKey = item.Key

		case "活动":
			if strings.TrimSpace(prevActKey) == "" {
				m.mu.Lock()
				m.lastActKey = item.Key
				m.lastActTitle = item.Title
				m.lastActLink = item.Link
				m.mu.Unlock()
				m.persistSnapshot()
				m.emitLog(appCtx, "INFO", "已获取当前最新活动(基线): "+item.Title)
				prevActKey = item.Key
				continue
			}
			if item.Key == prevActKey {
				m.emitLog(appCtx, "INFO", "活动未发生变化: "+item.Title)
				continue
			}

			m.emitLog(appCtx, "INFO", "检测到新活动: "+item.Title)
			m.mu.Lock()
			m.lastActKey = item.Key
			m.lastActTitle = item.Title
			m.lastActLink = item.Link
			m.mu.Unlock()
			m.persistSnapshot()

			if strings.TrimSpace(item.Link) != "" {
				runtime.BrowserOpenURL(appCtx, item.Link)
				m.emitLog(appCtx, "INFO", "已打开活动链接: "+item.Link)
			} else {
				m.emitLog(appCtx, "WARN", "未解析到活动链接")
			}

			if strings.TrimSpace(channelKey) == "" {
				m.emitLog(appCtx, "INFO", "未配置 ChannelKey，已跳过微信推送")
			} else {
				if err := m.sendWechatPush(ctx, channelKey, c.PushHead(), item.Title); err != nil {
					m.emitLog(appCtx, "ERROR", "微信推送失败: "+err.Error())
				} else {
					m.emitLog(appCtx, "INFO", "微信推送发送成功")
				}
			}
			prevActKey = item.Key
		}
	}

	if allFailed {
		return errors.New("公告与活动检查均失败")
	}
	return nil
}

type wechatPushPayload struct {
	ChannelKey string `json:"ChannelKey"`
	Head       string `json:"Head"`
	Body       string `json:"Body"`
}

func (m *Monitor) sendWechatPush(ctx context.Context, channelKey string, head string, body string) error {
	channelKey = strings.TrimSpace(channelKey)
	if channelKey == "" {
		return errors.New("ChannelKey 不能为空")
	}
	head = strings.TrimSpace(head)
	if head == "" {
		head = "消息通知"
	}
	body = strings.TrimSpace(body)

	payload := wechatPushPayload{ChannelKey: channelKey, Head: head, Body: body}

	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	pushClient := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wechatPushURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Host", "push.ijingniu.cn")

	resp, err := pushClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return errors.New("HTTP " + resp.Status + ": " + strings.TrimSpace(string(respBody)))
	}

	return nil
}

func (m *Monitor) emitLog(appCtx context.Context, level string, msg string) {
	if appCtx == nil {
		return
	}
	line := time.Now().Format("2006-01-02 15:04:05") + " [" + level + "] " + msg
	runtime.EventsEmit(appCtx, "log", line)
}
