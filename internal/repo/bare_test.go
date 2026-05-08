package repo

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo creates a minimal git repo with one commit on `main` and
// optionally a configured origin/HEAD pointing at main. Returns the
// repo path.
func initRepo(t *testing.T, withOriginHead bool) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q", "-b", "main", ".")
	mustRun(t, dir, "git", "config", "user.email", "test@example.com")
	mustRun(t, dir, "git", "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subpkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subpkg", "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", ".")
	mustRun(t, dir, "git", "commit", "-q", "-m", "init")
	if withOriginHead {
		// Point origin/HEAD at main without an actual remote.
		// `git symbolic-ref refs/remotes/origin/HEAD refs/remotes/origin/main`
		// requires the target ref to exist, so create a stub.
		mustRun(t, dir, "git", "update-ref", "refs/remotes/origin/main", "HEAD")
		mustRun(t, dir, "git", "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	}
	return dir
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, string(out))
	}
}

func TestBareInit_HappyPath(t *testing.T) {
	dir := initRepo(t, true)

	res, err := BareInit(BareInitOptions{Path: dir, Yes: true, Out: io.Discard})
	if err != nil {
		t.Fatalf("BareInit: %v", err)
	}
	if res.AlreadyBareLayout {
		t.Fatal("expected migration, got AlreadyBareLayout=true")
	}
	if res.DefaultBranch != "main" {
		t.Fatalf("DefaultBranch = %q, want main", res.DefaultBranch)
	}
	if res.Plan == "" {
		t.Fatal("expected non-empty Plan")
	}

	assertBareLayout(t, dir, "main")

	// README and subpkg should now live under main/.
	if _, err := os.Stat(filepath.Join(dir, "main", "README.md")); err != nil {
		t.Fatalf("main/README.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "main", "subpkg", "x.txt")); err != nil {
		t.Fatalf("main/subpkg/x.txt missing: %v", err)
	}

	// From within main/ git operates as a normal worktree.
	if out, err := runOK(filepath.Join(dir, "main"), "git", "status", "--porcelain"); err != nil {
		t.Fatalf("git status from main/: %v\n%s", err, out)
	}
	// And the bare repo itself is reachable for refs/objects from the
	// project root (e.g. `git -C .bare log`), even though `git status`
	// from the root won't work — there's no working tree there.
	if out, err := runOK(filepath.Join(dir, ".bare"), "git", "log", "--oneline", "-1"); err != nil {
		t.Fatalf("git log from .bare: %v\n%s", err, out)
	}
}

func TestBareInit_FallbackToCurrentBranch(t *testing.T) {
	// No origin/HEAD; should fall back to current branch.
	dir := initRepo(t, false)

	res, err := BareInit(BareInitOptions{Path: dir, Yes: true, Out: io.Discard})
	if err != nil {
		t.Fatalf("BareInit: %v", err)
	}
	if res.DefaultBranch != "main" {
		t.Fatalf("DefaultBranch = %q, want main", res.DefaultBranch)
	}
	assertBareLayout(t, dir, "main")
}

func TestBareInit_Idempotent(t *testing.T) {
	dir := initRepo(t, true)
	if _, err := BareInit(BareInitOptions{Path: dir, Yes: true, Out: io.Discard}); err != nil {
		t.Fatalf("first BareInit: %v", err)
	}

	var out bytes.Buffer
	res, err := BareInit(BareInitOptions{Path: dir, Yes: true, Out: &out})
	if err != nil {
		t.Fatalf("second BareInit: %v", err)
	}
	if !res.AlreadyBareLayout {
		t.Fatal("expected AlreadyBareLayout=true on second run")
	}
	if !strings.Contains(out.String(), "already a bare-layout") {
		t.Fatalf("expected idempotent message, got: %q", out.String())
	}
}

func TestBareInit_RejectsDirtyWorktree(t *testing.T) {
	dir := initRepo(t, true)
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := BareInit(BareInitOptions{Path: dir, Yes: true, Out: io.Discard})
	if err == nil {
		t.Fatal("expected error on dirty worktree")
	}
	if !strings.Contains(err.Error(), "not clean") {
		t.Fatalf("expected 'not clean' error, got: %v", err)
	}
	// And the repo must be untouched.
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf(".git should still exist after refusal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".bare")); err == nil {
		t.Fatal(".bare should not exist after refusal")
	}
}

func TestBareInit_RejectsStashes(t *testing.T) {
	dir := initRepo(t, true)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "stash", "push", "-q", "-m", "wip")

	_, err := BareInit(BareInitOptions{Path: dir, Yes: true, Out: io.Discard})
	if err == nil {
		t.Fatal("expected error when stashes present")
	}
	if !strings.Contains(err.Error(), "stashes") {
		t.Fatalf("expected 'stashes' error, got: %v", err)
	}
}

