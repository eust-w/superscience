package notebook

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"superscience/internal/store"
)

type RunResult struct {
	CellID   string   `json:"cellId"`
	Status   string   `json:"status"`
	Kernel   string   `json:"kernel"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
	Outputs  []Output `json:"outputs"`
	Duration int64    `json:"durationMs"`
}

type Output struct {
	OutputType     string         `json:"output_type"`
	Name           string         `json:"name,omitempty"`
	Text           string         `json:"text,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	ExecutionCount any            `json:"execution_count,omitempty"`
	EName          string         `json:"ename,omitempty"`
	EValue         string         `json:"evalue,omitempty"`
	Traceback      []string       `json:"traceback,omitempty"`
}

type Manager struct {
	mu       sync.Mutex
	sessions map[string]*pythonSession
}

type KernelStatus struct {
	NotebookID string `json:"notebookId"`
	Kernel     string `json:"kernel"`
	Status     string `json:"status"`
}

func NewManager() *Manager {
	return &Manager{sessions: map[string]*pythonSession{}}
}

var defaultManager = NewManager()

func RunCell(ctx context.Context, cell store.NotebookCell) (RunResult, error) {
	return defaultManager.RunCell(ctx, cell)
}

func Interrupt(notebookID, language string) error {
	return defaultManager.Interrupt(notebookID, language)
}

func Restart(notebookID, language string) error {
	return defaultManager.Restart(notebookID, language)
}

func Status(notebookID, language string) KernelStatus {
	return defaultManager.Status(notebookID, language)
}

func (m *Manager) RunCell(ctx context.Context, cell store.NotebookCell) (RunResult, error) {
	start := time.Now()
	if cell.Kind != "" && cell.Kind != "code" {
		return RunResult{CellID: cell.ID, Status: "skipped", Duration: time.Since(start).Milliseconds()}, nil
	}
	language := strings.ToLower(strings.TrimSpace(cell.Language))
	if language == "" {
		language = "python"
	}
	kernelName, err := kernelNameForLanguage(language)
	if err != nil {
		return RunResult{}, err
	}
	return m.runKernel(ctx, cell, start, kernelName)
}

func kernelNameForLanguage(language string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "python", "python3", "py":
		return "python3", nil
	case "r", "ir":
		return "ir", nil
	case "julia":
		return "julia", nil
	default:
		return "", fmt.Errorf("unsupported notebook language %q", language)
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = map[string]*pythonSession{}
	m.mu.Unlock()
	for _, session := range sessions {
		session.close()
	}
}

func (m *Manager) Interrupt(notebookID, language string) error {
	kernelName, err := kernelNameForLanguage(language)
	if err != nil {
		return err
	}
	key := kernelName + ":" + normalizedNotebookID(notebookID)
	m.mu.Lock()
	session := m.sessions[key]
	delete(m.sessions, key)
	m.mu.Unlock()
	if session == nil || session.closed() {
		return fmt.Errorf("%s kernel for notebook %s is not running", kernelName, normalizedNotebookID(notebookID))
	}
	session.close()
	return nil
}

func (m *Manager) Restart(notebookID, language string) error {
	return m.Interrupt(notebookID, language)
}

func (m *Manager) Status(notebookID, language string) KernelStatus {
	kernelName, err := kernelNameForLanguage(language)
	if err != nil {
		return KernelStatus{NotebookID: normalizedNotebookID(notebookID), Kernel: language, Status: "unsupported"}
	}
	key := kernelName + ":" + normalizedNotebookID(notebookID)
	m.mu.Lock()
	session := m.sessions[key]
	m.mu.Unlock()
	status := "stopped"
	if session != nil && !session.closed() {
		status = "running"
	}
	return KernelStatus{NotebookID: normalizedNotebookID(notebookID), Kernel: kernelName, Status: status}
}

func (m *Manager) runKernel(ctx context.Context, cell store.NotebookCell, start time.Time, kernelName string) (RunResult, error) {
	if strings.TrimSpace(cell.Source) == "" {
		return RunResult{CellID: cell.ID, Status: "ok", Kernel: kernelName, Duration: time.Since(start).Milliseconds()}, nil
	}
	key := kernelName + ":" + normalizedNotebookID(cell.NotebookID)
	session, err := m.kernelSession(ctx, key, kernelName)
	if err != nil {
		return RunResult{}, err
	}
	result, err := session.run(ctx, cell.ID, cell.Source)
	result.Duration = time.Since(start).Milliseconds()
	if result.CellID == "" {
		result.CellID = cell.ID
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		m.dropSession(key)
	}
	return result, err
}

func (m *Manager) kernelSession(ctx context.Context, key, kernelName string) (*pythonSession, error) {
	m.mu.Lock()
	if session := m.sessions[key]; session != nil && !session.closed() {
		m.mu.Unlock()
		return session, nil
	}
	m.mu.Unlock()

	session, err := startJupyterSession(ctx, kernelName)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if existing := m.sessions[key]; existing != nil && !existing.closed() {
		m.mu.Unlock()
		session.close()
		return existing, nil
	}
	m.sessions[key] = session
	m.mu.Unlock()
	return session, nil
}

