package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/davirain-su/lintel/internal/chain"
	"github.com/davirain-su/lintel/internal/chains"
	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/nearid"
	"github.com/davirain-su/lintel/internal/neartx"
	"github.com/davirain-su/lintel/internal/passkeywallet"
	"github.com/davirain-su/lintel/internal/smartaccount"
	"github.com/davirain-su/lintel/internal/solanapasskey"
	"github.com/davirain-su/lintel/internal/wallet"
	"github.com/davirain-su/lintel/internal/webauthn"
)

const (
	// ceremonyRPID is the relying party the loopback ceremony page uses. The
	// page's host is localhost, so the credential is bound to it.
	ceremonyRPID = "localhost"
	// passkeyGas is what the passkey wallet's w_execute_signed asks for.
	passkeyGas = 330_000_000_000_000
	// passkeyEVMChainID is the EVM chain the smart account address is read
	// from. The factory and the account address are the same on every EVM
	// chain the app reads, so one chain is enough to derive it.
	passkeyEVMChainID = "eip155:1"
)

// PasskeyAccount is the public half of a passkey the shell created. The
// private key stays with the authenticator; these fields are what the app
// needs to ask it to sign and to show the accounts it controls.
type PasskeyAccount struct {
	// ID is the credential id, base64url without padding.
	ID string `json:"id"`
	// PublicKey is the P-256 key as "p256:<base58 compressed>".
	PublicKey string `json:"publicKey"`
	// NEARAccount is the NEP-616 account the key controls: "0s" followed by
	// the StateInit hash. The same key also owns the EVM smart account and
	// the Solana vault.
	NEARAccount string `json:"nearAccount"`
	// EVMAddress is the Coinbase Smart Wallet address the key owns. It is the
	// same on every EVM chain; empty until it has been read from the factory.
	EVMAddress string `json:"evmAddress,omitempty"`
	// SolanaAddress is the LazorKit v1 vault PDA derived from the same P-256
	// key. It is recomputed on load. The vault is not created by this app,
	// and sending SOL from it is not wired.
	SolanaAddress string    `json:"solanaAddress,omitempty"`
	CreatedAt     time.Time `json:"createdAt,omitempty"`
}

// publicKey parses the stored P-256 key.
func (p *PasskeyAccount) publicKey() (passkeywallet.PublicKey, error) {
	if p == nil {
		return passkeywallet.PublicKey{}, errors.New("passkey: no passkey")
	}
	return passkeywallet.ParsePublicKey(p.PublicKey)
}

// credential turns the stored public half back into the credential the
// WebAuthn checks take.
func (p *PasskeyAccount) credential() (webauthn.Credential, error) {
	pub, err := p.publicKey()
	if err != nil {
		return webauthn.Credential{}, err
	}
	id, err := base64.RawURLEncoding.DecodeString(p.ID)
	if err != nil || len(id) == 0 || len(id) > 1023 {
		return webauthn.Credential{}, errors.New("passkey: the credential id is not base64url")
	}
	return webauthn.Credential{ID: id, X: pub.X, Y: pub.Y}, nil
}

// validatePasskey drops a stored passkey that no longer matches its own
// public key, and normalizes the rest. The account id is derived, never
// trusted from the file.
func validatePasskey(p *PasskeyAccount) *PasskeyAccount {
	if p == nil {
		return nil
	}
	id, err := base64.RawURLEncoding.DecodeString(p.ID)
	if err != nil || len(id) == 0 || len(id) > 1023 {
		return nil
	}
	pub, err := passkeywallet.ParsePublicKey(p.PublicKey)
	if err != nil {
		return nil
	}
	account := passkeywallet.AccountID(pub)
	if p.NEARAccount != "" && p.NEARAccount != account {
		return nil
	}
	p.NEARAccount = account
	p.PublicKey = pub.String()
	if p.EVMAddress != "" {
		addr, err := wallet.Normalize(wallet.EVM, p.EVMAddress)
		if err != nil {
			p.EVMAddress = ""
		} else {
			p.EVMAddress = addr
		}
	}
	// The Solana vault is derived, the same way the NEAR account id is. A
	// stored value that disagrees is corrected rather than trusted. A failure
	// here leaves the field empty; it does not drop the passkey.
	if vault, err := solanapasskey.VaultAddress(pub); err == nil {
		p.SolanaAddress = vault
	} else {
		p.SolanaAddress = ""
	}
	return p
}

