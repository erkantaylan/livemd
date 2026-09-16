---
name: kitchen-sink
description: Every markdown element livemd renders, on one page, so a styling
  change can be checked instead of guessed at.
status: fixture
---

# Kitchen sink

A paragraph of ordinary prose, long enough to wrap at least once so the reading
measure is visible. The quick brown fox jumps over the lazy dog, and then keeps
going for a while longer to be sure the line actually has to break somewhere.

Second paragraph, to check the space between them. It has **bold text**, *italic
text*, ***both at once***, ~~struck through~~, `inline code`, and a
[link to another fixture](./code.md).

## Heading level two

### Heading level three

#### Heading level four

##### Heading level five

###### Heading level six

## Lists

- First item
- Second item, long enough that it wraps onto a second line and you can see
  whether the hanging indent lines up with the text above it
- Third item
  - Nested one
  - Nested two
    - Nested three
- Fourth item

1. Ordered first
2. Ordered second
   1. Nested ordered
   2. Another
3. Ordered third

- [ ] Task not done
- [x] Task done

Term-style list:

- **`--port N`** — the port to serve on
- **`--detach`** — run as a background daemon

## Code

A fenced block at the top level:

```go
func main() {
	// A line long enough to test horizontal behaviour in a code block: it should scroll rather than wrap, because wrapped code is worse than scrolled code.
	fmt.Println("hello")
}
```

Indented code block:

    plain indented code
    second line

A block inside a list item, which is where alignment usually goes wrong:

1. Run the daemon:

   ```bash
   livemd start --detach
   ```

2. Add a file:

   ```bash
   livemd add README.md
   ```

3. Done.

Inside a bullet, with prose after it:

- Install it:

  ```sh
  go install ./...
  ```

  Then check the version.

## Table

| Column | What it holds | Notes |
| --- | --- | --- |
| `path` | Absolute path on disk | Compared case-insensitively on Windows |
| `active` | Whether a watcher is running | Session-scoped, never persisted |
| `deleted` | File vanished from disk | Row stays until removed |

## Quote

> A blockquote, which should sit inside the reading column with its rule on the
> left and its text indented from it.
>
> With a second paragraph.

## Rule

---

## Mixed

Text immediately before a code block:

```json
{"path": "/home/me/notes.md", "active": true}
```

Text immediately after it, with no blank-line surprises.
