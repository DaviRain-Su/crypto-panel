// Command intentsprobe hits the live 1Click API the way the Intents page
// does, to see the errors a user would see. Development tool, not shipped.
package main

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/davirain-su/lintel/internal/intents"
)

func main() {
	c := intents.New()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	tokens, err := c.Tokens(ctx)
	if err != nil {
		fmt.Println("tokens error:", err)
		return
	}
	byID := map[string]intents.Token{}
	for _, t := range tokens {
		byID[t.AssetID] = t
	}
	var wnear, usdc, btc, arb intents.Token
	for _, t := range tokens {
		switch {
		case t.Symbol == "wNEAR" && t.Blockchain == "near":
			wnear = t
		case t.Symbol == "USDC" && t.Blockchain == "eth":
			usdc = t
		case t.Symbol == "BTC" && t.Blockchain == "btc":
			btc = t
		case t.Symbol == "ARB" && t.Blockchain == "arb":
			arb = t
		}
	}
	fmt.Printf("picked: wnear=%s usdc=%s btc=%s arb=%s\n", wnear.AssetID, usdc.AssetID, btc.AssetID, arb.AssetID)

	evm := "0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed"
	try := func(name string, origin, dest intents.Token, recipient string) {
		if origin.AssetID == "" || dest.AssetID == "" {
			fmt.Println(name, ": asset not on the list")
			return
		}
		q, err := c.Quote(ctx, intents.QuoteRequest{
			OriginAsset:      origin.AssetID,
			DestinationAsset: dest.AssetID,
			Amount:           new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(origin.Decimals)), nil).String(),
			Recipient:        recipient,
			RefundTo:         evm,
		})
		if err != nil {
			fmt.Printf("%s: err=%v\n", name, err)
			return
		}
		fmt.Printf("%s: ok in=%s out=%s est=%dmin\n", name, q.AmountInText, q.AmountOutText, q.TimeEstimate)
	}
	try("wNEAR->USDC(eth)", wnear, usdc, evm)
	try("wNEAR->BTC", wnear, btc, "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kygt080")
	try("wNEAR->ARB", wnear, arb, evm)
	try("USDC(eth)->wNEAR", usdc, wnear, "davirian.near")
	// A real asset paid toward a recipient on the wrong chain.
	try("wNEAR->USDC, near recipient", wnear, usdc, "davirian.near")
}
