package shim

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestWorkspaceGitFlow(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ctx := context.Background()
	base := t.TempDir()

	seed := filepath.Join(base, "seed")
	gitTest(t, base, "init", "--initial-branch=main", "seed")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, seed, "add", ".")
	gitTest(t, seed, "-c", "user.name=Seed", "-c", "user.email=seed@test", "-c", "commit.gpgsign=false", "commit", "-m", "seed")
	gitTest(t, base, "clone", "--bare", "seed", "origin.git")
	origin := filepath.Join(base, "origin.git")

	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}

	repo, err := PrepareWorkspace(ctx, workspace, origin, "main", "dispatch/t1", "Bot <bot@test>")
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	if branch := gitTest(t, repo.Dir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "dispatch/t1" {
		t.Errorf("branch = %q", branch)
	}

	if commits, err := repo.SessionCommits(ctx, "main", "dispatch/t1"); err != nil || commits != 0 {
		t.Errorf("fresh branch commits = %d, err %v", commits, err)
	}

	if err := os.WriteFile(filepath.Join(repo.Dir, "change.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo.Dir, "add", ".")
	gitTest(t, repo.Dir, "-c", "commit.gpgsign=false", "commit", "-m", "session work")

	commits, err := repo.SessionCommits(ctx, "main", "dispatch/t1")
	if err != nil || commits != 1 {
		t.Fatalf("commits = %d, err %v", commits, err)
	}
	if err := repo.Push(ctx, "dispatch/t1"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	gitTest(t, origin, "rev-parse", "--verify", "refs/heads/dispatch/t1")

	if _, err := PrepareWorkspace(ctx, workspace, origin, "main", "dispatch/t2", ""); err != nil {
		t.Fatalf("re-prepare on warm workspace: %v", err)
	}
}

func TestSplitAuthor(t *testing.T) {
	cases := map[string][2]string{
		"Duty Agent <duty@dispatch.local>": {"Duty Agent", "duty@dispatch.local"},
		"just-a-name":                      {"just-a-name", ""},
		"":                                 {"", ""},
	}
	for input, want := range cases {
		name, email := splitAuthor(input)
		if name != want[0] || email != want[1] {
			t.Errorf("splitAuthor(%q) = %q, %q", input, name, email)
		}
	}
}