func TestBareInit_RejectsHalfState(t *testing.T) {
	// .bare/ exists but no pointer file -> inconsistent half-state.
	dir := initRepo(t, true)
	if err := os.Mkdir(filepath.Join(dir, ".bare"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := BareInit(BareInitOptions{Path: dir, Yes: true, Out: io.Discard})
	if err == nil {
		t.Fatal("expected error on half-state")
	}
	if !strings.Contains(err.Error(), "refusing to migrate") {
		t.Fatalf("expected 'refusing to migrate' error, got: %v", err)
	}
}

func TestBareInit_RejectsBareRepo(t *testing.T) {
	// Running bare-init on something that's already a bare git repo
	// (not bare-layout, just `git init --bare`) should refuse.
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q", "--bare", ".")

	_, err := BareInit(BareInitOptions{Path: dir, Yes: true, Out: io.Discard})
	if err == nil {
		t.Fatal("expected error on bare repo")
	}
}

func TestBareInit_RequiresConfirmation(t *testing.T) {
	dir := initRepo(t, true)

	// Empty stdin -> aborts.
	_, err := BareInit(BareInitOptions{
		Path: dir,
		Yes:  false,
		In:   strings.NewReader("\n"),
		Out:  io.Discard,
	})
	if err == nil {
		t.Fatal("expected abort on empty answer")
	}
	if !strings.Contains(err.Error(), "aborted") {
		t.Fatalf("expected 'aborted' error, got: %v", err)
	}
	// And the repo must be untouched.
	if _, err := os.Stat(filepath.Join(dir, ".bare")); err == nil {
		t.Fatal(".bare should not exist after abort")
	}
}

func TestBareInit_AcceptsInteractiveYes(t *testing.T) {
	dir := initRepo(t, true)
	_, err := BareInit(BareInitOptions{
		Path: dir,
		Yes:  false,
		In:   strings.NewReader("y\n"),
		Out:  io.Discard,
	})
	if err != nil {
		t.Fatalf("BareInit with interactive yes: %v", err)
	}
	assertBareLayout(t, dir, "main")
}

func TestIsBareLayout_FalseOnNormalClone(t *testing.T) {
	dir := initRepo(t, true)
	ok, err := IsBareLayout(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("normal clone should not be bare-layout")
	}
}

func TestResolveProjectRoot_AcceptsWorktreeAndBareDir(t *testing.T) {
	dir := initRepo(t, true)
	if _, err := BareInit(BareInitOptions{Path: dir, Yes: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"project root":  dir,
		"main worktree": filepath.Join(dir, "main"),
		"bare dir":      filepath.Join(dir, ".bare"),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok, err := ResolveProjectRoot(in)
			if err != nil {
				t.Fatalf("ResolveProjectRoot(%s): %v", in, err)
			}
			if !ok {
				t.Fatalf("ResolveProjectRoot(%s): ok=false, want true", in)
			}
			// Compare via EvalSymlinks because t.TempDir on macOS goes
			// through /var -> /private/var.
			wantResolved := evalOrSelf(t, dir)
			gotResolved := evalOrSelf(t, got)
			if gotResolved != wantResolved {
				t.Fatalf("ResolveProjectRoot(%s) = %s, want %s", in, gotResolved, wantResolved)
			}
		})
	}
}

func TestBareInit_PreservesIgnoredFiles(t *testing.T) {
	dir := initRepo(t, true)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("build/\nsecret.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, dir, "git", "add", ".gitignore")
	mustRun(t, dir, "git", "commit", "-q", "-m", "ignore")
	// Untracked-but-ignored files; clean tree from git's perspective.
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build", "out.bin"), []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("api-key"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := BareInit(BareInitOptions{Path: dir, Yes: true, Out: io.Discard}); err != nil {
		t.Fatalf("BareInit: %v", err)
	}

	// Tracked files should be in main/ (git's checkout).
	if _, err := os.Stat(filepath.Join(dir, "main", "README.md")); err != nil {
		t.Fatalf("main/README.md missing: %v", err)
	}
	// Ignored files must have been merged from staging.
	if _, err := os.Stat(filepath.Join(dir, "main", "secret.txt")); err != nil {
		t.Fatalf("main/secret.txt (ignored) missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "main", "build", "out.bin")); err != nil {
		t.Fatalf("main/build/out.bin (ignored) missing: %v", err)
	}
	// Staging dir must be cleaned up.
	if _, err := os.Stat(filepath.Join(dir, ".coda-lite-bare-init-staging")); err == nil {
		t.Fatal("staging dir leaked")
	}
}

func TestBareInit_FeatureStartFlowAfterMigration(t *testing.T) {
	dir := initRepo(t, true)
	if _, err := BareInit(BareInitOptions{Path: dir, Yes: true, Out: io.Discard}); err != nil {
		t.Fatal(err)
	}

	// Simulate what `feature start` does: resolve project root from
	// some user-friendly input, then `git -C <root>/.bare worktree
	// add <root>/<slug> -b feature/<slug>`.
	for _, input := range []string{dir, filepath.Join(dir, "main"), filepath.Join(dir, ".bare")} {
		root, ok, err := ResolveProjectRoot(input)
		if err != nil || !ok {
			t.Fatalf("ResolveProjectRoot(%s): ok=%v err=%v", input, ok, err)
		}
		// resolve symlinks for comparison; t.TempDir on macOS varies
		if got, want := evalOrSelf(t, root), evalOrSelf(t, dir); got != want {
			t.Fatalf("root=%s want=%s", got, want)
		}
	}

	root, _, _ := ResolveProjectRoot(dir)
	wt := filepath.Join(root, "x")
	mustRun(t, root, "git", "-C", filepath.Join(root, ".bare"), "worktree", "add", wt, "-b", "feature/x")
	if info, err := os.Stat(wt); err != nil || !info.IsDir() {
		t.Fatalf("feature worktree at %s missing: %v", wt, err)
	}
	// And the worktree is sibling to main/, not to the project dir.
	if filepath.Dir(wt) != root {
		t.Fatalf("worktree placed outside project dir: %s", wt)
	}
}

func TestResolveProjectRoot_FalseOnRandomDir(t *testing.T) {
	dir := t.TempDir()
	_, ok, err := ResolveProjectRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("random tempdir should not resolve to a bare-layout root")
	}
}

func evalOrSelf(t *testing.T, p string) string {
	t.Helper()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func runOK(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func assertBareLayout(t *testing.T, dir, defaultBranch string) {
	t.Helper()
	bare := filepath.Join(dir, ".bare")
	info, err := os.Stat(bare)
	if err != nil {
		t.Fatalf(".bare/ missing: %v", err)
	}
	if !info.IsDir() {
		t.Fatal(".bare exists but is not a directory")
	}
	pointer := filepath.Join(dir, ".git")
	body, err := os.ReadFile(pointer)
	if err != nil {
		t.Fatalf(".git pointer missing: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(body)), "gitdir:") {
		t.Fatalf(".git pointer body unexpected: %q", string(body))
	}
	wt := filepath.Join(dir, defaultBranch)
	if info, err := os.Stat(wt); err != nil || !info.IsDir() {
		t.Fatalf("worktree %s missing or not dir: %v", wt, err)
	}
	ok, err := IsBareLayout(dir)
	if err != nil {
		t.Fatalf("IsBareLayout: %v", err)
	}
	if !ok {
		t.Fatal("IsBareLayout=false after migration")
	}
}
