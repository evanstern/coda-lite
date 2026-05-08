// Package repo provides the bare-layout migration and detection.
//
// A bare-layout project is the convention every coda-lite repo is
// expected to use:
//
//	~/projects/<repo>/
//	├── .bare/              # core.bare = true; the real git database
//	├── .git                # text file: "gitdir: ./.bare"
//	├── <defaultBranch>/    # registered worktree on default branch
//	└── <slug>/             # registered worktree on feature/<slug>
//
// See designs/coda-lite-bare-layout.md for the full rationale.
package repo

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type BareInitOptions struct {
	Path string
	Yes  bool
	In   io.Reader
	Out  io.Writer
}

type BareInitResult struct {
	Path              string
	DefaultBranch     string
	AlreadyBareLayout bool
	Plan              string
}

// BareInit migrates a normal clone at opts.Path into the bare-layout
// convention. It is idempotent on already-converted repos.
//
// All validation runs before any filesystem mutation. A failure
// mid-migration leaves on-disk state visible (no automatic rollback):
// file moves on a real working tree are too easy to get wrong, and
// a clean abort with state visible is better than a silent half-rollback.
func BareInit(opts BareInitOptions) (BareInitResult, error) {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	in := opts.In
	if in == nil {
		in = os.Stdin
	}

	abs, err := filepath.Abs(opts.Path)
	if err != nil {
		return BareInitResult{}, fmt.Errorf("resolve path: %w", err)
	}
	res := BareInitResult{Path: abs}

	info, err := os.Stat(abs)
	if err != nil {
		return res, fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return res, fmt.Errorf("%s is not a directory", abs)
	}

	already, err := IsBareLayout(abs)
	if err != nil {
		return res, err
	}
	if already {
		res.AlreadyBareLayout = true
		fmt.Fprintf(out, "%s is already a bare-layout project; nothing to do.\n", abs)
		return res, nil
	}

	// Anything named .bare that isn't a bare-layout database (we
	// already returned early above when it was) is something we
	// must not clobber. Use Lstat so we catch symlinks and regular
	// files, not just directories.
	bareEntry := filepath.Join(abs, ".bare")
	if info, err := os.Lstat(bareEntry); err == nil {
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			return res, fmt.Errorf("%s/.bare exists as a symlink; refusing to clobber (inspect manually)", abs)
		case !info.IsDir():
			return res, fmt.Errorf("%s/.bare exists as a file; refusing to clobber (inspect manually)", abs)
		default:
			return res, fmt.Errorf("%s/.bare exists but the project is not a bare-layout repo; refusing to migrate (inspect manually)", abs)
		}
	} else if !os.IsNotExist(err) {
		return res, fmt.Errorf("stat %s: %w", bareEntry, err)
	}

	if err := requireNormalCheckout(abs); err != nil {
		return res, err
	}
	if err := requireCleanWorktree(abs); err != nil {
		return res, err
	}
	if err := requireNoLinkedWorktrees(abs); err != nil {
		return res, err
	}
	defaultBranch, err := resolveDefaultBranch(abs)
	if err != nil {
		return res, err
	}
	res.DefaultBranch = defaultBranch

	branchDir := filepath.Join(abs, defaultBranch)
	if _, err := os.Lstat(branchDir); err == nil {
		return res, fmt.Errorf("%s already exists; cannot move working tree into it", branchDir)
	} else if !os.IsNotExist(err) {
		return res, fmt.Errorf("stat %s: %w", branchDir, err)
	}

	plan := buildPlan(abs, defaultBranch)
	res.Plan = plan
	fmt.Fprint(out, plan)

	if !opts.Yes {
		fmt.Fprint(out, "\nProceed? [y/N]: ")
		reader := bufio.NewReader(in)
		line, _ := reader.ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			return res, fmt.Errorf("aborted by user")
		}
	}

	// Move .git -> .bare, flip core.bare, then write the .git pointer
	// IMMEDIATELY. The pointer write order matters: if any later
	// step (staging, worktree-add) fails, the on-disk state is at
	// least a recognisable bare-layout (.bare/ + pointer), which our
	// own IsBareLayout will accept and re-runs of bare-init will
	// no-op rather than refuse with "inconsistent half-state".
	gitDir := filepath.Join(abs, ".git")
	bareDir := filepath.Join(abs, ".bare")
	if err := os.Rename(gitDir, bareDir); err != nil {
		return res, fmt.Errorf("move .git -> .bare: %w", err)
	}
	if cout, err := exec.Command("git", "-C", bareDir, "config", "core.bare", "true").CombinedOutput(); err != nil {
		return res, fmt.Errorf("set core.bare: %s: %w", strings.TrimSpace(string(cout)), err)
	}
	pointerPath := filepath.Join(abs, ".git")
	if err := os.WriteFile(pointerPath, []byte("gitdir: ./.bare\n"), 0o644); err != nil {
		return res, fmt.Errorf("write .git pointer: %w", err)
	}

	// `git worktree add` refuses to populate a non-empty directory,
	// so we move existing working-tree contents to a holding name,
	// let git materialise a fresh checkout at <branchDir>, then
	// reconcile any files git didn't recreate (typically gitignored
	// files like build outputs or .env).
	stagingDir := filepath.Join(abs, ".coda-lite-bare-init-staging")
	if _, err := os.Lstat(stagingDir); err == nil {
		return res, fmt.Errorf("%s already exists; refusing to clobber", stagingDir)
	}
	if err := os.Mkdir(stagingDir, 0o755); err != nil {
		return res, fmt.Errorf("mkdir staging: %w", err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return res, fmt.Errorf("read %s: %w", abs, err)
	}
	for _, e := range entries {
		name := e.Name()
		// Skip .bare/ (the database), .git (the pointer we just
		// wrote), and the staging dir itself.
		if name == ".bare" || name == ".git" || name == filepath.Base(stagingDir) {
			continue
		}
		from := filepath.Join(abs, name)
		to := filepath.Join(stagingDir, name)
		if err := os.Rename(from, to); err != nil {
			return res, fmt.Errorf("stage %s: %w", from, err)
		}
	}

	if cout, err := exec.Command("git", "-C", bareDir, "worktree", "add", branchDir, defaultBranch).CombinedOutput(); err != nil {
		return res, fmt.Errorf("worktree add: %s: %w", strings.TrimSpace(string(cout)), err)
	}

	// Merge any files git didn't recreate (gitignored / not in this
	// commit) from staging into the new worktree, then remove
	// staging. Files git did recreate — tracked files at this commit
	// — are already correct in <branchDir>; we drop the staged copy.
	if err := mergeStagingIntoWorktree(stagingDir, branchDir); err != nil {
		return res, fmt.Errorf("merge staging: %w", err)
	}
	if err := os.RemoveAll(stagingDir); err != nil {
		return res, fmt.Errorf("remove staging: %w", err)
	}

	fmt.Fprintf(out, "\nMigration complete. New layout:\n")
	fmt.Fprint(out, renderTree(abs, defaultBranch))
	fmt.Fprintf(out, "\nNote: editors holding files at the old paths need to be reopened.\n")

	return res, nil
}

