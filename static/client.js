// WebSocket client for LiveMD
(function() {
    const fileList = document.getElementById('file-list');
    const logList = document.getElementById('log-list');
    const content = document.getElementById('content');
    const status = document.getElementById('status');

    let ws;
    let reconnectDelay = 1000;
    const maxReconnectDelay = 10000;

    let files = [];
    let logs = [];
    let activeFile = null;

    // Tab switching
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(t => t.classList.add('hidden'));
            btn.classList.add('active');
            document.getElementById(btn.dataset.tab + '-tab').classList.remove('hidden');
        });
    });

    function formatTime(isoString) {
        const date = new Date(isoString);
        const now = new Date();
        const diffMs = now - date;
        const diffMins = Math.floor(diffMs / 60000);
        const diffHours = Math.floor(diffMs / 3600000);
        const diffDays = Math.floor(diffMs / 86400000);

        if (diffMins < 1) return 'just now';
        if (diffMins < 60) return diffMins + 'm ago';
        if (diffHours < 24) return diffHours + 'h ago';
        if (diffDays < 7) return diffDays + 'd ago';

        return date.toLocaleDateString();
    }

    function formatLogTime(isoString) {
        const date = new Date(isoString);
        return date.toLocaleTimeString('en-US', { hour12: false });
    }

    function renderFileList() {
        if (files.length === 0) {
            fileList.innerHTML = `
                <div class="empty-state">
                    <p>No files being watched</p>
                    <code>livemd add file.md</code>
                </div>
            `;
            return;
        }

        fileList.innerHTML = files.map(f => `
            <div class="file-item ${f.path === activeFile ? 'active' : ''}" data-path="${f.path}">
                <button class="file-remove" data-path="${f.path}" title="Remove from watch">&times;</button>
                <div class="file-name">${escapeHtml(f.name)}</div>
                <div class="file-meta">
                    <span><span class="label">Tracking:</span> ${formatTime(f.trackTime)}</span>
                    <span><span class="label">Changed:</span> ${formatTime(f.lastChange)}</span>
                </div>
            </div>
        `).join('');

        // Add click handlers
        fileList.querySelectorAll('.file-item').forEach(el => {
            el.addEventListener('click', (e) => {
                // Don't select if clicking remove button
                if (e.target.classList.contains('file-remove')) return;
                selectFile(el.dataset.path);
            });
        });

        // Add remove handlers
        fileList.querySelectorAll('.file-remove').forEach(btn => {
            btn.addEventListener('click', (e) => {
                e.stopPropagation();
                removeFile(btn.dataset.path);
            });
        });
    }

    function removeFile(path) {
        fetch('/api/watch?path=' + encodeURIComponent(path), {
            method: 'DELETE'
        }).catch(err => {
            console.error('Failed to remove file:', err);
        });
    }

    function renderLogList() {
        if (logs.length === 0) {
            logList.innerHTML = `
                <div class="empty-state">
                    <p>No logs yet</p>
                </div>
            `;
            return;
        }

        // Show newest first
        const reversedLogs = [...logs].reverse();
        logList.innerHTML = reversedLogs.map(l => `
            <div class="log-entry ${l.level}">
                <span class="log-time">${formatLogTime(l.time)}</span>
                <span class="log-level">${l.level}</span>
                <span class="log-message">${escapeHtml(l.message)}</span>
            </div>
        `).join('');
    }

    function escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    function selectFile(path) {
        activeFile = path;
        renderFileList();

        const file = files.find(f => f.path === path);
        if (file && file.html) {
            content.innerHTML = file.html;
            document.title = file.name + ' - LiveMD';
        }
    }

    function connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(`${protocol}//${window.location.host}/ws`);

        ws.onopen = function() {
            status.textContent = 'live';
            status.className = 'status connected';
            reconnectDelay = 1000;
        };

        ws.onmessage = function(event) {
            const data = JSON.parse(event.data);

            switch (data.type) {
                case 'files':
                    // Full file list update
                    files = data.files || [];
                    renderFileList();

                    // Auto-select first file if none selected
                    if (!activeFile && files.length > 0) {
                        selectFile(files[0].path);
                    } else if (activeFile) {
                        // Re-render current file
                        const file = files.find(f => f.path === activeFile);
                        if (file && file.html) {
                            content.innerHTML = file.html;
                        }
                    }
                    break;

                case 'logs':
                    // Full logs update
                    logs = data.logs || [];
                    renderLogList();
                    break;

                case 'log':
                    // Single log entry
                    if (data.log) {
                        logs.push(data.log);
                        // Keep only last 100
                        if (logs.length > 100) {
                            logs = logs.slice(-100);
                        }
                        renderLogList();
                    }
                    break;

                case 'update':
                    // Single file update
                    if (data.file) {
                        const idx = files.findIndex(f => f.path === data.file.path);
                        if (idx >= 0) {
                            files[idx] = data.file;
                        } else {
                            files.push(data.file);
                        }
                        renderFileList();

                        // Update content if this is the active file
                        if (data.file.path === activeFile) {
                            const scrollY = window.scrollY;
                            content.innerHTML = data.file.html;
                            window.scrollTo(0, scrollY);
                        }
                    }
                    break;

                case 'removed':
                    // File removed
                    files = files.filter(f => f.path !== data.path);
                    renderFileList();

                    if (data.path === activeFile) {
                        activeFile = null;
                        if (files.length > 0) {
                            selectFile(files[0].path);
                        } else {
                            content.innerHTML = `
                                <div class="welcome">
                                    <h1>LiveMD</h1>
                                    <p>Add a markdown file to get started:</p>
                                    <pre><code>livemd add README.md</code></pre>
                                </div>
                            `;
                            document.title = 'LiveMD';
                        }
                    }
                    break;
            }
        };

        ws.onclose = function() {
            status.textContent = 'disconnected';
            status.className = 'status disconnected';

            // Reconnect with exponential backoff
            setTimeout(function() {
                reconnectDelay = Math.min(reconnectDelay * 1.5, maxReconnectDelay);
                connect();
            }, reconnectDelay);
        };

        ws.onerror = function(err) {
            console.error('WebSocket error:', err);
            ws.close();
        };
    }

    connect();
})();
