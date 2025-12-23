import './style.css';
import './app.css';

import { EventsOn } from '../wailsjs/runtime/runtime';
import { GetAppInfo, GetStatus, StartMonitoring, StopMonitoring } from '../wailsjs/go/main/App';

document.querySelector('#app').innerHTML = `
    <div class="container">
        <h1 class="title">怀旧天龙公告检测</h1>

        <div class="input-box">
            <input class="input" id="channelKey" type="text" autocomplete="off" placeholder="ChannelKey（可选，不填则不推送）" />
            <button class="btn" id="startBtn">开始监控</button>
            <button class="btn" id="stopBtn">结束监控</button>
        </div>

        <div class="result" id="status">状态：加载中...</div>

        <textarea class="log" id="log" readonly spellcheck="false"></textarea>

        <div class="footer">
            <div>说明：自动检测公告列表，新公告会打开浏览器进入公告页。</div>
            <div>操作：点击【开始监控】启动；需要推送则填写 ChannelKey；点击【结束监控】停止。</div>
            <div>作者：<span id="author"></span>　版本：<span id="version"></span></div>
        </div>
    </div>
`;

const channelKeyEl = document.getElementById('channelKey');
const startBtn = document.getElementById('startBtn');
const stopBtn = document.getElementById('stopBtn');
const statusEl = document.getElementById('status');
const logEl = document.getElementById('log');
const authorEl = document.getElementById('author');
const versionEl = document.getElementById('version');

function appendLog(line) {
    if (!line) return;
    logEl.value += (logEl.value ? '\n' : '') + line;
    logEl.scrollTop = logEl.scrollHeight;
}

function setButtons(running) {
    startBtn.disabled = !!running;
    stopBtn.disabled = !running;
}

async function refreshStatus() {
    try {
        const s = await GetStatus();
        setButtons(s.running);
        const checked = s.lastChecked ? `，最近检查：${s.lastChecked}` : '';
        const title = s.lastTitle ? `，最新标题：${s.lastTitle}` : '';
        statusEl.innerText = `状态：${s.running ? '运行中' : '已停止'}${checked}${title}`;
    } catch (e) {
        statusEl.innerText = '状态：获取失败';
        appendLog(String(e));
    }
}

startBtn.addEventListener('click', async () => {
    const key = (channelKeyEl.value || '').trim();
    try {
        await StartMonitoring(key);
        await refreshStatus();
    } catch (e) {
        appendLog(String(e));
    }
});

stopBtn.addEventListener('click', async () => {
    try {
        StopMonitoring();
        await refreshStatus();
    } catch (e) {
        appendLog(String(e));
    }
});

EventsOn('log', (line) => {
    appendLog(line);
});

channelKeyEl.focus();
GetAppInfo().then((info) => {
    if (authorEl) authorEl.innerText = info.author || '';
    if (versionEl) versionEl.innerText = info.version || '';
}).catch((e) => {
    appendLog(String(e));
});
refreshStatus();
