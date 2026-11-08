package plugin

import (
	"errors"
	"fmt"

	"github.com/MoeForge/moejs"

	"github.com/davirain-su/crypto-panel/internal/pages"
)

// NearSignRequest is what host.requestNearSign asks for: a NEAR receiver and
// the actions to take. The shell fills in the access key nonce and the recent
// block hash, decodes the result for the approval window, and only signs what
// the user approves.
type NearSignRequest struct {
	ChainID string `json:"chainId"`
	From    string `json:"from,omitempty"`
	// Receiver is the account or contract the transaction is sent to.
	Receiver string       `json:"receiver"`
	Actions  []NearAction `json:"actions"`
	// Description is the plugin's own words for the approval window. The
	// shell shows them beside its own decode, never instead of it.
	Description string `json:"description,omitempty"`
}

// NearAction is one action of a NEAR transaction. Type names which fields
// are read:
//
//	transfer             Amount (yoctoNEAR)
//	functionCall         Method, Args, Gas, Deposit (yoctoNEAR)
//	addFunctionCallKey   PublicKey, ReceiverID, MethodNames, Allowance
//
// Amounts are decimal strings, so a JavaScript number's precision cannot
// round a yoctoNEAR value. Args is the contract's JSON text, or
// "base64:<data>" for bytes that are not JSON.
type NearAction struct {
	Type        string   `json:"type"`
	Amount      string   `json:"amount,omitempty"`
	Method      string   `json:"method,omitempty"`
	Args        string   `json:"args,omitempty"`
	Gas         string   `json:"gas,omitempty"`
	Deposit     string   `json:"deposit,omitempty"`
	PublicKey   string   `json:"publicKey,omitempty"`
	ReceiverID  string   `json:"receiverId,omitempty"`
	MethodNames []string `json:"methodNames,omitempty"`
	Allowance   string   `json:"allowance,omitempty"`
}

// parseNearSignRequest reads the object host.requestNearSign takes. Keys are
// checked, so a typo is an error rather than a transaction with a field the
// plugin did not mean.
func parseNearSignRequest(r *moejs.Realm, v moejs.Value) (NearSignRequest, error) {
	if v.IsUndefined() || v.IsNull() {
		return NearSignRequest{}, errors.New("requestNearSign needs a transaction")
	}
	raw, err := r.ToGoStrict(v)
	if err != nil {
		return NearSignRequest{}, err
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return NearSignRequest{}, errors.New("requestNearSign needs an object")
	}
	var req NearSignRequest
	for key, value := range obj {
		switch key {
		case "chainId", "from", "receiver", "description":
			s, ok := value.(string)
			if !ok {
				return NearSignRequest{}, fmt.Errorf("requestNearSign: %s must be a string", key)
			}
			switch key {
			case "chainId":
				req.ChainID = s
			case "from":
				req.From = s
			case "receiver":
				req.Receiver = s
			case "description":
				req.Description = pages.Trim(s, 200)
			}
		case "actions":
			list, ok := value.([]any)
			if !ok {
				return NearSignRequest{}, errors.New("requestNearSign: actions must be an array")
			}
			if len(list) == 0 || len(list) > 8 {
				return NearSignRequest{}, errors.New("requestNearSign: actions must hold 1 to 8 actions")
			}
			for i, item := range list {
				action, err := parseNearAction(item)
				if err != nil {
					return NearSignRequest{}, fmt.Errorf("requestNearSign: action %d: %w", i+1, err)
				}
				req.Actions = append(req.Actions, action)
			}
		default:
			return NearSignRequest{}, fmt.Errorf("requestNearSign: key %q is not supported", key)
		}
	}
	if req.ChainID == "" {
		return NearSignRequest{}, errors.New("requestNearSign: chainId is required")
	}
	if req.Receiver == "" {
		return NearSignRequest{}, errors.New("requestNearSign: receiver is required")
	}
	if len(req.Actions) == 0 {
		return NearSignRequest{}, errors.New("requestNearSign: actions are required")
	}
	return req, nil
}

func parseNearAction(item any) (NearAction, error) {
	obj, ok := item.(map[string]any)
	if !ok {
		return NearAction{}, errors.New("must be an object")
	}
	var action NearAction
	for key, value := range obj {
		switch key {
		case "type", "amount", "method", "args", "gas", "deposit", "publicKey", "receiverId", "allowance":
			s, ok := value.(string)
			if !ok {
				return NearAction{}, fmt.Errorf("%s must be a string", key)
			}
			switch key {
			case "type":
				action.Type = s
			case "amount":
				action.Amount = s
			case "method":
				action.Method = pages.Trim(s, 256)
			case "args":
				action.Args = s
			case "gas":
				action.Gas = s
			case "deposit":
				action.Deposit = s
			case "publicKey":
				action.PublicKey = s
			case "receiverId":
				action.ReceiverID = s
			case "allowance":
				action.Allowance = s
			}
		case "methodNames":
			list, ok := value.([]any)
			if !ok {
				return NearAction{}, errors.New("methodNames must be an array")
			}
			if len(list) > 32 {
				return NearAction{}, errors.New("methodNames holds more than 32 names")
			}
			for _, name := range list {
				s, ok := name.(string)
				if !ok {
					return NearAction{}, errors.New("methodNames must hold strings")
				}
				action.MethodNames = append(action.MethodNames, pages.Trim(s, 256))
			}
		default:
			return NearAction{}, fmt.Errorf("key %q is not supported", key)
		}
	}
	switch action.Type {
	case "transfer", "functionCall", "addFunctionCallKey":
	default:
		return NearAction{}, fmt.Errorf("type %q is not an action this wallet signs", action.Type)
	}
	return action, nil
}
