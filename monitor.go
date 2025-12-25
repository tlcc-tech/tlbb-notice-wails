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

type Monitor struct {
	mu sync.Mutex

	appCtx context.Context

	running bool
	cancel  context.CancelFunc

	channelKey   string
	lastTitle    string
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

func (m *Monitor) checkOnce(ctx context.Context, appCtx context.Context) error {
	announceTitle, announceLink, announceErr := m.fetchLatestAnnouncement(ctx)
	actTitle, actLink, actErr := m.fetchLatestActivity(ctx)

	now := time.Now()

	m.mu.Lock()
	m.lastChecked = now
	previousAnnounceTitle := m.lastTitle
	previousActTitle := m.lastActTitle
	channelKey := m.channelKey
	if previousAnnounceTitle == "" && announceTitle != "" {
		m.lastTitle = announceTitle
	}
	if previousActTitle == "" && actTitle != "" {
		m.lastActTitle = actTitle
		m.lastActLink = actLink
	}
	m.mu.Unlock()

	if announceErr != nil {
		m.emitLog(appCtx, "ERROR", "公告检查失败: "+announceErr.Error())
	}
	if actErr != nil {
		m.emitLog(appCtx, "ERROR", "活动检查失败: "+actErr.Error())
	}
	if announceErr != nil && actErr != nil {
		return errors.New("公告与活动检查均失败")
	}

	// 公告
	if announceTitle == "" {
		m.emitLog(appCtx, "WARN", "未找到最新公告标题")
	} else if previousAnnounceTitle == "" {
		m.emitLog(appCtx, "INFO", "已获取当前最新公告(基线): "+announceTitle)
	} else if announceTitle == previousAnnounceTitle {
		m.emitLog(appCtx, "INFO", "公告标题未发生变化: "+announceTitle)
	} else {
		m.emitLog(appCtx, "INFO", "检测到新公告: "+announceTitle)

		m.mu.Lock()
		m.lastTitle = announceTitle
		m.mu.Unlock()

		if announceLink != "" {
			runtime.BrowserOpenURL(appCtx, announceLink)
			m.emitLog(appCtx, "INFO", "已打开公告链接: "+announceLink)
		} else {
			m.emitLog(appCtx, "WARN", "未解析到公告链接")
		}

		if strings.TrimSpace(channelKey) == "" {
			m.emitLog(appCtx, "INFO", "未配置 ChannelKey，已跳过微信推送")
		} else {
			if err := m.sendWechatPush(ctx, channelKey, "天龙发公告了", announceTitle); err != nil {
				m.emitLog(appCtx, "ERROR", "微信推送失败: "+err.Error())
			} else {
				m.emitLog(appCtx, "INFO", "微信推送发送成功")
			}
		}
	}

	// 活动（主页 swiper_news 第一个链接）
	if actTitle == "" {
		m.emitLog(appCtx, "WARN", "未找到最新活动标题")
	} else if previousActTitle == "" {
		m.emitLog(appCtx, "INFO", "已获取当前最新活动(基线): "+actTitle)
	} else if actTitle == previousActTitle {
		m.emitLog(appCtx, "INFO", "活动标题未发生变化: "+actTitle)
	} else {
		m.emitLog(appCtx, "INFO", "检测到新活动: "+actTitle)

		m.mu.Lock()
		m.lastActTitle = actTitle
		m.lastActLink = actLink
		m.mu.Unlock()

		if actLink != "" {
			runtime.BrowserOpenURL(appCtx, actLink)
			m.emitLog(appCtx, "INFO", "已打开活动链接: "+actLink)
		} else {
			m.emitLog(appCtx, "WARN", "未解析到活动链接")
		}

		if strings.TrimSpace(channelKey) == "" {
			m.emitLog(appCtx, "INFO", "未配置 ChannelKey，已跳过微信推送")
		} else {
			if err := m.sendWechatPush(ctx, channelKey, "天龙有新活动了", actTitle); err != nil {
				m.emitLog(appCtx, "ERROR", "微信推送失败: "+err.Error())
			} else {
				m.emitLog(appCtx, "INFO", "微信推送发送成功")
			}
		}
	}

	return nil
}

func (m *Monitor) fetchLatestAnnouncement(ctx context.Context) (title string, link string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, announceListURL, nil)
	if err != nil {
		return "", "", err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}

	// 公告标题选择器（与原 JS 保持一致）
	sel := doc.Find(".news_list_sc .news_list li a .news_txt h6.textcont").First()
	if sel.Length() == 0 {
		return "", "", nil
	}

	title = strings.TrimSpace(sel.Text())

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

	return title, link, nil
}

func (m *Monitor) fetchLatestActivity(ctx context.Context) (title string, link string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, activityJSONURL, nil)
	if err != nil {
		return "", "", err
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", "", errors.New("HTTP " + resp.Status + ": " + strings.TrimSpace(string(b)))
	}

	var items []struct {
		Title      string `json:"title"`
		HrefStatus int    `json:"href_status"`
		HrefURL    string `json:"href_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return "", "", err
	}
	if len(items) == 0 {
		return "", "", nil
	}

	first := items[0]
	title = strings.TrimSpace(first.Title)
	link = strings.TrimSpace(first.HrefURL)
	_ = first.HrefStatus
	return title, link, nil
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

	payload := map[string]string{
		"ChannelKey": channelKey,
		"Head":       head,
		"Body":       body,
	}

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
