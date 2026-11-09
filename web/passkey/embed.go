// Package passkeypage embeds the WebAuthn ceremony page the app serves on
// loopback. The page runs the create() and get() ceremonies in the system
// browser, where the platform authenticator lives, and posts the result back
// to the app.
package passkeypage

import "embed"

// Files is the ceremony page: index.html, ceremony.js and style.css.
//
//go:embed index.html ceremony.js style.css
var Files embed.FS