// passkeyCeremony is the browser side of one WebAuthn ceremony: it serves
// the page, opens it, and returns what the authenticator produced.
type passkeyCeremony interface {
	origin() string
	run(context.Context, webauthn.CeremonyRequest) (webauthn.CeremonyResult, error)
	close()
}

// createPasskey runs a create() ceremony and stores the public half. The
// challenge and user id are fresh for every registration; the page runs in
// the system browser, where the platform authenticator lives.
func (a *App) createPasskey() {
	if a.passkeyOp != nil || a.passkeyBusy || a.passkeySendBusy || a.settings.Passkey != nil {
		return
	}
	if a.newCeremony == nil || a.openExternal == nil {
		a.passkeyErr = a.t(i18n.PasskeyUnavailable)
		a.invalidate()
		return
	}
	op := a.beginPasskeyOperation(false)
	a.passkeyErr = ""
	a.passkeyNotice = ""
	a.invalidate()
	newCeremony, open := a.newCeremony, a.openExternal
	created := a.t(i18n.PasskeyCreated)
	failed := a.t(i18n.PasskeyFailed)
	a.goAsync(func() {
		if err := op.ctx.Err(); err != nil {
			a.failPasskey(op, failed, err)
			return
		}
		srv, err := newCeremony(open)
		if err != nil {
			a.failPasskey(op, failed, err)
			return
		}
		defer srv.close()
		challenge := make([]byte, 32)
		userID := make([]byte, 16)
		if _, err := rand.Read(challenge); err != nil {
			a.failPasskey(op, failed, err)
			return
		}
		if _, err := rand.Read(userID); err != nil {
			a.failPasskey(op, failed, err)
			return
		}
		res, err := srv.run(op.ctx, webauthn.CeremonyRequest{
			Mode:            webauthn.CeremonyCreate,
			Challenge:       challenge,
			UserID:          userID,
			UserName:        "lintel",
			UserDisplayName: "Lintel",
			RPName:          "Lintel",
		})
		if err != nil {
			a.failPasskey(op, failed, err)
			return
		}
		reg, err := webauthn.ParseRegistration(res.ClientDataJSON, res.AttestationObject, challenge, srv.origin(), ceremonyRPID)
		if err != nil {
			a.failPasskey(op, failed, err)
			return
		}
		if res.Kind != webauthn.CeremonyCreate || !bytes.Equal(res.CredentialID, reg.CredentialID) {
			a.failPasskey(op, failed, errors.New("passkey: registration returned another credential"))
			return
		}
		pub, err := passkeywallet.NewPublicKey(reg.X, reg.Y)
		if err != nil {
			a.failPasskey(op, failed, err)
			return
		}
		account := passkeywallet.AccountID(pub)
		pk := &PasskeyAccount{
			ID:          base64.RawURLEncoding.EncodeToString(reg.CredentialID),
			PublicKey:   pub.String(),
			NEARAccount: account,
			CreatedAt:   time.Now(),
		}
		if vault, err := solanapasskey.VaultAddress(pub); err == nil {
			pk.SolanaAddress = vault
		}
		a.finishPasskeyOperation(op, nil, func(err error) {
			if err != nil {
				a.passkeyErr = failed + " " + err.Error()
				return
			}
			if a.settings.Passkey != nil {
				a.passkeyErr = a.t(i18n.PasskeyCanceled)
				return
			}
			a.settings.Passkey = pk
			a.settings.ActiveWallet = wallet.PasskeyContainerID
			a.watchPasskeyAccount(account, a.t(i18n.PasskeyAddressLabel))
			a.watchPasskeyAccount(pk.SolanaAddress, a.t(i18n.PasskeyAddressLabel))
			a.save()
			a.passkeyNotice = created
			a.passkeyPrepared = true
			a.touch()
			a.requestRefresh()
			a.invalidate()
			a.refreshPasskeyEVMAddress()
		})
	})
}