func (m *Manager) dropSession(key string) {
	m.mu.Lock()
	session := m.sessions[key]
	delete(m.sessions, key)
	m.mu.Unlock()
	if session != nil {
		session.close()
	}
}

func normalizedNotebookID(notebookID string) string {
	if strings.TrimSpace(notebookID) == "" {
		return "default"
	}
	return notebookID
}

type pythonSession struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  *bytes.Buffer
	done    chan struct{}
	kernel  string

	stateMu  sync.Mutex
	isClosed bool
}

type pythonResponse struct {
	ID      string   `json:"id"`
	Status  string   `json:"status"`
	Kernel  string   `json:"kernel"`
	Stdout  string   `json:"stdout"`
	Stderr  string   `json:"stderr"`
	Outputs []Output `json:"outputs"`
}

func startJupyterSession(ctx context.Context, kernelName string) (*pythonSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	worker, err := jupyterWorkerPath()
	if err != nil {
		return nil, err
	}
	python := jupyterPython(worker)
	cmd := exec.Command(python, "-u", worker, "--kernel", kernelName)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	session := &pythonSession{
		cmd:     cmd,
		stdin:   stdin,
		scanner: scanner,
		stderr:  stderr,
		done:    make(chan struct{}),
		kernel:  kernelName,
	}
	go func() {
		_ = cmd.Wait()
		session.markClosed()
		close(session.done)
	}()
	if !scanner.Scan() {
		message := session.closeAndReadStderr()
		if message == "" {
			message = "Jupyter kernel worker exited before ready"
		}
		return nil, errors.New(message)
	}
	var ready pythonResponse
	if err := json.Unmarshal(scanner.Bytes(), &ready); err != nil {
		session.close()
		return nil, fmt.Errorf("invalid Jupyter kernel ready response: %w", err)
	}
	if ready.Status != "ready" {
		session.close()
		return nil, fmt.Errorf("Jupyter kernel %s failed to become ready: %s", kernelName, ready.Stderr)
	}
	return session, nil
}

func jupyterWorkerPath() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("SUPERSCIENCE_JUPYTER_WORKER")); configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("configured Jupyter worker: %w", err)
		}
		return configured, nil
	}
	workingDirectory, _ := os.Getwd()
	for directory := workingDirectory; directory != ""; directory = filepath.Dir(directory) {
		candidate := filepath.Join(directory, "workers", "python", "jupyter_kernel.py")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
	}
	return "", errors.New("Jupyter worker script was not found; set SUPERSCIENCE_JUPYTER_WORKER")
}

func jupyterPython(worker string) string {
	if configured := strings.TrimSpace(os.Getenv("SUPERSCIENCE_PYTHON")); configured != "" {
		return configured
	}
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(worker)))
	for _, candidate := range []string{
		filepath.Join(projectRoot, ".venv", "bin", "python"),
		filepath.Join(projectRoot, ".venv", "Scripts", "python.exe"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "python3"
}

func (s *pythonSession) run(ctx context.Context, cellID string, source string) (RunResult, error) {
	if s.closed() {
		return RunResult{}, errors.New("python kernel session is closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, _ := json.Marshal(map[string]string{"op": "execute", "id": cellID, "source": source})
	done := make(chan struct {
		response pythonResponse
		err      error
	}, 1)
	go func() {
		if _, err := fmt.Fprintln(s.stdin, string(payload)); err != nil {
			done <- struct {
				response pythonResponse
				err      error
			}{err: err}
			return
		}
		if !s.scanner.Scan() {
			err := s.scanner.Err()
			if err == nil {
				err = errors.New("python kernel exited")
			}
			if extra := s.closeAndReadStderr(); extra != "" {
				err = fmt.Errorf("%w: %s", err, extra)
			}
			done <- struct {
				response pythonResponse
				err      error
			}{err: err}
			return
		}
		var response pythonResponse
		if err := json.Unmarshal(s.scanner.Bytes(), &response); err != nil {
			done <- struct {
				response pythonResponse
				err      error
			}{err: err}
			return
		}
		done <- struct {
			response pythonResponse
			err      error
		}{response: response}
	}()

	select {
	case <-ctx.Done():
		s.close()
		return RunResult{CellID: cellID, Status: "cancelled", Stderr: ctx.Err().Error()}, ctx.Err()
	case item := <-done:
		if item.err != nil {
			return RunResult{CellID: cellID, Status: "error", Stderr: item.err.Error()}, item.err
		}
		status := item.response.Status
		if status == "" {
			status = "ok"
		}
		return RunResult{CellID: item.response.ID, Status: status, Kernel: s.kernel, Stdout: item.response.Stdout, Stderr: item.response.Stderr, Outputs: item.response.Outputs}, nil
	}
}

func (s *pythonSession) close() {
	s.stateMu.Lock()
	if s.isClosed {
		s.stateMu.Unlock()
		return
	}
	s.isClosed = true
	s.stateMu.Unlock()
	_ = s.stdin.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

func (s *pythonSession) closeAndReadStderr() string {
	s.close()
	<-s.done
	return strings.TrimSpace(s.stderr.String())
}

func (s *pythonSession) closed() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.isClosed
}

func (s *pythonSession) markClosed() {
	s.stateMu.Lock()
	s.isClosed = true
	s.stateMu.Unlock()
}
