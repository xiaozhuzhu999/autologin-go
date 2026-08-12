// ============================================================
// AutoLogin Pro - 前端逻辑
// ============================================================

let sites = [];
let editingSiteId = null;
let currentTaskID = null;
let loginEventSource = null;

// 页面关闭时通知后端退出
window.addEventListener('beforeunload', function() {
    navigator.sendBeacon('/api/quit');
});

const LOGIN_STEPS = [
    "VPN 连接",
    "打开登录页",
    "填写用户名",
    "填写密码",
    "动态口令",
    "验证码识别",
    "提交登录",
    "验证结果"
];

// ============================================================
//  视图切换
// ============================================================
function showView(name) {
    document.querySelectorAll('.view').forEach(v => v.classList.remove('active'));
    document.getElementById('view-' + name).classList.add('active');
}

// ============================================================
//  站点列表
// ============================================================
async function loadSites() {
    try {
        const resp = await fetch('/api/sites');
        sites = await resp.json();
        renderSites();
    } catch (e) {
        console.error('加载站点失败:', e);
    }
}

function renderSites() {
    const grid = document.getElementById('site-grid');
    if (!sites || sites.length === 0) {
        grid.innerHTML = '<div style="color:#707070;padding:20px;text-align:center;grid-column:1/-1;">暂无站点，点击右上角添加</div>';
        return;
    }

    grid.innerHTML = sites.map((site, i) => {
        const avatar = firstChar(site.name);
        const tags = buildTags(site);
        const lastLogin = formatLastLogin(site);

        return `
        <div class="site-card" ondblclick="startLogin(${site.id})">
            <div class="site-card-top">
                <div class="site-avatar">${avatar}</div>
                <div class="site-info">
                    <div class="site-name">${escapeHtml(site.name || '未命名站点')}</div>
                    <div class="site-url">${escapeHtml(site.url || '未配置网址')}</div>
                </div>
                <button class="btn-primary" onclick="event.stopPropagation();startLogin(${site.id})">一键登录</button>
            </div>
            <div class="site-tags">${tags}</div>
            <div class="site-card-bottom">
                <div class="site-last-login">${lastLogin}</div>
                <div class="site-actions">
                    <button class="btn-ghost" onclick="event.stopPropagation();openEditDialog(${site.id})">编辑</button>
                    <button class="btn-danger" onclick="event.stopPropagation();deleteSite(${site.id})">删除</button>
                </div>
            </div>
        </div>`;
    }).join('');
}

function firstChar(text) {
    const t = (text || '站').trim();
    return t ? t[0].toUpperCase() : '站';
}

function buildTags(site) {
    const tags = [];
    if (site.vpn_enabled) tags.push('<span class="tag-warn">需要 VPN</span>');
    if (site.captcha_mode === 'auto') tags.push('<span class="tag-success">自动验证码</span>');
    if (site.otp_mode === 'manual') tags.push('<span class="tag">动态口令</span>');
    if (site.otp_mode === 'none' && !site.vpn_enabled) tags.push('<span class="tag">快捷登录</span>');
    if (tags.length === 0) tags.push('<span class="tag">基础配置</span>');
    return tags.join('');
}

