package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	codalitepaths "github.com/evanstern/coda-lite/internal/paths"
	bareproj "github.com/evanstern/coda-lite/internal/repo"
	"github.com/evanstern/coda-lite/internal/scaffold"
	"github.com/evanstern/coda-lite/internal/tmux"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const Version = "0.2.0"

func Serve(ctx context.Context) error {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "coda-lite",
		Version: Version,
	}, nil)

	mcpsdk.AddTool(server,
		&mcpsdk.Tool{Name: "coda_lite_inbox", Description: "List unread messages for an agent."},
		toolInbox)
	mcpsdk.AddTool(server,
		&mcpsdk.Tool{Name: "coda_lite_msg", Description: "Send a message to an agent. Writes a markdown file into the recipient's inbox."},
		toolMsg)
	mcpsdk.AddTool(server,
		&mcpsdk.Tool{Name: "coda_lite_read", Description: "Mark a message read by moving it from inbox to outbox."},
		toolRead)

	mcpsdk.AddTool(server,
		&mcpsdk.Tool{Name: "coda_lite_agent_ls", Description: "List all agents and whether their tmux session is running."},
		toolAgentLs)
	mcpsdk.AddTool(server,
		&mcpsdk.Tool{Name: "coda_lite_agent_new", Description: "Scaffold a new agent directory."},
		toolAgentNew)
	mcpsdk.AddTool(server,
		&mcpsdk.Tool{Name: "coda_lite_agent_spawn", Description: "Start a tmux session running the agent's harness."},
		toolAgentSpawn)
	mcpsdk.AddTool(server,
		&mcpsdk.Tool{Name: "coda_lite_agent_stop", Description: "Kill the agent's tmux session. Memory and inbox survive."},
		toolAgentStop)

	mcpsdk.AddTool(server,
		&mcpsdk.Tool{Name: "coda_lite_feature_ls", Description: "List active feature sessions across all agents."},
		toolFeatureLs)
	mcpsdk.AddTool(server,
		&mcpsdk.Tool{Name: "coda_lite_feature_start", Description: "Create a git worktree, scaffold a feature dir, and spawn a tmux session running the harness."},
		toolFeatureStart)
	mcpsdk.AddTool(server,
		&mcpsdk.Tool{Name: "coda_lite_feature_finish", Description: "Mark a feature session done and kill its tmux session. Worktree is left in place."},
		toolFeatureFinish)
	mcpsdk.AddTool(server,
		&mcpsdk.Tool{Name: "coda_lite_feature_attach", Description: "Open a tmux window in the calling agent's session attached to the feature's worktree, so the agent can work in it."},
		toolFeatureAttach)

	mcpsdk.AddTool(server,
		&mcpsdk.Tool{Name: "coda_lite_repo_bare_init", Description: "Convert a normal git clone into a bare-layout coda-lite project (.bare/ + worktree-per-branch). Validates cleanliness, prints a plan, and refuses without yes=true."},
		toolRepoBareInit)

	return server.Run(ctx, &mcpsdk.StdioTransport{})
}

type AgentArgs struct {
	Agent string `json:"agent" jsonschema:"the agent name"`
}

type InboxResult struct {
	Messages []InboxMessage `json:"messages"`
}

type InboxMessage struct {
	Path      string `json:"path"`
	Sender    string `json:"sender"`
	Timestamp string `json:"timestamp"`
	Body      string `json:"body"`
}

