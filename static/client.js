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
    const trackBtn = document.getElementById('track-btn');
    const viewToggle = document.getElementById('view-toggle');
    const viewPreviewBtn = document.getElementById('view-preview-btn');
    const viewRawBtn = document.getElementById('view-raw-btn');
    const addPathInput = document.getElementById('add-path-input');
    const addPathBtn = document.getElementById('add-path-btn');
    const addPathError = document.getElementById('add-path-error');
    const copyBtn = document.getElementById('copy-btn');
    const contentSubheader = document.getElementById('content-subheader');
    const lineInfo = document.getElementById('line-info');

    let ws;
    let reconnectDelay = 1000;
    const maxReconnectDelay = 10000;

    let files = [];
    let folders = []; // followed folders (auto-add new files)
    let logs = [];
    let activeFile = null;
    // The untracked file currently on screen, if any: reached by following a
    // markdown link to a neighbour, rendered without joining the watch list.
    // At most one at a time — it exists only as long as it is being viewed.
    let ephemeral = null;

    function findFollowedFolder(path) {
        // case-insensitive on Windows; assume server already normalized
        return folders.find(f => f.path.toLowerCase() === path.toLowerCase());
    }
    let collapsedFolders = new Set();
    // Outcome of the most recent folder refresh: { path, text, busy, timer }.
    let refreshNote = null;
    let changelogLoaded = false;

    // --- Deep links: the address bar *is* the file's path, so
    // http://localhost:3000/home/me/notes.md is equally what you get after
    // clicking a link and what you can paste in from a terminal. ?file= is
    // still honoured for links saved before the switch. ---
    const winPathRe = /^[A-Za-z]:[\\/]/;

    // pathsEqual mirrors the daemon's comparison: separators normalized, and
    // case ignored on Windows only — two files on Linux may differ by case
    // alone.
    function pathsEqual(a, b) {
        if (!a || !b) return a === b;
        let na = a.replace(/\\/g, '/');
        let nb = b.replace(/\\/g, '/');
        if (winPathRe.test(a) || winPathRe.test(b)) {
            na = na.toLowerCase();
            nb = nb.toLowerCase();
        }
        return na === nb;
    }

    // encodeSegment escapes one path segment, then puts back the characters a
    // URL path may legally carry — chiefly the colon, so a Windows drive stays
    // readable as /C:/Users/... rather than /C%3A/Users/...
    function encodeSegment(seg) {
        return encodeURIComponent(seg).replace(/%(3A|40|26|3D|2B|24|2C)/gi, m => decodeURIComponent(m));
    }

    function pathToUrl(path, hash) {
        if (!path) return '/';
        let slashed = path.replace(/\\/g, '/');
        if (slashed[0] !== '/') slashed = '/' + slashed; // C:/Users/… → /C:/Users/…
        let url = slashed.split('/').map(encodeSegment).join('/');
        if (hasTwoViews(path) && viewMode(path) === 'raw') url += '?view=raw';
        // Keep the heading a link aimed at, so copying the URL copies the spot.
        if (hash) url += '#' + encodeURIComponent(hash);
        return url;
    }

    function urlToPath(pathname) {
        let p = pathname;
        try {
            p = decodeURIComponent(pathname);
        } catch (e) {
            /* hand-typed URL with a stray % — take it literally */
        }
        if (!p || p === '/') return null;
        if (/^\/[A-Za-z]:/.test(p)) p = p.slice(1); // drop the URL's leading slash
        return p;
    }

    const initialParams = new URLSearchParams(location.search);
    let pendingUrlFile = initialParams.get('file') || urlToPath(location.pathname);
    const pendingUrlView = initialParams.get('view');
    // Consumed by the next render, so /doc.md#install lands on the heading.
    let pendingHash = location.hash ? location.hash.slice(1) : '';
    // True while a URL-supplied path is still being resolved, so the default
    // "select the first file" never races ahead of it.
    let urlResolving = false;
    // Whether the pending selection completes a navigation the reader started
    // (tracking a file from a link) or just restores the URL this page loaded
    // with. The first earns a history entry; the second would duplicate one.
    let pendingUrlPush = false;
    // Set while the content area shows something other than a file — an error,
    // the welcome screen — so a metadata broadcast doesn't quietly paint the
    // previous document back over it.
    let contentOverride = false;

    // syncUrl mirrors the selected file in the address bar. Navigation pushes a
    // history entry so Back returns to the previous document; a change to the
    // same file (the Preview/Raw toggle) replaces it, or Back would walk
    // backwards through view flips instead of through documents.
    function syncUrl(path, push, hash) {
        const url = pathToUrl(path, hash);
        if (push && url !== location.pathname + location.search) {
            history.pushState({ path: path }, '', url);
        } else {
            history.replaceState({ path: path }, '', url);
        }
    }

    // Back, Forward, and the mouse's side buttons all land here. The URL is the
    // source of truth: re-open whatever file it names, without pushing again.
    window.addEventListener('popstate', () => {
        const params = new URLSearchParams(location.search);
        const path = urlToPath(location.pathname) || params.get('file');
        if (!path) {
            activeFile = null;
            ephemeral = null;
            showWelcome();
            renderFileList();
            return;
        }
        if (params.get('view') === 'raw') {
            viewModes[path] = 'raw';
        } else {
            delete viewModes[path];
        }
        saveViewModes();
        openPath(path, { push: false, hash: location.hash.slice(1) });
    });

    function showOpenError(path, msg) {
        contentOverride = true;
        setViewKind('document');
        content.innerHTML = `
            <div class="welcome">
                <h1 class="has-text-danger">Cannot open file</h1>
                <p><code>${escapeHtml(path)}</code></p>
                <p>${escapeHtml(msg)}</p>
            </div>
        `;
        updateContentHeader(null);
    }

    function showWelcome() {
        contentOverride = true;
        setViewKind('document');
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

    // showOutsideRoots is the answer to a link that points somewhere livemd
    // will not read on its own. Tracking it is a deliberate act, so it gets a
    // button rather than happening behind the reader's back.
    function showOutsideRoots(path) {
        contentOverride = true;
        setViewKind('document');
        content.innerHTML = `
            <div class="welcome">
                <h1 class="has-text-danger">Outside the tracked paths</h1>
                <p><code>${escapeHtml(path)}</code></p>
                <p>Links open files inside a folder you follow, or beside a file you track.
                   This one is somewhere else.</p>
                <p><button class="button" id="track-anyway-btn">Track this file</button></p>
            </div>
        `;
        updateContentHeader(null);
        const btn = document.getElementById('track-anyway-btn');
        if (btn) btn.addEventListener('click', () => trackPath(path));
    }

    // trackPath promotes a path into the watch list — the one action that grows
    // it — and selects the file once the server broadcasts it back.
    function trackPath(path) {
        addPath(path).then(res => {
            if (!res.ok) {
                showOpenError(path, res.msg || 'Could not track this path.');
                return;
            }
            if (!res.folder) {
                pendingUrlFile = path;
                pendingUrlPush = true;
                resolvePendingUrlFile.tried = false;
            }
        });
    }

    // fileFor resolves a path to something renderable: a tracked file, or the
    // untracked neighbour currently on screen.
    function fileFor(path) {
        const tracked = files.find(f => pathsEqual(f.path, path));
        if (tracked) return tracked;
        if (ephemeral && pathsEqual(ephemeral.path, path)) return ephemeral;
        return null;
    }

    function baseName(path) {
        return path.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || path;
    }

    // openPath is the single way a file reaches the screen, whoever asked — the
    // sidebar, a markdown link, a pasted URL, Back. A tracked file is shown
    // straight away; anything else is offered to the server first, which
    // renders it only if it sits inside a tracked root. That is what lets a
    // link to a sibling document work without the watch list quietly growing.
    // opts.onMissing overrides the refusal message (the URL bar uses it to fall
    // back to tracking, since typing a path is itself a request to open it).
    function openPath(path, opts) {
        opts = opts || {};
        const tracked = files.find(f => pathsEqual(f.path, path));
        if (tracked) {
            selectFile(tracked.path, opts);
            return;
        }

        const mode = effectiveMode(path);
        const url = '/api/render?path=' + encodeURIComponent(path) + (mode === 'raw' ? '&mode=raw' : '');
        urlResolving = true;
        fetch(url)
            .then(r => r.ok ? r.json() : Promise.reject(new Error('HTTP ' + r.status)))
            .then(d => {
                urlResolving = false;
                const actual = d.path || path;
                // The render is already in hand — seed the cache so selectFile
                // shows it without a second round trip.
                cachePut(cacheKey(actual, mode), { html: d.html, view: d.view });
                ephemeral = { path: actual, name: baseName(actual), untracked: true };
                selectFile(actual, opts);
            })
            .catch(() => {
                if (!opts.onMissing) {
                    urlResolving = false;
                    showOutsideRoots(path);
                    return;
                }
                // Stay "resolving" until the fallback settles, so the default
                // first-file selection doesn't slip in and steal the view.
                const done = opts.onMissing();
                if (done && done.then) done.then(clear, clear);
                else clear();
                function clear() { urlResolving = false; }
            });
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

    // applyPendingView lets a ?view= in the link win over the remembered
    // preference for that file.
    function applyPendingView(path) {
        if (pendingUrlView !== 'raw' && pendingUrlView !== 'preview') return;
        if (pendingUrlView === 'raw') {
            viewModes[path] = 'raw';
        } else {
            delete viewModes[path];
        }
        saveViewModes();
    }

    // resolvePendingUrlFile is called on every files broadcast until the
    // deep-linked file is on screen or opening it failed. Returns true while
    // it is still working, so the default first-file selection stays out of
    // the way.
    function resolvePendingUrlFile() {
        if (!pendingUrlFile) return urlResolving;

        const match = files.find(f => pathsEqual(f.path, pendingUrlFile) && !f.deleted);
        if (match) {
            pendingUrlFile = null;
            applyPendingView(match.path);
            const push = pendingUrlPush;
            pendingUrlPush = false;
            selectFile(match.path, { push: push, hash: pendingHash });
            pendingHash = '';
            return true;
        }

        if (!resolvePendingUrlFile.tried) {
            resolvePendingUrlFile.tried = true;
            const target = pendingUrlFile;
            pendingUrlFile = null;
            applyPendingView(target);
            const hash = pendingHash;
            pendingHash = '';
            // A path typed or pasted into the address bar is a request to open
            // that path, so when it falls outside every tracked root this does
            // what the Add box would: tracks the file, or follows it if it
            // turns out to be a directory. Clicking a link never does this —
            // reading a document is not consent to grow the watch list.
            openPath(target, {
                push: false,
                hash: hash,
                onMissing: () => addPath(target).then(res => {
                    if (!res.ok) {
                        showOpenError(target, res.msg || 'Could not open this path.');
                        return;
                    }
                    // A file comes back through the branch above once the
                    // server broadcasts it. A directory has nothing to select,
                    // so the default first-file pick takes over.
                    if (!res.folder) {
                        pendingUrlFile = target;
                        resolvePendingUrlFile.tried = false;
                    }
                }),
            });
        }
        return true;
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
    document.querySelectorAll('.sidebar-tabs li').forEach(li => {
        li.addEventListener('click', () => {
            document.querySelectorAll('.sidebar-tabs li').forEach(l => l.classList.remove('is-active'));
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

    // compactChains folds a folder holding nothing but one subfolder into a
    // single row — "skills/aspire/references" instead of three levels of
    // corridor. Real trees are mostly corridor, and each level costs both a row
    // and 12px of indent to say nothing.
    //
    // A followed folder is never folded away: it owns a Refresh button and has
    // to keep a row of its own.
    function compactChains(node) {
        for (const name of Object.keys(node.children)) {
            const original = node.children[name];
            compactChains(original); // depth first: fold the tail before the head
            let label = original.name;
            let deepest = original;
            while (deepest.files.length === 0 &&
                   Object.keys(deepest.children).length === 1 &&
                   !findFollowedFolder(deepest.path)) {
                const only = deepest.children[Object.keys(deepest.children)[0]];
                label += '/' + only.name;
                deepest = only;
            }
            if (deepest !== original) {
                delete node.children[name];
                // The row stands for the deepest folder — that is the path its
                // collapse state, Refresh and Remove all act on.
                node.children[label] = Object.assign({}, deepest, { name: label });
            }
        }
        return node;
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

    // folderRefreshControl renders the Refresh button for a followed folder, plus
    // the result of the last refresh while it is still fresh. Returns nothing for
    // a directory that is merely part of a path — only followed folders can be
    // re-walked.
    function folderRefreshControl(path) {
        if (!findFollowedFolder(path)) return '';
        const noted = refreshNote && pathsEqual(refreshNote.path, path);
        const note = noted ? `<span class="folder-refresh-note">${escapeHtml(refreshNote.text)}</span>` : '';
        return `<button class="folder-refresh" data-path="${escapeHtml(path)}" title="Look for files added to this folder since it was followed"${noted && refreshNote.busy ? ' disabled' : ''}>&#8635;</button>${note}`;
    }

    // collectFolderPaths lists every directory the tree will draw a row for, so
    // renderFileList can spot a followed folder that would otherwise have none.
    function collectFolderPaths(node, out) {
        for (const name of Object.keys(node.children)) {
            const child = node.children[name];
            out.push(child.path);
            collectFolderPaths(child, out);
        }
        return out;
    }

    // folderLabel dims the corridor part of a compacted chain, so the eye lands
    // on the folder the row actually stands for.
    // folderLabel dims the corridor part of a compacted chain and, when the row
    // is too narrow, sacrifices the corridor rather than the leaf: the ellipsis
    // belongs in "…/planets/sharp-skills", never in "Desktop/projects/alte…",
    // which hides the one segment that identifies the row.
    function folderLabel(name) {
        const parts = name.split('/');
        const leaf = parts.pop();
        // <bdi> isolates the path text so the rtl trick in the stylesheet moves
        // only the ellipsis — without it ".claude/skills" reorders to
        // "/claude.skills", because "." and "/" take their direction from
        // whatever surrounds them.
        const lead = parts.length ? `<span class="path-lead"><bdi>${escapeHtml(parts.join('/'))}/</bdi></span>` : '';
        return lead + `<span class="path-leaf">${escapeHtml(leaf)}</span>`;
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

            const refreshControl = folderRefreshControl(folder.path);

            html += `
                <div class="tree-folder ${isCollapsed ? 'collapsed' : ''}" data-path="${escapeHtml(folder.path)}" style="padding-left: ${indent}px">
                    <span class="folder-toggle" data-path="${escapeHtml(folder.path)}">${chevron}</span>
                    <span class="folder-icon">${folderSvg}</span>
                    <span class="folder-name" title="${escapeHtml(folder.path)}">${folderLabel(folderName)}</span>
                    ${refreshControl}
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
                    <span class="file-icon">${iconHtml}</span>
                    <span class="file-name" title="${escapeHtml(file.path)}">${escapeHtml(file.displayName)}</span>
                    <button class="file-remove" data-path="${escapeHtml(file.path)}" title="Remove from watch">&#10005;</button>
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
        const tree = compactChains(buildTree(files, commonPrefix));

        let html = '';
        if (commonPrefix) {
            const rootName = commonPrefix.split('/').pop() || commonPrefix;
            html += `<div class="tree-root" title="${escapeHtml(commonPrefix)}"><span class="root-name">${escapeHtml(rootName)}</span>${folderRefreshControl(commonPrefix)}</div>`;
        }

        // A followed folder that contributes no files gets no row from the tree
        // — and with it no way to ask for a refresh, which is the only way its
        // files would ever appear. Give it one of its own.
        const drawn = collectFolderPaths(tree, commonPrefix ? [commonPrefix] : []);
        for (const folder of folders) {
            if (drawn.some(p => pathsEqual(p, folder.path))) continue;
            const name = folder.path.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || folder.path;
            html += `
                <div class="tree-folder is-empty" title="${escapeHtml(folder.path)}">
                    <span class="folder-toggle"></span>
                    <span class="folder-icon"><svg width="16" height="16" viewBox="0 0 16 16"><path d="M1.5 2h4l1 1h8a.5.5 0 0 1 .5.5v10a.5.5 0 0 1-.5.5h-13a.5.5 0 0 1-.5-.5v-11a.5.5 0 0 1 .5-.5z" fill="#c09553"/></svg></span>
                    <span class="folder-name">${escapeHtml(name)}</span>
                    ${folderRefreshControl(folder.path)}
                    <button class="folder-remove" data-path="${escapeHtml(folder.path)}" title="Remove folder from watch">&#10005;</button>
                </div>
            `;
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

        fileList.querySelectorAll('.folder-refresh').forEach(btn => {
            btn.addEventListener('click', e => {
                e.stopPropagation(); // don't collapse the folder as well
                refreshFolder(btn.dataset.path);
            });
        });

        fileList.querySelectorAll('.tree-folder').forEach(el => {
            el.addEventListener('click', () => {
                toggleFolder(el.dataset.path);
            });
        });
    }

    // refreshFolder asks the daemon to walk a followed folder again and register
    // whatever has appeared since. The result has to outlive the re-render that
    // the refresh itself triggers, so it lives in state keyed by folder path
    // rather than on the button element, which is replaced by then.
    function refreshFolder(path) {
        setRefreshNote(path, '…', true);
        fetch('/api/folders/refresh', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: path }),
        })
            .then(r => r.ok ? r.json() : Promise.reject(new Error('HTTP ' + r.status)))
            .then(d => setRefreshNote(path, d.added ? '+' + d.added : 'nothing new', false))
            .catch(err => {
                console.error('Failed to refresh folder:', err);
                setRefreshNote(path, 'failed', false);
            });
    }

    function setRefreshNote(path, text, busy) {
        if (refreshNote) clearTimeout(refreshNote.timer);
        refreshNote = { path: path, text: text, busy: busy };
        if (!busy) {
            refreshNote.timer = setTimeout(() => {
                if (refreshNote && refreshNote.path === path) {
                    refreshNote = null;
                    renderFileList();
                }
            }, 2500);
        }
        renderFileList();
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

    // --- Preview/Raw toggle. "Preview" is the rendered document (goldmark for
    // markdown, a sandboxed iframe for HTML); "Raw" is the server's
    // syntax-highlighted source. The choice is remembered per file in
    // localStorage, and a ?view= parameter in the URL overrides it. ---
    const VIEW_STORE_KEY = 'livemd:viewModes';

    function loadViewModes() {
        try {
            return JSON.parse(localStorage.getItem(VIEW_STORE_KEY) || '{}') || {};
        } catch (e) {
            return {};
        }
    }

    // Only non-default ('raw') entries are stored, so this stays small and
    // prunes itself as files are flipped back to Preview.
    const viewModes = loadViewModes();

    function saveViewModes() {
        try {
            localStorage.setItem(VIEW_STORE_KEY, JSON.stringify(viewModes));
        } catch (e) {
            /* private mode / quota — the toggle still works for this session */
        }
    }

    function isHtmlFile(path) {
        return /\.html?$/i.test(path || '');
    }

    function isMarkdownFile(path) {
        return /\.(md|markdown|mdown|mkd)$/i.test(path || '');
    }

    // File types with both a rendered and a source view.
    function hasTwoViews(path) {
        return isHtmlFile(path) || isMarkdownFile(path);
    }

    function viewMode(path) {
        return viewModes[path] === 'raw' ? 'raw' : 'preview';
    }

    function updateViewToggle(file) {
        if (file && hasTwoViews(file.path)) {
            viewToggle.classList.remove('is-hidden');
            const mode = viewMode(file.path);
            viewPreviewBtn.classList.toggle('active', mode === 'preview');
            viewRawBtn.classList.toggle('active', mode === 'raw');
        } else {
            viewToggle.classList.add('is-hidden');
        }
    }

    // --- Content is fetched per file on demand. The WebSocket carries only
    // metadata, so the daemon never holds a render in memory and a 40MB file
    // costs nothing until you actually open it. ---
    const CACHE_LIMIT = 10;
    const contentCache = new Map(); // "path|mode" -> {html, view}, insertion-ordered

    function cacheKey(path, mode) {
        return path + '|' + mode;
    }

    function cachePut(key, entry) {
        contentCache.set(key, entry);
        while (contentCache.size > CACHE_LIMIT) {
            contentCache.delete(contentCache.keys().next().value);
        }
    }

    function invalidateCache(path) {
        for (const key of [...contentCache.keys()]) {
            if (key.slice(0, key.lastIndexOf('|')) === path) contentCache.delete(key);
        }
    }

    // fetchAndShow renders the given file+mode into the content area. Raw view
    // deliberately skips enhanceContent: mermaid and KaTeX must not run over
    // source text, or a $$...$$ block in raw markdown would be silently
    // replaced by rendered math.
    function fetchAndShow(file, mode, keepScroll) {
        const key = cacheKey(file.path, mode);
        const stale = () => file.path !== activeFile || effectiveMode(file.path) !== mode;

        const apply = (html, view) => {
            if (stale()) return;
            const scrollY = keepScroll ? content.scrollTop : 0;
            setViewKind(view);
            content.innerHTML = html;
            if (mode !== 'raw') enhanceContent(content);
            content.scrollTop = scrollY;
            updateSubheader(file);
            updateViewToggle(file);
            if (!keepScroll) consumePendingHash();
        };

        if (contentCache.has(key)) {
            const hit = contentCache.get(key);
            apply(hit.html, hit.view);
            return;
        }

        // Only show a placeholder if the fetch is slow enough to notice,
        // so small files don't flash.
        const spinner = setTimeout(() => {
            if (stale()) return;
            setViewKind('document');
            content.innerHTML = '<div class="loading-state">Loading ' + escapeHtml(file.name) + '…</div>';
        }, 150);

        const url = '/api/render?path=' + encodeURIComponent(file.path) + (mode === 'raw' ? '&mode=raw' : '');
        fetch(url)
            .then(r => {
                if (!r.ok) throw new Error('HTTP ' + r.status);
                return r.json();
            })
            .then(d => {
                clearTimeout(spinner);
                cachePut(key, { html: d.html, view: d.view });
                apply(d.html, d.view);
            })
            .catch(err => {
                clearTimeout(spinner);
                console.error('Failed to render:', err);
                if (!stale()) showOpenError(file.path, 'Could not render this file.');
            });
    }

    // setViewKind tells the stylesheet what it is laying out. Only "document"
    // gets the centred reading column; source dumps, tables and media take the
    // whole pane, because narrowing them destroys them.
    function setViewKind(view) {
        content.className = 'content view-' + (view || 'document');
    }

    // effectiveMode collapses the view choice to what actually gets fetched:
    // HTML preview is an iframe, not a render, so it never hits the cache.
    function effectiveMode(path) {
        return hasTwoViews(path) && viewMode(path) === 'raw' ? 'raw' : 'preview';
    }

    // currentLineTotal reads the invisible marker the server embeds in source
    // renders; null for viewers without line counts (markdown preview, media).
    function currentLineTotal() {
        const marker = content.querySelector('.line-info');
        if (!marker) return null;
        const total = parseInt(marker.dataset.total, 10);
        return isNaN(total) ? null : total;
    }

    // --- Copy button: fetches the raw file and puts it on the clipboard.
    // Hidden for media files, where "copy the bytes" makes no sense. ---
    const mediaExtRe = /\.(png|jpe?g|gif|webp|bmp|ico|avif|svg|pdf|mp3|wav|ogg|oga|m4a|flac|aac|opus|mp4|webm|mov|mkv|m4v)$/i;

    function copyText(text) {
        if (navigator.clipboard && window.isSecureContext) {
            return navigator.clipboard.writeText(text);
        }
        // Fallback for non-secure contexts (e.g. viewing over LAN IP).
        return new Promise((resolve, reject) => {
            const ta = document.createElement('textarea');
            ta.value = text;
            ta.style.position = 'fixed';
            ta.style.opacity = '0';
            document.body.appendChild(ta);
            ta.select();
            const ok = document.execCommand('copy');
            document.body.removeChild(ta);
            ok ? resolve() : reject(new Error('execCommand copy failed'));
        });
    }

    copyBtn.addEventListener('click', () => {
        if (!activeFile) return;
        fetch('/raw?path=' + encodeURIComponent(activeFile))
            .then(r => r.text())
            .then(copyText)
            .then(() => {
                copyBtn.textContent = 'Copied!';
                copyBtn.classList.add('copied');
            })
            .catch(() => {
                copyBtn.textContent = 'Failed';
            })
            .finally(() => {
                setTimeout(() => {
                    copyBtn.textContent = 'Copy';
                    copyBtn.classList.remove('copied');
                }, 1500);
            });
    });

    // updateSubheader keeps the second header row in sync: the line count where
    // one is meaningful, plus Copy for anything text-based. Hidden entirely for
    // media files and the welcome screen.
    function updateSubheader(file) {
        if (!file || mediaExtRe.test(file.path)) {
            contentSubheader.classList.add('is-hidden');
            return;
        }
        contentSubheader.classList.remove('is-hidden');
        const total = currentLineTotal();
        lineInfo.textContent = total ? total.toLocaleString() + ' lines' : '';
    }

    // --- In-document links. The server rewrites markdown destinations into
    // real URLs for this app, so a plain click would already load the right
    // page — but as a full navigation, throwing away the WebSocket and the
    // file list to rebuild them a moment later. Intercept the ordinary click
    // and route it through the SPA; leave modified clicks to the browser,
    // where ctrl/cmd/middle-click opens the file in a new tab precisely
    // because the href is genuine. ---

    // consumePendingHash scrolls to the heading a #fragment named, once the
    // document it belongs to is on screen.
    function consumePendingHash() {
        const hash = pendingHash;
        pendingHash = '';
        if (!hash) return;
        scrollToAnchor(hash);
    }

    function scrollToAnchor(id) {
        let el = null;
        try {
            el = content.querySelector('#' + CSS.escape(id));
        } catch (e) {
            el = document.getElementById(id);
        }
        if (el) el.scrollIntoView();
    }

    content.addEventListener('click', e => {
        if (e.defaultPrevented || e.button !== 0) return;
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
        const anchor = e.target.closest && e.target.closest('a[href]');
        if (!anchor || anchor.target === '_blank') return;

        let url;
        try {
            url = new URL(anchor.getAttribute('href'), location.href);
        } catch (err) {
            return;
        }
        if (url.origin !== location.origin) return;
        if (url.pathname === '/raw') return; // a direct fetch of the bytes

        e.preventDefault();

        // A bare #fragment is a jump inside the open document, not a
        // navigation — no history entry, no re-render.
        if (url.pathname === location.pathname && url.hash) {
            scrollToAnchor(url.hash.slice(1));
            return;
        }

        const path = urlToPath(url.pathname);
        if (!path) return;
        if (url.searchParams.get('view') === 'raw') {
            viewModes[path] = 'raw';
            saveViewModes();
        }
        openPath(path, { push: true, hash: url.hash.slice(1) });
    });

    // Ctrl+A selects only the rendered content, not the whole page chrome.
    document.addEventListener('keydown', e => {
        if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'a') {
            const t = e.target;
            if (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable) return;
            e.preventDefault();
            const range = document.createRange();
            range.selectNodeContents(content);
            const sel = window.getSelection();
            sel.removeAllRanges();
            sel.addRange(range);
        }
    });

    // Single place that puts a file's content on screen. Raw view fetches the
    // source render; HTML preview gets a sandboxed iframe (the timestamp query
    // busts cache on live updates); everything else uses the HTML the server
    // already broadcast.
    function renderContent(file, keepScroll) {
        // HTML in preview mode is the file itself in a sandboxed iframe — no
        // render needed, the browser loads it straight from /raw.
        if (isHtmlFile(file.path) && viewMode(file.path) === 'preview') {
            const bust = file.lastChange ? new Date(file.lastChange).getTime() : 0;
            const src = '/raw?path=' + encodeURIComponent(file.path) + '&t=' + bust;
            setViewKind('media');
            content.innerHTML = '<div class="html-preview"><iframe class="html-preview-frame" sandbox="allow-scripts allow-same-origin allow-forms allow-popups allow-modals" src="' + escapeHtml(src) + '"></iframe></div>';
            updateViewToggle(file);
            updateSubheader(file);
            pendingHash = ''; // nothing on this page for a fragment to find
            return;
        }
        fetchAndShow(file, effectiveMode(file.path), keepScroll);
    }

    function setViewMode(mode) {
        if (!activeFile) return;
        if (mode === 'raw') {
            viewModes[activeFile] = 'raw';
        } else {
            delete viewModes[activeFile];
        }
        saveViewModes();
        // Same document, different view: replace the history entry rather than
        // push one, so Back steps between files instead of view flips.
        syncUrl(activeFile, false);
        const file = fileFor(activeFile);
        if (file) renderContent(file);
    }

    viewPreviewBtn.addEventListener('click', () => setViewMode('preview'));
    viewRawBtn.addEventListener('click', () => setViewMode('raw'));

    function updateContentHeader(file) {
        updateViewToggle(file);
        updateSubheader(file);
        trackBtn.classList.toggle('is-hidden', !file || !file.untracked);
        if (file) {
            contentHeaderFilename.textContent = file.name;
            contentHeaderPath.textContent = file.path;
            if (file.untracked) {
                // Nothing is watching this file — say so, since the whole
                // promise of the app is that the page follows the file.
                contentHeaderChanged.textContent = 'Not tracked — no live reload';
            } else {
                contentHeaderChanged.textContent = file.lastChange ? 'Changed: ' + formatShortDateTime(file.lastChange) : '';
            }
        } else {
            contentHeaderFilename.textContent = 'No file selected';
            contentHeaderPath.textContent = '';
            contentHeaderChanged.textContent = '';
        }
    }

    trackBtn.addEventListener('click', () => {
        if (activeFile) trackPath(activeFile);
    });

    function selectFile(path, opts) {
        opts = opts || {};
        const file = fileFor(path);
        if (file && file.deleted) return; // Can't select deleted files

        contentOverride = false;
        const previousFile = activeFile;
        const previousTracked = previousFile && files.some(f => pathsEqual(f.path, previousFile));
        activeFile = path;
        if (!file || !file.untracked) ephemeral = null; // left the untracked view
        pendingHash = opts.hash || '';
        syncUrl(path, opts.push !== false && path !== previousFile, opts.hash);
        renderFileList();

        if (file) {
            renderContent(file);
            document.title = file.name + ' - LiveMD';
            updateContentHeader(file);
        }

        // Watching is a property of tracked files; an untracked neighbour has
        // no watcher on the daemon side, so there is nothing to activate.
        if (path !== previousFile && file && !file.untracked) {
            activateFile(path);
        }

        if (previousFile && previousFile !== path && previousTracked) {
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
            status.className = 'tag is-success';
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
                        // Landing on the first file isn't navigation the
                        // reader did, so it replaces the entry rather than
                        // pushing one Back would have to step through.
                        const firstNonDeleted = files.find(f => !f.deleted);
                        if (firstNonDeleted) selectFile(firstNonDeleted.path, { push: false });
                    } else if (activeFile) {
                        const file = files.find(f => pathsEqual(f.path, activeFile));
                        if (file && file.untracked !== true && ephemeral && pathsEqual(ephemeral.path, activeFile)) {
                            // The untracked file on screen just joined the
                            // watch list (Track, or a folder picked it up).
                            ephemeral = null;
                            selectFile(file.path, { push: false });
                        } else if (file && !file.deleted && !contentOverride) {
                            // Metadata-only broadcast (another file was added,
                            // activated, ...) — served from cache, and
                            // keepScroll stops it jumping to the top.
                            renderContent(file, true);
                            updateContentHeader(file);
                        } else if (file && file.deleted) {
                            contentOverride = true;
                            setViewKind('document');
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
                        invalidateCache(data.file.path); // stale after an edit
                        const idx = files.findIndex(f => f.path === data.file.path);
                        if (idx >= 0) {
                            files[idx] = data.file;
                        } else {
                            files.push(data.file);
                        }
                        renderFileList();

                        if (data.file.path === activeFile) {
                            // Scroll is restored inside the fetch callback.
                            renderContent(data.file, true);
                        }
                    }
                    break;

                case 'removed':
                    files = files.filter(f => f.path !== data.path);
                    renderFileList();

                    if (data.path === activeFile) {
                        activeFile = null;
                        ephemeral = null;
                        syncUrl(null, false);
                        const remaining = files.filter(f => !f.deleted);
                        if (remaining.length > 0) {
                            selectFile(remaining[0].path, { push: false });
                        } else {
                            showWelcome();
                        }
                    }
                    break;
            }
        };

        ws.onclose = function() {
            status.textContent = 'disconnected';
            status.className = 'tag is-danger';

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
