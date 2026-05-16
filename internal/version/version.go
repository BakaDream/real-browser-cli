package version

import "fmt"

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func String() string {
	if Version == "" {
		return "dev"
	}
	return Version
}

func UserAgent() string {
	return fmt.Sprintf("real-browser-cli/%s", String())
}