func toolInbox(ctx context.Context, req *mcpsdk.CallToolRequest, args AgentArgs) (*mcpsdk.CallToolResult, InboxResult, error) {
	dir, err := codalitepaths.MustAgentDir(args.Agent)
	if err != nil {
		return nil, InboxResult{}, err
	}
	inbox := filepath.Join(dir, "inbox")
	entries, err := os.ReadDir(inbox)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, InboxResult{Messages: []InboxMessage{}}, nil
		}
		return nil, InboxResult{}, err
	}
	out := InboxResult{Messages: []InboxMessage{}}
	for _, e := range entries {
		if e.IsDir() || !isInboxMessage(e.Name()) {
			continue
		}
		path := filepath.Join(inbox, e.Name())
		body, _ := os.ReadFile(path)
		ts, sender := parseInboxName(e.Name())
		out.Messages = append(out.Messages, InboxMessage{
			Path:      path,
			Sender:    sender,
			Timestamp: ts,
			Body:      string(body),
		})
	}
	sort.Slice(out.Messages, func(i, j int) bool { return out.Messages[i].Timestamp < out.Messages[j].Timestamp })
	return nil, out, nil
}

// isInboxMessage returns true only for files matching the coda-lite inbox
// message convention: <UTC-ts>-from-<sender>.md. Anything else (dotfiles,
// .prev/.read siblings, foreign artifacts) is ignored.
func isInboxMessage(filename string) bool {
	if strings.HasPrefix(filename, ".") {
		return false
	}
	if !strings.HasSuffix(filename, ".md") {
		return false
	}
	if !strings.Contains(filename, "-from-") {
		return false
	}
	return true
}

type MsgArgs struct {
	To   string `json:"to" jsonschema:"recipient agent name"`
	Body string `json:"body" jsonschema:"message body (markdown)"`
	From string `json:"from,omitempty" jsonschema:"sender name (defaults to $USER)"`
}

type MsgResult struct {
	Path string `json:"path"`
}

func toolMsg(ctx context.Context, req *mcpsdk.CallToolRequest, args MsgArgs) (*mcpsdk.CallToolResult, MsgResult, error) {
	if strings.TrimSpace(args.Body) == "" {
		return nil, MsgResult{}, errors.New("body is empty")
	}
	dir, err := codalitepaths.MustAgentDir(args.To)
	if err != nil {
		return nil, MsgResult{}, err
	}
	from := args.From
	if from == "" {
		from = os.Getenv("USER")
		if from == "" {
			from = "unknown"
		}
	}
	inbox := filepath.Join(dir, "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		return nil, MsgResult{}, err
	}
	ts := time.Now().UTC().Format("2006-01-02T15-04-05Z")
	path := filepath.Join(inbox, fmt.Sprintf("%s-from-%s.md", ts, sanitize(from)))
	if err := os.WriteFile(path, []byte(args.Body), 0o644); err != nil {
		return nil, MsgResult{}, err
	}
	return nil, MsgResult{Path: path}, nil
}

type ReadArgs struct {
	Path string `json:"path" jsonschema:"absolute path to the inbox message"`
}

type ReadResult struct {
	OK      bool   `json:"ok"`
	MovedTo string `json:"moved_to"`
}

func toolRead(ctx context.Context, req *mcpsdk.CallToolRequest, args ReadArgs) (*mcpsdk.CallToolResult, ReadResult, error) {
	src, err := filepath.Abs(args.Path)
	if err != nil {
		return nil, ReadResult{}, err
	}
	if _, err := os.Stat(src); err != nil {
		return nil, ReadResult{}, err
	}
	parent := filepath.Dir(src)
	if filepath.Base(parent) != "inbox" {
		return nil, ReadResult{}, fmt.Errorf("not in an inbox: %s", src)
	}
	agentRoot := filepath.Dir(parent)
	base := filepath.Base(src)
	sender := senderFromName(base)
	if sender == "" {
		sender = "unknown"
	}
	dst := filepath.Join(agentRoot, "outbox", "from-"+sender, "read", base)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, ReadResult{}, err
	}
	if err := os.Rename(src, dst); err != nil {
		return nil, ReadResult{}, err
	}
	return nil, ReadResult{OK: true, MovedTo: dst}, nil
}

type AgentLsResult struct {
	Agents []AgentInfo `json:"agents"`
}

