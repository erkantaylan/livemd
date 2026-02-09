// WebSocket client for LiveMD
(function() {
    const fileList = document.getElementById('file-list');
    const logList = document.getElementById('log-list');
    const changelogList = document.getElementById('changelog-list');
    const content = document.getElementById('content');
    const status = document.getElementById('status');
    const deletedBar = document.getElementById('deleted-bar');
    const removeDeletedBtn = document.getElementById('remove-deleted-btn');
    const checkUpdateBtn = document.getElementById('check-update-btn');
    const updateBanner = document.getElementById('update-banner');
    const updateText = document.getElementById('update-text');
    const versionLabel = document.getElementById('version-label');
    const contentHeaderFilename = document.getElementById('content-header-filename');
    const contentHeaderPath = document.getElementById('content-header-path');
    const contentHeaderChanged = document.getElementById('content-header-changed');

    let ws;
    let reconnectDelay = 1000;
    const maxReconnectDelay = 10000;

    let files = [];
    let logs = [];
    let activeFile = null;
    let collapsedFolders = new Set();
    let changelogLoaded = false;

    // Tab switching
    document.querySelectorAll('.tabs li').forEach(li => {
        li.addEventListener('click', () => {
            document.querySelectorAll('.tabs li').forEach(l => l.classList.remove('is-active'));
            document.querySelectorAll('.tab-content').forEach(t => t.classList.add('is-hidden'));
            li.classList.add('is-active');
            document.getElementById(li.dataset.tab + '-tab').classList.remove('is-hidden');
            if (li.dataset.tab === 'changelog') {
                loadChangelog();
            }
        });
    });

    // Remove all deleted files button
    removeDeletedBtn.addEventListener('click', () => {
        fetch('/api/files/remove-deleted', { method: 'POST' }).catch(err => {
            console.error('Failed to remove deleted files:', err);
        });
    });

    // Check for updates button
    checkUpdateBtn.addEventListener('click', () => {
        checkUpdateBtn.textContent = 'Checking...';
        checkUpdateBtn.disabled = true;
        checkForUpdates();
    });

    function checkForUpdates() {
        fetch('/api/version')
            .then(r => r.json())
            .then(info => {
                versionLabel.textContent = 'livemd ' + info.current;
                checkUpdateBtn.textContent = 'Check updates';
                checkUpdateBtn.disabled = false;

                if (info.updateAvailable) {
                    updateText.innerHTML = 'Update available: <a href="' + escapeHtml(info.latestUrl) + '" target="_blank">' + escapeHtml(info.latest) + '</a>';
                    updateBanner.classList.remove('is-hidden');
                } else {
                    updateBanner.classList.add('is-hidden');
                }
            })
            .catch(err => {
                console.error('Failed to check for updates:', err);
                checkUpdateBtn.textContent = 'Check updates';
                checkUpdateBtn.disabled = false;
            });
    }

    function loadChangelog() {
        if (changelogLoaded) return;
        changelogList.innerHTML = '<div class="empty-state"><p>Loading changelog...</p></div>';

        fetch('/api/releases')
            .then(r => r.json())
            .then(releases => {
                changelogLoaded = true;
                if (!releases || releases.length === 0) {
                    changelogList.innerHTML = '<div class="empty-state"><p>No releases found</p></div>';
                    return;
                }
                changelogList.innerHTML = releases.map(r => {
                    const date = r.published_at ? new Date(r.published_at).toLocaleDateString('en-US', { year: 'numeric', month: 'short', day: 'numeric' }) : '';
                    const title = r.name || r.tag_name;
                    return `
                        <div class="changelog-entry">
                            <div class="changelog-tag"><a href="${escapeHtml(r.html_url)}" target="_blank">${escapeHtml(title)}</a></div>
                            <div class="changelog-date">${escapeHtml(r.tag_name)} &middot; ${date}</div>
                            ${r.body ? '<div class="changelog-body">' + escapeHtml(r.body) + '</div>' : ''}
                        </div>
                    `;
                }).join('');
            })
            .catch(err => {
                console.error('Failed to load changelog:', err);
                changelogList.innerHTML = '<div class="empty-state"><p>Failed to load changelog</p></div>';
            });
    }

    function formatShortDateTime(isoString) {
        const date = new Date(isoString);
        const month = String(date.getMonth() + 1).padStart(2, '0');
        const day = String(date.getDate()).padStart(2, '0');
        const hours = String(date.getHours()).padStart(2, '0');
        const mins = String(date.getMinutes()).padStart(2, '0');
        return `${month}-${day} ${hours}:${mins}`;
    }

    function findCommonPrefix(paths) {
        if (paths.length === 0) return '';
        if (paths.length === 1) {
            const parts = paths[0].split('/');
            parts.pop();
            return parts.join('/');
        }

        const splitPaths = paths.map(p => p.split('/'));
        const minLen = Math.min(...splitPaths.map(p => p.length));
        let commonParts = [];

        for (let i = 0; i < minLen - 1; i++) {
            const part = splitPaths[0][i];
            if (splitPaths.every(p => p[i] === part)) {
                commonParts.push(part);
            } else {
                break;
            }
        }

        return commonParts.join('/');
    }

    function buildTree(files, commonPrefix) {
        const tree = { children: {}, files: [] };
        const prefixLen = commonPrefix ? commonPrefix.length + 1 : 0;

        for (const file of files) {
            const relativePath = file.path.slice(prefixLen);
            const parts = relativePath.split('/');
            const fileName = parts.pop();

            let current = tree;
            let currentPath = commonPrefix;

            for (const part of parts) {
                currentPath = currentPath ? currentPath + '/' + part : part;
                if (!current.children[part]) {
                    current.children[part] = {
                        children: {},
                        files: [],
                        path: currentPath,
                        name: part
                    };
                }
                current = current.children[part];
            }

            current.files.push({ ...file, displayName: fileName });
        }

        return tree;
    }

    function renderTreeNode(node, depth = 0) {
        let html = '';
        const indent = depth * 12;

        const folderNames = Object.keys(node.children).sort();

        for (const folderName of folderNames) {
            const folder = node.children[folderName];
            const isCollapsed = collapsedFolders.has(folder.path);
            const chevron = isCollapsed ? '&#9654;' : '&#9660;';

            html += `
                <div class="tree-folder ${isCollapsed ? 'collapsed' : ''}" data-path="${escapeHtml(folder.path)}" style="padding-left: ${indent}px">
                    <span class="folder-toggle" data-path="${escapeHtml(folder.path)}">${chevron}</span>
                    <span class="folder-icon">&#9662;</span>
                    <span class="folder-name">${escapeHtml(folderName)}</span>
                </div>
            `;

            if (!isCollapsed) {
                html += renderTreeNode(folder, depth + 1);
            }
        }

        const sortedFiles = [...node.files].sort((a, b) =>
            a.displayName.localeCompare(b.displayName)
        );

        for (const file of sortedFiles) {
            const isDeleted = file.deleted;
            const watchIcon = isDeleted ? '&#10005;' : (file.active ? '&#9679;' : '&#9675;');
            const watchTitle = isDeleted ? 'File deleted from disk' : (file.active ? 'Actively watching' : 'Registered (click to watch)');
            const deletedClass = isDeleted ? 'deleted' : '';
            const stateClass = file.active ? 'watching' : 'registered';

            html += `
                <div class="file-item tree-file ${file.path === activeFile ? 'active' : ''} ${stateClass} ${deletedClass}" data-path="${escapeHtml(file.path)}" style="padding-left: ${indent}px">
                    <button class="file-remove" data-path="${escapeHtml(file.path)}" title="Remove from watch">&#10005;</button>
                    <span class="file-icon" title="${watchTitle}">${watchIcon}</span>
                    <div class="file-info">
                        <div class="file-name" title="${escapeHtml(file.path)}">${isDeleted ? '<span class="has-text-danger">' + escapeHtml(file.displayName) + '</span>' : escapeHtml(file.displayName)}</div>
                    </div>
                </div>
            `;
        }

        return html;
    }

    function toggleFolder(path) {
        if (collapsedFolders.has(path)) {
            collapsedFolders.delete(path);
        } else {
            collapsedFolders.add(path);
        }
        renderFileList();
    }

    function formatLogTime(isoString) {
        const date = new Date(isoString);
        return date.toLocaleTimeString('en-US', { hour12: false });
    }

    function updateDeletedBar() {
        const hasDeleted = files.some(f => f.deleted);
        if (hasDeleted) {
            deletedBar.classList.remove('is-hidden');
        } else {
            deletedBar.classList.add('is-hidden');
        }
    }

    function renderFileList() {
        if (files.length === 0) {
            fileList.innerHTML = `
                <div class="empty-state">
                    <p>No files being watched</p>
                    <code>livemd add file.md</code>
                </div>
            `;
            updateDeletedBar();
            return;
        }

        const paths = files.map(f => f.path);
        const commonPrefix = findCommonPrefix(paths);
        const tree = buildTree(files, commonPrefix);

        let html = '';
        if (commonPrefix) {
            const rootName = commonPrefix.split('/').pop() || commonPrefix;
            html += `<div class="tree-root" title="${escapeHtml(commonPrefix)}">${escapeHtml(rootName)}</div>`;
        }
        html += renderTreeNode(tree, commonPrefix ? 1 : 0);

        fileList.innerHTML = html;
        updateDeletedBar();

        fileList.querySelectorAll('.tree-file').forEach(el => {
            el.addEventListener('click', (e) => {
                if (e.target.classList.contains('file-remove')) return;
                selectFile(el.dataset.path);
            });
        });

        fileList.querySelectorAll('.file-remove').forEach(btn => {
            btn.addEventListener('click', (e) => {
                e.stopPropagation();
                removeFile(btn.dataset.path);
            });
        });

        fileList.querySelectorAll('.folder-toggle').forEach(el => {
            el.addEventListener('click', (e) => {
                e.stopPropagation();
                toggleFolder(el.dataset.path);
            });
        });

        fileList.querySelectorAll('.tree-folder').forEach(el => {
            el.addEventListener('click', () => {
                toggleFolder(el.dataset.path);
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

    function updateContentHeader(file) {
        if (file) {
            contentHeaderFilename.textContent = file.name;
            contentHeaderPath.textContent = file.path;
            contentHeaderChanged.textContent = file.lastChange ? 'Changed: ' + formatShortDateTime(file.lastChange) : '';
        } else {
            contentHeaderFilename.textContent = 'No file selected';
            contentHeaderPath.textContent = '';
            contentHeaderChanged.textContent = '';
        }
    }

    function selectFile(path) {
        const file = files.find(f => f.path === path);
        if (file && file.deleted) return; // Can't select deleted files

        const previousFile = activeFile;
        activeFile = path;
        renderFileList();

        if (file && file.html) {
            content.innerHTML = file.html;
            document.title = file.name + ' - LiveMD';
            updateContentHeader(file);
        }

        if (path && path !== previousFile) {
            activateFile(path);
        }

        if (previousFile && previousFile !== path) {
            deactivateFile(previousFile);
        }
    }

    function activateFile(path) {
        fetch('/api/files/activate?path=' + encodeURIComponent(path), {
            method: 'POST'
        }).catch(err => {
            console.error('Failed to activate file:', err);
        });
    }

    function deactivateFile(path) {
        fetch('/api/files/deactivate?path=' + encodeURIComponent(path), {
            method: 'POST'
        }).catch(err => {
            console.error('Failed to deactivate file:', err);
        });
    }

    function connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(`${protocol}//${window.location.host}/ws`);

        ws.onopen = function() {
            status.textContent = 'live';
            status.className = 'tag is-success is-light';
            reconnectDelay = 1000;
            // Check version on connect
            checkForUpdates();
        };

        ws.onmessage = function(event) {
            const data = JSON.parse(event.data);

            switch (data.type) {
                case 'files':
                    files = data.files || [];
                    renderFileList();

                    if (!activeFile && files.length > 0) {
                        const firstNonDeleted = files.find(f => !f.deleted);
                        if (firstNonDeleted) selectFile(firstNonDeleted.path);
                    } else if (activeFile) {
                        const file = files.find(f => f.path === activeFile);
                        if (file && file.html && !file.deleted) {
                            content.innerHTML = file.html;
                            updateContentHeader(file);
                        } else if (file && file.deleted) {
                            content.innerHTML = `
                                <div class="welcome">
                                    <h1 class="has-text-danger">File Deleted</h1>
                                    <p>${escapeHtml(file.name)} has been deleted from disk.</p>
                                </div>
                            `;
                            updateContentHeader(null);
                        }
                    }
                    break;

                case 'logs':
                    logs = data.logs || [];
                    renderLogList();
                    break;

                case 'log':
                    if (data.log) {
                        logs.push(data.log);
                        if (logs.length > 100) {
                            logs = logs.slice(-100);
                        }
                        renderLogList();
                    }
                    break;

                case 'update':
                    if (data.file) {
                        const idx = files.findIndex(f => f.path === data.file.path);
                        if (idx >= 0) {
                            files[idx] = data.file;
                        } else {
                            files.push(data.file);
                        }
                        renderFileList();

                        if (data.file.path === activeFile) {
                            const scrollY = window.scrollY;
                            content.innerHTML = data.file.html;
                            window.scrollTo(0, scrollY);
                        }
                    }
                    break;

                case 'removed':
                    files = files.filter(f => f.path !== data.path);
                    renderFileList();

                    if (data.path === activeFile) {
                        activeFile = null;
                        const remaining = files.filter(f => !f.deleted);
                        if (remaining.length > 0) {
                            selectFile(remaining[0].path);
                        } else {
                            content.innerHTML = `
                                <div class="welcome">
                                    <h1>LiveMD</h1>
                                    <p>Add a markdown file to get started:</p>
                                    <pre><code>livemd add README.md</code></pre>
                                </div>
                            `;
                            document.title = 'LiveMD';
                            updateContentHeader(null);
                        }
                    }
                    break;
            }
        };

        ws.onclose = function() {
            status.textContent = 'disconnected';
            status.className = 'tag is-danger is-light';

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