// failPasskey ends a create() ceremony with the page's or the check's error.
func (a *App) failPasskey(op *passkeyOperation, prefix string, err error) {
	a.finishPasskeyOperation(op, err, func(err error) {
		a.passkeyErr = prefix + " " + err.Error()
	})
}

// removePasskey forgets the passkey in the app. The credential itself stays
// in the authenticator or password manager; the app cannot delete it.
func (a *App) removePasskey() {
	a.cancelPasskeyOperations()
	pk := a.settings.Passkey
	if pk == nil {
		return
	}
	a.settings.Passkey = nil
	if a.settings.ActiveWallet == wallet.PasskeyContainerID {
		a.settings.ActiveWallet = ""
	}
	a.forgetPasskeyAccount(pk.NEARAccount)
	if pk.EVMAddress != "" {
		a.forgetPasskeyAccount(pk.EVMAddress)
	}
	if pk.SolanaAddress != "" {
		a.forgetPasskeyAccount(pk.SolanaAddress)
	}
	a.passkeyErr = ""
	a.passkeyNotice = ""
	a.passkeyPrepared = false
	a.passkeySendErr = ""
	a.passkeySendNotice = ""
	a.passkeySendHash = ""
	a.save()
	a.touch()
	a.requestRefresh()
	a.invalidate()
}

// watchPasskeyChains adds every address the passkey controls to the watch
// list. The Solana vault is derived locally, so an older settings file gains
// it the next time the app starts.
func (a *App) watchPasskeyChains() {
	pk := a.settings.Passkey
	if pk == nil {
		return
	}
	before := len(a.settings.Accounts)
	a.watchPasskeyAccount(pk.NEARAccount, a.t(i18n.PasskeyAddressLabel))
	a.watchPasskeyAccount(pk.EVMAddress, a.t(i18n.PasskeyAddressLabel))
	a.watchPasskeyAccount(pk.SolanaAddress, a.t(i18n.PasskeyAddressLabel))
	if len(a.settings.Accounts) != before {
		a.save()
	}
}

// watchPasskeyAccount adds addr to the watch list once.
func (a *App) watchPasskeyAccount(addr, label string) {
	if addr == "" || a.settings.Has(addr) {
		return
	}
	a.settings.Accounts = append(a.settings.Accounts, wallet.Address{Addr: addr, Label: label})
}

// forgetPasskeyAccount removes addr from the watch list. It does not touch
// the wallet's own accounts.
func (a *App) forgetPasskeyAccount(addr string) {
	if addr == "" {
		return
	}
	kept := a.settings.Accounts[:0]
	for _, acc := range a.settings.Accounts {
		if acc.Addr == addr {
			continue
		}
		kept = append(kept, acc)
	}
	a.settings.Accounts = kept
}

// refreshPasskeyEVMAddress asks the Coinbase Smart Wallet factory for the
// address the same passkey owns. The address is counterfactual: it is the
// same before and after the account is deployed, and the same on every EVM
// chain the factory is on.
func (a *App) refreshPasskeyEVMAddress() {
	pk := a.settings.Passkey
	if pk == nil || pk.EVMAddress != "" || a.passkeyEVMBusy {
		return
	}
	pub, err := pk.publicKey()
	if err != nil {
		return
	}
	owner, err := smartaccount.PasskeyOwner(pub.X, pub.Y)
	if err != nil {
		return
	}
	caller := a.passkeyEVMCaller()
	if caller == nil {
		return
	}
	a.passkeyEVMBusy = true
	goAsync, onMain := a.goAsync, a.onMain
	goAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		account := smartaccount.Account{Factory: smartaccount.DefaultFactory, Owners: [][]byte{owner}}
		addr, err := account.Address(ctx, caller)
		onMain(func() {
			a.passkeyEVMBusy = false
			if err != nil || a.settings.Passkey == nil || a.settings.Passkey.PublicKey != pk.PublicKey {
				a.invalidate()
				return
			}
			normalized, nerr := wallet.Normalize(wallet.EVM, addr.Hex())
			if nerr != nil {
				a.invalidate()
				return
			}
			a.settings.Passkey.EVMAddress = normalized
			a.watchPasskeyAccount(normalized, "Passkey · EVM")
			a.save()
			a.touch()
			a.requestRefresh()
			a.invalidate()
		})
	})
}

