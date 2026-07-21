package chromekit

import (
	"fmt"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

// Permission is a w3c-style permission name. The CDP browser API expects
// these strings; we expose constants for the ones automation actually uses.
type Permission string

const (
	PermissionGeolocation       Permission = "geolocation"
	PermissionCamera            Permission = "camera"
	PermissionMicrophone        Permission = "microphone"
	PermissionNotifications     Permission = "notifications"
	PermissionClipboardRead     Permission = "clipboard-read"
	PermissionClipboardWrite    Permission = "clipboard-write"
	PermissionMIDI              Permission = "midi"
	PermissionMIDISysex         Permission = "midi-sysex"
	PermissionBackgroundSync    Permission = "background-sync"
	PermissionPersistentStorage Permission = "persistent-storage"
)

// GrantPermissions auto-approves the listed permissions for origin so the
// browser doesn't show its grant prompt. Pass an empty origin to apply to
// every origin in the current browser context.
//
// Each permission is set independently via Browser.setPermission — failure
// on one short-circuits the rest, leaving prior grants in place.
func (b *Browser) GrantPermissions(origin string, perms ...Permission) error {
	if b == nil || b.ctx == nil {
		return ErrContextCanceled
	}
	for _, p := range perms {
		desc := &browser.PermissionDescriptor{Name: string(p)}
		// MIDI sysex needs the sysex flag in addition to the descriptor name.
		if p == PermissionMIDISysex {
			desc.Name = "midi"
			desc.Sysex = true
		}
		action := browser.SetPermission(desc, browser.PermissionSettingGranted)
		if origin != "" {
			action = action.WithOrigin(origin)
		}
		if err := chromedp.Run(b.ctx, action); err != nil {
			return fmt.Errorf("%w: grant %q: %s", ErrEvaluate, p, err.Error())
		}
	}
	return nil
}

// ResetPermissions clears every override set by GrantPermissions, restoring
// the browser's default prompt-on-demand behavior.
func (b *Browser) ResetPermissions() error {
	if b == nil || b.ctx == nil {
		return ErrContextCanceled
	}
	if err := chromedp.Run(b.ctx, browser.ResetPermissions()); err != nil {
		return fmt.Errorf("%w: reset perms: %s", ErrEvaluate, err.Error())
	}
	return nil
}
