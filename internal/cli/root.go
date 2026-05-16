package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bakadream/real-browser-cli/internal/runtime"
	"github.com/bakadream/real-browser-cli/internal/server"
	"github.com/spf13/cobra"
)

type globalOptions struct {
	JSON     bool
	Quiet    bool
	DebugIDs bool
	Timeout  float64
	Tab      string
}

var globals globalOptions

func Execute() error {
	return newRoot().Execute()
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "real-browser",
		Short:         "Browser automation CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&globals.JSON, "json", false, "output JSON")
	root.PersistentFlags().BoolVar(&globals.Quiet, "quiet", false, "only output essential values")
	root.PersistentFlags().BoolVar(&globals.DebugIDs, "debug-ids", false, "include browser internal ids")
	root.PersistentFlags().Float64Var(&globals.Timeout, "timeout", 30, "request timeout seconds")
	root.PersistentFlags().StringVar(&globals.Tab, "tab", "", "target tab handle, label, or Chrome tab id")

	root.AddCommand(
		versionCmd(),
		updateCmd(),
		updateHelperCmd(),
		doctorCmd(),
		daemonCmd(),
		pluginCmd(),
		tabCmd(),
		openCmd(),
		backForwardCmd("back"),
		backForwardCmd("forward"),
		reloadCmd(),
		snapshotCmd(),
		getCmd(),
		actionCmd("click", true, false),
		actionCmd("dblclick", true, false),
		actionCmd("hover", true, false),
		actionCmd("focus", true, false),
		fillCmd(),
		typeCmd(),
		pressCmd(),
		actionCmd("select", true, true),
		actionCmd("check", true, false),
		actionCmd("uncheck", true, false),
		scrollCmd(),
		actionCmd("drag", true, true),
		uploadCmd(),
		waitCmd(),
		evalCmd(),
		cdpCmd(),
		cookiesCmd(),
		storageCmd(),
		screenshotCmd(),
		pdfCmd(),
		consoleCmd(),
		errorsCmd(),
		networkCmd(),
		dialogCmd(),
		traceCmd(),
		exportCmd(),
		batchCmd(),
	)
	return root
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{Use: "doctor", Short: "Check daemon and browser plugin health", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("doctor", nil)
	}}
}

func daemonCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "daemon", Short: "Manage the local daemon", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	}}
	cmd.AddCommand(
		daemonStartCmd(),
		daemonStatusCmd(),
		daemonStopCmd(),
		daemonRestartCmd(),
		daemonRunCmd(),
		daemonTokenCmd(),
	)
	return cmd
}

func daemonRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "run",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			errCh := make(chan error, 1)
			go func() { errCh <- server.RunDaemon() }()
			select {
			case <-ctx.Done():
				return nil
			case err := <-errCh:
				return err
			}
		},
	}
}

func daemonStartCmd() *cobra.Command {
	return &cobra.Command{Use: "start", Short: "Start the local daemon", RunE: func(cmd *cobra.Command, args []string) error {
		alreadyStarted := isAlive()
		if err := ensureServer(); err != nil {
			_ = printDaemonStartFailed()
			return err
		}
		return printDaemonStart(alreadyStarted)
	}}
}

func daemonStatusCmd() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show daemon and plugin status", RunE: func(cmd *cobra.Command, args []string) error {
		data, err := request(http.MethodGet, "/v1/health", nil, time.Second)
		if err != nil {
			return printDaemonStatus(daemonHealth{})
		}
		if globals.JSON {
			return printRaw(data)
		}
		health, err := parseDaemonHealth(data)
		if err != nil {
			return err
		}
		return printDaemonStatus(health)
	}}
}

func daemonStopCmd() *cobra.Command {
	return &cobra.Command{Use: "stop", Short: "Stop the local daemon", RunE: func(cmd *cobra.Command, args []string) error {
		_, err := request(http.MethodPost, "/v1/shutdown", map[string]any{}, 3*time.Second)
		if err != nil {
			if !isAlive() {
				return printDaemonStatus(daemonHealth{})
			}
			_ = printDaemonStopFailed()
			return err
		}
		return printDaemonStopSuccess()
	}}
}

