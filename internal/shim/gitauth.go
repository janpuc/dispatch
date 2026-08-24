package shim

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitSSHKeyFileName is the credentials-Secret key holding a private deploy
// key; when present, the shim wires SSH-based git access with it.
const GitSSHKeyFileName = "GIT_SSH_KEY"

func prepareGitAuth(ctx context.Context, cfg Config, gitURL string) error {
	if gitURL == "" {
		return nil
	}
	os.Setenv("GIT_TERMINAL_PROMPT", "0")
	if err := prepareGitToken(ctx, cfg); err != nil {
		return err
	}
	return prepareGitSSH(ctx, cfg, gitURL)
}

func prepareGitToken(ctx context.Context, cfg Config) error {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token == "" {
		return nil
	}
	credentials := "https://x-access-token:" + token + "@github.com\n"
	credentialsPath := filepath.Join(cfg.Home, ".git-credentials")
	if err := os.WriteFile(credentialsPath, []byte(credentials), 0o600); err != nil {
		return err
	}
	_, err := gitRun(ctx, cfg.Home,
		"config", "--file", filepath.Join(cfg.Home, ".gitconfig"),
		"credential.helper", "store --file="+credentialsPath,
	)
	return err
}

func prepareGitSSH(ctx context.Context, cfg Config, gitURL string) error {
	source := filepath.Join(cfg.CredentialsDir, GitSSHKeyFileName)
	if !fileExists(source) {
		return nil
	}
	host := SSHHost(gitURL)
	if host == "" {
		return nil
	}

	sshDir := filepath.Join(cfg.Home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return err
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		raw = append(raw, '\n')
	}
	keyPath := filepath.Join(sshDir, "dispatch_deploy_key")
	if err := os.WriteFile(keyPath, raw, 0o600); err != nil {
		return err
	}

	knownHosts := filepath.Join(sshDir, "known_hosts")
	if !fileExists(knownHosts) {
		if err := scanHostKeys(ctx, host, knownHosts); err != nil {
			return err
		}
	}

	os.Setenv("GIT_SSH_COMMAND", fmt.Sprintf(
		"ssh -i %s -o UserKnownHostsFile=%s -o IdentitiesOnly=yes", keyPath, knownHosts,
	))
	return nil
}

func scanHostKeys(ctx context.Context, host, destination string) error {
	out, err := exec.CommandContext(ctx, "ssh-keyscan", host).Output()
	if err != nil {
		return fmt.Errorf("scanning host keys for %s: %w", host, err)
	}
	return os.WriteFile(destination, out, 0o644)
}

// SSHHost extracts the SSH host from a git remote URL; empty for non-SSH
// remotes.
func SSHHost(gitURL string) string {
	if strings.HasPrefix(gitURL, "ssh://") {
		parsed, err := url.Parse(gitURL)
		if err != nil {
			return ""
		}
		return parsed.Hostname()
	}
	at := strings.Index(gitURL, "@")
	colon := strings.Index(gitURL, ":")
	if at > 0 && colon > at && !strings.Contains(gitURL, "://") {
		return gitURL[at+1 : colon]
	}
	return ""
}
