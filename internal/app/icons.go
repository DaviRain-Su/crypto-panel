package app

import "github.com/egoist/mygo/ui"

// Icons drawn on a 24-point grid, in the color of the text, in a 1.75-point
// stroke.

func strokeIcon(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

var (
	iconPulse       = strokeIcon(`<path d="M3 12h4l3-7 4 14 3-7h4"/>`)
	iconChart       = strokeIcon(`<path d="M3 3v18h18"/><path d="m7 14 4-5 3 3 5-7"/>`)
	iconWallet      = strokeIcon(`<path d="M19 7V5a2 2 0 0 0-2-2H5a2 2 0 0 0 0 4h15a1 1 0 0 1 1 1v4h-3a2 2 0 0 0 0 4h3a1 1 0 0 0 1-1v-2"/><path d="M3 5v14a2 2 0 0 0 2 2h15a1 1 0 0 0 1-1v-4"/>`)
	iconLock        = strokeIcon(`<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>`)
	iconGrid        = strokeIcon(`<rect x="3" y="3" width="7" height="7" rx="2"/><rect x="14" y="3" width="7" height="7" rx="2"/><rect x="3" y="14" width="7" height="7" rx="2"/><rect x="14" y="14" width="7" height="7" rx="2"/>`)
	iconGear        = strokeIcon(`<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>`)
	iconSparkle     = strokeIcon(`<path d="M12 3l1.8 4.9 4.9 1.8-4.9 1.8L12 16.4l-1.8-4.9L5.3 9.7l4.9-1.8z"/><path d="M18.5 15.5l.7 1.9 1.9.7-1.9.7-.7 1.9-.7-1.9-1.9-.7 1.9-.7z"/>`)
	iconRefresh     = strokeIcon(`<path d="M20.5 12a8.5 8.5 0 1 1-2.5-6"/><path d="M20.5 3.5V9H15"/>`)
	iconTrash       = strokeIcon(`<path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>`)
	iconTerminal    = strokeIcon(`<path d="m4 17 6-6-6-6"/><path d="M12 19h8"/>`)
	iconStore       = strokeIcon(`<path d="M4 9.5 5.2 4h13.6L20 9.5"/><path d="M4 9.5a2.5 2.5 0 0 0 5 0 2.5 2.5 0 0 0 5 0 2.5 2.5 0 0 0 5 0"/><path d="M5 12v7.5h14V12"/><path d="M9.5 19.5V15h5v4.5"/>`)
	iconShield      = strokeIcon(`<path d="M12 3 4.5 6v5.5c0 4.6 3.2 8.3 7.5 9.5 4.3-1.2 7.5-4.9 7.5-9.5V6z"/><path d="m9 12 2 2 4-4.5"/>`)
	iconKey         = strokeIcon(`<circle cx="8" cy="15" r="4"/><path d="m10.8 12.2 8.7-8.7"/><path d="m16.5 6.5 2 2"/><path d="m13.5 9.5 2 2"/>`)
	iconCopy        = strokeIcon(`<rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V6a2 2 0 0 1 2-2h9"/>`)
	iconLockOpen    = strokeIcon(`<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 7.5-2"/>`)
	iconChevron     = strokeIcon(`<path d="m9 6 6 6-6 6"/>`)
	iconFingerprint = strokeIcon(`<path d="M12 11a2 2 0 0 0-2 2c0 2.5-.5 4.5-1.5 6"/><path d="M8.5 6.5A6 6 0 0 1 18 12c0 1.5-.2 3-.6 4.3"/><path d="M5.5 9A6 6 0 0 0 5 12c0 2.5-.4 4.6-1.2 6.5"/><path d="M12 7a5 5 0 0 0-5 5c0 2.2-.4 4.2-1.2 6"/><path d="M15.5 12c0 2.8-.4 5.2-1.2 7.2"/>`)
)
