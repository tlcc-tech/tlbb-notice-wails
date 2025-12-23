import './style.css';
import './app.css';

import { EventsOn, WindowHide } from '../wailsjs/runtime/runtime';
import { GetAppInfo, GetStatus, QuitApp, StartMonitoring, StopMonitoring } from '../wailsjs/go/main/App';

document.querySelector('#app').innerHTML = `
    <div class="container">
        <h1 class="title">怀旧天龙公告检测</h1>

        <div class="input-box">
            <input class="input" id="channelKey" type="text" autocomplete="off" placeholder="ChannelKey（可选，不填则不推送）" />
            <button class="btn" id="startBtn">开始监控</button>
            <button class="btn" id="stopBtn">结束监控</button>
            <button class="btn" id="minToTrayBtn" style="display:none;">最小化到托盘</button>
        </div>

        <div class="result" id="status">状态：加载中...</div>

        <textarea class="log" id="log" readonly spellcheck="false"></textarea>

        <div class="footer">
            <div class="footer-left">
                <div>说明：自动检测公告列表，新公告会打开浏览器进入公告页。</div>
                <div>操作：点击【开始监控】启动；需要推送则填写 ChannelKey；点击【结束监控】停止。</div>
                <div>作者：<span id="author"></span>　版本：<span id="version"></span></div>
            </div>
            <div class="footer-right">
                <img class="footer-img" src="/qrcode.jpg" alt="qrcode" />
            </div>
        </div>
    </div>

    <div class="modal-mask" id="closePromptMask" style="display:none;">
        <div class="modal">
            <div class="modal-title">提示</div>
            <div class="modal-content">当前正在监控，是否最小化到托盘？</div>
            <div class="modal-actions">
                <button class="btn" id="closePromptMinBtn">最小化到托盘</button>
                <button class="btn" id="closePromptExitBtn">退出软件</button>
                <button class="btn" id="closePromptCancelBtn">取消</button>
            </div>
        </div>
    </div>
`;

const channelKeyEl = document.getElementById('channelKey');
const startBtn = document.getElementById('startBtn');
const stopBtn = document.getElementById('stopBtn');
const minToTrayBtn = document.getElementById('minToTrayBtn');
const statusEl = document.getElementById('status');
const logEl = document.getElementById('log');
const authorEl = document.getElementById('author');
const versionEl = document.getElementById('version');

const closePromptMask = document.getElementById('closePromptMask');
const closePromptMinBtn = document.getElementById('closePromptMinBtn');
const closePromptExitBtn = document.getElementById('closePromptExitBtn');
const closePromptCancelBtn = document.getElementById('closePromptCancelBtn');

function appendLog(line) {
    if (!line) return;
    logEl.value += (logEl.value ? '\n' : '') + line;
    logEl.scrollTop = logEl.scrollHeight;
}

function setButtons(running) {
    startBtn.disabled = !!running;
    stopBtn.disabled = !running;

    if (minToTrayBtn) {
        minToTrayBtn.disabled = !running;
        minToTrayBtn.style.display = running ? '' : 'none';
    }
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

function showClosePrompt() {
    if (closePromptMask) closePromptMask.style.display = '';
}

function hideClosePrompt() {
    if (closePromptMask) closePromptMask.style.display = 'none';
}

minToTrayBtn?.addEventListener('click', () => {
    try {
        WindowHide();
    } catch (e) {
        appendLog(String(e));
    }
});

closePromptMinBtn?.addEventListener('click', () => {
    hideClosePrompt();
    try {
        WindowHide();
    } catch (e) {
        appendLog(String(e));
    }
});

closePromptExitBtn?.addEventListener('click', async () => {
    hideClosePrompt();
    try {
        await QuitApp();
    } catch (e) {
        appendLog(String(e));
    }
});

closePromptCancelBtn?.addEventListener('click', () => {
    hideClosePrompt();
});

EventsOn('log', (line) => {
    appendLog(line);
});

// 后端拦截关闭按钮时触发
EventsOn('app:close-requested', () => {
    showClosePrompt();
});

channelKeyEl.focus();
GetAppInfo().then((info) => {
    if (authorEl) authorEl.innerText = info.author || '';
    if (versionEl) versionEl.innerText = info.version || '';
}).catch((e) => {
    appendLog(String(e));
});
refreshStatus();
