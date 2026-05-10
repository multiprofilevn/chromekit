package chromekit

// DialogType enumerates the JavaScript dialog kinds Chrome reports.
type DialogType string

const (
	DialogAlert        DialogType = "alert"
	DialogConfirm      DialogType = "confirm"
	DialogPrompt       DialogType = "prompt"
	DialogBeforeUnload DialogType = "beforeunload"
)

// DialogEvent describes a JS dialog the page just opened.
type DialogEvent struct {
	Type          DialogType
	Message       string
	DefaultPrompt string // only meaningful for DialogPrompt
}

// DialogAction tells chromekit how to dismiss the dialog. Default zero value
// is "dismiss/cancel" (Accept=false).
type DialogAction struct {
	Accept     bool
	PromptText string // only used when Accept=true on a prompt dialog
}

// OnDialog registers a single handler invoked for every alert/confirm/prompt
// raised by the page. Replaces any previous handler. Pass nil to revert to
// the default behavior (auto-dismiss — keeps the tab from hanging on alerts).
//
// The handler runs on a goroutine spawned by the listener; do not block on
// chromekit calls back into the same page from inside it.
func (p *Page) OnDialog(handler func(DialogEvent) DialogAction) {
	p.listenerMu.Lock()
	p.dialogHandler = handler
	p.listenerMu.Unlock()
	p.initListeners()
}
