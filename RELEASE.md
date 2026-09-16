# Releasing livemd

The whole release is one pushed tag. `.github/workflows/release.yml` fires on
any `v*` tag, cross-compiles linux and windows binaries with the version baked
in, and creates the GitHub release with them attached.

Everything below is the checklist around that one push. Follow it top to bottom.

## Before you start

```bash
gh auth status                  # must be logged in; the tag push alone is not enough
git fetch origin --tags         # refs here go stale; fetch before believing anything
git status -sb | head -1        # must say ahead-only, never "behind"
git tag --sort=-v:refname | head -3
gh release list --limit 3
```

Two ways this lies to you:

- **`git tag | tail` sorts alphabetically** and will name `v0.9.0` as newest
  while `v1.1.0` exists. Always `--sort=-v:refname`.
- **`ahead N` before a fetch means nothing.** This clone sat diverged from
  origin for days — `ahead 6` was really `ahead 6, behind 4`, and the missing
  four were what the last release was built from. If `git status -sb` says
  `behind`, stop: integrate first, and re-run the checks, because the merged
  result is not what either side tested.

## 1. Check the tree is releasable

```bash
gofmt -l .                      # must print nothing
go vet ./... && go test ./...
node --check static/client.js   # the Go tests do not cover the client
git status --short              # must be clean; `go build ./...` drops a
                                # live-md binary, which .gitignore now covers
```

Then look at the app, because nothing above renders a page:

```bash
livemd add ./testdata/render -r     # the daemon only serves files inside a
                                    # tracked root, so the fixtures must be
                                    # followed before they will open
livemd list                         # note the port
```

Open `testdata/render/kitchen-sink.md` in the browser and check: every top-level
block shares one left edge, front matter is a metadata card rather than a
heading, and every code block has the same background. `code.md` and `wide.md`
cover fences in lists and content wider than the column. See `design.md`.

Drop the fixtures again afterwards if you do not want them in the watch list —
the `✕` on the folder row, or `livemd remove`.

## 2. Pick the version

Semver against the **CLI and the saved state**, not the internals — this is a
local viewer, so `livemd add`, `livemd start` and `~/.livemd-state.json` are the
public surface. The HTTP API is private to the bundled client and can change in
a minor.

- **Patch** — fixes only.
- **Minor** — new behaviour; old state files still load and no CLI flag changed
  meaning. A removed HTTP endpoint or a redesigned interface still fits here.
- **Major** — a CLI command or flag changes meaning, or an old state file stops
  loading.

## 3. Write the changelog

Group by what it means for someone updating, in this order, dropping empty
sections: **Added, Changed, Removed, Fixed**.

Write for a person deciding whether to update. `git log --oneline` is the raw
material, not the output — one line per user-visible change, not per commit, and
say what it means rather than what was edited. "Followed folders no longer
disappear after a restart" beats "fix persistState ordering".

Keep it in the release body. There is no `CHANGELOG.md` in this repo, and the
workflow's `generate_release_notes` produces a commit list, which step 6
replaces.

## 4. Push the commits

```bash
git push origin master          # do NOT pipe this through head/tail
```

The tag must point at a commit that is already on the remote, or the workflow
checks out a commit nobody else can see.

Read the result before continuing. Piping the push through `tail` hands the
pipeline `tail`'s exit status, so a rejected push looks like a success to
`&&` — which is how a tag once got pushed onto a line that was never on the
remote. If the push is rejected, go back to the divergence check above.

## 5. Tag and push

```bash
VERSION=v1.2.0
git tag -a "$VERSION" -m "LiveMD $VERSION"
git push origin "$VERSION"
```

Annotated (`-a`), not lightweight: the tag carries its own date and author, and
`git describe` prefers it.

## 6. Watch the build, then write the notes

```bash
gh run watch "$(gh run list --workflow=release.yml --limit 1 --json databaseId -q '.[0].databaseId')"
gh release view "$VERSION"                      # confirm both binaries attached
gh release edit "$VERSION" --notes-file notes.md
```

The workflow creates the release with generated notes, so the changelog goes on
afterwards. Check the release page shows `livemd-linux-amd64` and
`livemd-windows-amd64.exe`.

## 7. Update this machine

`livemd install` **refuses to run from a `dev` build** — "Cannot self-install a
dev build" — and after testing you are almost certainly on one. Download the
published asset instead, which has the merit of testing the artifact people will
actually get:

```bash
gh release download "$VERSION" -p livemd-linux-amd64 -D /tmp
chmod +x /tmp/livemd-linux-amd64
/tmp/livemd-linux-amd64 version         # confirm the version was baked in
livemd stop
cp /tmp/livemd-linux-amd64 "$(command -v livemd)"
livemd start --detach
```

Then confirm the daemon came back whole — the binary changed underneath it, so
the watch list is the thing to check:

```bash
livemd version
livemd list                     # same files and folders as before
```

From a release build, `livemd install` is the normal path and does all of this.

## If it goes wrong

- **Tag pushed by mistake.** Cancel the run and delete the tag both sides
  before anything downloads it:
  `gh run cancel <id>`, `git push origin ":refs/tags/$VERSION"`,
  `git tag -d "$VERSION"`. Confirm with `gh release list` that no release was
  created.
- **Workflow failed, tag already pushed.** Fix the cause, then move the tag:
  `git tag -d "$VERSION" && git push origin ":refs/tags/$VERSION"`, commit the
  fix, and start again from step 4. Only do this while nobody has downloaded the
  release.
- **Release created with no binaries.** The build matrix failed after the release
  job started. Re-run from the Actions tab rather than retagging.
- **`livemd install` reports no update.** The daemon caches its update check for
  the session; restart it, or check `gh release list` actually shows the new tag
  as `Latest`.
