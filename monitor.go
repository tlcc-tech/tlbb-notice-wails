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
	forumListURL    = "https://bbs.tlhj.changyou.com/forum.php?mod=forumdisplay&fid=2"
	minIntervalSec  = 300
	maxIntervalSec  = 600

	xizhiDefaultHost = "xizhi.qqoq.net"
)

type MonitorStatus struct {
	Running           bool   `json:"running"`
	LastTitle         string `json:"lastTitle"`
	LastActivityTitle string `json:"lastActivityTitle"`
	LastActivityLink  string `json:"lastActivityLink"`
	LastForumTitle    string `json:"lastForumTitle"`
	LastForumLink     string `json:"lastForumLink"`
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

func (announcementChecker) Name() string     { return "公告" }
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

func (activityChecker) Name() string     { return "活动" }
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

func (activityChecker) FetchAll(ctx context.Context, client *http.Client) ([]latestItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, activityJSONURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, errors.New("HTTP " + resp.Status + ": " + strings.TrimSpace(string(b)))
	}

	var items []struct {
		Title      string `json:"title"`
		HrefStatus int    `json:"href_status"`
		HrefURL    string `json:"href_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}

	out := make([]latestItem, 0, len(items))
	for _, it := range items {
		_ = it.HrefStatus
		title := strings.TrimSpace(it.Title)
		link := strings.TrimSpace(it.HrefURL)
		key := strings.TrimSpace(link)
		if key == "" {
			key = strings.TrimSpace(title)
		}
		if key == "" {
			continue
		}
		out = append(out, latestItem{Key: key, Title: title, Link: link})
	}
	return out, nil
}

type forumChecker struct{}

func (forumChecker) Name() string     { return "论坛" }
func (forumChecker) PushHead() string { return "天龙论坛有新帖了" }

func (forumChecker) FetchLatest(ctx context.Context, client *http.Client) (latestItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, forumListURL, nil)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return latestItem{}, err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return latestItem{}, err
	}

	base, _ := url.Parse(forumListURL)

	resolveHref := func(href string) string {
		href = strings.TrimSpace(href)
		if href == "" {
			return ""
		}
		if base == nil {
			return href
		}
		ref, parseErr := url.Parse(href)
		if parseErr != nil {
			return ""
		}
		return base.ResolveReference(ref).String()
	}

	pick := func(sel *goquery.Selection) latestItem {
		title := strings.TrimSpace(sel.Text())
		href, _ := sel.Attr("href")
		link := resolveHref(href)
		key := strings.TrimSpace(link)
		if key == "" {
			key = strings.TrimSpace(title)
		}
		return latestItem{Key: key, Title: title, Link: link}
	}

	findTitleAnchor := func(row *goquery.Selection) *goquery.Selection {
		anchorSelectors := []string{
			"th a.s.xst[href*='mod=viewthread']",
			"th a.s.xst[href*='thread-']",
			"th a.xst[href*='mod=viewthread']",
			"th a.xst[href*='thread-']",
		}
		for _, sel := range anchorSelectors {
			a := row.Find(sel).First()
			if a.Length() > 0 {
				return a
			}
		}
		return nil
	}

	rowSelectors := []string{
		"#threadlisttableid > tbody[id^='stickthread_']",
		"#threadlisttableid > tbody[id^='normalthread_']",
	}

	for _, rowSel := range rowSelectors {
		var found latestItem
		doc.Find(rowSel).EachWithBreak(func(_ int, row *goquery.Selection) bool {
			a := findTitleAnchor(row)
			if a == nil || a.Length() == 0 {
				return true
			}
			item := pick(a)
			if strings.TrimSpace(item.Key) == "" || strings.TrimSpace(item.Title) == "" {
				return true
			}
			found = item
			return false
		})
		if strings.TrimSpace(found.Key) != "" {
			return found, nil
		}
	}

	// 兜底：只在 threadlisttableid 区域取标题，避免抓到页面其他 viewthread 链接。
	fallback := doc.Find("#threadlisttableid th a.s.xst[href*='mod=viewthread'], #threadlisttableid th a.s.xst[href*='thread-'], #threadlisttableid th a.xst[href*='mod=viewthread'], #threadlisttableid th a.xst[href*='thread-']").First()
	if fallback.Length() > 0 {
		item := pick(fallback)
		if strings.TrimSpace(item.Key) != "" {
			return item, nil
		}
	}

	return latestItem{}, nil
}

type Monitor struct {
	mu sync.Mutex

	appCtx context.Context

	running bool
	cancel  context.CancelFunc

	channelKey     string
	lastKey        string
	lastTitle      string
	lastActKey     string
	lastActTitle   string
	lastActLink    string
	lastForumKey   string
	lastForumTitle string
	lastForumLink  string
	actSeenKeys    []string
	lastChecked    time.Time

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
		m.lastForumKey = strings.TrimSpace(s.LastForumKey)
		m.lastForumTitle = strings.TrimSpace(s.LastForumTitle)
		m.lastForumLink = strings.TrimSpace(s.LastForumLink)
		m.actSeenKeys = append([]string(nil), s.ActivitySeenKeys...)

		// 兼容旧数据：若只有 title 没有 key，则用 title 作为 key。
		if m.lastKey == "" {
			m.lastKey = m.lastTitle
		}
		if m.lastActKey == "" {
			m.lastActKey = m.lastActTitle
		}
		if m.lastForumKey == "" {
			m.lastForumKey = m.lastForumTitle
		}
		if len(m.actSeenKeys) == 0 && m.lastActKey != "" {
			m.actSeenKeys = []string{m.lastActKey}
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
		LastForumTitle:    m.lastForumTitle,
		LastForumLink:     m.lastForumLink,
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
		ChannelKey:        m.channelKey,
		LastAnnounceKey:   m.lastKey,
		LastAnnounceTitle: m.lastTitle,
		LastActivityKey:   m.lastActKey,
		LastActivityTitle: m.lastActTitle,
		LastActivityLink:  m.lastActLink,
		LastForumKey:      m.lastForumKey,
		LastForumTitle:    m.lastForumTitle,
		LastForumLink:     m.lastForumLink,
		ActivitySeenKeys:  append([]string(nil), m.actSeenKeys...),
	})
	m.mu.Unlock()

	m.emitLog(appCtx, "INFO", "监控已启动")
	if channelKey == "" {
		m.emitLog(appCtx, "WARN", "未填写推送链接/Key：将跳过微信推送，仅打开链接")
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
		ChannelKey:        m.channelKey,
		LastAnnounceKey:   m.lastKey,
		LastAnnounceTitle: m.lastTitle,
		LastActivityKey:   m.lastActKey,
		LastActivityTitle: m.lastActTitle,
		LastActivityLink:  m.lastActLink,
		LastForumKey:      m.lastForumKey,
		LastForumTitle:    m.lastForumTitle,
		LastForumLink:     m.lastForumLink,
		ActivitySeenKeys:  append([]string(nil), m.actSeenKeys...),
	}
	m.mu.Unlock()
	_ = saveSettings(s)
}

type activityAllFetcher interface {
	FetchAll(ctx context.Context, client *http.Client) ([]latestItem, error)
}

func (m *Monitor) checkOnce(ctx context.Context, appCtx context.Context) error {
	checks := []checker{announcementChecker{}, activityChecker{}, forumChecker{}}

	now := time.Now()

	m.mu.Lock()
	m.lastChecked = now
	channelKey := m.channelKey
	prevAnnKey := m.lastKey
	prevActKey := m.lastActKey
	prevForumKey := m.lastForumKey
	seenAct := append([]string(nil), m.actSeenKeys...)
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
				m.emitLog(appCtx, "INFO", "未配置推送链接/Key，已跳过微信推送")
			} else {
				if err := m.sendWechatPush(ctx, channelKey, c.PushHead(), item.Title, item.Link); err != nil {
					m.emitLog(appCtx, "ERROR", "微信推送失败: "+err.Error())
				} else {
					m.emitLog(appCtx, "INFO", "微信推送发送成功")
				}
			}
			prevAnnKey = item.Key

		case "活动":
			af, ok := c.(activityAllFetcher)
			if !ok {
				// 理论不会发生；兜底：仍按单条逻辑处理
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
				prevActKey = item.Key
				continue
			}

			all, err := af.FetchAll(ctx, m.httpClient)
			if err != nil {
				m.emitLog(appCtx, "ERROR", "活动检查失败: "+err.Error())
				continue
			}
			if len(all) == 0 {
				m.emitLog(appCtx, "WARN", "未找到最新活动标题")
				continue
			}

			// 基线：首次运行时把整个列表作为已见集合，避免第一次就打开/推送。
			if strings.TrimSpace(prevActKey) == "" {
				keys := make([]string, 0, len(all))
				for _, it := range all {
					keys = append(keys, it.Key)
				}
				m.mu.Lock()
				m.lastActKey = all[0].Key
				m.lastActTitle = all[0].Title
				m.lastActLink = all[0].Link
				m.actSeenKeys = keys
				m.mu.Unlock()
				m.persistSnapshot()
				m.emitLog(appCtx, "INFO", "已获取当前最新活动(基线): "+all[0].Title)
				prevActKey = all[0].Key
				seenAct = append([]string(nil), keys...)
				continue
			}

			seenSet := map[string]struct{}{}
			for _, k := range seenAct {
				k = strings.TrimSpace(k)
				if k == "" {
					continue
				}
				seenSet[k] = struct{}{}
			}

			var newItems []latestItem
			for _, it := range all {
				if _, ok := seenSet[it.Key]; !ok {
					newItems = append(newItems, it)
				}
			}

			if len(newItems) == 0 {
				m.emitLog(appCtx, "INFO", "活动未发现新增: "+all[0].Title)
				continue
			}

			picked := newItems[len(newItems)-1]
			m.emitLog(appCtx, "INFO", "检测到新活动: "+picked.Title)

			// 更新已见列表并限制长度
			for _, it := range newItems {
				seenAct = append(seenAct, it.Key)
			}
			const maxSeen = 200
			if len(seenAct) > maxSeen {
				seenAct = seenAct[len(seenAct)-maxSeen:]
			}

			m.mu.Lock()
			m.lastActKey = picked.Key
			m.lastActTitle = picked.Title
			m.lastActLink = picked.Link
			m.actSeenKeys = append([]string(nil), seenAct...)
			m.mu.Unlock()
			m.persistSnapshot()

			if strings.TrimSpace(picked.Link) != "" {
				runtime.BrowserOpenURL(appCtx, picked.Link)
				m.emitLog(appCtx, "INFO", "已打开活动链接: "+picked.Link)
			} else {
				m.emitLog(appCtx, "WARN", "未解析到活动链接")
			}

			if strings.TrimSpace(channelKey) == "" {
				m.emitLog(appCtx, "INFO", "未配置推送链接/Key，已跳过微信推送")
			} else {
				if err := m.sendWechatPush(ctx, channelKey, c.PushHead(), picked.Title, picked.Link); err != nil {
					m.emitLog(appCtx, "ERROR", "微信推送失败: "+err.Error())
				} else {
					m.emitLog(appCtx, "INFO", "微信推送发送成功")
				}
			}
			prevActKey = picked.Key

		case "论坛":
			if strings.TrimSpace(prevForumKey) == "" {
				m.mu.Lock()
				m.lastForumKey = item.Key
				m.lastForumTitle = item.Title
				m.lastForumLink = item.Link
				m.mu.Unlock()
				m.persistSnapshot()
				m.emitLog(appCtx, "INFO", "已获取当前最新论坛帖子(基线): "+item.Title)
				prevForumKey = item.Key
				continue
			}

			if item.Key == prevForumKey {
				m.emitLog(appCtx, "INFO", "论坛首帖未发生变化: "+item.Title)
				continue
			}

			m.emitLog(appCtx, "INFO", "检测到论坛新帖: "+item.Title)
			m.mu.Lock()
			m.lastForumKey = item.Key
			m.lastForumTitle = item.Title
			m.lastForumLink = item.Link
			m.mu.Unlock()
			m.persistSnapshot()

			if strings.TrimSpace(item.Link) != "" {
				runtime.BrowserOpenURL(appCtx, item.Link)
				m.emitLog(appCtx, "INFO", "已打开论坛帖子链接: "+item.Link)
			} else {
				m.emitLog(appCtx, "WARN", "未解析到论坛帖子链接")
			}

			if strings.TrimSpace(channelKey) == "" {
				m.emitLog(appCtx, "INFO", "未配置推送链接/Key，已跳过微信推送")
			} else {
				if err := m.sendWechatPush(ctx, channelKey, c.PushHead(), item.Title, item.Link); err != nil {
					m.emitLog(appCtx, "ERROR", "微信推送失败: "+err.Error())
				} else {
					m.emitLog(appCtx, "INFO", "微信推送发送成功")
				}
			}
			prevForumKey = item.Key
		}
	}

	if allFailed {
		return errors.New("公告、活动与论坛检查均失败")
	}
	return nil
}

func (m *Monitor) sendWechatPush(ctx context.Context, channelKey string, head string, title string, link string) error {
	channelKey = strings.TrimSpace(channelKey)
	if channelKey == "" {
		return errors.New("推送链接/Key 不能为空")
	}
	head = strings.TrimSpace(head)
	if head == "" {
		head = "消息通知"
	}
	body := buildRichPushContent(title, link)

	pushURL, err := buildXizhiPushURL(channelKey, head, body)
	if err != nil {
		return err
	}

	pushClient := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pushURL, nil)
	if err != nil {
		return err
	}

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

func buildRichPushContent(title string, link string) string {
	title = strings.TrimSpace(title)
	link = strings.TrimSpace(link)

	if title == "" {
		title = "有新消息"
	}
	if link == "" {
		return title + "\n来源：天龙怀旧公告检测\n链接：无"
	}

	return title + "\n来源：天龙怀旧公告检测\n链接：" + link
}

func buildXizhiPushURL(pushInput string, title string, content string) (string, error) {
	pushInput = strings.TrimSpace(pushInput)
	if pushInput == "" {
		return "", errors.New("推送链接/Key 不能为空")
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = "消息通知"
	}
	content = strings.TrimSpace(content)

	// 1) 允许直接粘贴完整链接，例如：
	// https://xizhi.qqoq.net/XZxxxx.send
	if strings.Contains(pushInput, "://") {
		u, err := url.Parse(pushInput)
		if err != nil {
			return "", err
		}
		if u.Scheme == "" || u.Host == "" {
			return "", errors.New("无效推送链接")
		}
		q := u.Query()
		q.Set("title", title)
		q.Set("content", content)
		u.RawQuery = q.Encode()
		return u.String(), nil
	}

	// 2) 允许只填 key 或填 "XZxxxx.send"
	key := strings.TrimSpace(pushInput)
	if strings.Contains(key, "/") {
		parts := strings.Split(key, "/")
		key = strings.TrimSpace(parts[len(parts)-1])
	}
	key = strings.TrimSuffix(key, ".send")
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("无效推送 key")
	}

	u := &url.URL{
		Scheme: "https",
		Host:   xizhiDefaultHost,
		Path:   "/" + key + ".send",
	}
	q := u.Query()
	q.Set("title", title)
	q.Set("content", content)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (m *Monitor) emitLog(appCtx context.Context, level string, msg string) {
	if appCtx == nil {
		return
	}
	line := time.Now().Format("2006-01-02 15:04:05") + " [" + level + "] " + msg
	runtime.EventsEmit(appCtx, "log", line)
}
