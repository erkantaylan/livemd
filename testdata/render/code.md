# Code rendering

Back to the [kitchen sink](./kitchen-sink.md).

## Long lines

```python
def a_function_with_a_very_long_signature(first_argument, second_argument, third_argument, fourth_argument, fifth_argument=None):
    return first_argument
```

## Several languages in a row

```javascript
const files = await fetch('/api/files').then(r => r.json());
console.log(files.map(f => f.path));
```

```css
.folder-refresh:hover {
    background: var(--accent-weak);
}
```

```sql
SELECT path, active FROM watched_files WHERE deleted = 0 ORDER BY path;
```

```
no language tag at all — this should still render as a code block
```

## Inline code in prose

Calling `livemd add ./docs -r` follows a folder; `livemd list` shows what is
tracked; and `PathsEqual(a, b)` is how two paths are compared. A longer run of
`inline code that goes on for a while inside a sentence` should not break the
line spacing around it.

## Diff-ish content

```diff
-    padding: var(--s-6) max(var(--s-5), calc((100% - var(--measure)) / 2));
+    padding: var(--s-6) max(var(--s-5), calc((100% - var(--measure-wide)) / 2));
```
