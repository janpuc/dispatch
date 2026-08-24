package shim

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHHost(t *testing.T) {
	cases := map[string]string{
		"git@github.com:janpuc/home-ops.git":     "github.com",
		"ssh://git@nas.home:2222/jan/sq.git":     "nas.home",
		"https://github.com/janpuc/dispatch.git": "",
		"":                                       "",
	}
	for input, want := range cases {
		if got := SSHHost(input); got != want {
			t.Errorf("SSHHost(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPrepareGitToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GITHUB_TOKEN", "ghs_testtoken")
	t.Setenv("HOME", home)

	cfg := Config{Home: home}
	if err := prepareGitToken(context.Background(), cfg); err != nil {
		t.Fatalf("prepareGitToken: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".git-credentials"))
	if err != nil {
		t.Fatalf("credentials file: %v", err)
	}
	if !strings.Contains(string(raw), "x-access-token:ghs_testtoken@github.com") {
		t.Errorf("credentials = %q", raw)
	}
	config, err := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if err != nil || !strings.Contains(string(config), "store --file=") {
		t.Errorf("gitconfig = %q err = %v", config, err)
	}
}

func TestPrepareGitSSHWritesKey(t *testing.T) {
	home := t.TempDir()
	credentials := t.TempDir()
	if err := os.WriteFile(filepath.Join(credentials, GitSSHKeyFileName), []byte("PRIVATE-KEY-NO-NEWLINE"), 0o600); err != nil {
		t.Fatal(err)
	}
	knownHosts := filepath.Join(home, ".ssh", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(knownHosts), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(knownHosts, []byte("github.com ssh-ed25519 AAAA\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_SSH_COMMAND", "")
	cfg := Config{Home: home, CredentialsDir: credentials}
	if err := prepareGitSSH(context.Background(), cfg, "git@github.com:janpuc/home-ops.git"); err != nil {
		t.Fatalf("prepareGitSSH: %v", err)
	}
	key, err := os.ReadFile(filepath.Join(home, ".ssh", "dispatch_deploy_key"))
	if err != nil {
		t.Fatalf("key file: %v", err)
	}
	if !strings.HasSuffix(string(key), "\n") {
		t.Error("key is missing the trailing newline ssh requires")
	}
	sshCommand := os.Getenv("GIT_SSH_COMMAND")
	if !strings.Contains(sshCommand, "dispatch_deploy_key") || !strings.Contains(sshCommand, "known_hosts") {
		t.Errorf("GIT_SSH_COMMAND = %q", sshCommand)
	}
}