func daemonRestartCmd() *cobra.Command {
	return &cobra.Command{Use: "restart", Short: "Restart the local daemon", RunE: func(cmd *cobra.Command, args []string) error {
		_, _ = request(http.MethodPost, "/v1/shutdown", map[string]any{}, 3*time.Second)
		waitServerStopped(5 * time.Second)
		if err := ensureServer(); err != nil {
			_ = printDaemonRestartFailed()
			return err
		}
		data, err := request(http.MethodGet, "/v1/health", nil, 3*time.Second)
		if err != nil {
			_ = printDaemonRestartFailed()
			return err
		}
		health, err := parseDaemonHealth(data)
		if err != nil {
			return err
		}
		return printDaemonRestartSuccess(health)
	}}
}

func daemonTokenCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage daemon authentication token"}
	cmd.AddCommand(&cobra.Command{Use: "rotate", Short: "Rotate the daemon token and refresh plugin files", RunE: func(cmd *cobra.Command, args []string) error {
		_, _ = request(http.MethodPost, "/v1/shutdown", map[string]any{}, 3*time.Second)
		waitServerStopped(5 * time.Second)
		paths, cfg, err := runtime.RotateToken()
		if err != nil {
			return err
		}
		if err := runtime.ReleasePlugin(paths, cfg.Token); err != nil {
			return err
		}
		return printLocalResponse("daemon.token.rotate", map[string]any{"pluginPath": paths.PluginDir, "message": "token rotated; reload the unpacked browser extension"})
	}})
	return cmd
}

func pluginCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "plugin", Short: "Manage the browser extension files"}
	cmd.AddCommand(&cobra.Command{Use: "update", Short: "Write the bundled browser extension files", RunE: func(cmd *cobra.Command, args []string) error {
		paths, cfg, err := runtime.EnsureConfig()
		if err != nil {
			return err
		}
		if err := runtime.ReleasePlugin(paths, cfg.Token); err != nil {
			return err
		}
		if hash, err := runtime.PluginTemplateHash(); err == nil {
			cfg.PluginTemplateHash = hash
			cfg.PluginReleasedAt = time.Now().UTC().Format(time.RFC3339)
			cfg.UpdatedAt = cfg.PluginReleasedAt
			_ = runtime.SaveConfig(paths, cfg)
		}
		return printLocalResponse("plugin.update", map[string]any{"path": paths.PluginDir, "updated": true})
	}})
	cmd.AddCommand(&cobra.Command{Use: "path", Short: "Print the browser extension directory", RunE: func(cmd *cobra.Command, args []string) error {
		paths, cfg, err := runtime.EnsureConfig()
		if err != nil {
			return err
		}
		_, released, err := runtime.EnsurePluginReleased(paths, cfg)
		if err != nil {
			return err
		}
		return printLocalResponse("plugin.path", map[string]any{"path": paths.PluginDir, "released": released})
	}})
	return cmd
}

func tabCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "tab", Short: "Manage browser tabs", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	}}
	cmd.AddCommand(&cobra.Command{Use: "list", Short: "List connected browser tabs", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("tab.list", nil)
	}})
	var label string
	var background bool
	newCmd := &cobra.Command{Use: "new [url]", Short: "Open a new browser tab", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		url := "about:blank"
		if len(args) > 0 {
			url = args[0]
		}
		return runRPC("tab.new", map[string]any{"url": url, "label": label, "background": background})
	}}
	newCmd.Flags().StringVar(&label, "label", "", "tab label")
	newCmd.Flags().BoolVar(&background, "background", false, "open in background")
	cmd.AddCommand(newCmd)
	cmd.AddCommand(&cobra.Command{Use: "use <tab>", Short: "Set the default browser tab", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("tab.use", map[string]any{"tab": args[0]})
	}})
	cmd.AddCommand(&cobra.Command{Use: "close [tab]", Short: "Close a browser tab", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		params := map[string]any{}
		if len(args) > 0 {
			params["tab"] = args[0]
		}
		return runRPC("tab.close", params)
	}})
	cmd.AddCommand(&cobra.Command{Use: "label <tab> <label>", Short: "Assign a label to a browser tab", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("tab.label", map[string]any{"tab": args[0], "label": args[1]})
	}})
	return cmd
}

