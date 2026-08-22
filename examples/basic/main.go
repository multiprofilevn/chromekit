package main

import (
	"log"
	"os"
	"time"

	"github.com/multiprofilevn/chromekit"
)

// Start Chrome first with a debug port, e.g.:
//
//	chrome.exe --remote-debugging-port=9222 --user-data-dir=C:\tmp\profile
//
// Then run this example to attach.
func main() {
	b, err := chromekit.Connect(9222)
	if err != nil {
		log.Fatal(err)
	}
	defer b.Close()

	p := b.Current()
	if err := p.Navigate("https://example.com", 30*time.Second); err != nil {
		log.Fatal(err)
	}

	title, _ := p.GetTitle()
	log.Printf("title: %s", title)

	if err := p.ClickElement("a"); err != nil {
		log.Printf("click: %v", err)
	}

	img, err := p.Screenshot()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("out.png", img, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Println("saved out.png")
}
