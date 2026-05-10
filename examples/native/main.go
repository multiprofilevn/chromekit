package main

import (
	"context"
	"log"
	"time"

	"github.com/tuwibu/chromekit"
)

// Start Chrome first with a debug port + visible window, e.g.:
//   chrome.exe --remote-debugging-port=9222 --user-data-dir=C:\tmp\profile
// Native input only works on Windows with the Chrome window focused.
// WithNativeWindow tells chromekit where the window sits on screen so DOM
// coordinates can be converted to absolute screen pixels.
func main() {
	b, err := chromekit.Connect(9222,
		chromekit.WithInputBackend(chromekit.BackendNative),
		chromekit.WithNativeWindow(0, 0, 1.0),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer b.Close()

	p := b.Current()
	if err := p.Navigate("https://duckduckgo.com", 30*time.Second); err != nil {
		log.Fatal(err)
	}

	// Focus brings the Chrome window foreground before native input fires;
	// required when other apps have stolen focus since launch.
	focusCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := b.Focus(focusCtx); err != nil {
		log.Printf("focus: %v", err)
	}

	if err := p.ClickElement("input[name=q]"); err != nil {
		log.Printf("click search box: %v", err)
	}
	if err := p.Keyboard().Type("chromekit"); err != nil {
		log.Printf("type: %v", err)
	}
	time.Sleep(2 * time.Second)
}