func openCmd() *cobra.Command {
	var newTab bool
	var background bool
	cmd := &cobra.Command{Use: "open <url>", Short: "Navigate a tab to a URL", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("open", map[string]any{"url": args[0], "newTab": newTab, "background": background})
	}}
	cmd.Flags().BoolVar(&newTab, "new-tab", false, "open in a new tab")
	cmd.Flags().BoolVar(&background, "background", false, "open in background")
	return cmd
}

func backForwardCmd(name string) *cobra.Command {
	short := map[string]string{
		"back":    "Navigate back in the default tab",
		"forward": "Navigate forward in the default tab",
	}[name]
	return &cobra.Command{Use: name, Short: short, RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC(name, nil)
	}}
}

func reloadCmd() *cobra.Command {
	var hard bool
	cmd := &cobra.Command{Use: "reload", Short: "Reload the default tab", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("reload", map[string]any{"hard": hard})
	}}
	cmd.Flags().BoolVar(&hard, "hard", false, "ignore cache")
	return cmd
}

func snapshotCmd() *cobra.Command {
	var locators bool
	var text bool
	var selector string
	cmd := &cobra.Command{Use: "snapshot", Short: "Capture a page snapshot with element refs", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("snapshot", map[string]any{"locators": locators, "text": text, "selector": selector})
	}}
	cmd.Flags().BoolVar(&locators, "locators", false, "include locator candidates")
	cmd.Flags().BoolVar(&text, "text", false, "text-only snapshot")
	cmd.Flags().StringVar(&selector, "selector", "", "snapshot a DOM subtree")
	return cmd
}

func getCmd() *cobra.Command {
	return &cobra.Command{Use: "get title|url|text|html|markdown|value|attr|box|count|styles [target] [name]", Short: "Read page or element data", Args: cobra.RangeArgs(1, 3), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		params := map[string]any{"kind": args[0]}
		if len(args) > 1 {
			params["target"] = args[1]
		}
		if len(args) > 2 {
			params["name"] = args[2]
		}
		return runRPC("get", params)
	}}
}

func actionCmd(name string, needsTarget bool, needsValue bool) *cobra.Command {
	use := name
	if needsTarget {
		use += " <target>"
	}
	if needsValue {
		use += " <value>"
	}
	short := map[string]string{
		"click":    "Click an element",
		"dblclick": "Double-click an element",
		"hover":    "Hover over an element",
		"focus":    "Focus an element",
		"select":   "Select an option value",
		"check":    "Check a checkbox or radio",
		"uncheck":  "Uncheck a checkbox or radio",
		"drag":     "Drag one element onto another",
	}[name]
	return &cobra.Command{Use: use, Short: short, Args: func(cmd *cobra.Command, args []string) error {
		required := 0
		if needsTarget {
			required++
		}
		if needsValue {
			required++
		}
		return cobra.ExactArgs(required)(cmd, args)
	}, RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		params := map[string]any{}
		if needsTarget {
			params["target"] = args[0]
		}
		if needsValue {
			params["value"] = args[1]
		}
		return runRPC("action."+name, params)
	}}
}

func fillCmd() *cobra.Command {
	var clear bool
	cmd := &cobra.Command{Use: "fill <target> <text>", Short: "Fill an input or editable element", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("action.fill", map[string]any{"target": args[0], "value": args[1], "clear": clear})
	}}
	cmd.Flags().BoolVar(&clear, "clear", false, "clear before filling")
	return cmd
}

func typeCmd() *cobra.Command {
	return &cobra.Command{Use: "type <text>", Short: "Type text into the active element", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("action.type", map[string]any{"value": args[0]})
	}}
}

func pressCmd() *cobra.Command {
	return &cobra.Command{Use: "press <key>", Short: "Press a keyboard key", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("action.press", map[string]any{"value": args[0]})
	}}
}

