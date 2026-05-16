package browserplugin

import "embed"

//go:embed background.js config.js content.js disable_dialogs.js manifest.json popup.html popup.js
var Files embed.FS

var FileNames = []string{
	"background.js",
	"config.js",
	"content.js",
	"disable_dialogs.js",
	"manifest.json",
	"popup.html",
	"popup.js",
}
