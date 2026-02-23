package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Server struct {
	cfg      Config
	mu       sync.RWMutex
	watched  []WatchEntry
	prevDiffs map[string]string // repoPath -> last diff
	lastDate string
	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
}

func newServer(cfg Config) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		cfg:       cfg,
		prevDiffs: make(map[string]string),
		lastDate:  time.Now().Format("2006-01-02"),
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (s *Server) run() error {
	// Check PID file
	if pid, err := readPidFile(); err == nil {
		if isProcessRunning(pid) {
			fmt.Fprintf(os.Stderr, "devlog server is already running (PID %d)\n", pid)
			return nil
		}
		// Stale PID file
		os.Remove(pidFilePath())
	}

	// Write PID file
	pidPath := pidFilePath()
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		return fmt.Errorf("creating runtime dir: %w", err)
	}
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0o644); err != nil {
		return fmt.Errorf("writing PID file: %w", err)
	}
	defer os.Remove(pidPath)

	// Clean stale socket
	sockPath := socketPath()
	if _, err := os.Stat(sockPath); err == nil {
		// Socket exists — check if a server is listening
		conn, err := net.Dial("unix", sockPath)
		if err != nil {
			// Not listening — stale socket
			os.Remove(sockPath)
		} else {
			conn.Close()
			fmt.Fprintln(os.Stderr, "devlog server is already running")
			return nil
		}
	}

	// Create socket
	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("creating socket: %w", err)
	}
	s.listener = listener
	defer func() {
		listener.Close()
		os.Remove(sockPath)
	}()

	// Load persisted state
	state, _ := loadState()
	s.mu.Lock()
	s.watched = state.Watched
	s.mu.Unlock()

	log.Printf("devlog server started (PID %d), watching %d repos", os.Getpid(), len(s.watched))

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Start socket listener goroutine
	go s.acceptLoop()

	// Start snapshot ticker goroutine
	go s.snapshotLoop()

	// Wait for shutdown signal or context cancellation
	select {
	case sig := <-sigCh:
		log.Printf("received %v, shutting down", sig)
	case <-s.ctx.Done():
		log.Println("shutting down")
	}

	s.cancel()
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				log.Printf("accept error: %v", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return
	}

	var req IPCRequest
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		resp := IPCResponse{OK: false, Error: "invalid request"}
		data, _ := json.Marshal(resp)
		conn.Write(append(data, '\n'))
		return
	}

	var resp IPCResponse
	switch req.Command {
	case "watch":
		resp = s.handleWatch(req)
	case "unwatch":
		resp = s.handleUnwatch(req)
	case "status":
		resp = s.handleStatus()
	case "stop":
		resp = s.handleStop()
	default:
		resp = IPCResponse{OK: false, Error: "unknown command: " + req.Command}
	}

	data, _ := json.Marshal(resp)
	conn.Write(append(data, '\n'))
}

func (s *Server) handleWatch(req IPCRequest) IPCResponse {
	var args WatchArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return IPCResponse{OK: false, Error: "invalid args: " + err.Error()}
	}

	// Resolve repo root
	repoRoot, err := resolveRepoRoot(args.Path)
	if err != nil {
		return IPCResponse{OK: false, Error: err.Error()}
	}

	name := args.Name
	if name == "" {
		name = filepath.Base(repoRoot)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already watched
	for _, w := range s.watched {
		if w.Path == repoRoot {
			// Already watched, return current list
			return s.watchedResponse()
		}
	}

	// Check for name collision
	for _, w := range s.watched {
		if w.Name == name {
			return IPCResponse{OK: false, Error: fmt.Sprintf(
				"name conflict: %q is already used by %s", name, w.Path)}
		}
	}

	s.watched = append(s.watched, WatchEntry{Path: repoRoot, Name: name})
	s.persistState()

	return s.watchedResponse()
}

func (s *Server) handleUnwatch(req IPCRequest) IPCResponse {
	var args UnwatchArgs
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return IPCResponse{OK: false, Error: "invalid args: " + err.Error()}
	}

	repoRoot, err := resolveRepoRoot(args.Path)
	if err != nil {
		return IPCResponse{OK: false, Error: err.Error()}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	found := false
	var newWatched []WatchEntry
	for _, w := range s.watched {
		if w.Path == repoRoot {
			found = true
			delete(s.prevDiffs, w.Path)
			continue
		}
		newWatched = append(newWatched, w)
	}
	s.watched = newWatched

	if !found {
		return s.watchedResponse()
	}

	s.persistState()
	return s.watchedResponse()
}

func (s *Server) handleStatus() IPCResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, _ := json.Marshal(StatusData{
		Watched: s.watched,
		PID:     os.Getpid(),
	})
	return IPCResponse{OK: true, Data: json.RawMessage(data)}
}

func (s *Server) handleStop() IPCResponse {
	// Schedule shutdown after responding
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.cancel()
	}()
	data, _ := json.Marshal(struct{}{})
	return IPCResponse{OK: true, Data: json.RawMessage(data)}
}

func (s *Server) watchedResponse() IPCResponse {
	data, _ := json.Marshal(WatchResponseData{Watched: s.watched})
	return IPCResponse{OK: true, Data: json.RawMessage(data)}
}

func (s *Server) persistState() {
	state := State{Watched: s.watched}
	if err := saveState(state); err != nil {
		log.Printf("warning: failed to save state: %v", err)
	}
}

func (s *Server) snapshotLoop() {
	// Take an initial snapshot immediately
	s.takeSnapshots()

	interval := time.Duration(s.cfg.SnapshotInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.takeSnapshots()
		}
	}
}

func (s *Server) takeSnapshots() {
	today := time.Now().Format("2006-01-02")

	// Date boundary: reset dedup state
	if today != s.lastDate {
		s.prevDiffs = make(map[string]string)
		s.lastDate = today
	}

	s.mu.RLock()
	repos := make([]WatchEntry, len(s.watched))
	copy(repos, s.watched)
	s.mu.RUnlock()

	for _, entry := range repos {
		prevDiff := s.prevDiffs[entry.Path]
		gitFile := resolveGitPath(s.cfg, today, entry.Name)
		diff, err := takeSnapshot(entry.Path, entry.Name, gitFile, prevDiff)
		if err != nil {
			log.Printf("warning: snapshot %s (%s): %v", entry.Name, entry.Path, err)
			continue
		}
		if diff != "" {
			s.prevDiffs[entry.Path] = diff
		}
	}
}