func scrollCmd() *cobra.Command {
	var x, y float64
	var target string
	cmd := &cobra.Command{Use: "scroll", Short: "Scroll the page or an element", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("action.scroll", map[string]any{"x": x, "y": y, "target": target})
	}}
	cmd.Flags().Float64Var(&x, "x", 0, "horizontal scroll")
	cmd.Flags().Float64Var(&y, "y", 0, "vertical scroll")
	cmd.Flags().StringVar(&target, "target", "", "target element")
	return cmd
}

func uploadCmd() *cobra.Command {
	return &cobra.Command{Use: "upload <target> <path>", Short: "Upload a file through a file input", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("action.upload", map[string]any{"target": args[0], "path": args[1]})
	}}
}

func waitCmd() *cobra.Command {
	var ms int
	var text, selector, ref, js, load string
	cmd := &cobra.Command{Use: "wait [milliseconds]", Short: "Wait for time, text, selectors, refs, JS, or load state", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		if len(args) == 1 {
			var parsed int
			if _, err := fmt.Sscanf(args[0], "%d", &parsed); err != nil {
				return fmt.Errorf("wait milliseconds must be an integer")
			}
			ms = parsed
		}
		return runRPC("wait", map[string]any{"ms": ms, "text": text, "selector": selector, "ref": ref, "js": js, "load": load, "timeout": globals.Timeout})
	}}
	cmd.Flags().IntVar(&ms, "ms", 0, "sleep milliseconds")
	cmd.Flags().StringVar(&text, "text", "", "wait for text")
	cmd.Flags().StringVar(&selector, "selector", "", "wait for selector")
	cmd.Flags().StringVar(&ref, "ref", "", "wait for ref")
	cmd.Flags().StringVar(&js, "js", "", "wait for JS predicate")
	cmd.Flags().StringVar(&load, "load", "", "wait for domcontentloaded|load|networkidle")
	return cmd
}

func evalCmd() *cobra.Command {
	var file string
	var stdin bool
	var waitJS string
	var waitTimeout, waitInterval float64
	cmd := &cobra.Command{Use: "eval [js]", Short: "Run JavaScript in the default tab", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		script := ""
		if len(args) > 0 {
			script = args[0]
		}
		if file != "" {
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			script = string(data)
		}
		if stdin {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			script = string(data)
		}
		return runRPC("eval", map[string]any{"script": script, "waitJs": waitJS, "waitTimeout": waitTimeout, "waitInterval": waitInterval})
	}}
	cmd.Flags().StringVar(&file, "file", "", "read JS from file")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "read JS from stdin")
	cmd.Flags().StringVar(&waitJS, "wait-js", "", "condition JS")
	cmd.Flags().Float64Var(&waitTimeout, "wait-timeout", 3, "wait timeout seconds")
	cmd.Flags().Float64Var(&waitInterval, "wait-interval", 0.1, "wait interval seconds")
	return cmd
}

func cdpCmd() *cobra.Command {
	var paramsFlag string
	cmd := &cobra.Command{Use: "cdp <method> [params-json]", Short: "Send a Chrome DevTools Protocol command", Args: cobra.RangeArgs(1, 2), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		raw := paramsFlag
		if len(args) > 1 {
			raw = args[1]
		}
		params := map[string]any{}
		if raw != "" {
			if err := json.Unmarshal([]byte(raw), &params); err != nil {
				return err
			}
		}
		return runRPC("cdp", map[string]any{"method": args[0], "params": params})
	}}
	cmd.Flags().StringVar(&paramsFlag, "params", "", "CDP params JSON")
	return cmd
}

func cookiesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cookies", Short: "Inspect and modify browser cookies"}
	var url, name, value, domain, path string
	list := &cobra.Command{Use: "list", Short: "List cookies", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("cookies.list", map[string]any{"url": url})
	}}
	list.Flags().StringVar(&url, "url", "", "cookie URL")
	set := &cobra.Command{Use: "set", Short: "Set a cookie", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("cookies.set", map[string]any{"url": url, "name": name, "value": value, "domain": domain, "path": path})
	}}
	set.Flags().StringVar(&url, "url", "", "cookie URL")
	set.Flags().StringVar(&name, "name", "", "cookie name")
	set.Flags().StringVar(&value, "value", "", "cookie value")
	set.Flags().StringVar(&domain, "domain", "", "cookie domain")
	set.Flags().StringVar(&path, "path", "", "cookie path")
	del := &cobra.Command{Use: "delete", Short: "Delete a cookie", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("cookies.delete", map[string]any{"url": url, "name": name})
	}}
	del.Flags().StringVar(&url, "url", "", "cookie URL")
	del.Flags().StringVar(&name, "name", "", "cookie name")
	clear := &cobra.Command{Use: "clear", Short: "Clear cookies", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("cookies.clear", map[string]any{"url": url})
	}}
	clear.Flags().StringVar(&url, "url", "", "cookie URL")
	cmd.AddCommand(list, set, del, clear)
	return cmd
}

func storageCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "storage", Short: "Inspect and modify web storage"}
	cmd.AddCommand(storageAreaCmd("local"), storageAreaCmd("session"))
	return cmd
}

func storageAreaCmd(area string) *cobra.Command {
	areaName := map[string]string{"local": "localStorage", "session": "sessionStorage"}[area]
	cmd := &cobra.Command{Use: area, Short: "Manage " + areaName}
	shorts := map[string]string{
		"get":    "Get a " + areaName + " value",
		"set":    "Set a " + areaName + " value",
		"delete": "Delete a " + areaName + " value",
		"clear":  "Clear " + areaName,
	}
	for _, op := range []string{"get", "set", "delete", "clear"} {
		op := op
		cmd.AddCommand(&cobra.Command{Use: op + " [key] [value]", Short: shorts[op], Args: cobra.RangeArgs(0, 2), RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureServer(); err != nil {
				return err
			}
			params := map[string]any{}
			if len(args) > 0 {
				params["key"] = args[0]
			}
			if len(args) > 1 {
				params["value"] = args[1]
			}
			return runRPC("storage."+area+"."+op, params)
		}})
	}
	return cmd
}

func screenshotCmd() *cobra.Command {
	var full, annotate bool
	cmd := &cobra.Command{Use: "screenshot [path]", Short: "Capture a page screenshot", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		params := map[string]any{"full": full, "annotate": annotate}
		if len(args) > 0 {
			params["path"] = args[0]
		}
		return runRPC("screenshot", params)
	}}
	cmd.Flags().BoolVar(&full, "full", false, "full page screenshot")
	cmd.Flags().BoolVar(&annotate, "annotate", false, "annotate refs before screenshot")
	return cmd
}

func pdfCmd() *cobra.Command {
	return &cobra.Command{Use: "pdf <path>", Short: "Save the page as a PDF", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("pdf", map[string]any{"path": args[0]})
	}}
}

func consoleCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "console", Short: "Inspect browser console messages"}
	var level string
	list := &cobra.Command{Use: "list", Short: "List console messages", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("console.list", map[string]any{"level": level})
	}}
	list.Flags().StringVar(&level, "level", "", "log|warn|error")
	cmd.AddCommand(list, clearRPCCommand("clear", "console.clear", "Clear console messages"))
	return cmd
}

func errorsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "errors", Short: "Inspect page errors"}
	cmd.AddCommand(rpcLeaf("list", "errors.list", nil, "List page errors"), clearRPCCommand("clear", "errors.clear", "Clear page errors"))
	return cmd
}

func networkCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "network", Short: "Inspect and control network activity"}
	var status, typ string
	var includeExtension bool
	list := &cobra.Command{Use: "list", Short: "List captured network requests", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("network.list", map[string]any{"status": status, "type": typ, "includeExtension": includeExtension})
	}}
	list.Flags().StringVar(&status, "status", "", "status filter, e.g. 2xx")
	list.Flags().StringVar(&typ, "type", "", "request type")
	list.Flags().BoolVar(&includeExtension, "include-extension", false, "include chrome-extension:// requests")
	cmd.AddCommand(list)
	cmd.AddCommand(&cobra.Command{Use: "get <requestId>", Short: "Show a captured network response body", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("network.get", map[string]any{"requestId": args[0]})
	}})
	cmd.AddCommand(clearRPCCommand("clear", "network.clear", "Clear captured network requests"))
	har := &cobra.Command{Use: "har", Short: "Record and export HAR data"}
	har.AddCommand(rpcLeaf("start", "network.har.start", nil, "Start HAR recording"), rpcLeaf("stop", "network.har.stop", nil, "Stop HAR recording"))
	har.AddCommand(&cobra.Command{Use: "save <path>", Short: "Save HAR data to a file", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("network.har.save", map[string]any{"path": args[0], "includeExtension": includeExtension})
	}})
	har.PersistentFlags().BoolVar(&includeExtension, "include-extension", false, "include chrome-extension:// requests")
	cmd.AddCommand(har)
	cmd.AddCommand(&cobra.Command{Use: "block <pattern>", Short: "Block requests matching a pattern", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("network.block", map[string]any{"pattern": args[0]})
	}})
	cmd.AddCommand(&cobra.Command{Use: "unblock <pattern>", Short: "Unblock requests matching a pattern", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("network.unblock", map[string]any{"pattern": args[0]})
	}})
	return cmd
}

func dialogCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "dialog", Short: "Handle browser dialogs"}
	cmd.AddCommand(rpcLeaf("status", "dialog.status", nil, "Show the current dialog status"))
	var prompt string
	accept := &cobra.Command{Use: "accept", Short: "Accept the current dialog", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC("dialog.accept", map[string]any{"prompt": prompt})
	}}
	accept.Flags().StringVar(&prompt, "prompt", "", "prompt text")
	cmd.AddCommand(accept, rpcLeaf("dismiss", "dialog.dismiss", nil, "Dismiss the current dialog"))
	return cmd
}

func traceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "trace", Short: "Inspect recorded CLI actions"}
	cmd.AddCommand(rpcLeaf("show", "trace.show", nil, "Show recorded CLI actions"), clearRPCCommand("clear", "trace.clear", "Clear recorded CLI actions"))
	return cmd
}

func exportCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "export", Short: "Export recorded actions as scripts"}
	var out string
	for _, name := range []string{"playwright", "drissionpage"} {
		name := name
		short := map[string]string{
			"playwright":   "Export trace as a Playwright test",
			"drissionpage": "Export trace as a DrissionPage script",
		}[name]
		c := &cobra.Command{Use: name, Short: short, RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureServer(); err != nil {
				return err
			}
			data, err := rpc("export."+name, globals.Tab, nil, seconds(globals.Timeout), true, globals.DebugIDs)
			if err != nil {
				return err
			}
			if out != "" {
				var resp struct {
					Data struct {
						Content string `json:"content"`
					} `json:"data"`
				}
				if json.Unmarshal(data, &resp) == nil {
					if err := os.WriteFile(out, []byte(resp.Data.Content), 0o644); err != nil {
						return err
					}
					result := map[string]any{"path": out, "bytes": len([]byte(resp.Data.Content))}
					if globals.JSON {
						return printLocalResponse("export."+name, result)
					}
					if globals.Quiet {
						_, err := fmt.Fprintln(output, out)
						return err
					}
					_, err := fmt.Fprintln(output, out)
					return err
				}
			}
			if globals.JSON {
				return printRaw(data)
			}
			return printRPCDefault("export."+name, data)
		}}
		c.Flags().StringVar(&out, "out", "", "output file")
		cmd.AddCommand(c)
	}
	return cmd
}

func batchCmd() *cobra.Command {
	var file string
	var stdin bool
	var bail bool
	cmd := &cobra.Command{Use: "batch", Short: "Run multiple browser commands from JSON", RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		var data []byte
		var err error
		if file != "" {
			data, err = os.ReadFile(file)
		} else if stdin {
			data, err = io.ReadAll(os.Stdin)
		} else {
			err = fmt.Errorf("batch requires --stdin or --file")
		}
		if err != nil {
			return err
		}
		var items []any
		if err := json.Unmarshal(data, &items); err != nil {
			return err
		}
		return runRPC("batch", map[string]any{"items": items, "bail": bail})
	}}
	cmd.Flags().StringVar(&file, "file", "", "batch JSON file")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "read batch JSON from stdin")
	cmd.Flags().BoolVar(&bail, "bail", false, "stop on first error")
	return cmd
}