type AgentInfo struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Path    string `json:"path"`
	Harness string `json:"harness"`
}

func toolAgentLs(ctx context.Context, req *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, AgentLsResult, error) {
	root, err := codalitepaths.AgentsDir()
	if err != nil {
		return nil, AgentLsResult{}, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, AgentLsResult{Agents: []AgentInfo{}}, nil
		}
		return nil, AgentLsResult{}, err
	}
	out := AgentLsResult{Agents: []AgentInfo{}}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir, _ := codalitepaths.AgentDir(e.Name())
		running, _ := tmux.SessionExists(e.Name())
		harness := scaffold.HarnessFor(dir).Name
		out.Agents = append(out.Agents, AgentInfo{
			Name:    e.Name(),
			Running: running,
			Path:    dir,
			Harness: harness,
		})
	}
	sort.Slice(out.Agents, func(i, j int) bool { return out.Agents[i].Name < out.Agents[j].Name })
	return nil, out, nil
}

type AgentNewArgs struct {
	Name string `json:"name" jsonschema:"agent name (no slashes, spaces, or leading dots)"`
}

type AgentNewResult struct {
	Path     string `json:"path"`
	AgentsMD string `json:"agents_md"`
}

func toolAgentNew(ctx context.Context, req *mcpsdk.CallToolRequest, args AgentNewArgs) (*mcpsdk.CallToolResult, AgentNewResult, error) {
	if err := codalitepaths.ValidateName(args.Name); err != nil {
		return nil, AgentNewResult{}, err
	}
	exists, dir, err := codalitepaths.AgentExists(args.Name)
	if err != nil {
		return nil, AgentNewResult{}, err
	}
	if exists {
		return nil, AgentNewResult{}, fmt.Errorf("agent %q already exists at %s", args.Name, dir)
	}
	if err := scaffold.NewAgent(dir, args.Name); err != nil {
		return nil, AgentNewResult{}, err
	}
	return nil, AgentNewResult{Path: dir, AgentsMD: filepath.Join(dir, "AGENTS.md")}, nil
}

type AgentSpawnResult struct {
	SessionName string `json:"session_name"`
	OK          bool   `json:"ok"`
}

func toolAgentSpawn(ctx context.Context, req *mcpsdk.CallToolRequest, args AgentArgs) (*mcpsdk.CallToolResult, AgentSpawnResult, error) {
	dir, err := codalitepaths.MustAgentDir(args.Agent)
	if err != nil {
		return nil, AgentSpawnResult{}, err
	}
	if running, _ := tmux.SessionExists(args.Agent); running {
		return nil, AgentSpawnResult{}, fmt.Errorf("agent %q is already running", args.Agent)
	}
	harness := scaffold.HarnessFor(dir)
	cmd, err := harness.SpawnCommand()
	if err != nil {
		return nil, AgentSpawnResult{}, err
	}
	if err := tmux.NewSession(args.Agent, dir, cmd); err != nil {
		return nil, AgentSpawnResult{}, err
	}
	return nil, AgentSpawnResult{SessionName: args.Agent, OK: true}, nil
}

type AgentStopResult struct {
	OK bool `json:"ok"`
}

func toolAgentStop(ctx context.Context, req *mcpsdk.CallToolRequest, args AgentArgs) (*mcpsdk.CallToolResult, AgentStopResult, error) {
	if _, err := codalitepaths.MustAgentDir(args.Agent); err != nil {
		return nil, AgentStopResult{}, err
	}
	if running, _ := tmux.SessionExists(args.Agent); !running {
		return nil, AgentStopResult{OK: true}, nil
	}
	if err := tmux.KillSession(args.Agent); err != nil {
		return nil, AgentStopResult{}, err
	}
	return nil, AgentStopResult{OK: true}, nil
}

type FeatureLsResult struct {
	Features []FeatureInfo `json:"features"`
}

