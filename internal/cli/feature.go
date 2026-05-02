package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/evanstern/coda-lite/internal/scaffold"
	"github.com/evanstern/coda-lite/internal/tmux"
)

func runFeatureStart(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("feature start: usage: coda-lite feature start <agent> <slug> --repo <path>")
	}
	agent := args[0]
	slug := args[1]
	if err := validateName(slug); err != nil {
		return err
	}

	var repo string
	for i := 2; i < len(args); i++ {
		if args[i] == "--repo" && i+1 < len(args) {
			repo = args[i+1]
			i++
		}
	}
	if repo == "" {
		return fmt.Errorf("feature start: --repo <path> is required")
	}
	repoAbs, err := filepath.Abs(repo)
	if err != nil {
		return fmt.Errorf("resolve repo: %w", err)
	}

	agentRoot, err := mustAgentDir(agent)
	if err != nil {
		return err
	}

	worktree := filepath.Join(filepath.Dir(repoAbs), filepath.Base(repoAbs)+"-"+slug)
	if _, err := os.Stat(worktree); err == nil {
		return fmt.Errorf("worktree path already exists: %s", worktree)
	}

	branch := "feature/" + slug
	cmd := exec.Command("git", "-C", repoAbs, "worktree", "add", worktree, "-b", branch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git worktree add: %w", err)
	}

	featureDir := filepath.Join(agentRoot, "features", slug)
	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		return fmt.Errorf("create feature dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(featureDir, "worktree-path"), []byte(worktree+"\n"), 0o644); err != nil {
		return fmt.Errorf("write worktree-path: %w", err)
	}
	if err := os.WriteFile(filepath.Join(featureDir, "status"), []byte("active\n"), 0o644); err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	implementPath := filepath.Join(worktree, "IMPLEMENT.md")
	if _, err := os.Stat(implementPath); os.IsNotExist(err) {
		if err := os.WriteFile(implementPath, []byte(scaffold.ImplementTemplate(agent, slug)), 0o644); err != nil {
			return fmt.Errorf("write IMPLEMENT.md: %w", err)
		}
	}

	sessionName := agent + "-" + slug
	harness := scaffold.HarnessFor(agentRoot)
	spawnCmd, err := harness.SpawnCommand()
	if err != nil {
		return err
	}
	if err := tmux.NewSession(sessionName, worktree, spawnCmd); err != nil {
		return fmt.Errorf("tmux new-session: %w", err)
	}

	fmt.Printf("Feature %q started.\n", slug)
	fmt.Printf("  Worktree:    %s\n", worktree)
	fmt.Printf("  Branch:      %s\n", branch)
	fmt.Printf("  Brief:       %s\n", implementPath)
	fmt.Printf("  Session:     %s (attach: tmux attach -t %s)\n", sessionName, sessionName)
	return nil
}

func runFeatureLs(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("feature ls: takes no arguments")
	}
	root, err := agentsDir()
	if err != nil {
		return err
	}
	agents, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", root, err)
	}

	type row struct {
		agent    string
		slug     string
		status   string
		worktree string
	}
	rows := []row{}
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
			r := row{agent: a.Name(), slug: f.Name(), status: "?"}
			if b, err := os.ReadFile(filepath.Join(featuresDir, f.Name(), "status")); err == nil {
				r.status = strings.TrimSpace(string(b))
			}
			if b, err := os.ReadFile(filepath.Join(featuresDir, f.Name(), "worktree-path")); err == nil {
				r.worktree = strings.TrimSpace(string(b))
			}
			rows = append(rows, r)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].agent != rows[j].agent {
			return rows[i].agent < rows[j].agent
		}
		return rows[i].slug < rows[j].slug
	})
	if len(rows) == 0 {
		fmt.Println("No features.")
		return nil
	}
	for _, r := range rows {
		fmt.Printf("%-15s  %-25s  %-8s  %s\n", r.agent, r.slug, r.status, r.worktree)
	}
	return nil
}

func runFeatureFinish(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("feature finish: usage: coda-lite feature finish <slug>")
	}
	slug := args[0]
	root, err := agentsDir()
	if err != nil {
		return err
	}
	agents, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read %s: %w", root, err)
	}

	var featureDir, worktree, agent string
	for _, a := range agents {
		if strings.HasPrefix(a.Name(), ".") {
			continue
		}
		candidate := filepath.Join(root, a.Name(), "features", slug)
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
		return fmt.Errorf("no feature found with slug %q", slug)
	}

	sessionName := agent + "-" + slug
	if running, _ := tmux.SessionExists(sessionName); running {
		_ = tmux.KillSession(sessionName)
	}

	if err := os.WriteFile(filepath.Join(featureDir, "status"), []byte("done\n"), 0o644); err != nil {
		return fmt.Errorf("write status: %w", err)
	}

	fmt.Printf("Feature %q marked done.\n", slug)
	if worktree != "" {
		fmt.Printf("Worktree at %s left in place.\n", worktree)
		fmt.Printf("Remove with: git -C %s worktree remove %s\n", findRepoFromWorktree(worktree), worktree)
	}
	return nil
}

func findRepoFromWorktree(worktree string) string {
	out, err := exec.Command("git", "-C", worktree, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return "<repo>"
	}
	commonDir := strings.TrimSpace(string(out))
	repo := filepath.Dir(commonDir)
	if repo == "" || repo == "." {
		return "<repo>"
	}
	return repo
}