// IsBareLayout reports whether path is a bare-layout project:
// <path>/.bare/ is a real directory (not a symlink) and <path>/.git
// is a regular file containing a gitdir pointer that resolves to
// <path>/.bare. Symlinks for either entry are treated as not-a-
// bare-layout, matching what BareInit will accept on migration.
func IsBareLayout(path string) (bool, error) {
	bare := filepath.Join(path, ".bare")
	bareInfo, err := os.Lstat(bare)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", bare, err)
	}
	if bareInfo.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	if !bareInfo.IsDir() {
		return false, nil
	}

	pointer := filepath.Join(path, ".git")
	pointerInfo, err := os.Lstat(pointer)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", pointer, err)
	}
	if !pointerInfo.Mode().IsRegular() {
		return false, nil
	}
	body, err := os.ReadFile(pointer)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", pointer, err)
	}
	gitdir := parseGitdirPointer(string(body))
	if gitdir == "" {
		return false, nil
	}
	resolved := gitdir
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(path, resolved)
	}
	return filepath.Clean(resolved) == filepath.Clean(bare), nil
}

// SuggestBareInitTarget returns a reasonable path to suggest as the
// argument for `coda-lite repo bare-init` when the user pointed
// `feature start` at something that is not (yet) a bare-layout
// project. It runs `git rev-parse --show-toplevel` from path; on
// success, that's the working-tree root the user likely wants to
// migrate. On failure (path is not in a git repo at all) it returns
// path unchanged so the caller can still produce a message.
func SuggestBareInitTarget(path string) string {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return path
	}
	top := strings.TrimSpace(string(out))
	if top == "" {
		return path
	}
	return top
}

