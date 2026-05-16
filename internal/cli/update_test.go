package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/bakadream/real-browser-cli/internal/version"
)

func captureOutput(fn func() error) (string, error) {
	previous := output
	var buf bytes.Buffer
	output = &buf
	defer func() {
		output = previous
	}()
	err := fn()
	return strings.TrimSpace(buf.String()), err
}

func TestReleaseAssetName(t *testing.T) {
	tests := []struct {
		goos  string
		arch  string
		asset string
	}{
		{"darwin", "arm64", "real-browser-darwin-arm64"},
		{"darwin", "amd64", "real-browser-darwin-amd64"},
		{"linux", "amd64", "real-browser-linux-amd64"},
		{"linux", "arm64", "real-browser-linux-arm64"},
		{"windows", "amd64", "real-browser-windows-amd64.exe"},
	}
	for _, test := range tests {
		got, err := releaseAssetName(test.goos, test.arch)
		if err != nil {
			t.Fatalf("releaseAssetName(%s, %s) failed: %v", test.goos, test.arch, err)
		}
		if got != test.asset {
			t.Fatalf("releaseAssetName(%s, %s) = %q, want %q", test.goos, test.arch, got, test.asset)
		}
	}
	if _, err := releaseAssetName("freebsd", "amd64"); err == nil {
		t.Fatal("unsupported platform should fail")
	}
}

func TestParseChecksums(t *testing.T) {
	hash := strings.Repeat("a", sha256.Size*2)
	checksums, err := parseChecksums([]byte(fmt.Sprintf("%s  real-browser-linux-amd64\n", hash)))
	if err != nil {
		t.Fatalf("parseChecksums failed: %v", err)
	}
	if checksums["real-browser-linux-amd64"] != hash {
		t.Fatalf("unexpected checksum: %q", checksums["real-browser-linux-amd64"])
	}
	if _, err := parseChecksums([]byte("not-a-checksum\n")); err == nil {
		t.Fatal("invalid checksum line should fail")
	}
}

func TestPrepareUpdate(t *testing.T) {
	binary := []byte("binary-data")
	sum := sha256.Sum256(binary)
	hash := hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/bakadream/real-browser-cli/releases/latest":
			_, _ = fmt.Fprintf(w, `{
				"tag_name": "v1.2.3",
				"assets": [
					{"name": "real-browser-linux-amd64", "browser_download_url": "%s/binary"},
					{"name": "checksums.txt", "browser_download_url": "%s/checksums.txt"}
				]
			}`, serverURL(r), serverURL(r))
		case "/checksums.txt":
			_, _ = fmt.Fprintf(w, "%s  real-browser-linux-amd64\n", hash)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	previousBase := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() {
		githubAPIBaseURL = previousBase
	}()

	plan, err := prepareUpdate(server.Client(), "", "real-browser-linux-amd64")
	if err != nil {
		t.Fatalf("prepareUpdate failed: %v", err)
	}
	if plan.Release.TagName != "v1.2.3" {
		t.Fatalf("unexpected tag: %q", plan.Release.TagName)
	}
	if plan.ExpectedHash != hash {
		t.Fatalf("unexpected hash: %q", plan.ExpectedHash)
	}
}

func TestRunUpdateRejectsRunningDaemon(t *testing.T) {
	assetName, err := releaseAssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	server := newUpdateTestServer(t, assetName)
	defer server.Close()

	previousBase := githubAPIBaseURL
	previousDaemonAlive := daemonAlive
	previousVersion := version.Version
	githubAPIBaseURL = server.URL
	daemonAlive = func() bool { return true }
	version.Version = "v0.0.1"
	defer func() {
		githubAPIBaseURL = previousBase
		daemonAlive = previousDaemonAlive
		version.Version = previousVersion
	}()

	got, err := captureOutput(func() error {
		return runUpdate("", updateOptions{Timeout: 1})
	})
	if err == nil {
		t.Fatal("running daemon should abort update")
	}
	want := strings.Join([]string{
		"real browser daemon is running.",
		"stop it before update: real-browser daemon stop",
	}, "\n")
	if got != want {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestInstallDownloadedBinaryUnix(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("windows uses helper replacement")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "real-browser")
	tmp := filepath.Join(dir, "real-browser.new")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := captureOutput(func() error {
		return installDownloadedBinary(exe, tmp, "v2.0.0")
	})
	if err != nil {
		t.Fatalf("installDownloadedBinary failed: %v", err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("target was not replaced: %q", data)
	}
	backup, err := os.ReadFile(exe + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "old" {
		t.Fatalf("backup mismatch: %q", backup)
	}
}

func newUpdateTestServer(t *testing.T, assetName string) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256([]byte("binary"))
	hash := hex.EncodeToString(sum[:])
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/bakadream/real-browser-cli/releases/latest":
			_, _ = fmt.Fprintf(w, `{
				"tag_name": "v1.0.0",
				"assets": [
					{"name": %q, "browser_download_url": "%s/binary"},
					{"name": "checksums.txt", "browser_download_url": "%s/checksums.txt"}
				]
			}`, assetName, server.URL, server.URL)
		case "/checksums.txt":
			_, _ = fmt.Fprintf(w, "%s  %s\n", hash, assetName)
		case "/binary":
			_, _ = w.Write([]byte("binary"))
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}
