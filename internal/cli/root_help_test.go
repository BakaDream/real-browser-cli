package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPublicCommandsHaveShortDescriptions(t *testing.T) {
	root := newRoot()
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if !cmd.Hidden && strings.TrimSpace(cmd.Short) == "" {
			t.Fatalf("%s is missing Short", cmd.CommandPath())
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}

func TestRootHelpShowsCommandDescriptions(t *testing.T) {
	out := executeHelp(t, "--help")
	assertHelpContains(t, out, "open", "Navigate a tab to a URL")
	assertHelpContains(t, out, "snapshot", "Capture a page snapshot with element refs")
	assertHelpContains(t, out, "network", "Inspect and control network activity")
	assertHelpContains(t, out, "plugin", "Manage the browser extension files")
}

func TestGroupHelpShowsSubcommandDescriptions(t *testing.T) {
	daemon := executeHelp(t, "daemon", "--help")
	assertHelpContains(t, daemon, "start", "Start the local daemon")
	assertHelpOmitsCommand(t, daemon, "plugin")

	plugin := executeHelp(t, "plugin", "--help")
	assertHelpContains(t, plugin, "update", "Write the bundled browser extension files")
	assertHelpContains(t, plugin, "path", "Print the browser extension directory")

	tab := executeHelp(t, "tab", "--help")
	assertHelpContains(t, tab, "list", "List connected browser tabs")
	assertHelpContains(t, tab, "new", "Open a new browser tab")

	network := executeHelp(t, "network", "--help")
	assertHelpContains(t, network, "list", "List captured network requests")
	assertHelpContains(t, network, "har", "Record and export HAR data")
}

func executeHelp(t *testing.T, args ...string) string {
	t.Helper()
	var buf bytes.Buffer
	cmd := newRoot()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help command failed: %v", err)
	}
	return buf.String()
}

func assertHelpContains(t *testing.T, help string, command string, description string) {
	t.Helper()
	if !strings.Contains(help, command) || !strings.Contains(help, description) {
		t.Fatalf("help output missing %q with description %q:\n%s", command, description, help)
	}
}

func assertHelpOmitsCommand(t *testing.T, help string, command string) {
	t.Helper()
	for _, line := range strings.Split(help, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), command+" ") {
			t.Fatalf("help output should not contain command %q:\n%s", command, help)
		}
	}
}

func TestDaemonPluginCommandRemoved(t *testing.T) {
	for _, args := range [][]string{
		{"daemon", "plugin", "path"},
		{"daemon", "plugin", "update"},
	} {
		cmd := newRoot()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected %q to be unavailable", strings.Join(args, " "))
		}
	}
}
