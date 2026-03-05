// Package windowname provides randomized window and page titles for every
// launch, reducing the visibility of the application in the taskbar, window
// lists, and webview title bars.
//
// Titles are generated once per process and cached for the session.  The D2R
// game-window title is generated per call so each window gets its own value.
package windowname

import (
	"fmt"
	"math/rand"
	"sync"
)

// webviewTitles is a pool of plausible application names used for the
// webview window and HTML page titles.
var webviewTitles = []string{
	"Microsoft Visual Studio Code",
	"Windows PowerShell",
	"File Explorer",
	"Notepad",
	"Calculator",
	"Resource Monitor",
	"System Information",
	"Microsoft Edge",
	"Task Scheduler",
	"Performance Monitor",
	"Event Viewer",
	"Device Manager",
	"Disk Management",
	"Services",
	"Registry Editor",
	"Command Prompt",
	"Windows Security",
	"Settings",
	"Control Panel",
	"Paint",
}

// gameWindowTitles is a pool of plausible window titles used for the
// D2R game window, replacing the default "D2R - [PID] - …" pattern.
var gameWindowTitles = []string{
	"Microsoft Word",
	"Google Chrome",
	"Mozilla Firefox",
	"Windows Explorer",
	"Adobe Reader",
	"Steam Client",
	"Epic Games Launcher",
	"Spotify",
	"Visual Studio 2022",
	"Outlook",
	"OneNote",
	"VLC media player",
	"Notepad++",
	"Discord",
	"Slack",
	"Teams",
	"OBS Studio",
	"Paint 3D",
	"Photos",
	"Movies & TV",
}

var (
	sessionTitle string
	once         sync.Once
)

// SessionTitle returns a fixed, random title chosen once for this process
// lifetime. It is used as the initial webview window title and for HTML
// page <title> tags so they all look consistent per session.
func SessionTitle() string {
	once.Do(func() {
		sessionTitle = webviewTitles[rand.Intn(len(webviewTitles))]
	})
	return sessionTitle
}

// RandomWebviewTitle returns a random title from the webview pool.
// Use this for periodic title rotation of the webview window.
func RandomWebviewTitle() string {
	return webviewTitles[rand.Intn(len(webviewTitles))]
}

// RandomGameTitle returns a unique, random title for a D2R game window.
// Each call produces a potentially different value so that multiple game
// windows do not share the same title.
func RandomGameTitle() string {
	title := gameWindowTitles[rand.Intn(len(gameWindowTitles))]
	// Append a small random suffix to avoid collisions when multiple
	// game windows are open simultaneously.
	return fmt.Sprintf("%s (%d)", title, 1000+rand.Intn(9000))
}
