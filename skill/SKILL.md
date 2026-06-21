---
name: ww
description: Write the current answer as a self-contained HTML page and serve it locally with the `ww` command, reusing one temp dir and one server per session. Use when the user invokes /ww, or says "serve with ww" / "serve ww" / "serve this with ww" / "answer with ww", or otherwise asks to render the answer as a served HTML page.
---

# Serve answer as HTML with `ww`

When triggered, write your answer as a self-contained HTML page and serve it
locally with `ww`, instead of (or in addition to) a plain-text reply. Give the
user a clickable `http://localhost:PORT/...` URL.

This skill ships alongside `ww` (https://github.com/girtsf/ww). Install it by
copying or symlinking this directory to `~/.claude/skills/ww`, and make sure the
`ww` binary is on your `PATH` (`go install github.com/girtsf/ww@latest`).

## Trigger

`/ww`, or phrases like "serve with ww", "serve ww", "serve this with ww",
"answer with ww".

## Steps

1. Start or reuse the session server:

   ```
   bash ~/.claude/skills/ww/serve.sh
   ```

   It prints three lines: `DIR=...`, `URL=...`, `STATUS=started|reused`.

2. Write your answer as ONE self-contained HTML file into `DIR` (use the Write
   tool). Name it a short kebab-case slug of the topic, e.g.
   `DIR/binary-search.html`. If that name already exists, append `-2`, `-3`, ...

3. Give the user the page URL = `URL` + filename, e.g.
   `http://localhost:5074/binary-search.html`. Mention that the directory index
   at `URL` lists every page served this session.

## Rules

- **One temp dir + one server per session**; reuse `DIR` for every answer.
- If the server is already up, `serve.sh` reuses it and reports `STATUS=reused`
  with the **same base URL** — do not start another server.
- **30-minute idle timeout**: `ww` shuts itself down. Do NOT run a keep-alive or
  monitor loop; let it die.
- If it has timed out, `serve.sh` starts a fresh server (new port) on the next
  call. Do not try to revive the dead one.

## HTML guidance

- Self-contained: inline `<style>`, no external network deps when avoidable.
- Render the *actual answer* — prose, code blocks, tables — cleanly: readable
  font, constrained `max-width`, monospace for code.
- `ww` auto-serves `index.html` at a directory URL. For one constant page URL
  across the session, name the file `index.html` so it loads at `URL` directly;
  otherwise use a topic slug and link the specific `.html` file. A directory
  with no `index.html` shows a file listing.