// ResolveProjectRoot maps a user-friendly path to a bare-layout
// project root. Accepts the project root itself, <root>/.bare, any
// worktree under <root>, or any path nested inside one of those
// (e.g. <root>/main/internal/foo). It walks up parents until it
// finds a bare-layout root or hits the filesystem root. Returns
// ok=false (with the absolute form of the input) when no ancestor
// qualifies.
//
// If path points at a file rather than a directory, the search
// starts from its parent.
func ResolveProjectRoot(path string) (string, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	start := abs
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		start = filepath.Dir(abs)
	} else if err != nil {
		return abs, false, err
	}

	cur := start
	for {
		ok, err := IsBareLayout(cur)
		if err != nil {
			return abs, false, err
		}
		if ok {
			return cur, true, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, false, nil
		}
		cur = parent
	}
}

func requireNormalCheckout(path string) error {
	insideOut, err := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s is not a git working tree: %s: %w", path, strings.TrimSpace(string(insideOut)), err)
	}
	if strings.TrimSpace(string(insideOut)) != "true" {
		return fmt.Errorf("%s is not inside a git working tree", path)
	}
	bareOut, err := exec.Command("git", "-C", path, "rev-parse", "--is-bare-repository").CombinedOutput()
	if err != nil {
		return fmt.Errorf("rev-parse --is-bare-repository: %s: %w", strings.TrimSpace(string(bareOut)), err)
	}
	if strings.TrimSpace(string(bareOut)) != "false" {
		return fmt.Errorf("%s is already a bare repository", path)
	}
	topOut, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").CombinedOutput()
	if err != nil {
		return fmt.Errorf("rev-parse --show-toplevel: %s: %w", strings.TrimSpace(string(topOut)), err)
	}
	top := strings.TrimSpace(string(topOut))
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	// EvalSymlinks normalises macOS /var vs /private/var and friends
	// so the equality check doesn't reject perfectly fine inputs.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if resolved, err := filepath.EvalSymlinks(top); err == nil {
		top = resolved
	}
	if abs != top {
		return fmt.Errorf("%s is not the root of the working tree (root is %s)", abs, top)
	}
	// Refuse linked worktrees: their .git is a pointer file (or
	// symlink) into the parent repo's .git/worktrees/<name>/, not
	// a real git directory. Renaming that into .bare would orphan
	// the parent repo and produce a half-migrated mess.
	gitInfo, err := os.Lstat(filepath.Join(abs, ".git"))
	if err != nil {
		return fmt.Errorf("stat %s/.git: %w", abs, err)
	}
	if !gitInfo.IsDir() {
		return fmt.Errorf("%s appears to be a linked worktree (.git is not a directory); run bare-init on the main repo instead", abs)
	}
	return nil
}

func requireCleanWorktree(path string) error {
	statusOut, err := exec.Command("git", "-C", path, "status", "--porcelain").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git status: %s: %w", strings.TrimSpace(string(statusOut)), err)
	}
	if strings.TrimSpace(string(statusOut)) != "" {
		return fmt.Errorf("working tree is not clean:\n%s", strings.TrimRight(string(statusOut), "\n"))
	}
	stashOut, err := exec.Command("git", "-C", path, "stash", "list").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git stash list: %s: %w", strings.TrimSpace(string(stashOut)), err)
	}
	if strings.TrimSpace(string(stashOut)) != "" {
		return fmt.Errorf("repository has stashes; clear them before migrating:\n%s", strings.TrimRight(string(stashOut), "\n"))
	}
	return nil
}

// requireNoLinkedWorktrees refuses to migrate when the repo already
// has additional worktrees registered. Renaming .git -> .bare would
// invalidate those worktrees' pointer files (which reference the
// pre-migration common dir), silently breaking other checkouts the
// user might have outside the project dir.
func requireNoLinkedWorktrees(path string) error {
	out, err := exec.Command("git", "-C", path, "worktree", "list", "--porcelain").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree list: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// Porcelain format: each worktree is a block separated by a
	// blank line, starting with `worktree <path>`. Count blocks.
	blocks := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			blocks++
		}
	}
	if blocks > 1 {
		return fmt.Errorf("repo has %d registered worktrees; bare-init would orphan the linked ones. remove or move them first:\n%s", blocks, strings.TrimRight(string(out), "\n"))
	}
	return nil
}

