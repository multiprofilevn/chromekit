// Visual demo for KeyboardAPI.ClearInput — opens a real Chrome window with a
// data: URL containing a pre-filled <input>, pauses so you can see the value,
// then runs ClearInput and pauses again so you can confirm the field is empty.
//
// Run from project root:
//
//	go run ./examples/clearinput-demo
package main

import (
	"log"
	"time"

	"github.com/multiprofilevn/chromekit"
	"github.com/multiprofilevn/chromekit/input"
)

const page = `data:text/html,` +
	`<html><body style="font:24px sans-serif;padding:40px">` +
	`<h2>chromekit ClearInput demo</h2>` +
	`<p>Field value below — should clear when ClearInput runs.</p>` +
	`<input id="q" type="text" value="admin123" style="font:24px monospace;padding:10px;width:400px">` +
	`<p id="log"></p>` +
	`<script>document.getElementById('q').focus();</script>` +
	`</body></html>`

func main() {
	const chromePath = `C:\Program Files\Google\Chrome\Application\chrome.exe`
	b, err := chromekit.Launch(chromePath, chromekit.WithHeadless(false))
	if err != nil {
		log.Fatalf("launch: %v", err)
	}
	defer b.Close()

	p := b.Current()
	if err := p.Navigate(page, 15*time.Second); err != nil {
		log.Fatal(err)
	}
	if err := p.BringToFront(); err != nil {
		log.Printf("[demo] BringToFront warn: %v", err)
	}

	log.Println("[demo] page loaded — field has 'admin123' (nhìn 5s)")
	time.Sleep(5 * time.Second)

	log.Println("[demo] step 1/3 — sending Ctrl+A (select all, KHÔNG có Delete)")
	_ = p.Evaluate(`document.getElementById('q').focus();`, nil)
	time.Sleep(500 * time.Millisecond)
	if err := p.Keyboard().Press("a", input.ModCtrl); err != nil {
		log.Fatalf("Press: %v", err)
	}
	var v string
	_ = p.Evaluate(`document.getElementById('q').value`, &v)
	log.Printf("[demo] value after Ctrl+A = %q (KO đc có 'a' thừa — fix #1384)", v)
	log.Println("[demo] nhìn field 4s — text vẫn 'admin123' và đang highlight ...")
	time.Sleep(4 * time.Second)

	log.Println("[demo] step 2/3 — gọi ClearInput(\"#q\") (Ctrl+A + Delete)")
	if err := p.Keyboard().ClearInput("#q"); err != nil {
		log.Fatalf("ClearInput: %v", err)
	}
	_ = p.Evaluate(`document.getElementById('q').value`, &v)
	log.Printf("[demo] value after ClearInput = %q (expect rỗng)", v)
	log.Println("[demo] nhìn field 4s — phải rỗng ...")
	time.Sleep(4 * time.Second)

	log.Println("[demo] step 3/3 — type 'newpassword' vào field rỗng để xác nhận hoạt động bình thường")
	if err := p.Keyboard().Type("newpassword"); err != nil {
		log.Fatalf("Type: %v", err)
	}
	_ = p.Evaluate(`document.getElementById('q').value`, &v)
	log.Printf("[demo] value cuối = %q", v)
	log.Println("[demo] giữ window 8s để bạn xem ...")
	time.Sleep(8 * time.Second)
}
