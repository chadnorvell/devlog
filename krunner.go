package main

import (
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	krunnerBusName   = "org.chadnorvell.devlog.krunner"
	krunnerPath      = "/krunner"
	krunnerInterface = "org.kde.krunner1"
)

// KRunner implements the org.kde.krunner1 D-Bus interface.
type KRunner struct {
	server *Server
}

// RemoteMatch is a KRunner match result (D-Bus signature: sssida{sv}).
type RemoteMatch struct {
	ID                string
	Text              string
	IconName          string
	CategoryRelevance int32
	Relevance         float64
	Properties        map[string]dbus.Variant
}

// Actions returns available sub-actions (none for devlog).
func (k *KRunner) Actions() ([]struct {
	ID       string
	Text     string
	IconName string
}, *dbus.Error) {
	return nil, nil
}

// Match responds to KRunner queries starting with #.
func (k *KRunner) Match(query string) ([]RemoteMatch, *dbus.Error) {
	if !strings.HasPrefix(query, "#") {
		return nil, nil
	}

	project, content := parseKRunnerQuery(query)
	if project == "" {
		return nil, nil
	}

	k.server.mu.RLock()
	watched := make([]WatchEntry, len(k.server.watched))
	copy(watched, k.server.watched)
	k.server.mu.RUnlock()

	var matches []RemoteMatch
	exactFound := false
	for _, w := range watched {
		if !strings.HasPrefix(w.Name, project) {
			continue
		}

		var catRelevance int32
		var relevance float64
		if w.Name == project {
			// ExactMatch
			catRelevance = 100
			relevance = 1.0
			exactFound = true
		} else {
			// PossibleMatch (prefix)
			catRelevance = 10
			relevance = 0.5
		}

		matchID := encodeMatchID(w.Name, content)
		text := "#" + w.Name
		if content != "" {
			text += " " + content
		}

		matches = append(matches, RemoteMatch{
			ID:                matchID,
			Text:              text,
			IconName:          "document-edit",
			CategoryRelevance: catRelevance,
			Relevance:         relevance,
			Properties:        map[string]dbus.Variant{},
		})
	}

	// If the project name didn't exactly match a watched project,
	// offer it as a lower-relevance option so users can log notes
	// for unwatched projects.
	if !exactFound && content != "" {
		matchID := encodeMatchID(project, content)
		text := "#" + project + " " + content
		matches = append(matches, RemoteMatch{
			ID:                matchID,
			Text:              text,
			IconName:          "document-edit",
			CategoryRelevance: 10,
			Relevance:         0.3,
			Properties: map[string]dbus.Variant{
				"subtext": dbus.MakeVariant("unwatched project"),
			},
		})
	}

	return matches, nil
}

// Run executes the selected match action.
func (k *KRunner) Run(matchID string, actionID string) *dbus.Error {
	project, content := decodeMatchID(matchID)
	if project == "" {
		return nil
	}

	executable, err := os.Executable()
	if err != nil {
		log.Printf("krunner: executable err: %v", err)
		return nil
	}

	var cmd *exec.Cmd
	if strings.TrimSpace(content) == "" {
		cmd = exec.Command(executable, "-g", "-p", project)
	} else {
		cmd = exec.Command(executable, "-m", content, "-p", project)
	}

	if err := cmd.Start(); err != nil {
		log.Printf("krunner: start command err: %v", err)
		return nil
	}
	go cmd.Wait()
	log.Printf("krunner: launched `%s`", strings.Join(cmd.Args, " "))

	return nil
}

// Teardown is called when KRunner unloads the plugin.
func (k *KRunner) Teardown() *dbus.Error {
	return nil
}

// parseKRunnerQuery splits a #-prefixed query into project prefix and content.
// Input: "#proj some content" -> ("proj", "some content")
// Input: "#proj" -> ("proj", "")
func parseKRunnerQuery(s string) (project, content string) {
	if !strings.HasPrefix(s, "#") {
		return "", ""
	}
	s = s[1:]
	if s == "" {
		return "", ""
	}
	idx := strings.IndexByte(s, ' ')
	if idx < 0 {
		return s, ""
	}
	return s[:idx], strings.TrimSpace(s[idx+1:])
}

func encodeMatchID(project, content string) string {
	return project + ":" + content
}

func decodeMatchID(matchID string) (project, content string) {
	idx := strings.IndexByte(matchID, ':')
	if idx < 0 {
		return matchID, ""
	}
	return matchID[:idx], matchID[idx+1:]
}

const krunnerIntrospectXML = `
<node>
  <interface name="org.kde.krunner1">
    <method name="Actions">
      <arg name="matches" type="a(sss)" direction="out"/>
    </method>
    <method name="Match">
      <arg name="query" type="s" direction="in"/>
      <arg name="matches" type="a(sssida{sv})" direction="out"/>
    </method>
    <method name="Run">
      <arg name="matchId" type="s" direction="in"/>
      <arg name="actionId" type="s" direction="in"/>
    </method>
    <method name="Teardown">
    </method>
  </interface>
</node>
`

// startKRunner attempts to register on the D-Bus session bus as a KRunner plugin.
// Returns a cleanup function, or nil if D-Bus or kdialog is unavailable.
func startKRunner(s *Server) func() {
	if _, err := exec.LookPath("kdialog"); err != nil {
		log.Printf("krunner: kdialog not found, skipping D-Bus registration")
		return nil
	}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("krunner: D-Bus session bus unavailable, skipping: %v", err)
		return nil
	}

	kr := &KRunner{server: s}

	if err := conn.Export(kr, krunnerPath, krunnerInterface); err != nil {
		log.Printf("krunner: failed to export interface: %v", err)
		conn.Close()
		return nil
	}

	if err := conn.Export(introspect.Introspectable(krunnerIntrospectXML), krunnerPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		log.Printf("krunner: failed to export introspection: %v", err)
		conn.Close()
		return nil
	}

	reply, err := conn.RequestName(krunnerBusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		log.Printf("krunner: failed to request bus name: %v", err)
		conn.Close()
		return nil
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		log.Printf("krunner: bus name %s already taken", krunnerBusName)
		conn.Close()
		return nil
	}

	log.Printf("krunner: registered on D-Bus as %s", krunnerBusName)

	return func() {
		conn.ReleaseName(krunnerBusName)
		conn.Close()
		log.Printf("krunner: unregistered from D-Bus")
	}
}