type FeatureInfo struct {
	Agent    string `json:"agent"`
	Slug     string `json:"slug"`
	Status   string `json:"status"`
	Worktree string `json:"worktree"`
}

func toolFeatureLs(ctx context.Context, req *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, FeatureLsResult, error) {
	root, err := codalitepaths.AgentsDir()
	if err != nil {
		return nil, FeatureLsResult{}, err
	}
	agents, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, FeatureLsResult{Features: []FeatureInfo{}}, nil
		}
		return nil, FeatureLsResult{}, err
	}
	out := FeatureLsResult{Features: []FeatureInfo{}}
	for _, a := range agents {
		if strings.HasPrefix(a.Name(), ".") {
			continue
		}
		featuresDir := filepath.Join(root, a.Name(), "features")
		feats, err := os.ReadDir(featuresDir)
		if err != nil {
			continue
		}
		for _, f := range feats {
			if !f.IsDir() {
				continue
			}
			info := FeatureInfo{Agent: a.Name(), Slug: f.Name(), Status: "?"}
			if b, err := os.ReadFile(filepath.Join(featuresDir, f.Name(), "status")); err == nil {
				info.Status = strings.TrimSpace(string(b))
			}
			if b, err := os.ReadFile(filepath.Join(featuresDir, f.Name(), "worktree-path")); err == nil {
				info.Worktree = strings.TrimSpace(string(b))
			}
			out.Features = append(out.Features, info)
		}
	}
	sort.Slice(out.Features, func(i, j int) bool {
		if out.Features[i].Agent != out.Features[j].Agent {
			return out.Features[i].Agent < out.Features[j].Agent
		}
		return out.Features[i].Slug < out.Features[j].Slug
	})
	return nil, out, nil
}

type FeatureStartArgs struct {
	Agent string `json:"agent" jsonschema:"agent that owns this feature"`
	Slug  string `json:"slug" jsonschema:"feature slug, becomes branch and worktree suffix"`
	Repo  string `json:"repo" jsonschema:"absolute path to the git repo to branch from"`
}

type FeatureStartResult struct {
	Worktree    string `json:"worktree"`
	Branch      string `json:"branch"`
	SessionName string `json:"session_name"`
	BriefPath   string `json:"brief_path"`
}