// passkeyEVMCaller is the read-only EVM access the factory call needs. When
// another family is selected, it reads through Ethereum's public endpoint,
// because the address is the same on every EVM chain.
func (a *App) passkeyEVMCaller() smartaccount.ContractCaller {
	reader := a.reader
	if reader == nil {
		return nil
	}
	if a.network().Family != chain.EVM {
		eth, ok := chains.ByID(passkeyEVMChainID)
		if !ok {
			return nil
		}
		r, err := chains.NewReader(eth, "")
		if err != nil {
			return nil
		}
		reader = r
	}
	return readerCaller{reader: reader}
}

// readerCaller adapts the app's chain reader to smartaccount's view calls.
type readerCaller struct{ reader chain.Reader }

func (r readerCaller) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if call.To == nil {
		return nil, errors.New("passkey: the call has no target")
	}
	raw, err := r.reader.RPC(ctx, "eth_call", []any{
		map[string]any{"to": call.To.Hex(), "data": hexutil.Encode(call.Data)},
		"latest",
	})
	if err != nil {
		return nil, err
	}
	var out string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("passkey: reading eth_call: %w", err)
	}
	data, err := hexutil.Decode(out)
	if err != nil {
		return nil, fmt.Errorf("passkey: decoding eth_call: %w", err)
	}
	return data, nil
}

