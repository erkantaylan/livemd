# Wide content

Things that are wider than the reading column, to check they are handled rather
than clipped.

## A wide table

| Path | Kind | Size | Modified | Watcher | Notes |
| --- | --- | --- | --- | --- | --- |
| `/home/me/projects/alpha/README.md` | markdown | 4.2 KB | 2026-09-16 14:32 | active | The main entry point for the project |
| `/home/me/projects/alpha/docs/architecture.md` | markdown | 18.7 KB | 2026-09-15 09:11 | registered | Long enough to push the table past the column |
| `/home/me/projects/alpha/internal/server/handler.go` | go | 31.0 KB | 2026-09-16 11:02 | registered | Source file, not prose |

## A very long line of prose

This single paragraph is deliberately written as one very long line in the source file so that it has to wrap in the browser, and the point of it is to confirm that the reading measure applies to prose regardless of how the author happened to wrap the source, because soft wrapping in the source should never change the rendered result.

## A long unbroken token

A URL with no spaces: https://example.com/a/very/long/path/that/keeps/going/and/going/without/any/opportunity/to/break/until/the/end

An unbroken word: supercalifragilisticexpialidociousandthensomemoretomakeitreallyquitelongindeed