func rpcLeaf(use string, command string, params map[string]any, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, RunE: func(cmd *cobra.Command, args []string) error {
		if err := ensureServer(); err != nil {
			return err
		}
		return runRPC(command, params)
	}}
}

func clearRPCCommand(use string, command string, short string) *cobra.Command {
	return rpcLeaf(use, command, nil, short)
}

func runRPC(command string, params map[string]any) error {
	data, err := rpc(command, globals.Tab, params, seconds(globals.Timeout), globals.JSON, globals.DebugIDs)
	if err != nil {
		if globals.JSON && len(data) > 0 {
			_ = printRaw(data)
		}
		return err
	}
	if globals.JSON {
		if err := printRaw(data); err != nil {
			return err
		}
		if resp, err := parseAPIResponse(data); err == nil && !resp.Success && resp.Error != nil {
			return fmt.Errorf("%s", formatError(resp.Error.Code, resp.Error.Message))
		}
		return nil
	}
	return printRPCDefault(command, data)
}

func printRPCDefault(command string, data []byte) error {
	resp, err := parseAPIResponse(data)
	if err != nil {
		return printRaw(data)
	}
	if !resp.Success {
		if resp.Error == nil {
			return fmt.Errorf("error")
		}
		return fmt.Errorf("%s", formatError(resp.Error.Code, resp.Error.Message))
	}
	var value any
	_ = json.Unmarshal(resp.Data, &value)
	text, ok := formatRPCDefault(command, value)
	if !ok {
		return nil
	}
	_, err = fmt.Fprint(output, text)
	return err
}

func formatRPCDefault(command string, value any) (string, bool) {
	m, _ := value.(map[string]any)
	if globals.Quiet {
		if text, ok := quietValue(command, m); ok {
			return line(text), true
		}
		return "", false
	}
	switch command {
	case "get":
		for _, key := range []string{"title", "url", "text", "html", "markdown", "value"} {
			if text, _ := m[key].(string); text != "" {
				return line(text), true
			}
			if value, exists := m[key]; exists && value != nil {
				return line(formatPlainValue(value)), true
			}
		}
	case "snapshot":
		if text, _ := m["snapshot"].(string); text != "" {
			return line(text), true
		}
	case "tab.list":
		return formatTabList(m), true
	case "doctor":
		return formatDoctor(m), true
	case "console.list":
		return formatList("console", m["console"]), true
	case "errors.list":
		return formatList("errors", m["errors"]), true
	case "network.list":
		return formatList("requests", m["requests"]), true
	case "cookies.list":
		return formatList("cookies", m["cookies"]), true
	case "export.playwright", "export.drissionpage":
		if content, _ := m["content"].(string); content != "" {
			return content, true
		}
	case "screenshot", "pdf", "network.har.save":
		if path, _ := m["path"].(string); path != "" {
			return line(path), true
		}
	}
	if strings.HasPrefix(command, "action.") {
		action, _ := m["action"].(string)
		if action == "" {
			action = strings.TrimPrefix(command, "action.")
		}
		if target, _ := m["target"].(string); target != "" {
			return line(fmt.Sprintf("%s: %s", action, target)), true
		}
		return line(action + ": done"), true
	}
	if strings.HasPrefix(command, "tab.") || command == "open" || command == "reload" || command == "wait" || strings.HasPrefix(command, "dialog.") || strings.HasPrefix(command, "network.") || strings.HasPrefix(command, "trace.") || strings.HasPrefix(command, "cookies.") || strings.HasPrefix(command, "storage.") {
		return line(shortSummary(command, m)), true
	}
	if m != nil {
		if text, _ := m["value"].(string); text != "" {
			return line(text), true
		}
	}
	return "", false
}

