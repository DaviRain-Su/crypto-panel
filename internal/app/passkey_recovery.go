package app

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/davirain-su/lintel/internal/i18n"
	"github.com/davirain-su/lintel/internal/passkeywallet"
	"github.com/davirain-su/lintel/internal/webauthn"
)

const (
	passkeyRecoveryFormat  = "crypto-panel-passkey-recovery"
	passkeyRecoveryVersion = 1
	maxPasskeyRecoverySize = 16 << 10
)

// passkeyRecovery 只保存公开身份资料，恢复时仍须使用原凭据；文件不能签名或重建凭据。
// EVM 地址在控制权验证成功后从 factory 重新读取，不从文件导入。
type passkeyRecovery struct {
	Format       string `json:"format"`
	Version      int    `json:"version"`
	RPID         string `json:"rpId"`
	CredentialID string `json:"credentialId"`
	PublicKey    string `json:"publicKey"`
	NEARAccount  string `json:"nearAccount"`
}

func passkeyRecoveryJSON(pk *PasskeyAccount) ([]byte, error) {
	if pk == nil {
		return nil, errors.New("passkey: no passkey to export")
	}
	record := passkeyRecovery{
		Format: passkeyRecoveryFormat, Version: passkeyRecoveryVersion, RPID: ceremonyRPID,
		CredentialID: pk.ID, PublicKey: pk.PublicKey, NEARAccount: pk.NEARAccount,
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	if _, err := parsePasskeyRecovery(data); err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// parsePasskeyRecovery 拒绝未知或重复字段、尾随 JSON 和非规范编码。
// 地址沿用已有钱包实现派生，恢复流程绝不使用新建凭据替代原凭据。
func parsePasskeyRecovery(data []byte) (*PasskeyAccount, error) {
	if len(data) == 0 || len(data) > maxPasskeyRecoverySize {
		return nil, errors.New("passkey: recovery file must be 1 to 16384 bytes")
	}
	var record passkeyRecovery
	fields := map[string]any{
		"format": &record.Format, "version": &record.Version, "rpId": &record.RPID,
		"credentialId": &record.CredentialID, "publicKey": &record.PublicKey, "nearAccount": &record.NEARAccount,
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	start, err := dec.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errors.New("passkey: recovery file must be a JSON object")
	}
	seen := make(map[string]bool, len(fields))
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("passkey: recovery field: %w", err)
		}
		name, _ := token.(string)
		target, ok := fields[name]
		if !ok || seen[name] {
			return nil, fmt.Errorf("passkey: unknown or duplicate recovery field %q", name)
		}
		seen[name] = true
		if err := dec.Decode(target); err != nil {
			return nil, fmt.Errorf("passkey: recovery field %q: %w", name, err)
		}
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("passkey: recovery file: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("passkey: recovery file has trailing data")
	}
	if len(seen) != len(fields) || record.Format != passkeyRecoveryFormat || record.Version != passkeyRecoveryVersion || record.RPID != ceremonyRPID {
		return nil, errors.New("passkey: unsupported recovery format, version or relying party")
	}
	if len(record.CredentialID) == 0 || len(record.CredentialID) > 1364 {
		return nil, errors.New("passkey: recovery credential id is missing or too long")
	}
	id, err := base64.RawURLEncoding.Strict().DecodeString(record.CredentialID)
	if err != nil || len(id) == 0 || len(id) > 1023 || base64.RawURLEncoding.EncodeToString(id) != record.CredentialID {
		return nil, errors.New("passkey: recovery credential id must be canonical base64url")
	}
	if len(record.PublicKey) == 0 || len(record.PublicKey) > 51 {
		return nil, errors.New("passkey: recovery public key is missing or too long")
	}
	pub, err := passkeywallet.ParsePublicKey(record.PublicKey)
	if err != nil || pub.String() != record.PublicKey {
		return nil, errors.New("passkey: recovery public key is not a canonical P-256 key")
	}
	if len(record.NEARAccount) != 42 || record.NEARAccount != passkeywallet.AccountID(pub) {
		return nil, errors.New("passkey: recovery account does not match its public key")
	}
	return &PasskeyAccount{ID: record.CredentialID, PublicKey: pub.String(), NEARAccount: record.NEARAccount}, nil
}

func readPasskeyRecoveryFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxPasskeyRecoverySize {
		return nil, errors.New("passkey: recovery file must be a regular file of 1 to 16384 bytes")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPasskeyRecoverySize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPasskeyRecoverySize {
		return nil, errors.New("passkey: recovery file is too large")
	}
	return data, nil
}

// restorePasskey 要求原凭据完成一次新的 WebAuthn 挑战，通过用户验证并校验
// 本次 localhost ceremony 的 origin、relying party 与签名后，才保存公开身份。
func (a *App) restorePasskey(data []byte) {
	if a.passkeyOp != nil || a.passkeyBusy || a.passkeySendBusy || a.settings.Passkey != nil {
		return
	}
	pk, err := parsePasskeyRecovery(data)
	if err != nil {
		a.passkeyErr = a.t(i18n.PasskeyRecoveryFailed, err.Error())
		a.invalidate()
		return
	}
	cred, err := pk.credential()
	if err != nil {
		a.passkeyErr = a.t(i18n.PasskeyRecoveryFailed, err.Error())
		a.invalidate()
		return
	}
	if a.newCeremony == nil || a.openExternal == nil {
		a.passkeyErr = a.t(i18n.PasskeyUnavailable)
		a.invalidate()
		return
	}
	op := a.beginPasskeyOperation(false)
	a.passkeyErr, a.passkeyNotice = "", ""
	a.invalidate()
	newCeremony, open := a.newCeremony, a.openExternal
	failed := a.t(i18n.PasskeyFailed)
	a.goAsync(func() {
		if err := op.ctx.Err(); err != nil {
			a.failPasskey(op, failed, err)
			return
		}
		challenge := make([]byte, 32)
		if _, err := rand.Read(challenge); err != nil {
			a.failPasskey(op, failed, err)
			return
		}
		srv, err := newCeremony(open)
		if err != nil {
			a.failPasskey(op, failed, err)
			return
		}
		defer srv.close()
		res, err := srv.run(op.ctx, webauthn.CeremonyRequest{
			Mode: webauthn.CeremonyGet, Challenge: challenge, CredentialID: cred.ID,
		})
		if err != nil {
			a.failPasskey(op, failed, err)
			return
		}
		if _, err := verifyPasskeyAssertion(res, cred, challenge, srv.origin()); err != nil {
			a.failPasskey(op, failed, err)
			return
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
			a.watchPasskeyAccount(pk.NEARAccount, "Passkey")
			a.save()
			a.passkeyNotice = a.t(i18n.PasskeyRestored)
			a.touch()
			a.requestRefresh()
			a.refreshPasskeyEVMAddress()
		})
	})
}
