// Lintel is a native desktop app. The window is a sidebar of Portfolio,
// Wallet, Apps, Intents, DApp, and Settings. A wallet is a named container
// of EVM, NEAR, and Solana accounts whose keys live in the OS keychain.
// Plugins are sandboxed and return page data; every signature goes through
// the shell's own decoded approval.
package main

import (
	"log"

	"github.com/egoist/mygo"

	"github.com/davirain-su/lintel/internal/app"
)

func main() {
	mygo.App.SetName("Lintel")
	// Opening the app again shows the running one's window instead of
	// starting a second copy. Asked before anything else, so the second one
	// touches no data on its way out.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	a, err := app.New()
	if err != nil {
		log.Fatal(err)
	}
	mygo.App.WhenReady(a.Ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
