package app

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/neartx"
	"github.com/davirain-su/lintel/internal/plugin"
	"github.com/davirain-su/lintel/internal/wallet"
)

// nearSignDeps is what signing one NEAR transaction needs. The app fills it
// in; tests replace the parts that touch the window and the keychain.
type nearSignDeps struct {
	sourceName  string
	sourceKind  string
	description string
	// account picks the NEAR account to sign with, or fails when none can.
	account func(from string) (wallet.NearAccount, error)
	// key reads an account's ed25519 key, only while it is unlocked.
	key func(accountID string) (ed25519.PrivateKey, bool)
	// approve shows the shell's decode and reports the user's answer.
	approve func(approvalRequest) bool
	rpc     neartx.RPC
}

// signNearForPlugin is host.requestNearSign: it prepares the transaction the
// plugin asked for, shows the shell's own decode, and signs and broadcasts
// only what the user approved. It runs on the plugin's call goroutine, so
// the network work never touches the main thread.
func (a *App) signNearForPlugin(m plugin.Manifest, readers map[string]chain.Reader, req plugin.NearSignRequest) (plugin.SignResult, error) {
	reader, ok := readers[req.ChainID]
	if !ok {
		return plugin.SignResult{}, fmt.Errorf("chain %q is not available", req.ChainID)
	}
	network, ok := chains.ByID(req.ChainID)
	if !ok {
		return plugin.SignResult{}, fmt.Errorf("chain %q is not one this app knows", req.ChainID)
	}
	if network.Family != chain.Near {
		return plugin.SignResult{}, fmt.Errorf("chain %q is not a NEAR network", req.ChainID)
	}
	return signNearTransaction(nearSignDeps{
		sourceName:  m.Name,
		description: req.Description,
		account:     a.nearSignAccount,
		key:         a.nearSignKey,
		approve:     a.askApproval,
		rpc: func(ctx context.Context, method string, params any) (json.RawMessage, error) {
			return reader.RPC(ctx, method, params)
		},
	}, network, req)
}

// signNearTransaction prepares, asks, and only then signs and sends. Nothing
// is signed without approve returning true, and a rejection broadcasts
// nothing.
func signNearTransaction(deps nearSignDeps, network chain.Network, req plugin.NearSignRequest) (plugin.SignResult, error) {
	acc, err := deps.account(req.From)
	if err != nil {
		return plugin.SignResult{}, err
	}
	publicKey, err := neartx.ParsePublicKey(acc.PublicKey)
	if err != nil {
		return plugin.SignResult{}, fmt.Errorf("the account's public key: %w", err)
	}
	actions, err := nearActions(req.Actions)
	if err != nil {
		return plugin.SignResult{}, err
	}
	// Adding a key is a transaction to the account itself; every other
	// action must name its receiver.
	receiver := req.Receiver
	if receiver == "" {
		for _, a := range req.Actions {
			if a.Type != "addFunctionCallKey" {
				return plugin.SignResult{}, errors.New("the transaction has no receiver")
			}
		}
		receiver = acc.AccountID
	}

	prepareCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	tx, sum, err := neartx.Prepare(prepareCtx, deps.rpc, neartx.Signer{
		AccountID: acc.AccountID,
		PublicKey: publicKey,
	}, neartx.Request{ReceiverID: receiver, Actions: actions})
	cancel()
	if err != nil {
		return plugin.SignResult{}, err
	}

	if !deps.approve(approvalRequest{
		pluginName:  deps.sourceName,
		sourceKind:  deps.sourceKind,
		description: deps.description,
		network:     network,
		near:        sum,
		fromAddr:    acc.AccountID,
		fromLabel:   acc.Label,
	}) {
		return plugin.SignResult{}, errors.New("the user rejected the transaction")
	}

	// The account may have been locked while the window was open.
	key, ok := deps.key(acc.AccountID)
	if !ok {
		return plugin.SignResult{}, errors.New("the account was locked before it could sign")
	}
	signed, hash, err := neartx.Sign(key, tx)
	if err != nil {
		return plugin.SignResult{}, err
	}
	sendCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	sent, err := neartx.Send(sendCtx, deps.rpc, signed, hash)
	if err != nil {
		return plugin.SignResult{}, err
	}
	return plugin.SignResult{Hash: sent}, nil
}