function formatLastLogin(site) {
    if (site.last_login_at) {
        const statusMap = { success: '成功', failed: '失败' };
        const statusText = statusMap[site.last_login_status] || '';
        return `上次登录：${site.last_login_at}${statusText ? ' · ' + statusText : ''}`;
    }
    return '上次登录：暂无记录';
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// ============================================================
//  站点编辑
// ============================================================
function openEditDialog(siteId) {
    editingSiteId = siteId || null;
    const dialog = document.getElementById('edit-dialog');
    const title = document.getElementById('edit-dialog-title');

    if (siteId) {
        const site = sites.find(s => s.id === siteId);
        if (!site) return;
        title.textContent = '编辑站点';
        fillEditForm(site);
    } else {
        title.textContent = '添加站点';
        fillEditForm(null);
    }

    dialog.style.display = 'flex';
}

function closeEditDialog() {
    document.getElementById('edit-dialog').style.display = 'none';
    editingSiteId = null;
}

function fillEditForm(site) {
    const defaults = {
        name: '', url: '', username: '', password: '',
        otp_mode: 'none', captcha_mode: 'auto',
        vpn_enabled: false,
        vpn_config: { exe_path: '', window_title: '', connect_wait: 20 },
        selectors: {
            username_input: "input[type='text'], input[name='username'], input[id='username']",
            password_input: "input[type='password'], input[name='password'], input[id='password']",
            otp_input: "input[name='totp'], input[name='otp'], input[name='code']",
            captcha_input: "input[name='captcha'], input[placeholder*='验证码']",
            captcha_img: "img.captcha, .captcha img",
            submit_button: "button[type='submit'], .login-btn, button:has-text('登录')",
        }
    };

    const s = site || defaults;
    const vpn = s.vpn_config || defaults.vpn_config;
    const sel = s.selectors || defaults.selectors;

    document.getElementById('edit-name').value = s.name || '';
    document.getElementById('edit-url').value = s.url || '';
    document.getElementById('edit-username').value = s.username || '';
    document.getElementById('edit-password').value = s.password || '';
    document.getElementById('edit-otp-mode').value = s.otp_mode || 'none';
    document.getElementById('edit-captcha-mode').value = s.captcha_mode || 'auto';
    document.getElementById('edit-vpn-enabled').checked = s.vpn_enabled || false;
    document.getElementById('edit-vpn-exe').value = vpn.exe_path || '';
    document.getElementById('edit-vpn-title').value = vpn.window_title || '';
    document.getElementById('edit-vpn-wait').value = vpn.connect_wait || 20;
    document.getElementById('edit-sel-username').value = sel.username_input || '';
    document.getElementById('edit-sel-password').value = sel.password_input || '';
    document.getElementById('edit-sel-otp').value = sel.otp_input || '';
    document.getElementById('edit-sel-captcha').value = sel.captcha_input || '';
    document.getElementById('edit-sel-captcha-img').value = sel.captcha_img || '';
    document.getElementById('edit-sel-submit').value = sel.submit_button || '';

    toggleVpnFields();
}

function toggleVpnFields() {
    const checked = document.getElementById('edit-vpn-enabled').checked;
    document.getElementById('vpn-fields').style.display = checked ? 'block' : 'none';
}

document.getElementById('edit-vpn-enabled').addEventListener('change', toggleVpnFields);

function collectSiteData() {
    return {
        name: document.getElementById('edit-name').value,
        url: document.getElementById('edit-url').value,
        username: document.getElementById('edit-username').value,
        password: document.getElementById('edit-password').value,
        otp_mode: document.getElementById('edit-otp-mode').value,
        captcha_mode: document.getElementById('edit-captcha-mode').value,
        vpn_enabled: document.getElementById('edit-vpn-enabled').checked,
        vpn_config: {
            exe_path: document.getElementById('edit-vpn-exe').value,
            window_title: document.getElementById('edit-vpn-title').value,
            connect_wait: parseInt(document.getElementById('edit-vpn-wait').value) || 20,
        },
        selectors: {
            username_input: document.getElementById('edit-sel-username').value,
            password_input: document.getElementById('edit-sel-password').value,
            otp_input: document.getElementById('edit-sel-otp').value,
            captcha_input: document.getElementById('edit-sel-captcha').value,
            captcha_img: document.getElementById('edit-sel-captcha-img').value,
            submit_button: document.getElementById('edit-sel-submit').value,
        }
    };
}

async function saveSite() {
    const data = collectSiteData();
    if (!data.name.trim()) {
        alert('请输入站点名称');
        return;
    }
    if (!data.url.trim()) {
        alert('请输入登录网址');
        return;
    }

    try {
        let resp;
        if (editingSiteId) {
            resp = await fetch(`/api/sites/${editingSiteId}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(data)
            });
        } else {
            resp = await fetch('/api/sites', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(data)
            });
        }

        if (resp.ok) {
            closeEditDialog();
            await loadSites();
        } else {
            const err = await resp.json();
            alert('保存失败: ' + (err.error || '未知错误'));
        }
    } catch (e) {
        alert('保存失败: ' + e.message);
    }
}

async function deleteSite(siteId) {
    if (!confirm('确定要删除这个站点吗？')) return;
    try {
        const resp = await fetch(`/api/sites/${siteId}`, { method: 'DELETE' });
        if (resp.ok) {
            await loadSites();
        }
    } catch (e) {
        alert('删除失败: ' + e.message);
    }
}

// ============================================================
//  登录流程
// ============================================================
async function startLogin(siteId) {
    const site = sites.find(s => s.id === siteId);
    if (!site) return;

    // 重置 UI
    document.getElementById('login-site-name').textContent = site.name;
    document.getElementById('log-content').innerHTML = '';
    document.getElementById('login-result').style.display = 'none';
    document.getElementById('cancel-login-btn').style.display = '';
    renderSteps();

    showView('login');
    appendLog('info', `正在启动登录: ${site.name}...`);

    try {
        // 发起登录请求
        const resp = await fetch(`/api/sites/${siteId}/login`, { method: 'POST' });
        if (!resp.ok) {
            const err = await resp.json().catch(() => ({}));
            appendLog('error', `启动登录失败: ${err.error || resp.statusText}`);
            showResult(false, err.error || '启动登录失败');
            return;
        }
        const result = await resp.json();
        currentTaskID = result.task_id;

        if (!currentTaskID) {
            appendLog('error', '启动登录失败: 未获取到任务 ID');
            showResult(false, '启动登录失败');
            return;
        }

        appendLog('info', '登录任务已创建，正在执行...');
        // 建立 SSE 连接
        connectSSE(currentTaskID);
    } catch (e) {
        appendLog('error', '启动登录失败: ' + e.message);
        showResult(false, '启动登录失败: ' + e.message);
    }
}

function connectSSE(taskID) {
    if (loginEventSource) {
        loginEventSource.close();
    }

    loginEventSource = new EventSource(`/api/login/stream/${taskID}`);

    loginEventSource.addEventListener('task', (e) => {
        const data = JSON.parse(e.data);
        appendLog('info', `开始登录: ${data.site_name}`);
    });

    loginEventSource.onmessage = (e) => {
        const msg = JSON.parse(e.data);

        if (msg.type === 'log') {
            // 解析日志级别
            const match = msg.content.match(/^\[(\w+)\]\s*(.*)/);
            if (match) {
                appendLog(match[1], match[2]);
            } else {
                appendLog('info', msg.content);
            }
        } else if (msg.type === 'step') {
            updateStep(msg.step.index, msg.step.name, msg.step.state);
        } else if (msg.type === 'success') {
            appendLog('info', msg.content);
            showResult(true, msg.content);
            loginEventSource.close();
        } else if (msg.type === 'failed') {
            appendLog('error', msg.content);
            showResult(false, msg.content);
            loginEventSource.close();
        }
    };

    loginEventSource.onerror = () => {
        loginEventSource.close();
        // 仅在未显示结果时提示连接断开
        const result = document.getElementById('login-result');
        if (result.style.display === 'none') {
            appendLog('warning', '与服务端的连接已断开');
        }
    };
}

function renderSteps() {
    const panel = document.getElementById('steps-panel');
    panel.innerHTML = LOGIN_STEPS.map((name, i) => `
        <div class="step-item" id="step-${i}">
            <div class="step-number">${i + 1}</div>
            <div class="step-name">${name}</div>
            <div class="step-status"></div>
        </div>
    `).join('');
}

function updateStep(index, name, state) {
    const el = document.getElementById(`step-${index}`);
    if (!el) return;

    el.classList.remove('active', 'done');
    el.classList.add(state);

    const statusEl = el.querySelector('.step-status');
    if (state === 'active') {
        statusEl.textContent = '进行中...';
    } else if (state === 'done') {
        statusEl.textContent = '✓';
    } else {
        statusEl.textContent = '';
    }
}

function appendLog(level, message) {
    const content = document.getElementById('log-content');
    const line = document.createElement('div');
    line.className = `log-line ${level}`;
    const time = new Date().toLocaleTimeString('zh-CN', { hour12: false });
    line.textContent = `[${time}] ${message}`;
    content.appendChild(line);
    content.scrollTop = content.scrollHeight;
}

function showResult(success, message) {
    const result = document.getElementById('login-result');
    const icon = document.getElementById('result-icon');
    const text = document.getElementById('result-text');

    result.style.display = 'block';
    if (success) {
        icon.className = 'result-icon success';
        icon.textContent = '✓';
        text.textContent = message || '登录成功';
    } else {
        icon.className = 'result-icon failed';
        icon.textContent = '✕';
        text.textContent = message || '登录失败';
    }

    document.getElementById('cancel-login-btn').style.display = 'none';
}

async function cancelLogin() {
    if (!currentTaskID) return;
    try {
        await fetch(`/api/login/${currentTaskID}/cancel`, { method: 'POST' });
        appendLog('warning', '登录已取消');
        if (loginEventSource) {
            loginEventSource.close();
        }
        showResult(false, '用户取消登录');
    } catch (e) {
        appendLog('error', '取消失败: ' + e.message);
    }
}

// ============================================================
//  退出应用
// ============================================================
async function quitApp() {
    try {
        await fetch('/api/quit', { method: 'POST' });
    } catch (e) {
        // 忽略错误（服务端可能已关闭）
    }
}

// ============================================================
//  初始化
// ============================================================
document.addEventListener('DOMContentLoaded', () => {
    loadSites();
});