func toolFeatureStart(ctx context.Context, req *mcpsdk.CallToolRequest, args FeatureStartArgs) (*mcpsdk.CallToolResult, FeatureStartResult, error) {
	if err := codalitepaths.ValidateName(args.Slug); err != nil {
		return nil, FeatureStartResult{}, err
	}
	// repo is required; an empty value would resolve via filepath.Abs
	// to the MCP server's CWD and create a worktree in an unintended
	// repository.
	repo := strings.TrimSpace(args.Repo)
	if repo == "" {
		return nil, FeatureStartResult{}, fmt.Errorf("repo is required")
	}
	if !filepath.IsAbs(repo) {
		return nil, FeatureStartResult{}, fmt.Errorf("repo must be absolute (got %q)", args.Repo)
	}
	repoAbs, err := filepath.Abs(repo)
	if err != nil {
		return nil, FeatureStartResult{}, err
	}
	agentRoot, err := codalitepaths.MustAgentDir(args.Agent)
	if err != nil {
		return nil, FeatureStartResult{}, err
	}
	projectRoot, ok, err := bareproj.ResolveProjectRoot(repoAbs)
	if err != nil {
		return nil, FeatureStartResult{}, fmt.Errorf("resolve repo: %w", err)
	}
	if !ok {
		suggest := bareproj.SuggestBareInitTarget(repoAbs)
		return nil, FeatureStartResult{}, fmt.Errorf("repo at %s is not a bare-layout coda-lite project.\nrun: coda-lite repo bare-init %s", repoAbs, suggest)
	}
	worktree := filepath.Join(projectRoot, args.Slug)
	if _, err := os.Stat(worktree); err == nil {
		return nil, FeatureStartResult{}, fmt.Errorf("worktree already exists: %s", worktree)
	}
	branch := "feature/" + args.Slug
	if out, err := exec.Command("git", "-C", filepath.Join(projectRoot, ".bare"), "worktree", "add", worktree, "-b", branch).CombinedOutput(); err != nil {
		return nil, FeatureStartResult{}, fmt.Errorf("git worktree add: %s: %w", string(out), err)
	}
	featureDir := filepath.Join(agentRoot, "features", args.Slug)
	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		return nil, FeatureStartResult{}, err
	}
	if err := os.WriteFile(filepath.Join(featureDir, "worktree-path"), []byte(worktree+"\n"), 0o644); err != nil {
		return nil, FeatureStartResult{}, err
	}
	if err := os.WriteFile(filepath.Join(featureDir, "status"), []byte("active\n"), 0o644); err != nil {
		return nil, FeatureStartResult{}, err
	}
	briefPath := filepath.Join(worktree, "IMPLEMENT.md")
	if _, err := os.Stat(briefPath); os.IsNotExist(err) {
		_ = os.WriteFile(briefPath, []byte(scaffold.ImplementTemplate(args.Agent, args.Slug)), 0o644)
	}
	sessionName := args.Agent + "-" + args.Slug
	harness := scaffold.HarnessFor(agentRoot)
	cmd, err := harness.SpawnCommand()
	if err != nil {
		return nil, FeatureStartResult{}, err
	}
	if err := tmux.NewSession(sessionName, worktree, cmd); err != nil {
		return nil, FeatureStartResult{}, err
	}
	return nil, FeatureStartResult{
		Worktree:    worktree,
		Branch:      branch,
		SessionName: sessionName,
		BriefPath:   briefPath,
	}, nil
}

type FeatureFinishArgs struct {
	Slug string `json:"slug" jsonschema:"feature slug to finish"`
}

type FeatureFinishResult struct {
	OK       bool   `json:"ok"`
	Worktree string `json:"worktree"`
}

func toolFeatureFinish(ctx context.Context, req *mcpsdk.CallToolRequest, args FeatureFinishArgs) (*mcpsdk.CallToolResult, FeatureFinishResult, error) {
	root, err := codalitepaths.AgentsDir()
	if err != nil {
		return nil, FeatureFinishResult{}, err
	}
	agents, err := os.ReadDir(root)
	if err != nil {
		return nil, FeatureFinishResult{}, err
	}
	var featureDir, worktree, agent string
	for _, a := range agents {
		if strings.HasPrefix(a.Name(), ".") {
			continue
		}
		candidate := filepath.Join(root, a.Name(), "features", args.Slug)
		if _, err := os.Stat(candidate); err == nil {
			featureDir = candidate
			agent = a.Name()
			if b, err := os.ReadFile(filepath.Join(candidate, "worktree-path")); err == nil {
				worktree = strings.TrimSpace(string(b))
			}
			break
		}
	}
	if featureDir == "" {
		return nil, FeatureFinishResult{}, fmt.Errorf("no feature found with slug %q", args.Slug)
	}
	sessionName := agent + "-" + args.Slug
	if running, _ := tmux.SessionExists(sessionName); running {
		_ = tmux.KillSession(sessionName)
	}
	if err := os.WriteFile(filepath.Join(featureDir, "status"), []byte("done\n"), 0o644); err != nil {
		return nil, FeatureFinishResult{}, err
	}
	return nil, FeatureFinishResult{OK: true, Worktree: worktree}, nil
}

type FeatureAttachArgs struct {
	Agent string `json:"agent" jsonschema:"calling agent name (whose tmux session gets the new window)"`
	Slug  string `json:"slug" jsonschema:"feature slug to attach to"`
}

type FeatureAttachResult struct {
	WindowName string `json:"window_name"`
	Worktree   string `json:"worktree"`
	OK         bool   `json:"ok"`
}