func quietValue(command string, m map[string]any) (string, bool) {
	if m == nil {
		return "", false
	}
	for _, key := range []string{"path", "tabId", "id", "chromeTabId", "url", "title", "text", "html", "markdown", "content", "value"} {
		if text, _ := m[key].(string); text != "" {
			return text, true
		}
		if value, exists := m[key]; exists && value != nil {
			return formatPlainValue(value), true
		}
	}
	if command == "tab.list" {
		return "", false
	}
	return "", false
}

func line(text string) string {
	if strings.HasSuffix(text, "\n") {
		return text
	}
	return text + "\n"
}

func formatTabList(m map[string]any) string {
	items, _ := m["tabs"].([]any)
	if len(items) == 0 {
		return "no items\n"
	}
	lines := []string{"tab\tactive\ttitle\turl"}
	for _, item := range items {
		tab, _ := item.(map[string]any)
		active := ""
		if b, _ := tab["active"].(bool); b {
			active = "*"
		}
		lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%s", stringFromMap(tab, "id"), active, stringFromMap(tab, "title"), stringFromMap(tab, "url")))
	}
	return strings.Join(lines, "\n") + "\n"
}

func formatDoctor(m map[string]any) string {
	return fmt.Sprintf("daemon: running\nplugin: %s\ntabs: %.0f\ndefault tab: %s\n", connectedText(boolFromMap(m, "bridge")), numberFromMap(m, "tabsCount"), stringFromMap(m, "activeTab"))
}

func formatList(name string, value any) string {
	items, _ := value.([]any)
	if len(items) == 0 {
		return "no items\n"
	}
	return fmt.Sprintf("%s: %d\n", name, len(items))
}

func shortSummary(command string, m map[string]any) string {
	if m == nil {
		return command + ": done"
	}
	if path, _ := m["path"].(string); path != "" {
		return path
	}
	if tabID, _ := m["tabId"].(string); tabID != "" {
		return fmt.Sprintf("%s: %s", command, tabID)
	}
	if tabID, _ := m["id"].(string); tabID != "" {
		return fmt.Sprintf("%s: %s", command, tabID)
	}
	if removed := numberFromMap(m, "removed"); removed > 0 {
		return fmt.Sprintf("%s: %.0f removed", command, removed)
	}
	if handled, _ := m["handled"].(bool); handled {
		return command + ": handled"
	}
	if matched, _ := m["matched"].(bool); matched {
		return command + ": matched"
	}
	return command + ": done"
}

func stringFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if value, _ := m[key].(string); value != "" {
		return value
	}
	return fmt.Sprint(m[key])
}

func boolFromMap(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	value, _ := m[key].(bool)
	return value
}

func numberFromMap(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return 0
	}
}

func connectedText(ok bool) string {
	if ok {
		return "connected"
	}
	return "not connected"
}

func formatPlainValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		data, err := json.Marshal(v)
		if err == nil {
			return string(data)
		}
		return fmt.Sprint(v)
	}
}

func printLocalResponse(command string, data map[string]any) error {
	resp := map[string]any{
		"success": true,
		"data":    data,
		"meta": map[string]any{
			"command":  command,
			"warnings": []string{},
		},
	}
	if globals.JSON {
		return printJSON(resp)
	}
	if globals.Quiet {
		for _, key := range []string{"path", "pluginPath"} {
			if text, _ := data[key].(string); text != "" {
				_, err := fmt.Fprintln(output, text)
				return err
			}
		}
		return nil
	}
	if path, _ := data["path"].(string); path != "" {
		_, err := fmt.Fprintln(output, path)
		return err
	}
	if path, _ := data["pluginPath"].(string); path != "" {
		if _, err := fmt.Fprintln(output, path); err != nil {
			return err
		}
	}
	if message, _ := data["message"].(string); message != "" {
		_, err := fmt.Fprintln(output, message)
		return err
	}
	return nil
}

func seconds(value float64) time.Duration {
	if value < 0.1 {
		value = 0.1
	}
	return time.Duration(value * float64(time.Second))
}

func commandName(args ...string) string {
	return strings.Join(args, ".")
}
