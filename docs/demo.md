# LiveMD Feature Demo

This file exercises every rendering feature. If something looks wrong here, it's broken.

---

## 1. Text basics

**Bold**, *italic*, ***bold italic***, ~~strikethrough~~, `inline code`, and a [link](https://github.com/erkantaylan/livemd).
Autolink test: https://example.com and www.example.com

> Blockquote level 1
> > Nested blockquote
> > > Level 3 with **bold** and `code`

Hard wrap test: this line
should break here (WithHardWraps is on).

## 2. Headings

### H3
#### H4
##### H5
###### H6

## 3. Lists

1. Ordered one
2. Ordered two
   1. Nested ordered
   2. Another
      - Mixed unordered inside ordered
      - Deeper
        - [ ] Task inside a 4-level nest
- Unordered
- With items
  - Nested

### Task lists

- [x] Done item
- [ ] Pending item
- [ ] ~~Cancelled item~~

## 4. Tables

| Feature | Status | Notes |
|---------|:------:|------:|
| GFM tables | ✔ | center + right aligned columns |
| Long cell | ✔ | This is a much longer cell to test wrapping behavior of wide-ish content in tables |
| `code in cell` | ✔ | **bold in cell** |

Wide table (horizontal scroll test):

| A | B | C | D | E | F | G | H | I | J | K | L |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 | 11 | 12 |

## 5. Code blocks

```go
// Go with syntax highlighting
func main() {
	msg := fmt.Sprintf("hello %s", "livemd")
	fmt.Println(msg)
}
```

```python
# Python
def fib(n: int) -> int:
    return n if n < 2 else fib(n - 1) + fib(n - 2)
```

```json
{ "name": "livemd", "tags": ["markdown", "live"], "port": 3001 }
```

```
plain fenced block with no language
<html> entities & such should be escaped literally
```

## 6. Math (KaTeX)

Inline: $e^{i\pi} + 1 = 0$ and $\frac{a}{b} \neq \sqrt{c}$

Display:

$$
\int_{-\infty}^{\infty} e^{-x^2}\,dx = \sqrt{\pi}
$$

$$
\begin{pmatrix} a & b \\ c & d \end{pmatrix}
\begin{pmatrix} x \\ y \end{pmatrix}
=
\begin{pmatrix} ax + by \\ cx + dy \end{pmatrix}
$$

## 7. Inline HTML (unsafe mode is on)

<details>
<summary>Click to expand a &lt;details&gt; block</summary>

Hidden content with **markdown** inside.

</details>

<kbd>Ctrl</kbd> + <kbd>C</kbd> — <mark>highlighted</mark> — <sub>sub</sub> and <sup>sup</sup>

## 8. Mermaid — one of each type

### 8.1 Flowchart

```mermaid
graph TD
    A[CLI: livemd add] --> B{File type?}
    B -->|markdown| C[goldmark]
    B -->|code| D[chroma]
    B -->|media| E["/raw endpoint"]
    C --> F[Browser]
    D --> F
    E --> F
    F -->|websocket| G((Live updates))
```

### 8.2 Sequence diagram

```mermaid
sequenceDiagram
    autonumber
    participant CLI
    participant Daemon
    participant Browser
    CLI->>Daemon: POST /api/watch
    Daemon->>Daemon: render file
    Daemon-->>Browser: ws: files
    Browser->>Daemon: POST /api/files/activate
    Note over Daemon: fsnotify watcher starts
    loop on file change
        Daemon-->>Browser: ws: update
    end
```

### 8.3 Class diagram

```mermaid
classDiagram
    class Hub {
        +map files
        +map watchers
        +AddFile(path) error
        +RemoveFile(path) error
    }
    class Renderer {
        +Render(path) string
    }
    class Watcher {
        +Watch(path, onChange, onDelete)
        +Close()
    }
    Hub --> Renderer : uses
    Hub "1" --> "*" Watcher : manages
```

### 8.4 State diagram

```mermaid
stateDiagram-v2
    [*] --> Registered : livemd add
    Registered --> Watching : selected in browser
    Watching --> Registered : deselected
    Watching --> Deleted : file removed from disk
    Deleted --> Watching : file reappears
    Registered --> [*] : livemd remove
```

### 8.5 Entity relationship

```mermaid
erDiagram
    STATE ||--o{ FILE : contains
    STATE ||--o{ FOLDER : contains
    FOLDER ||--o{ FILE : discovers
    FILE {
        string path
        bool active
    }
    FOLDER {
        string path
        bool live
    }
```

### 8.6 Gantt chart

```mermaid
gantt
    title LiveMD releases
    dateFormat YYYY-MM-DD
    section Core
    Markdown viewer      :done, 2026-02-01, 10d
    Multi-user + installer :done, 2026-02-11, 3d
    section Viewers
    Media + mermaid + math :done, 2026-05-01, 2d
    HTML preview toggle    :active, 2026-07-23, 1d
```

### 8.7 Pie chart

```mermaid
pie showData
    title File types watched
    "Markdown" : 62
    "Code" : 25
    "Images" : 8
    "Other" : 5
```

### 8.8 Git graph

```mermaid
gitGraph
    commit id: "v0.8.0"
    branch feature
    commit id: "media viewers"
    commit id: "mermaid + math"
    checkout main
    merge feature tag: "v0.10.0"
    commit id: "html toggle" tag: "v0.11.0"
```

### 8.9 User journey

```mermaid
journey
    title Viewing a file
    section Setup
      Install livemd: 5: User
      Start daemon: 5: User
    section Daily use
      Add file: 4: User
      Read rendered output: 5: User
      Edit file, watch it update: 5: User
```

### 8.10 Mindmap

```mermaid
mindmap
  root((LiveMD))
    Viewers
      Markdown
        GFM
        Mermaid
        KaTeX
      Code
      Media
        Images
        PDF
        Audio/Video
    Daemon
      fsnotify
      WebSocket
    CLI
      add / remove
      install
```

### 8.11 Timeline

```mermaid
timeline
    title Release history
    2026-02 : v0.6.0 : v0.7.0 : v0.8.0 multi-user
    2026-05 : v0.9.0 : v0.10.0 media + mermaid
    2026-07 : v0.11.0 HTML preview toggle
```

### 8.12 Quadrant chart

```mermaid
quadrantChart
    title Viewer effort vs value
    x-axis Low effort --> High effort
    y-axis Low value --> High value
    quadrant-1 Do next
    quadrant-2 Quick wins
    quadrant-3 Skip
    quadrant-4 Big bets
    Markdown: [0.3, 0.9]
    Mermaid: [0.6, 0.8]
    HTML toggle: [0.2, 0.6]
    PDF: [0.4, 0.5]
```

## 9. Mermaid — limit testing

### 9.1 Large flowchart (50 nodes)

```mermaid
graph LR
    n0 --> n1 --> n2 --> n3 --> n4 --> n5 --> n6 --> n7 --> n8 --> n9
    n9 --> n10 --> n11 --> n12 --> n13 --> n14 --> n15 --> n16 --> n17 --> n18 --> n19
    n19 --> n20 --> n21 --> n22 --> n23 --> n24 --> n25 --> n26 --> n27 --> n28 --> n29
    n29 --> n30 --> n31 --> n32 --> n33 --> n34 --> n35 --> n36 --> n37 --> n38 --> n39
    n39 --> n40 --> n41 --> n42 --> n43 --> n44 --> n45 --> n46 --> n47 --> n48 --> n49
    n0 --> n25
    n10 --> n35
    n5 --> n45
    n49 --> n0
```

### 9.2 Dense subgraph nesting

```mermaid
graph TB
    subgraph L1[Level 1]
        subgraph L2[Level 2]
            subgraph L3[Level 3]
                subgraph L4[Level 4]
                    deep[Deepest node]
                end
                c[node]
            end
            b[node]
        end
        a[node]
    end
    a --> b --> c --> deep
```

### 9.3 Long sequence diagram (20 messages)

```mermaid
sequenceDiagram
    participant A
    participant B
    participant C
    A->>B: msg 1
    B->>C: msg 2
    C->>A: msg 3
    A->>B: msg 4
    B->>C: msg 5
    C->>A: msg 6
    A->>B: msg 7
    B->>C: msg 8
    C->>A: msg 9
    A->>B: msg 10
    B->>C: msg 11
    C->>A: msg 12
    A->>B: msg 13
    B->>C: msg 14
    C->>A: msg 15
    A->>B: msg 16
    B->>C: msg 17
    C->>A: msg 18
    A->>B: msg 19
    B->>C: msg 20
```

### 9.4 Invalid mermaid (error handling — should fail gracefully, not break the page)

```mermaid
graph TD
    this is not ---> valid mermaid syntax {{{
```

### 9.5 Unicode + special characters in labels

```mermaid
graph LR
    A["Ünïcödé & <entities>"] --> B["quotes 'single' #quot;double#quot;"]
    B --> C["emoji 🎉 🚀 ✅"]
    C --> D["Türkçe: ğüşıöç"]
```

## 10. Images

External image (network required):

![Go gopher](https://go.dev/blog/gopher/gopher.png)

Broken image (alt text fallback):

![this image does not exist](/raw?path=/nonexistent.png)

## 11. Edge cases

Empty code fence:

```
```

Escaped characters: \*not italic\*, \`not code\`, \# not a heading

Horizontal rules:

---
***
___

A very long unbroken word to test overflow: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

Literal HTML entities: &amp; &lt; &gt; &copy; &mdash;

Footnote syntax[^1] (not enabled in goldmark GFM — should render as plain text, that's expected).

[^1]: this will not become a real footnote.

*End of demo — if everything above rendered, all viewers are healthy.*
