// Crypto Panel is a desktop app for watching crypto. It is watch-only:
// addresses are added, balances are read from one pinned endpoint, plugins
// run in their own sandboxed runtimes, and no key ever enters the app.
package main

import (
	"log"

	"github.com/egoist/mygo"

	"github.com/davirain-su/crypto-panel/internal/app"
)

func main() {
	mygo.App.SetName("Crypto Panel")
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