func toolFeatureAttach(ctx context.Context, req *mcpsdk.CallToolRequest, args FeatureAttachArgs) (*mcpsdk.CallToolResult, FeatureAttachResult, error) {
	if running, _ := tmux.SessionExists(args.Agent); !running {
		return nil, FeatureAttachResult{}, fmt.Errorf("agent %q is not running; can't attach a feature window", args.Agent)
	}
	agentRoot, err := codalitepaths.MustAgentDir(args.Agent)
	if err != nil {
		return nil, FeatureAttachResult{}, err
	}
	featureDir := filepath.Join(agentRoot, "features", args.Slug)
	if _, err := os.Stat(featureDir); err != nil {
		return nil, FeatureAttachResult{}, fmt.Errorf("no feature %q for agent %q", args.Slug, args.Agent)
	}
	worktree := ""
	if b, err := os.ReadFile(filepath.Join(featureDir, "worktree-path")); err == nil {
		worktree = strings.TrimSpace(string(b))
	}
	if worktree == "" {
		return nil, FeatureAttachResult{}, fmt.Errorf("feature %q has no worktree-path", args.Slug)
	}
	if err := tmux.NewWindow(args.Agent, args.Slug, worktree); err != nil {
		return nil, FeatureAttachResult{}, err
	}
	return nil, FeatureAttachResult{WindowName: args.Slug, Worktree: worktree, OK: true}, nil
}

type RepoBareInitArgs struct {
	Path string `json:"path" jsonschema:"absolute path to the project to migrate"`
	Yes  bool   `json:"yes,omitempty" jsonschema:"skip the interactive confirmation prompt; required to perform the migration over MCP since there is no stdin"`
}

type RepoBareInitResult struct {
	Path              string `json:"path"`
	DefaultBranch     string `json:"default_branch"`
	AlreadyBareLayout bool   `json:"already_bare_layout"`
	Plan              string `json:"plan"`
}

func toolRepoBareInit(ctx context.Context, req *mcpsdk.CallToolRequest, args RepoBareInitArgs) (*mcpsdk.CallToolResult, RepoBareInitResult, error) {
	// path is required and must be absolute. Empty path would
	// otherwise resolve via filepath.Abs to the MCP server's CWD,
	// which is almost certainly not what the caller meant for a
	// destructive migration.
	path := strings.TrimSpace(args.Path)
	if path == "" {
		return nil, RepoBareInitResult{}, fmt.Errorf("path is required")
	}
	if !filepath.IsAbs(path) {
		return nil, RepoBareInitResult{}, fmt.Errorf("path must be absolute (got %q)", args.Path)
	}
	if !args.Yes {
		// MCP has no stdin for an interactive confirm. The CLI gets
		// the prompt path; over MCP, callers must opt in explicitly.
		return nil, RepoBareInitResult{}, fmt.Errorf("yes=true is required: bare-init is destructive and there is no interactive confirm over MCP")
	}
	res, err := bareproj.BareInit(bareproj.BareInitOptions{
		Path: path,
		Yes:  true,
		Out:  io.Discard,
	})
	if err != nil {
		return nil, RepoBareInitResult{}, err
	}
	return nil, RepoBareInitResult{
		Path:              res.Path,
		DefaultBranch:     res.DefaultBranch,
		AlreadyBareLayout: res.AlreadyBareLayout,
		Plan:              res.Plan,
	}, nil
}

func parseInboxName(filename string) (timestamp, sender string) {
	idx := strings.Index(filename, "-from-")
	if idx < 0 {
		return "", ""
	}
	timestamp = filename[:idx]
	sender = strings.TrimSuffix(filename[idx+len("-from-"):], ".md")
	return
}

func senderFromName(filename string) string {
	idx := strings.Index(filename, "-from-")
	if idx < 0 {
		return ""
	}
	return strings.TrimSuffix(filename[idx+len("-from-"):], ".md")
}

func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}
