package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/bakadream/real-browser-cli/internal/version"
	"github.com/spf13/cobra"
)

const (
	githubOwner      = "bakadream"
	githubRepository = "real-browser-cli"
	checksumsAsset   = "checksums.txt"
)

var githubAPIBaseURL = "https://api.github.com"
var daemonAlive = isAlive

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type updateOptions struct {
	Force   bool
	DryRun  bool
	Timeout float64
}

type updatePlan struct {
	Release       githubRelease
	BinaryAsset   releaseAsset
	ChecksumAsset releaseAsset
	ExpectedHash  string
}

func updateCmd() *cobra.Command {
	opts := updateOptions{}
	cmd := &cobra.Command{
		Use:   "update [tag]",
		Short: "Update real-browser from GitHub Releases",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tag := ""
			if len(args) > 0 {
				tag = args[0]
			}
			return runUpdate(tag, opts)
		},
	}
	cmd.Flags().BoolVar(&opts.Force, "force", false, "download even when the current version is already up to date")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "check release assets without replacing the binary")
	cmd.Flags().Float64Var(&opts.Timeout, "timeout", 60, "request timeout seconds")
	return cmd
}

func updateHelperCmd() *cobra.Command {
	var target string
	var next string
	var backup string
	cmd := &cobra.Command{
		Use:    "__update-helper",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if goruntime.GOOS != "windows" {
				return errors.New("update helper is only supported on windows")
			}
			if target == "" || next == "" || backup == "" {
				return errors.New("missing update helper paths")
			}
			return runWindowsUpdateHelper(target, next, backup)
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "target binary path")
	cmd.Flags().StringVar(&next, "next", "", "new binary path")
	cmd.Flags().StringVar(&backup, "backup", "", "backup binary path")
	return cmd
}

func runUpdate(tag string, opts updateOptions) error {
	client := &http.Client{Timeout: seconds(opts.Timeout)}
	assetName, err := releaseAssetName(goruntime.GOOS, goruntime.GOARCH)
	if err != nil {
		return err
	}
	plan, err := prepareUpdate(client, tag, assetName)
	if err != nil {
		return err
	}
	if !opts.Force && sameVersion(version.String(), plan.Release.TagName) {
		return printUpdateLine(fmt.Sprintf("real-browser is already up to date: %s", plan.Release.TagName))
	}
	if opts.DryRun {
		return printUpdateLine(fmt.Sprintf("real-browser update available: %s (%s)", plan.Release.TagName, assetName))
	}
	if daemonAlive() {
		if err := printUpdateLine("real browser daemon is running."); err != nil {
			return err
		}
		if err := printUpdateLine("stop it before update: real-browser daemon stop"); err != nil {
			return err
		}
		return errors.New("real-browser update aborted because daemon is running")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	tmp, actualHash, err := downloadAsset(client, plan.BinaryAsset.BrowserDownloadURL, filepath.Dir(exe))
	if err != nil {
		return err
	}
	if !strings.EqualFold(actualHash, plan.ExpectedHash) {
		_ = os.Remove(tmp)
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", plan.BinaryAsset.Name, plan.ExpectedHash, actualHash)
	}
	return installDownloadedBinary(exe, tmp, plan.Release.TagName)
}

func prepareUpdate(client *http.Client, tag string, assetName string) (updatePlan, error) {
	release, err := fetchRelease(client, tag)
	if err != nil {
		return updatePlan{}, err
	}
	binaryAsset, ok := findAsset(release.Assets, assetName)
	if !ok {
		return updatePlan{}, fmt.Errorf("release %s does not contain asset %s", release.TagName, assetName)
	}
	checksumAsset, ok := findAsset(release.Assets, checksumsAsset)
	if !ok {
		return updatePlan{}, fmt.Errorf("release %s does not contain %s", release.TagName, checksumsAsset)
	}
	checksumData, err := httpGet(client, checksumAsset.BrowserDownloadURL)
	if err != nil {
		return updatePlan{}, err
	}
	checksums, err := parseChecksums(checksumData)
	if err != nil {
		return updatePlan{}, err
	}
	expectedHash, ok := checksums[assetName]
	if !ok {
		return updatePlan{}, fmt.Errorf("%s does not contain checksum for %s", checksumsAsset, assetName)
	}
	return updatePlan{
		Release:       release,
		BinaryAsset:   binaryAsset,
		ChecksumAsset: checksumAsset,
		ExpectedHash:  expectedHash,
	}, nil
}

func fetchRelease(client *http.Client, tag string) (githubRelease, error) {
	path := fmt.Sprintf("/repos/%s/%s/releases/latest", githubOwner, githubRepository)
	if tag != "" {
		path = fmt.Sprintf("/repos/%s/%s/releases/tags/%s", githubOwner, githubRepository, url.PathEscape(tag))
	}
	data, err := githubGet(client, githubAPIBaseURL+path)
	if err != nil {
		return githubRelease{}, err
	}
	var release githubRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return githubRelease{}, err
	}
	if release.TagName == "" {
		return githubRelease{}, errors.New("release response missing tag_name")
	}
	return release, nil
}

