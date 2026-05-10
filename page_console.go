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

// OnConsole registers a single handler invoked for every console.* call.
// Replaces any previous handler. Pass nil to disable.
//
// Handler runs on a goroutine — do not block on chromekit calls into the
// same page from inside it.
func (p *Page) OnConsole(handler func(ConsoleEvent)) {
	p.listenerMu.Lock()
	p.consoleHandler = handler
	p.listenerMu.Unlock()
	p.initListeners()
}