// deployPasskey creates the passkey's NEAR account with a deterministic
// StateInit. The account's own key signs nothing here: the wallet's unlocked
// NEAR account relays the transaction and pays its fee.
func (a *App) deployPasskey() {
	if a.passkeyOp != nil || a.passkeySendBusy || a.passkeyBusy {
		return
	}
	if a.settings.Passkey == nil {
		return
	}
	pk := *a.settings.Passkey
	network := a.network()
	if network.Family != chain.Near || network.ChainName != "mainnet" {
		a.passkeySendErr = a.t(i18n.PasskeyMainnetOnly)
		a.invalidate()
		return
	}
	if a.reader == nil {
		a.passkeySendErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	pub, err := pk.publicKey()
	if err != nil {
		a.passkeySendErr = err.Error()
		a.invalidate()
		return
	}
	relayer, err := a.nearSignAccount("")
	if err != nil {
		a.passkeySendErr = a.t(i18n.PasskeyNoRelayer)
		a.invalidate()
		return
	}
	stateInit := passkeywallet.StateInit(pub)
	summary := &neartx.Summary{
		SignerID: pk.NEARAccount, PublicKey: pub.String(), ReceiverID: pk.NEARAccount,
		Actions: []neartx.ActionSummary{{Kind: neartx.ActionDeterministicStateInit, StateInitSize: len(stateInit)}},
	}
	description := a.t(i18n.PasskeyDeployNote, relayer.AccountID)
	rejected := a.t(i18n.PasskeyRejected)
	alreadyOnChain := a.t(i18n.PasskeyAlreadyOnChain)
	reader := a.reader
	state := passkeyRelayState{passkey: pk, networkID: network.ID, rpcURL: a.settings.RPCURL, relayer: relayer}
	op := a.beginPasskeyOperation(true)
	a.passkeySendErr, a.passkeySendNotice, a.passkeySendHash = "", "", ""
	a.invalidate()
	a.goAsync(func() {
		if err := a.preflightPasskeyRelay(op, state, reader.RPC); err != nil {
			a.failPasskeySend(op, err)
			return
		}
		ctx, cancel := context.WithTimeout(op.ctx, 30*time.Second)
		deployed, err := passkeyDeployed(ctx, reader.RPC, pk.NEARAccount)
		cancel()
		if err != nil {
			a.failPasskeySend(op, err)
			return
		}
		if err := a.passkeyRelayValid(op, state); err != nil {
			a.failPasskeySend(op, err)
			return
		}
		if deployed {
			a.finishPasskeyOperation(op, nil, func(err error) {
				if err != nil {
					a.passkeySendErr = err.Error()
					return
				}
				a.passkeySendNotice = alreadyOnChain
			})
			return
		}
		if !a.approvePasskey(op, approvalRequest{
			pluginName: "Lintel", sourceKind: "wallet", description: description,
			network: network, near: summary, fromAddr: pk.NEARAccount, fromLabel: a.t(i18n.PasskeyAddressLabel),
		}) {
			if err := op.ctx.Err(); err != nil {
				a.failPasskeySend(op, err)
				return
			}
			a.failPasskeySend(op, errors.New(rejected))
			return
		}
		hash, err := a.relayPasskeyNear(op, state, reader.RPC, []neartx.Action{
			neartx.DeterministicStateInit(stateInit, new(big.Int)),
		})
		if err != nil {
			a.failPasskeySend(op, err)
			return
		}
		a.finishPasskeyOperation(op, nil, func(err error) {
			if err != nil {
				a.passkeySendErr = err.Error()
				return
			}
			a.passkeySendHash = hash
			a.passkeySendNotice = a.t(i18n.PasskeyDeployed, hash)
			a.touch()
			a.requestRefresh()
		})
	})
}

// sendPasskeyNear sends NEAR from the passkey account. The user approves the
// decoded transfer first, then the passkey signs the wallet contract's
// message in the browser. The wallet's unlocked NEAR account relays the
// transaction and pays its fee; the passkey's own account needs no key.
func (a *App) sendPasskeyNear() {
	if a.passkeyOp != nil || a.passkeySendBusy || a.passkeyBusy {
		return
	}
	if a.settings.Passkey == nil {
		return
	}
	pk := *a.settings.Passkey
	network := a.network()
	if network.Family != chain.Near || network.ChainName != "mainnet" {
		a.passkeySendErr = a.t(i18n.PasskeyMainnetOnly)
		a.invalidate()
		return
	}
	to, err := nearid.ParseAccountID(strings.TrimSpace(a.passkeySendTo))
	if err != nil {
		a.passkeySendErr = a.t(i18n.WalletNearSendInvalidTo)
		a.invalidate()
		return
	}
	amount, ok := chain.ParseUnits(a.passkeySendAmount, nearDecimals)
	if !ok || amount.Sign() <= 0 {
		a.passkeySendErr = a.t(i18n.WalletNearSendInvalidAmount)
		a.invalidate()
		return
	}
	if a.reader == nil {
		a.passkeySendErr = a.t(i18n.WalletUnavailable)
		a.invalidate()
		return
	}
	pub, err := pk.publicKey()
	if err != nil {
		a.passkeySendErr = err.Error()
		a.invalidate()
		return
	}
	cred, err := pk.credential()
	if err != nil {
		a.passkeySendErr = err.Error()
		a.invalidate()
		return
	}
	if a.newCeremony == nil || a.openExternal == nil {
		a.passkeySendErr = a.t(i18n.PasskeyUnavailable)
		a.invalidate()
		return
	}
	relayer, err := a.nearSignAccount("")
	if err != nil {
		a.passkeySendErr = a.t(i18n.PasskeyNoRelayer)
		a.invalidate()
		return
	}
	message := passkeywallet.NewTransferMessage(network.ChainName, pk.NEARAccount, to, amount, time.Now())
	challenge, err := message.Hash()
	if err != nil {
		a.passkeySendErr = err.Error()
		a.invalidate()
		return
	}
	summary := &neartx.Summary{
		SignerID:   pk.NEARAccount,
		PublicKey:  pub.String(),
		ReceiverID: to,
		Actions: []neartx.ActionSummary{{
			Kind:   neartx.ActionTransfer,
			Amount: amount,
		}},
	}
	description := a.t(i18n.PasskeySendNote, relayer.AccountID)
	rejected := a.t(i18n.PasskeyRejected)
	stateInit := passkeywallet.StateInit(pub)
	reader := a.reader
	newCeremony, open := a.newCeremony, a.openExternal
	state := passkeyRelayState{passkey: pk, networkID: network.ID, rpcURL: a.settings.RPCURL, relayer: relayer}
	op := a.beginPasskeyOperation(true)
	a.passkeySendErr = ""
	a.passkeySendNotice = ""
	a.passkeySendHash = ""
	a.invalidate()
	a.goAsync(func() {
		if err := a.preflightPasskeyRelay(op, state, reader.RPC); err != nil {
			a.failPasskeySend(op, err)
			return
		}
		if !a.approvePasskey(op, approvalRequest{
			pluginName:  "Lintel",
			sourceKind:  "wallet",
			description: description,
			network:     network,
			near:        summary,
			fromAddr:    pk.NEARAccount,
			fromLabel:   a.t(i18n.PasskeyAddressLabel),
		}) {
			if err := op.ctx.Err(); err != nil {
				a.failPasskeySend(op, err)
				return
			}
			a.failPasskeySend(op, errors.New(rejected))
			return
		}
		if err := a.passkeyRelayValid(op, state); err != nil {
			a.failPasskeySend(op, err)
			return
		}
		srv, err := newCeremony(open)
		if err != nil {
			a.failPasskeySend(op, err)
			return
		}
		res, err := srv.run(op.ctx, webauthn.CeremonyRequest{
			Mode:         webauthn.CeremonyGet,
			Challenge:    challenge,
			CredentialID: cred.ID,
		})
		origin := srv.origin()
		srv.close()
		if err != nil {
			a.failPasskeySend(op, err)
			return
		}
		assertion, err := verifyPasskeyAssertion(res, cred, challenge, origin)
		if err != nil {
			a.failPasskeySend(op, err)
			return
		}
		if err := a.passkeyRelayValid(op, state); err != nil {
			a.failPasskeySend(op, err)
			return
		}
		proof, err := passkeywallet.NewProof(assertion)
		if err != nil {
			a.failPasskeySend(op, err)
			return
		}
		args, err := passkeywallet.ExecuteArgs(message, proof)
		if err != nil {
			a.failPasskeySend(op, err)
			return
		}
		// The StateInit creates the account if it is not on chain yet, and is
		// a no-op when it is: the id is derived from the same state.
		actions := []neartx.Action{
			neartx.DeterministicStateInit(stateInit, new(big.Int)),
			neartx.FunctionCall("w_execute_signed", args, passkeyGas, big.NewInt(1)),
		}
		hash, err := a.relayPasskeyNear(op, state, reader.RPC, actions)
		if err != nil {
			a.failPasskeySend(op, err)
			return
		}
		a.finishPasskeyOperation(op, nil, func(err error) {
			if err != nil {
				a.passkeySendErr = err.Error()
				return
			}
			a.passkeySendHash = hash
			a.passkeySendNotice = a.t(i18n.WalletNearSendSent, hash)
			a.passkeySendTo = ""
			a.passkeySendAmount = ""
			a.touch()
			a.requestRefresh()
			a.invalidate()
		})
	})
}

// failPasskeySend ends a deploy or send with err.
func (a *App) failPasskeySend(op *passkeyOperation, err error) {
	a.finishPasskeyOperation(op, err, func(err error) {
		a.passkeySendErr = err.Error()
	})
}

// verifyPasskeyAssertion 同时检查返回的 credential ID，
// 浏览器响应必须证明对当前所用公开身份的控制权。
func verifyPasskeyAssertion(res webauthn.CeremonyResult, cred webauthn.Credential, challenge []byte, origin string) (webauthn.Assertion, error) {
	if res.Kind != webauthn.CeremonyGet || !bytes.Equal(res.CredentialID, cred.ID) {
		return webauthn.Assertion{}, errors.New("passkey: assertion returned another credential")
	}
	assertion := webauthn.Assertion{
		AuthenticatorData: res.AuthenticatorData,
		ClientDataJSON:    res.ClientDataJSON,
		Signature:         res.Signature,
	}
	if err := assertion.Verify(cred, challenge, origin, ceremonyRPID); err != nil {
		return webauthn.Assertion{}, err
	}
	return assertion, nil
}

// passkeyDeployed reports whether the passkey's NEAR account is on chain.
// The account id is derived from the StateInit, so the account exists exactly
// when it has been deployed.
func passkeyDeployed(ctx context.Context, rpc neartx.RPC, account string) (bool, error) {
	_, err := rpc(ctx, "query", map[string]any{
		"request_type": "view_account",
		"finality":     "final",
		"account_id":   account,
	})
	if err != nil {
		s := err.Error()
		if strings.Contains(s, "UNKNOWN_ACCOUNT") || strings.Contains(s, "does not exist while viewing") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