func githubGet(client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", version.UserAgent())
	return doRequest(client, req)
}

func httpGet(client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	return doRequest(client, req)
}

func doRequest(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func releaseAssetName(goos string, goarch string) (string, error) {
	switch {
	case goos == "darwin" && goarch == "arm64":
		return "real-browser-darwin-arm64", nil
	case goos == "darwin" && goarch == "amd64":
		return "real-browser-darwin-amd64", nil
	case goos == "linux" && goarch == "amd64":
		return "real-browser-linux-amd64", nil
	case goos == "linux" && goarch == "arm64":
		return "real-browser-linux-arm64", nil
	case goos == "windows" && goarch == "amd64":
		return "real-browser-windows-amd64.exe", nil
	default:
		return "", fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
	}
}

func findAsset(assets []releaseAsset, name string) (releaseAsset, bool) {
	for _, asset := range assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func parseChecksums(data []byte) (map[string]string, error) {
	result := make(map[string]string)
	for lineNumber, rawLine := range bytes.Split(data, []byte{'\n'}) {
		line := strings.TrimSpace(string(rawLine))
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid checksum line %d", lineNumber+1)
		}
		hash := strings.ToLower(fields[0])
		if _, err := hex.DecodeString(hash); err != nil || len(hash) != sha256.Size*2 {
			return nil, fmt.Errorf("invalid sha256 on line %d", lineNumber+1)
		}
		name := strings.TrimPrefix(fields[1], "*")
		result[name] = hash
	}
	if len(result) == 0 {
		return nil, errors.New("checksums.txt is empty")
	}
	return result, nil
}

func sameVersion(current string, target string) bool {
	current = strings.TrimSpace(current)
	target = strings.TrimSpace(target)
	if current == "" || current == "dev" || target == "" {
		return false
	}
	return current == target
}

func downloadAsset(client *http.Client, url string, dir string) (string, string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
	}
	file, err := os.CreateTemp(dir, ".real-browser-update-*")
	if err != nil {
		return "", "", err
	}
	tmp := file.Name()
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(file, hasher), resp.Body); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return "", "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", "", err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return "", "", err
	}
	return tmp, hex.EncodeToString(hasher.Sum(nil)), nil
}

func installDownloadedBinary(exe string, tmp string, targetVersion string) error {
	if goruntime.GOOS == "windows" {
		return installDownloadedBinaryWindows(exe, tmp, targetVersion)
	}
	if err := copyFile(exe, exe+".bak", 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := printUpdateLine(fmt.Sprintf("real-browser updated successfully: %s -> %s", version.String(), targetVersion)); err != nil {
		return err
	}
	return printUpdateLine("restart daemon manually to use the new version: real-browser daemon restart")
}

func installDownloadedBinaryWindows(exe string, tmp string, targetVersion string) error {
	next := exe + ".new"
	_ = os.Remove(next)
	if err := os.Rename(tmp, next); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	helper := filepath.Join(filepath.Dir(exe), fmt.Sprintf("real-browser-update-helper-%d.exe", os.Getpid()))
	if err := copyFile(exe, helper, 0o755); err != nil {
		return err
	}
	cmd := exec.Command(helper, "__update-helper", "--target", exe, "--next", next, "--backup", exe+".bak")
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := cmd.Process.Release(); err != nil {
		return err
	}
	if err := printUpdateLine(fmt.Sprintf("real-browser update scheduled: %s -> %s", version.String(), targetVersion)); err != nil {
		return err
	}
	return printUpdateLine("restart daemon manually to use the new version: real-browser daemon restart")
}

func runWindowsUpdateHelper(target string, next string, backup string) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		_ = os.Remove(backup)
		if err := os.Rename(target, backup); err == nil {
			if err := os.Rename(next, target); err != nil {
				_ = os.Rename(backup, target)
				return err
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("replace %s timed out; new binary remains at %s", target, next)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func copyFile(src string, dst string, perm os.FileMode) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()
	outputFile, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(outputFile, input); err != nil {
		_ = outputFile.Close()
		return err
	}
	if err := outputFile.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}

func printUpdateLine(line string) error {
	_, err := fmt.Fprintln(output, line)
	return err
}
