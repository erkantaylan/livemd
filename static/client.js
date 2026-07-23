// WebSocket client for LiveMD
(function() {
    // --- Lazy enhancements: mermaid diagrams + KaTeX math ---
    // Both libraries are loaded from CDN only when their patterns are detected,
    // so the typical markdown-only use case stays free of extra weight.
    let mermaidPromise = null;
    function loadMermaid() {
        if (mermaidPromise) return mermaidPromise;
        mermaidPromise = new Promise((resolve, reject) => {
            const s = document.createElement('script');
            s.src = 'https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js';
            s.onload = () => {
                window.mermaid.initialize({ startOnLoad: false, securityLevel: 'strict' });
                resolve(window.mermaid);
            };
            s.onerror = reject;
            document.head.appendChild(s);
        });
        return mermaidPromise;
    }

    let katexPromise = null;
    function loadKatex() {
        if (katexPromise) return katexPromise;
        katexPromise = new Promise((resolve, reject) => {
            const link = document.createElement('link');
            link.rel = 'stylesheet';
            link.href = 'https://cdn.jsdelivr.net/npm/katex@0.16.9/dist/katex.min.css';
            document.head.appendChild(link);
            const s1 = document.createElement('script');
            s1.src = 'https://cdn.jsdelivr.net/npm/katex@0.16.9/dist/katex.min.js';
            s1.onload = () => {
                const s2 = document.createElement('script');
                s2.src = 'https://cdn.jsdelivr.net/npm/katex@0.16.9/dist/contrib/auto-render.min.js';
                s2.onload = () => resolve(window.renderMathInElement);
                s2.onerror = reject;
                document.head.appendChild(s2);
            };
            s1.onerror = reject;
            document.head.appendChild(s1);
        });
        return katexPromise;
    }

    function enhanceContent(root) {
        if (!root) return;
        // Mermaid: server emits <div class="mermaid">...</div>; reset processed
        // attributes so re-renders work after live updates.
        const mermaidNodes = root.querySelectorAll('.mermaid');
        if (mermaidNodes.length) {
            mermaidNodes.forEach(n => n.removeAttribute('data-processed'));
            loadMermaid().then(m => m.run({ nodes: mermaidNodes })).catch(() => {});
        }
        // Math: only load KaTeX if a $ appears in the content (cheap heuristic).
        if (root.textContent && root.textContent.indexOf('$') !== -1) {
            loadKatex().then(render => {
                render(root, {
                    delimiters: [
                        { left: '$$', right: '$$', display: true },
                        { left: '$',  right: '$',  display: false },
                        { left: '\\[', right: '\\]', display: true },
                        { left: '\\(', right: '\\)', display: false },
                    ],
                    throwOnError: false,
                });
            }).catch(() => {});
        }
    }

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
    const viewToggle = document.getElementById('view-toggle');
    const viewPreviewBtn = document.getElementById('view-preview-btn');
    const viewSourceBtn = document.getElementById('view-source-btn');
    const addPathInput = document.getElementById('add-path-input');
    const addPathBtn = document.getElementById('add-path-btn');
    const addPathError = document.getElementById('add-path-error');

    let ws;
    let reconnectDelay = 1000;
    const maxReconnectDelay = 10000;

    let files = [];
    let folders = []; // followed folders (auto-add new files)
    let logs = [];
    let activeFile = null;

    function findFollowedFolder(path) {
        // case-insensitive on Windows; assume server already normalized
        return folders.find(f => f.path.toLowerCase() === path.toLowerCase());
    }
    let collapsedFolders = new Set();
    let changelogLoaded = false;

    // --- Deep links: the URL always mirrors the selected file (?file=<path>),
    // so any page state is copy-pasteable. Opening a link to an untracked file
    // auto-tracks it via /api/watch. ---
    let pendingUrlFile = new URLSearchParams(location.search).get('file');

    function syncUrl(path) {
        const url = path ? '/?file=' + encodeURIComponent(path) : '/';
        history.replaceState(null, '', url);
    }

    function showOpenError(path, msg) {
        content.innerHTML = `
            <div class="welcome">
                <h1 class="has-text-danger">Cannot open file</h1>
                <p><code>${escapeHtml(path)}</code></p>
                <p>${escapeHtml(msg)}</p>
            </div>
        `;
        updateContentHeader(null);
    }

    // addPath tracks a file via the API; if the path turns out to be a
    // directory, falls back to following it as a folder.
    function addPath(path) {
        return fetch('/api/watch', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: path, active: true }),
        }).then(r => {
            if (r.ok) return { ok: true };
            return r.text().then(msg => {
                if (msg.indexOf('is a directory') !== -1) {
                    return fetch('/api/folders', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ path: path }),
                    }).then(fr => fr.ok ? { ok: true, folder: true } : fr.text().then(fm => ({ ok: false, msg: fm })));
                }
                return { ok: false, msg: msg };
            });
        }).catch(err => ({ ok: false, msg: String(err) }));
    }

    // resolvePendingUrlFile is called on every files broadcast until the
    // deep-linked file is selected or adding it failed.
    function resolvePendingUrlFile() {
        if (!pendingUrlFile) return false;
        const match = files.find(f => f.path === pendingUrlFile && !f.deleted);
        if (match) {
            const target = pendingUrlFile;
            pendingUrlFile = null;
            selectFile(target);
            return true;
        }
        if (!resolvePendingUrlFile.tried) {
            resolvePendingUrlFile.tried = true;
            const target = pendingUrlFile;
            addPath(target).then(res => {
                if (!res.ok) {
                    pendingUrlFile = null;
                    showOpenError(target, res.msg || 'Could not track this path.');
                }
                // On success the server broadcasts a files update, which
                // re-enters resolvePendingUrlFile and selects the file.
            });
        }
        return true; // still resolving — suppress default first-file selection
    }

    function showAddPathError(msg) {
        addPathError.textContent = msg;
        addPathError.classList.remove('is-hidden');
    }

    function submitAddPath() {
        const path = addPathInput.value.trim();
        if (!path) return;
        addPathError.classList.add('is-hidden');
        addPath(path).then(res => {
            if (res.ok) {
                addPathInput.value = '';
                if (!res.folder) pendingUrlFile = pendingUrlFile || path;
            } else {
                showAddPathError(res.msg || 'Failed to add path');
            }
        });
    }

    addPathBtn.addEventListener('click', submitAddPath);
    addPathInput.addEventListener('keydown', e => {
        if (e.key === 'Enter') submitAddPath();
    });

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

    // File extension to Devicon class mapping
    const extIconMap = {
        '.go': 'devicon-go-original-wordmark colored',
        '.js': 'devicon-javascript-plain colored',
        '.ts': 'devicon-typescript-plain colored',
        '.jsx': 'devicon-react-original colored',
        '.tsx': 'devicon-react-original colored',
        '.py': 'devicon-python-plain colored',
        '.rb': 'devicon-ruby-plain colored',
        '.rs': 'devicon-rust-original',
        '.java': 'devicon-java-plain colored',
        '.cs': 'devicon-csharp-plain colored',
        '.html': 'devicon-html5-plain colored',
        '.htm': 'devicon-html5-plain colored',
        '.css': 'devicon-css3-plain colored',
        '.json': 'devicon-json-plain colored',
        '.yaml': 'devicon-yaml-plain colored',
        '.yml': 'devicon-yaml-plain colored',
        '.xml': 'devicon-xml-plain colored',
        '.svg': 'devicon-xml-plain colored',
        '.md': 'devicon-markdown-original',
        '.markdown': 'devicon-markdown-original',
        '.sh': 'devicon-bash-plain',
        '.bash': 'devicon-bash-plain',
        '.docker': 'devicon-docker-plain colored',
        '.dockerfile': 'devicon-docker-plain colored',
        '.swift': 'devicon-swift-plain colored',
        '.kt': 'devicon-kotlin-plain colored',
        '.dart': 'devicon-dart-plain colored',
        '.php': 'devicon-php-plain colored',
        '.lua': 'devicon-lua-plain colored',
        '.c': 'devicon-c-plain colored',
        '.h': 'devicon-c-plain colored',
        '.cpp': 'devicon-cplusplus-plain colored',
        '.hpp': 'devicon-cplusplus-plain colored',
        '.scala': 'devicon-scala-plain colored',
        '.ex': 'devicon-elixir-plain colored',
        '.exs': 'devicon-elixir-plain colored',
        '.erl': 'devicon-erlang-plain colored',
        '.hs': 'devicon-haskell-plain colored',
        '.toml': 'devicon-tomcat-line colored',
        '.vue': 'devicon-vuejs-plain colored',
        '.svelte': 'devicon-svelte-plain colored',
        '.tf': 'devicon-terraform-plain colored',
        '.sql': 'devicon-azuresqldatabase-plain colored',
        '.r': 'devicon-r-plain colored',
        '.razor': 'devicon-dotnetcore-plain colored',
    };

    const filenameIconMap = {
        'makefile': 'devicon-cmake-plain colored',
        'dockerfile': 'devicon-docker-plain colored',
        'go.mod': 'devicon-go-original-wordmark colored',
        'go.sum': 'devicon-go-original-wordmark colored',
        'package.json': 'devicon-nodejs-plain colored',
        'tsconfig.json': 'devicon-typescript-plain colored',
        '.gitignore': 'devicon-git-plain colored',
    };

    function getFileIconClass(filename) {
        const lower = filename.toLowerCase();
        if (filenameIconMap[lower]) return filenameIconMap[lower];
        const dot = lower.lastIndexOf('.');
        if (dot >= 0) {
            const ext = lower.slice(dot);
            if (extIconMap[ext]) return extIconMap[ext];
        }
        return '';
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
            const folderSvg = isCollapsed
                ? '<svg width="16" height="16" viewBox="0 0 16 16"><path d="M1.5 2h4l1 1h8a.5.5 0 0 1 .5.5v10a.5.5 0 0 1-.5.5h-13a.5.5 0 0 1-.5-.5v-11a.5.5 0 0 1 .5-.5z" fill="#c09553"/></svg>'
                : '<svg width="16" height="16" viewBox="0 0 16 16"><path d="M1.5 2h4l1 1h8a.5.5 0 0 1 .5.5V5H1V2.5a.5.5 0 0 1 .5-.5z" fill="#c09553"/><path d="M.5 5.5h14.5l-2 9H2z" fill="#dcb67a"/></svg>';

            const followed = findFollowedFolder(folder.path);
            const liveBadge = followed
                ? `<label class="folder-live" title="Auto-add new files dropped into this folder"><input type="checkbox" data-path="${escapeHtml(folder.path)}" class="folder-live-toggle" ${followed.live ? 'checked' : ''}><span>live</span></label>`
                : '';

            html += `
                <div class="tree-folder ${isCollapsed ? 'collapsed' : ''}" data-path="${escapeHtml(folder.path)}" style="padding-left: ${indent}px">
                    <span class="folder-toggle" data-path="${escapeHtml(folder.path)}">${chevron}</span>
                    <span class="folder-icon">${folderSvg}</span>
                    <span class="folder-name">${escapeHtml(folderName)}</span>
                    ${liveBadge}
                    <button class="folder-remove" data-path="${escapeHtml(folder.path)}" title="Remove folder from watch">&#10005;</button>
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
            const deletedClass = isDeleted ? 'deleted' : '';
            const stateClass = file.active ? 'watching' : 'registered';
            const iconClass = getFileIconClass(file.displayName);
            const iconHtml = iconClass ? `<i class="${iconClass}"></i>` : '<span class="file-icon-default">&#9679;</span>';

            html += `
                <div class="file-item tree-file ${file.path === activeFile ? 'active' : ''} ${stateClass} ${deletedClass}" data-path="${escapeHtml(file.path)}" style="padding-left: ${indent}px">
                    <button class="file-remove" data-path="${escapeHtml(file.path)}" title="Remove from watch">&#10005;</button>
                    <span class="file-icon">${iconHtml}</span>
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

        fileList.querySelectorAll('.folder-remove').forEach(btn => {
            btn.addEventListener('click', (e) => {
                e.stopPropagation();
                removeFolder(btn.dataset.path);
            });
        });

        fileList.querySelectorAll('.folder-live-toggle').forEach(cb => {
            cb.addEventListener('click', e => e.stopPropagation());
            cb.addEventListener('change', () => {
                toggleFolderLive(cb.dataset.path, cb.checked);
            });
        });

        fileList.querySelectorAll('.tree-folder').forEach(el => {
            el.addEventListener('click', () => {
                toggleFolder(el.dataset.path);
            });
        });
    }

    function toggleFolderLive(path, live) {
        fetch('/api/folders/toggle-live', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: path, live: live }),
        }).catch(err => {
            console.error('Failed to toggle live:', err);
        });
    }

    function removeFile(path) {
        fetch('/api/watch?path=' + encodeURIComponent(path), {
            method: 'DELETE'
        }).catch(err => {
            console.error('Failed to remove file:', err);
        });
    }

    function removeFolder(path) {
        fetch('/api/files/remove-folder?path=' + encodeURIComponent(path), {
            method: 'POST'
        }).catch(err => {
            console.error('Failed to remove folder:', err);
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

    // --- HTML view toggle: "Preview" renders the page in an iframe served from
    // /raw; "Source" shows the server's syntax-highlighted code view. ---
    const htmlViewModes = {}; // path -> 'preview' | 'source', remembered per file

    function isHtmlFile(path) {
        return /\.html?$/i.test(path || '');
    }

    function htmlViewMode(path) {
        return htmlViewModes[path] || 'preview';
    }

    function updateViewToggle(file) {
        if (file && isHtmlFile(file.path)) {
            viewToggle.classList.remove('is-hidden');
            const mode = htmlViewMode(file.path);
            viewPreviewBtn.classList.toggle('active', mode === 'preview');
            viewSourceBtn.classList.toggle('active', mode === 'source');
        } else {
            viewToggle.classList.add('is-hidden');
        }
    }

    // Single place that puts a file's content on screen. HTML files in preview
    // mode get an iframe (the timestamp query busts cache on live updates);
    // everything else uses the server-rendered HTML.
    function renderContent(file) {
        if (isHtmlFile(file.path) && htmlViewMode(file.path) === 'preview') {
            const bust = file.lastChange ? new Date(file.lastChange).getTime() : 0;
            const src = '/raw?path=' + encodeURIComponent(file.path) + '&t=' + bust;
            content.innerHTML = '<div class="html-preview"><iframe class="html-preview-frame" sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-modals" src="' + escapeHtml(src) + '"></iframe></div>';
        } else {
            content.innerHTML = file.html;
            enhanceContent(content);
        }
        updateViewToggle(file);
    }

    function setHtmlViewMode(mode) {
        if (!activeFile) return;
        htmlViewModes[activeFile] = mode;
        const file = files.find(f => f.path === activeFile);
        if (file && file.html) renderContent(file);
    }

    viewPreviewBtn.addEventListener('click', () => setHtmlViewMode('preview'));
    viewSourceBtn.addEventListener('click', () => setHtmlViewMode('source'));

    function updateContentHeader(file) {
        updateViewToggle(file);
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
        syncUrl(path);
        renderFileList();

        if (file && file.html) {
            renderContent(file);
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
                    folders = data.folders || [];
                    renderFileList();

                    if (resolvePendingUrlFile()) {
                        // deep-linked file selected (or still being tracked)
                    } else if (!activeFile && files.length > 0) {
                        const firstNonDeleted = files.find(f => !f.deleted);
                        if (firstNonDeleted) selectFile(firstNonDeleted.path);
                    } else if (activeFile) {
                        const file = files.find(f => f.path === activeFile);
                        if (file && file.html && !file.deleted) {
                            renderContent(file);
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
                            const scrollY = content.scrollTop;
                            renderContent(data.file);
                            content.scrollTop = scrollY;
                        }
                    }
                    break;

                case 'removed':
                    files = files.filter(f => f.path !== data.path);
                    renderFileList();

                    if (data.path === activeFile) {
                        activeFile = null;
                        syncUrl(null);
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

    // Sidebar resizer
    const sidebar = document.querySelector('.sidebar');
    const resizer = document.getElementById('sidebar-resizer');

    let isResizing = false;

    resizer.addEventListener('mousedown', (e) => {
        isResizing = true;
        document.body.classList.add('sidebar-resizing');
        resizer.classList.add('dragging');
        e.preventDefault();
    });

    document.addEventListener('mousemove', (e) => {
        if (!isResizing) return;
        const newWidth = e.clientX;
        if (newWidth >= 120 && newWidth <= 600) {
            sidebar.style.width = newWidth + 'px';
        }
    });

    document.addEventListener('mouseup', () => {
        if (!isResizing) return;
        isResizing = false;
        document.body.classList.remove('sidebar-resizing');
        resizer.classList.remove('dragging');
    });

    connect();
})();
