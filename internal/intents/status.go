package intents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// StatusState is a documented 1Click execution state.
type StatusState string

const (
	StatusPendingDeposit    StatusState = "PENDING_DEPOSIT"
	StatusKnownDepositTx    StatusState = "KNOWN_DEPOSIT_TX"
	StatusIncompleteDeposit StatusState = "INCOMPLETE_DEPOSIT"
	StatusProcessing        StatusState = "PROCESSING"
	StatusSuccess           StatusState = "SUCCESS"
	StatusRefunded          StatusState = "REFUNDED"
	StatusFailed            StatusState = "FAILED"
)

// Status is the execution state and the original signed quote returned by
// 1Click. QuoteResponse remains raw so callers retain every detail, while the
// client guarantees that its signature and timestamp have been verified.
type Status struct {
	Status        StatusState     `json:"status"`
	CorrelationID string          `json:"correlationId"`
	UpdatedAt     string          `json:"updatedAt"`
	QuoteResponse json.RawMessage `json:"quoteResponse"`
	SwapDetails   json.RawMessage `json:"swapDetails"`
}

// Status reads execution state. It does not create a quote or submit a deposit
// transaction hash. The address is trimmed; a memo, when supplied, is forwarded
// byte-for-byte, including surrounding spaces.
func (c *Client) Status(ctx context.Context, depositAddress, depositMemo string) (Status, error) {
	depositAddress = strings.TrimSpace(depositAddress)
	if depositAddress == "" {
		return Status{}, errors.New("the deposit address is required")
	}
	query := url.Values{"depositAddress": {depositAddress}}
	if depositMemo != "" {
		query.Set("depositMemo", depositMemo)
	}
	var raw json.RawMessage
	if err := c.get(ctx, "/v0/status?"+query.Encode(), &raw); err != nil {
		return Status{}, err
	}
	var out Status
	if err := json.Unmarshal(raw, &out); err != nil {
		return Status{}, fmt.Errorf("1Click: the status could not be read: %w", err)
	}
	if !validStatus(out.Status) {
		return Status{}, fmt.Errorf("1Click: unknown execution status %q", out.Status)
	}
	if len(out.QuoteResponse) == 0 || string(out.QuoteResponse) == "null" {
		return Status{}, errors.New("1Click: the status has no original quote")
	}
	if _, err := verifySignedQuoteResponse(out.QuoteResponse, c.key, c.now(), false); err != nil {
		return Status{}, fmt.Errorf("1Click: status quote verification failed: %w", err)
	}
	return out, nil
}

func validStatus(status StatusState) bool {
	switch status {
	case StatusPendingDeposit, StatusKnownDepositTx, StatusIncompleteDeposit,
		StatusProcessing, StatusSuccess, StatusRefunded, StatusFailed:
		return true
	default:
		return false
	}
}
