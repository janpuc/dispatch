// Package shim implements dispatch-run, the runner-side half of the session
// contract (ADR-0003): prepare the workspace, drive the agent CLI headless,
// record the transcript on the session record path, and report the result to
// the operator through the pod termination message.
package shim

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/janpuc/dispatch/internal/task"
)

const (
	transcriptFileName = "transcript.jsonl"
	stderrFileName     = "stderr.log"
	reportFileName     = "report.md"
	sessionBranchBase  = "dispatch/"
	interruptWaitDelay = 45 * time.Second
)

// Config is the runner-pod contract, populated from the environment the
// operator stamps into session Jobs.
type Config struct {
	TaskFile       string
	WorkspaceDir   string
	SessionsDir    string
	CredentialsDir string
	Home           string
	TerminationLog string
	Now            func() time.Time
}

// ConfigFromEnv reads the contract from the environment.
func ConfigFromEnv() Config {
	return Config{
		TaskFile:       envOr("DISPATCH_TASK_FILE", filepath.Join(task.MountPath, task.FileName)),
		WorkspaceDir:   envOr("DISPATCH_WORKSPACE", "/workspace"),
		SessionsDir:    os.Getenv("DISPATCH_SESSIONS"),
		CredentialsDir: envOr("DISPATCH_CREDENTIALS", "/credentials"),
		Home:           envOr("HOME", "/workspace/.dispatch-home"),
		TerminationLog: TerminationLogPath,
		Now:            time.Now,
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// SessionDir is where this session's record lands: the shared sessions volume
// when mounted, else inside the workspace (ADR-0005).
func (c Config) SessionDir(agent, session string) string {
	base := c.SessionsDir
	if base == "" {
		base = filepath.Join(c.WorkspaceDir, ".dispatch", "sessions")
	}
	return filepath.Join(base, agent, c.Now().UTC().Format("2006-01-02"), session)
}

// Run executes one session end to end and always leaves a Result behind: in
// the session directory and on the termination log. The error reports why the
// session failed.
func Run(ctx context.Context, cfg Config) (Result, error) {
	result := Result{Outcome: OutcomeFailed}

	doc, sessionDir, err := prepare(cfg)
	if err != nil {
		result.Summary = err.Error()
		writeResult(cfg, sessionDir, result)
		return result, err
	}

	err = execute(ctx, cfg, doc, sessionDir, &result)
	if err != nil && result.Summary == "" {
		result.Summary = err.Error()
	}
	writeResult(cfg, sessionDir, result)
	return result, err
}

func prepare(cfg Config) (task.File, string, error) {
	var doc task.File
	raw, err := os.ReadFile(cfg.TaskFile)
	if err != nil {
		return doc, "", fmt.Errorf("reading task file: %w", err)
	}
	if err := unmarshalTask(raw, &doc); err != nil {
		return doc, "", err
	}
	sessionDir := cfg.SessionDir(doc.Agent, doc.Session)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return doc, "", fmt.Errorf("creating session dir: %w", err)
	}
	if err := seedCredentials(cfg); err != nil {
		return doc, sessionDir, err
	}
	return doc, sessionDir, nil
}

func execute(ctx context.Context, cfg Config, doc task.File, sessionDir string, result *Result) error {
	result.Artifacts.Transcript = filepath.Join(sessionDir, transcriptFileName)
	doc.Prompt = strings.ReplaceAll(doc.Prompt, task.ReportPathToken, filepath.Join(sessionDir, reportFileName))

	workdir, repo, err := ensureWorkdir(ctx, cfg, doc)
	if err != nil {
		return err
	}

	runCtx := ctx
	if doc.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(doc.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	stream, waitErr, err := runCLI(runCtx, cfg, doc, workdir, sessionDir)
	if err != nil {
		return err
	}

	if stream != nil {
		result.SessionID = stream.SessionID
		result.Summary = truncate(stream.Result, 4000)
		result.Usage = Usage{
			Billing:          "subscription",
			InputTokens:      stream.Usage.InputTokens,
			OutputTokens:     stream.Usage.OutputTokens,
			CacheReadTokens:  stream.Usage.CacheReadInputTokens,
			APIEquivalentUSD: formatCost(stream.TotalCostUSD),
			Turns:            stream.NumTurns,
		}
	}
	if report := filepath.Join(sessionDir, reportFileName); fileExists(report) {
		result.Artifacts.Report = report
	}
	publishBranch(ctx, doc, repo, result)

	switch {
	case runCtx.Err() != nil:
		return fmt.Errorf("session timed out after %ds", doc.TimeoutSeconds)
	case waitErr != nil:
		return fmt.Errorf("agent CLI failed: %w", waitErr)
	case stream == nil:
		return fmt.Errorf("transcript ended without a result line")
	case stream.IsError:
		return fmt.Errorf("agent CLI reported an error result")
	}
	result.Outcome = OutcomeCompleted
	return nil
}

func ensureWorkdir(ctx context.Context, cfg Config, doc task.File) (string, *Repo, error) {
	if doc.GitURL == "" {
		scratch := filepath.Join(cfg.WorkspaceDir, "scratch")
		return scratch, nil, os.MkdirAll(scratch, 0o755)
	}
	branch := defaultBranch(doc)
	repo, err := PrepareWorkspace(ctx, cfg.WorkspaceDir, doc.GitURL, branch, sessionBranch(doc), doc.GitAuthor)
	if err != nil {
		return "", nil, err
	}
	return repo.Dir, repo, nil
}

func runCLI(ctx context.Context, cfg Config, doc task.File, workdir, sessionDir string) (*StreamResult, error, error) {
	transcript, err := os.Create(filepath.Join(sessionDir, transcriptFileName))
	if err != nil {
		return nil, nil, err
	}
	defer transcript.Close()
	stderrLog, err := os.Create(filepath.Join(sessionDir, stderrFileName))
	if err != nil {
		return nil, nil, err
	}
	defer stderrLog.Close()

	invocation := BuildInvocation(doc)
	cmd := exec.CommandContext(ctx, invocation.Bin, invocation.Args...)
	cmd.Dir = workdir
	cmd.Stdin = strings.NewReader(invocation.Prompt)
	cmd.Stderr = stderrLog
	cmd.Env = append(os.Environ(),
		"HOME="+cfg.Home,
		"DISPATCH_REPORT="+filepath.Join(sessionDir, reportFileName),
	)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = interruptWaitDelay

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("starting %s: %w", invocation.Bin, err)
	}

	scrubber := NewScrubber()
	stream, streamErr := ProcessStream(stdout, transcript, scrubber.Scrub)
	waitErr := cmd.Wait()
	if waitErr == nil && streamErr != nil {
		waitErr = streamErr
	}
	return stream, waitErr, nil
}

func publishBranch(ctx context.Context, doc task.File, repo *Repo, result *Result) {
	if repo == nil {
		return
	}
	branch := sessionBranch(doc)
	commits, err := repo.SessionCommits(ctx, defaultBranch(doc), branch)
	if err != nil || commits == 0 {
		return
	}
	result.Artifacts.Branches = append(result.Artifacts.Branches, branch)
	if err := repo.Push(ctx, branch); err != nil {
		result.FollowUps = append(result.FollowUps, "branch "+branch+" was not pushed: "+err.Error())
	}
}

func seedCredentials(cfg Config) error {
	entries, err := os.ReadDir(cfg.CredentialsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	target := filepath.Join(cfg.Home, ".claude")
	if err := os.MkdirAll(target, 0o700); err != nil {
		return err
	}
	for _, entry := range entries {
		source := filepath.Join(cfg.CredentialsDir, entry.Name())
		info, err := os.Stat(source)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		destination := filepath.Join(target, entry.Name())
		if fileExists(destination) {
			continue
		}
		if err := copyFile(source, destination); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func writeResult(cfg Config, sessionDir string, result Result) {
	if sessionDir != "" {
		_ = result.Write(sessionDir)
	}
	if cfg.TerminationLog != "" {
		_ = result.WriteTerminationMessage(cfg.TerminationLog)
	}
}

func sessionBranch(doc task.File) string {
	return sessionBranchBase + doc.Session
}

func defaultBranch(doc task.File) string {
	if doc.DefaultBranch != "" {
		return doc.DefaultBranch
	}
	return "main"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func formatCost(cost float64) string {
	if cost <= 0 {
		return ""
	}
	return fmt.Sprintf("%.4f", cost)
}
