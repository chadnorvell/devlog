package main

import (
	"encoding/json"
	"testing"
)

func TestIPCRequestSerialization(t *testing.T) {
	args, _ := json.Marshal(WatchArgs{Path: "/home/user/dev/foo", Name: "foo"})
	req := IPCRequest{
		Command: "watch",
		Args:    json.RawMessage(args),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded IPCRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Command != "watch" {
		t.Errorf("expected watch, got %q", decoded.Command)
	}

	var decodedArgs WatchArgs
	if err := json.Unmarshal(decoded.Args, &decodedArgs); err != nil {
		t.Fatalf("unmarshal args: %v", err)
	}
	if decodedArgs.Path != "/home/user/dev/foo" {
		t.Errorf("expected /home/user/dev/foo, got %q", decodedArgs.Path)
	}
}

func TestIPCResponseSerialization(t *testing.T) {
	// Success response
	data, _ := json.Marshal(WatchResponseData{
		Watched: []WatchEntry{{Path: "/a", Name: "a"}},
	})
	resp := IPCResponse{OK: true, Data: json.RawMessage(data)}

	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded IPCResponse
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.OK {
		t.Error("expected ok=true")
	}

	// Error response
	resp = IPCResponse{OK: false, Error: "something went wrong"}
	encoded, _ = json.Marshal(resp)
	json.Unmarshal(encoded, &decoded)
	if decoded.OK {
		t.Error("expected ok=false")
	}
	if decoded.Error != "something went wrong" {
		t.Errorf("expected error message, got %q", decoded.Error)
	}
}

func TestIsServerNotRunning(t *testing.T) {
	// Set socket to a path that doesn't exist
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	_, err := ipcSend(IPCRequest{Command: "status"})
	if err == nil {
		t.Fatal("expected error connecting to nonexistent socket")
	}
	if !isServerNotRunning(err) {
		t.Errorf("expected isServerNotRunning=true for error: %v", err)
	}
}
