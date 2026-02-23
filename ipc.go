package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
)

type IPCRequest struct {
	Command string          `json:"command"`
	Args    json.RawMessage `json:"args,omitempty"`
}

type WatchArgs struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

type UnwatchArgs struct {
	Path string `json:"path"`
}

type IPCResponse struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

type StatusData struct {
	Watched []WatchEntry `json:"watched"`
	PID     int          `json:"pid"`
}

type WatchResponseData struct {
	Watched []WatchEntry `json:"watched"`
}

func ipcSend(req IPCRequest) (IPCResponse, error) {
	conn, err := net.Dial("unix", socketPath())
	if err != nil {
		return IPCResponse{}, fmt.Errorf("connecting to server: %w", err)
	}
	defer conn.Close()

	data, err := json.Marshal(req)
	if err != nil {
		return IPCResponse{}, fmt.Errorf("marshaling request: %w", err)
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		return IPCResponse{}, fmt.Errorf("writing request: %w", err)
	}

	buf := make([]byte, 64*1024)
	n, err := conn.Read(buf)
	if err != nil {
		return IPCResponse{}, fmt.Errorf("reading response: %w", err)
	}

	var resp IPCResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return IPCResponse{}, fmt.Errorf("parsing response: %w", err)
	}
	return resp, nil
}

func isServerNotRunning(err error) bool {
	if err == nil {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ECONNREFUSED) {
			return true
		}
		// Also check for "no such file or directory" (socket doesn't exist)
		if opErr.Err != nil {
			var sysErr *os.SyscallError
			if errors.As(opErr.Err, &sysErr) {
				return errors.Is(sysErr.Err, syscall.ENOENT) || errors.Is(sysErr.Err, syscall.ECONNREFUSED)
			}
		}
	}
	return false
}
