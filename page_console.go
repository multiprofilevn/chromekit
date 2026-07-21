package chromekit

// ConsoleLevel mirrors the Chrome console method that produced the entry.
type ConsoleLevel string

const (
	ConsoleLog   ConsoleLevel = "log"
	ConsoleInfo  ConsoleLevel = "info"
	ConsoleWarn  ConsoleLevel = "warn"
	ConsoleError ConsoleLevel = "error"
	ConsoleDebug ConsoleLevel = "debug"
)

// ConsoleEvent describes one console.* call from the page.
type ConsoleEvent struct {
	Level ConsoleLevel
	Text  string // arguments space-joined, primitives stringified, objects JSON-encoded
}

// maxConcurrentConsoleHandlers bounds how many handler invocations may be in
// flight at once. Beyond it, events are dropped rather than queued: console
// output is diagnostic, and a page can emit it faster than any handler can
// consume it.
const maxConcurrentConsoleHandlers = 8

// OnConsole registers a single handler invoked for every console.* call.
// Replaces any previous handler. Pass nil to disable.
//
// The handler runs on its own goroutine, so it may call back into this page.
// Bursts are bounded: while maxConcurrentConsoleHandlers invocations are in
// flight, further events are dropped.
func (p *Page) OnConsole(handler func(ConsoleEvent)) {
	p.listenerMu.Lock()
	p.consoleHandler = handler
	if p.consoleSem == nil {
		p.consoleSem = make(chan struct{}, maxConcurrentConsoleHandlers)
	}
	p.listenerMu.Unlock()
	p.initListeners()
}