// nearActions turns the plugin's stated actions into the shell's own, with
// every amount parsed as a decimal yoctoNEAR string.
func nearActions(in []plugin.NearAction) ([]neartx.Action, error) {
	if len(in) == 0 {
		return nil, errors.New("the transaction has no actions")
	}
	out := make([]neartx.Action, 0, len(in))
	for i, a := range in {
		switch a.Type {
		case "transfer":
			amount, err := neartx.ParseAmount(a.Amount)
			if err != nil {
				return nil, fmt.Errorf("action %d: %w", i+1, err)
			}
			if amount.Sign() <= 0 {
				return nil, fmt.Errorf("action %d: a transfer needs an amount", i+1)
			}
			out = append(out, neartx.Transfer(amount))
		case "functionCall":
			args, err := nearArgs(a.Args)
			if err != nil {
				return nil, fmt.Errorf("action %d: %w", i+1, err)
			}
			gas, err := nearGas(a.Gas)
			if err != nil {
				return nil, fmt.Errorf("action %d: %w", i+1, err)
			}
			deposit, err := neartx.ParseAmount(a.Deposit)
			if err != nil {
				return nil, fmt.Errorf("action %d: %w", i+1, err)
			}
			out = append(out, neartx.FunctionCall(a.Method, args, gas, deposit))
		case "addFunctionCallKey":
			publicKey, err := neartx.ParsePublicKey(a.PublicKey)
			if err != nil {
				return nil, fmt.Errorf("action %d: %w", i+1, err)
			}
			var allowance *big.Int
			if strings.TrimSpace(a.Allowance) != "" {
				allowance, err = neartx.ParseAmount(a.Allowance)
				if err != nil {
					return nil, fmt.Errorf("action %d: %w", i+1, err)
				}
			}
			out = append(out, neartx.AddFunctionCallKey(publicKey, allowance, a.ReceiverID, a.MethodNames))
		default:
			return nil, fmt.Errorf("action %d: %q is not an action this wallet signs", i+1, a.Type)
		}
	}
	return out, nil
}

// nearArgs reads the contract arguments: JSON text as written, or
// "base64:<data>" for bytes that are not JSON.
func nearArgs(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	if rest, ok := strings.CutPrefix(s, "base64:"); ok {
		raw, err := base64.StdEncoding.DecodeString(rest)
		if err != nil {
			return nil, errors.New("args is not valid base64")
		}
		return raw, nil
	}
	return []byte(s), nil
}

// nearGas reads a decimal gas amount in gas units. Empty asks for the
// default.
func nearGas(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	gas, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, errors.New("gas is not a whole number")
	}
	return gas, nil
}

// nearSignAccount picks the NEAR account to sign with, on the main thread:
// the one the plugin named, or the first unlocked one. Only an unlocked
// account can sign, so the approval never promises a signature the shell
// cannot make.
func (a *App) nearSignAccount(from string) (wallet.NearAccount, error) {
	var (
		acc wallet.NearAccount
		err error
	)
	a.onMain(func() {
		if a.nearKeyring == nil {
			err = errors.New("the NEAR wallet is unavailable")
			return
		}
		if from != "" {
			accountID, nerr := wallet.Normalize(wallet.Near, from)
			if nerr != nil {
				err = errors.New("that is not a NEAR account")
				return
			}
			if !a.settings.IsNearWallet(accountID) {
				err = errors.New("that account is not in the wallet")
				return
			}
			if !a.nearKeyring.Unlocked(accountID) {
				err = errors.New("that account is locked")
				return
			}
			acc = a.nearAccountOf(accountID)
			return
		}
		for _, candidate := range a.settings.NearWallet {
			if a.nearKeyring.Unlocked(candidate.AccountID) {
				acc = candidate
				return
			}
		}
		err = errors.New("no unlocked NEAR account can sign")
	})
	return acc, err
}

func (a *App) nearAccountOf(accountID string) wallet.NearAccount {
	for _, acc := range a.settings.NearWallet {
		if acc.AccountID == accountID {
			return acc
		}
	}
	return wallet.NearAccount{AccountID: accountID}
}

// nearSignKey reads accountID's ed25519 key, on the main thread, only while
// it is unlocked.
func (a *App) nearSignKey(accountID string) (ed25519.PrivateKey, bool) {
	var (
		key ed25519.PrivateKey
		ok  bool
	)
	a.onMain(func() {
		if a.nearKeyring == nil {
			return
		}
		key, ok = a.nearKeyring.Key(accountID)
	})
	return key, ok
}
