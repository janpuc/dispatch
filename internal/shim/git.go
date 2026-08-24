package shim

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Repo is the session's git checkout inside the workspace.
type Repo struct {
	Dir string
}

func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// PrepareWorkspace clones the origin on first use, then syncs and cuts the
// session branch from the origin default branch (ADR-0005).
func PrepareWorkspace(ctx context.Context, root, gitURL, defaultBranch, sessionBranch, author string) (*Repo, error) {
	dir := filepath.Join(root, "repo")
	if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, os.ErrNotExist) {
		if _, err := gitRun(ctx, root, "clone", gitURL, dir); err != nil {
			return nil, err
		}
	}
	repo := &Repo{Dir: dir}
	if name, email := splitAuthor(author); name != "" {
		if _, err := gitRun(ctx, dir, "config", "user.name", name); err != nil {
			return nil, err
		}
		if _, err := gitRun(ctx, dir, "config", "user.email", email); err != nil {
			return nil, err
		}
	}
	if _, err := gitRun(ctx, dir, "fetch", "origin"); err != nil {
		return nil, err
	}
	if _, err := gitRun(ctx, dir, "checkout", "-B", sessionBranch, "origin/"+defaultBranch); err != nil {
		return nil, err
	}
	return repo, nil
}

func splitAuthor(author string) (string, string) {
	openIdx := strings.LastIndex(author, "<")
	closeIdx := strings.LastIndex(author, ">")
	if openIdx <= 0 || closeIdx < openIdx {
		return strings.TrimSpace(author), ""
	}
	return strings.TrimSpace(author[:openIdx]), strings.TrimSpace(author[openIdx+1 : closeIdx])
}

// SessionCommits counts commits the session branch carries beyond the origin
// default branch.
func (r *Repo) SessionCommits(ctx context.Context, defaultBranch, sessionBranch string) (int, error) {
	out, err := gitRun(ctx, r.Dir, "rev-list", "--count", "origin/"+defaultBranch+".."+sessionBranch)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(out)
}

// Push publishes the session branch to origin.
func (r *Repo) Push(ctx context.Context, sessionBranch string) error {
	_, err := gitRun(ctx, r.Dir, "push", "origin", sessionBranch)
	return err
}
