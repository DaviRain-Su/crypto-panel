package app

import (
	"context"
	"crypto/ed25519"
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

	"github.com/davirain-su/crypto-panel/internal/chain"
	"github.com/davirain-su/crypto-panel/internal/chains"
	"github.com/davirain-su/crypto-panel/internal/i18n"
	"github.com/davirain-su/crypto-panel/internal/nearid"
	"github.com/davirain-su/crypto-panel/internal/neartx"
	"github.com/davirain-su/crypto-panel/internal/passkeywallet"
	"github.com/davirain-su/crypto-panel/internal/smartaccount"
	"github.com/davirain-su/crypto-panel/internal/wallet"
	"github.com/davirain-su/crypto-panel/internal/webauthn"
)

const (
	// ceremonyRPID is the relying party the loopback ceremony page uses. The
	// page's host is localhost, so the credential is bound to it.
	ceremonyRPID = "localhost"
	// passkeyGas is what the passkey wallet's w_execute_signed asks for.
	passkeyGas = 330_000_000_000_000
	// passkeyEVMChainID is the EVM chain the smart account address is read
	// from. The factory and the account address are the same on every EVM
	// chain the panel reads, so one chain is enough to derive it.
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
	// the StateInit hash. The same key also owns the EVM smart account.
	NEARAccount string `json:"nearAccount"`
	// EVMAddress is the Coinbase Smart Wallet address the key owns. It is the
	// same on every EVM chain; empty until it has been read from the factory.
	EVMAddress string    `json:"evmAddress,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
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
	if a.passkeyBusy || a.settings.Passkey != nil {
		return
	}
	if a.newCeremony == nil || a.openExternal == nil {
		a.passkeyErr = a.t(i18n.PasskeyUnavailable)
		a.invalidate()
		return
	}
	a.passkeyBusy = true
	a.passkeyErr = ""
	a.passkeyNotice = ""
	a.invalidate()
	newCeremony, open := a.newCeremony, a.openExternal
	created := a.t(i18n.PasskeyCreated)
	failed := a.t(i18n.PasskeyFailed)
	a.goAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		srv, err := newCeremony(open)
		if err != nil {
			a.failPasskey(failed, err)
			return
		}
		defer srv.close()
		challenge := make([]byte, 32)
		userID := make([]byte, 16)
		if _, err := rand.Read(challenge); err != nil {
			a.failPasskey(failed, err)
			return
		}
		if _, err := rand.Read(userID); err != nil {
			a.failPasskey(failed, err)
			return
		}
		res, err := srv.run(ctx, webauthn.CeremonyRequest{
			Mode:            webauthn.CeremonyCreate,
			Challenge:       challenge,
			UserID:          userID,
			UserName:        "crypto-panel",
			UserDisplayName: "Crypto Panel",
			RPName:          "Crypto Panel",
		})
		if err != nil {
			a.failPasskey(failed, err)
			return
		}
		reg, err := webauthn.ParseRegistration(res.ClientDataJSON, res.AttestationObject, challenge, srv.origin(), ceremonyRPID)
		if err != nil {
			a.failPasskey(failed, err)
			return
		}
		pub, err := passkeywallet.NewPublicKey(reg.X, reg.Y)
		if err != nil {
			a.failPasskey(failed, err)
			return
		}
		account := passkeywallet.AccountID(pub)
		pk := &PasskeyAccount{
			ID:          base64.RawURLEncoding.EncodeToString(reg.CredentialID),
			PublicKey:   pub.String(),
			NEARAccount: account,
			CreatedAt:   time.Now(),
		}
		a.onMain(func() {
			a.passkeyBusy = false
			a.settings.Passkey = pk
			a.watchPasskeyAccount(account, "Passkey")
			a.save()
			a.passkeyNotice = created
			a.touch()
			a.requestRefresh()
			a.invalidate()
			a.refreshPasskeyEVMAddress()
		})
	})
}

// failPasskey ends a create() ceremony with the page's or the check's error.
func (a *App) failPasskey(prefix string, err error) {
	a.onMain(func() {
		a.passkeyBusy = false
		a.passkeyErr = prefix + " " + err.Error()
		a.invalidate()
	})
}

// removePasskey forgets the passkey in the app. The credential itself stays
// in the authenticator or password manager; the app cannot delete it.
func (a *App) removePasskey() {
	pk := a.settings.Passkey
	if pk == nil {
		return
	}
	a.settings.Passkey = nil
	a.forgetPasskeyAccount(pk.NEARAccount)
	if pk.EVMAddress != "" {
		a.forgetPasskeyAccount(pk.EVMAddress)
	}
	a.passkeyErr = ""
	a.passkeyNotice = ""
	a.passkeySendErr = ""
	a.passkeySendNotice = ""
	a.passkeySendHash = ""
	a.save()
	a.touch()
	a.requestRefresh()
	a.invalidate()
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
	if a.passkeySendBusy {
		return
	}
	pk := a.settings.Passkey
	if pk == nil {
		return
	}
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
		SignerID:   pk.NEARAccount,
		PublicKey:  pub.String(),
		ReceiverID: pk.NEARAccount,
		Actions: []neartx.ActionSummary{{
			Kind:          neartx.ActionDeterministicStateInit,
			StateInitSize: len(stateInit),
		}},
	}
	description := a.t(i18n.PasskeyDeployNote, relayer.AccountID)
	rejected := a.t(i18n.PasskeyRejected)
	relayerLocked := a.t(i18n.PasskeyRelayerLocked)
	alreadyOnChain := a.t(i18n.PasskeyAlreadyOnChain)
	reader := a.reader
	approve := a.approver()
	a.passkeySendBusy = true
	a.passkeySendErr = ""
	a.passkeySendNotice = ""
	a.passkeySendHash = ""
	a.invalidate()
	a.goAsync(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		deployed, err := passkeyDeployed(ctx, reader.RPC, pk.NEARAccount)
		cancel()
		if err != nil {
			a.failPasskeySend(err)
			return
		}
		if deployed {
			a.onMain(func() {
				a.passkeySendBusy = false
				a.passkeySendNotice = alreadyOnChain
				a.invalidate()
			})
			return
		}
		if !approve(approvalRequest{
			pluginName:  "Crypto Panel",
			sourceKind:  "wallet",
			description: description,
			network:     network,
			near:        summary,
			fromAddr:    pk.NEARAccount,
			fromLabel:   "Passkey",
		}) {
			a.failPasskeySend(errors.New(rejected))
			return
		}
		key, ok := a.nearSignKey(relayer.AccountID)
		if !ok {
			a.failPasskeySend(errors.New(relayerLocked))
			return
		}
		hash, err := relayNearTx(context.Background(), reader.RPC, relayer, key, pk.NEARAccount, []neartx.Action{
			neartx.DeterministicStateInit(stateInit, new(big.Int)),
		})
		if err != nil {
			a.failPasskeySend(err)
			return
		}
		a.onMain(func() {
			a.passkeySendBusy = false
			a.passkeySendHash = hash
			a.passkeySendNotice = a.t(i18n.PasskeyDeployed, hash)
			a.touch()
			a.requestRefresh()
			a.invalidate()
		})
	})
}

// sendPasskeyNear sends NEAR from the passkey account. The user approves the
// decoded transfer first, then the passkey signs the wallet contract's
// message in the browser. The wallet's unlocked NEAR account relays the
// transaction and pays its fee; the passkey's own account needs no key.
func (a *App) sendPasskeyNear() {
	if a.passkeySendBusy {
		return
	}
	pk := a.settings.Passkey
	if pk == nil {
		return
	}
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
	relayerLocked := a.t(i18n.PasskeyRelayerLocked)
	stateInit := passkeywallet.StateInit(pub)
	reader := a.reader
	approve := a.approver()
	newCeremony, open := a.newCeremony, a.openExternal
	a.passkeySendBusy = true
	a.passkeySendErr = ""
	a.passkeySendNotice = ""
	a.passkeySendHash = ""
	a.invalidate()
	a.goAsync(func() {
		if !approve(approvalRequest{
			pluginName:  "Crypto Panel",
			sourceKind:  "wallet",
			description: description,
			network:     network,
			near:        summary,
			fromAddr:    pk.NEARAccount,
			fromLabel:   "Passkey",
		}) {
			a.failPasskeySend(errors.New(rejected))
			return
		}
		key, ok := a.nearSignKey(relayer.AccountID)
		if !ok {
			a.failPasskeySend(errors.New(relayerLocked))
			return
		}
		ceremonyCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		srv, err := newCeremony(open)
		if err != nil {
			cancel()
			a.failPasskeySend(err)
			return
		}
		res, err := srv.run(ceremonyCtx, webauthn.CeremonyRequest{
			Mode:         webauthn.CeremonyGet,
			Challenge:    challenge,
			CredentialID: cred.ID,
		})
		origin := srv.origin()
		srv.close()
		cancel()
		if err != nil {
			a.failPasskeySend(err)
			return
		}
		assertion := webauthn.Assertion{
			AuthenticatorData: res.AuthenticatorData,
			ClientDataJSON:    res.ClientDataJSON,
			Signature:         res.Signature,
		}
		if err := assertion.Verify(cred, challenge, origin, ceremonyRPID); err != nil {
			a.failPasskeySend(err)
			return
		}
		proof, err := passkeywallet.NewProof(assertion)
		if err != nil {
			a.failPasskeySend(err)
			return
		}
		args, err := passkeywallet.ExecuteArgs(message, proof)
		if err != nil {
			a.failPasskeySend(err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		// The StateInit creates the account if it is not on chain yet, and is
		// a no-op when it is: the id is derived from the same state.
		actions := []neartx.Action{
			neartx.DeterministicStateInit(stateInit, new(big.Int)),
			neartx.FunctionCall("w_execute_signed", args, passkeyGas, big.NewInt(1)),
		}
		hash, err := relayNearTx(ctx, reader.RPC, relayer, key, pk.NEARAccount, actions)
		if err != nil {
			a.failPasskeySend(err)
			return
		}
		a.onMain(func() {
			a.passkeySendBusy = false
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
func (a *App) failPasskeySend(err error) {
	a.onMain(func() {
		a.passkeySendBusy = false
		a.passkeySendErr = err.Error()
		a.invalidate()
	})
}

// relayNearTx prepares, signs and broadcasts a transaction from the wallet's
// own NEAR account. It is the passkey flows' relayer: the account pays the
// fee, and the passkey's signature authorizes only the inner action.
func relayNearTx(ctx context.Context, rpc neartx.RPC, relayer wallet.NearAccount, key ed25519.PrivateKey, receiver string, actions []neartx.Action) (string, error) {
	publicKey, err := neartx.ParsePublicKey(relayer.PublicKey)
	if err != nil {
		return "", fmt.Errorf("the relayer's public key: %w", err)
	}
	prepareCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	tx, _, err := neartx.Prepare(prepareCtx, rpc, neartx.Signer{
		AccountID: relayer.AccountID,
		PublicKey: publicKey,
	}, neartx.Request{ReceiverID: receiver, Actions: actions})
	cancel()
	if err != nil {
		return "", err
	}
	signed, hash, err := neartx.Sign(key, tx)
	if err != nil {
		return "", err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return neartx.Send(sendCtx, rpc, signed, hash)
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
