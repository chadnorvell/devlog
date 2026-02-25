package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ccEntry struct {
	Type      string     `json:"type"`
	Timestamp string     `json:"timestamp"`
	SessionID string     `json:"sessionId"`
	Message   *ccMessage `json:"message"`
}

type ccMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type ccContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func preprocessClaudeCodeSessions(dir string, date string, loc *time.Location) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return "", err
	}

	type sessionResult struct {
		transcript string
		firstTime  time.Time
	}

	var sessions []sessionResult
	for _, path := range matches {
		transcript, firstTime, err := parseSessionForDate(path, date, loc)
		if err != nil {
			continue
		}
		if transcript != "" {
			sessions = append(sessions, sessionResult{transcript, firstTime})
		}
	}

	if len(sessions) == 0 {
		return "", nil
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].firstTime.Before(sessions[j].firstTime)
	})

	var b strings.Builder
	for i, s := range sessions {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s.transcript)
	}

	return b.String(), nil
}

func parseSessionForDate(path string, targetDate string, loc *time.Location) (string, time.Time, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", time.Time{}, err
	}
	defer f.Close()

	var entries []ccEntry
	var firstTime time.Time

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var entry ccEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		if entry.Type != "user" && entry.Type != "assistant" {
			continue
		}
		if entry.Timestamp == "" || entry.Message == nil {
			continue
		}

		t, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			continue
		}
		localTime := t.In(loc)
		if localTime.Format("2006-01-02") != targetDate {
			continue
		}

		if firstTime.IsZero() || localTime.Before(firstTime) {
			firstTime = localTime
		}

		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return "", time.Time{}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "=== SESSION started %s ===\n", firstTime.Format("15:04"))

	for _, entry := range entries {
		if entry.Message.Role == "user" {
			text := extractUserText(entry.Message.Content)
			if text != "" {
				fmt.Fprintf(&b, "\n> %s\n", text)
			}
		} else if entry.Message.Role == "assistant" {
			blocks := extractAssistantBlocks(entry.Message.Content)
			for _, block := range blocks {
				switch block.Type {
				case "text":
					fmt.Fprintf(&b, "\n%s\n", block.Text)
				case "tool_use":
					summary := summarizeToolInput(block.Name, block.Input)
					fmt.Fprintf(&b, "\n%s\n", summary)
				}
			}
		}
	}

	return b.String(), firstTime, nil
}

func extractUserText(content json.RawMessage) string {
	// Try as string first
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}
	// If it's an array (tool results), skip
	return ""
}

func extractAssistantBlocks(content json.RawMessage) []ccContentBlock {
	var blocks []ccContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil
	}
	// Filter out thinking blocks
	var result []ccContentBlock
	for _, b := range blocks {
		if b.Type == "thinking" {
			continue
		}
		result = append(result, b)
	}
	return result
}

func hasEntriesOnDate(dir string, targetDate string, loc *time.Location) bool {
	matches, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return false
	}

	for _, path := range matches {
		if checkFileForDate(path, targetDate, loc) {
			return true
		}
	}
	return false
}

func checkFileForDate(path string, targetDate string, loc *time.Location) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var entry struct {
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Timestamp == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			continue
		}
		if t.In(loc).Format("2006-01-02") == targetDate {
			return true
		}
	}
	return false
}

func summarizeToolInput(name string, input json.RawMessage) string {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("[Tool: %s]", name)
	}

	var keyParam string
	switch name {
	case "Read", "Edit", "Write":
		keyParam = "file_path"
	case "Bash":
		keyParam = "command"
	case "Grep", "Glob":
		keyParam = "pattern"
	case "WebSearch":
		keyParam = "query"
	case "WebFetch":
		keyParam = "url"
	case "Task":
		keyParam = "prompt"
	default:
		return fmt.Sprintf("[Tool: %s]", name)
	}

	raw, ok := params[keyParam]
	if !ok {
		return fmt.Sprintf("[Tool: %s]", name)
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Sprintf("[Tool: %s]", name)
	}

	return fmt.Sprintf("[Tool: %s %s=%q]", name, keyParam, value)
}
