# coda-lite

Single-user AI dev tool. Long-running personality-driven agents over
tmux + opencode + markdown files. No daemon, no database, no plugin
host. The filesystem is the bus.

This is the honest minimal version of [coda](https://github.com/evanstern/coda).
If you need multi-user, plugin ecosystems, cross-runtime A2A, formal
typed messaging — use coda. If you're one person who wants persistent
AI colleagues that learn your project — this.

## What you get

- **Long-running agents** as tmux sessions running opencode
- **Memory** as markdown files in the agent's directory (`memory/`, `wiki/`)
- **Inter-agent comms** as files dropped in the recipient's `inbox/`
- **Direct interaction** via `tmux attach`
- **Worktree-aware feature sessions** via `coda-lite feature start`
- **Provider-agnosticism** via opencode's native multi-LLM support

## What you don't get

- Notifications. Recipients poll their inbox. Per AGENTS.md instructions.
- Typed messages. Body is markdown. Use a header convention if you want.
- Routing tables, ack/recv semantics, plugin host, daemon, SQLite.
- Multi-user, multi-host, cross-runtime A2A. Single bash user, single host.

## MCP server

`coda-lite mcp serve` runs an MCP server on stdio that exposes the same
operations as the CLI to opencode-running agents. New agents get this
wired into their `opencode.json` automatically.

Tools exposed (v0.2):

- `coda_lite_inbox(agent)` — list unread messages
- `coda_lite_msg(to, body, from?)` — send a message
- `coda_lite_read(path)` — mark a message read

- `coda_lite_agent_ls()` — list agents and tmux status
- `coda_lite_agent_new(name)` — scaffold a new agent
- `coda_lite_agent_spawn(name)` — start the tmux session
- `coda_lite_agent_stop(name)` — kill the tmux session

- `coda_lite_feature_ls()` — list active feature sessions
- `coda_lite_feature_start(agent, slug, repo)` — worktree + tmux + scaffold
- `coda_lite_feature_attach(agent, slug)` — open a tmux window in the agent's
   session attached to the feature's worktree (so agents can spawn work and
   then go work in it)
- `coda_lite_feature_finish(slug)` — mark done, kill the feature session

## Quick start

```bash
# Build (requires Go 1.22+)
go build -o ~/bin/coda-lite ./cmd/coda-lite

# Create your first agent
coda-lite agent new zach

# Edit ~/agents/zach/AGENTS.md to taste

# Start the agent's session
coda-lite agent spawn zach

# Talk to them
coda-lite agent attach zach

# Send them a message from outside the session
coda-lite msg zach "hey, can you check #189?"

# As zach, list your inbox
coda-lite inbox zach

# Mark a message read after handling it
coda-lite read ~/agents/zach/inbox/2026-05-01T14:30:00-from-evan.md
```

## Layout

```
~/agents/
  zach/
    AGENTS.md           # personality + boot instructions
    memory/             # daily narrative
    wiki/               # curated knowledge
    inbox/              # unread messages
    outbox/             # sent + read messages, archived by sender
    features/           # active worktree-backed feature sessions
    .coda-lite-meta     # name, harness, created
```

## Design

Filesystem is the bus. Agent directory is identity + memory + queue.
Tmux session is "is this agent currently running." Markdown is everything.

For the architectural reasoning behind this shape, see the design notes
in `docs/design.md`.