// resolveDefaultBranch: origin/HEAD -> current branch -> refuse.
// Each candidate must also have a local ref (refs/heads/<name>);
// `worktree add <dir> <name>` after the migration would fail
// otherwise, leaving the repo half-migrated. This commonly hits
// clones made with --single-branch --branch <other>, where
// origin/HEAD names a branch that was never fetched locally.
func resolveDefaultBranch(path string) (string, error) {
	if out, err := exec.Command("git", "-C", path, "symbolic-ref", "refs/remotes/origin/HEAD").Output(); err == nil {
		ref := strings.TrimSpace(string(out))
		const prefix = "refs/remotes/origin/"
		if strings.HasPrefix(ref, prefix) {
			name := strings.TrimPrefix(ref, prefix)
			if name != "" && localBranchExists(path, name) {
				return name, nil
			}
		}
	}
	out, err := exec.Command("git", "-C", path, "symbolic-ref", "--short", "HEAD").Output()
	if err == nil {
		name := strings.TrimSpace(string(out))
		if name != "" && localBranchExists(path, name) {
			return name, nil
		}
	}
	return "", fmt.Errorf("could not resolve default branch with a local ref (origin/HEAD unset or names a branch not fetched locally; HEAD detached)")
}

func localBranchExists(path, name string) bool {
	err := exec.Command("git", "-C", path, "show-ref", "--verify", "--quiet", "refs/heads/"+name).Run()
	return err == nil
}

func buildPlan(path, defaultBranch string) string {
	p := quoteIfNeeded(path)
	br := quoteIfNeeded(defaultBranch)
	var b strings.Builder
	fmt.Fprintf(&b, "Plan for %s:\n", p)
	fmt.Fprintf(&b, "  1. mv %s/.git -> %s/.bare; set core.bare=true\n", p, p)
	fmt.Fprintf(&b, "  2. write %s/.git pointer (\"gitdir: ./.bare\")\n", p)
	fmt.Fprintf(&b, "  3. stage working-tree files aside\n")
	fmt.Fprintf(&b, "  4. git -C %s/.bare worktree add %s/%s %s\n", p, p, br, br)
	fmt.Fprintf(&b, "  5. merge gitignored/extra files from staging into %s/%s/\n", p, br)
	return b.String()
}

// quoteIfNeeded wraps s in single quotes when it contains whitespace
// or shell metacharacters, so the plan's path renders unambiguously
// for paths like `/home/me/My Project`. Bare paths stay bare so the
// plan reads naturally for the common case. Single-quote chars in
// the input are escaped using the standard `'\”` sequence.
func quoteIfNeeded(s string) string {
	if !strings.ContainsAny(s, " \t\n\"'\\$`*?(){}[]<>|&;#") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func renderTree(path, defaultBranch string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s/\n", path)
	fmt.Fprintf(&b, "  .bare/\n")
	fmt.Fprintf(&b, "  .git           (-> ./.bare)\n")
	fmt.Fprintf(&b, "  %s/\n", defaultBranch)
	return b.String()
}

// mergeStagingIntoWorktree walks staging and moves any entries that
// don't already exist in worktree (top-level only, then recursively
// for directories that overlap). Anything already present in
// worktree is git's fresh checkout — we trust it and drop the staged
// copy.
func mergeStagingIntoWorktree(staging, worktree string) error {
	entries, err := os.ReadDir(staging)
	if err != nil {
		return err
	}
	for _, e := range entries {
		src := filepath.Join(staging, e.Name())
		dst := filepath.Join(worktree, e.Name())
		dstInfo, err := os.Lstat(dst)
		if err != nil {
			if os.IsNotExist(err) {
				if err := os.Rename(src, dst); err != nil {
					return fmt.Errorf("move %s -> %s: %w", src, dst, err)
				}
				continue
			}
			return err
		}
		// Both sides exist. If both are directories, recurse.
		// Otherwise leave the worktree side (git's checkout) and
		// drop the staged copy.
		srcInfo, err := os.Lstat(src)
		if err != nil {
			return err
		}
		if srcInfo.IsDir() && dstInfo.IsDir() {
			if err := mergeStagingIntoWorktree(src, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseGitdirPointer(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "gitdir:") {
			return ""
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	}
	return ""
}
