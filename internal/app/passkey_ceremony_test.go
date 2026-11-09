package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/davirain-su/crypto-panel/internal/webauthn"
)

// openPosting returns an open function that reads the ceremony state from
// the page URL and posts body to /result.
func openPosting(t *testing.T, body func(state string) any) func(string) error {
	t.Helper()
	return func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		fragment, err := url.ParseQuery(u.Fragment)
		if err != nil {
			return err
		}
		state := fragment.Get("state")
		if state == "" {
			return errors.New("the page URL has no state")
		}
		data, err := json.Marshal(body(state))
		if err != nil {
			return err
		}
		resp, err := http.Post(u.Scheme+"://"+u.Host+"/result", "application/json", bytes.NewReader(data))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			msg, _ := io.ReadAll(resp.Body)
			return errors.New("result POST: " + resp.Status + ": " + string(msg))
		}
		return nil
	}
}

func TestCeremonyServerReturnsTheResult(t *testing.T) {
	var opened string
	open := openPosting(t, func(state string) any {
		return map[string]any{
			"kind":              "get",
			"state":             state,
			"credentialId":      "AAAA",
			"clientDataJSON":    "e30",
			"authenticatorData": "AAAA",
			"signature":         "AAAA",
		}
	})
	srv, err := newCeremonyServer(func(raw string) error {
		opened = raw
		return open(raw)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := srv.run(ctx, webauthn.CeremonyRequest{
		Mode:         webauthn.CeremonyGet,
		Challenge:    bytes.Repeat([]byte{1}, 32),
		CredentialID: []byte{2, 3, 4},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != webauthn.CeremonyGet || string(res.CredentialID) != "\x00\x00\x00" {
		t.Fatalf("result = %+v", res)
	}
	if !strings.HasPrefix(opened, "http://localhost:") || !strings.Contains(opened, "#") {
		t.Fatalf("opened %q", opened)
	}
}

func TestCeremonyServerReturnsThePageError(t *testing.T) {
	open := openPosting(t, func(state string) any {
		return map[string]any{"error": "Canceled", "message": "The user canceled.", "state": state}
	})
	srv, err := newCeremonyServer(open)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = srv.run(ctx, webauthn.CeremonyRequest{
		Mode:         webauthn.CeremonyGet,
		Challenge:    bytes.Repeat([]byte{1}, 32),
		CredentialID: []byte{2, 3, 4},
	})
	var ceremonyErr *webauthn.CeremonyError
	if !errors.As(err, &ceremonyErr) || ceremonyErr.Code != "Canceled" {
		t.Fatalf("err = %v", err)
	}
}

func TestCeremonyServerRejectsAnotherState(t *testing.T) {
	srv, err := newCeremonyServer(func(raw string) error {
		u, _ := url.Parse(raw)
		data, _ := json.Marshal(map[string]any{
			"kind":              "get",
			"state":             "not-the-state",
			"credentialId":      "AAAA",
			"clientDataJSON":    "e30",
			"authenticatorData": "AAAA",
			"signature":         "AAAA",
		})
		resp, err := http.Post(u.Scheme+"://"+u.Host+"/result", "application/json", bytes.NewReader(data))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			return errors.New("a foreign state was accepted")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err = srv.run(ctx, webauthn.CeremonyRequest{
		Mode:         webauthn.CeremonyGet,
		Challenge:    bytes.Repeat([]byte{1}, 32),
		CredentialID: []byte{2, 3, 4},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestCeremonyPageServesTheAssets(t *testing.T) {
	srv, err := newCeremonyServer(func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer srv.close()

	for _, path := range []string{"/", "/ceremony.js", "/style.css"} {
		resp, err := http.Get(srv.origin() + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || len(body) == 0 {
			t.Errorf("%s: %s, %d bytes", path, resp.Status, len(body))
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control = %q", path, resp.Header.Get("Cache-Control"))
		}
	}
	resp, err := http.Get(srv.origin() + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Crypto Panel") || !strings.Contains(string(body), "connect-src") {
		t.Error("the page is not the ceremony page")
	}
}

func TestCeremonyServerChecksErrorStateAndConsumesOnlyOneAnswer(t *testing.T) {
	srv, err := newCeremonyServer(func(raw string) error {
		u, _ := url.Parse(raw)
		fragment, _ := url.ParseQuery(u.Fragment)
		state := fragment.Get("state")
		for _, tc := range []struct {
			state  string
			status int
		}{
			{"foreign-state", http.StatusConflict},
			{"", http.StatusConflict},
			{state, http.StatusNoContent},
			{state, http.StatusConflict},
		} {
			data, _ := json.Marshal(map[string]string{"error": "Canceled", "message": "closed", "state": tc.state})
			resp, err := http.Post(u.Scheme+"://"+u.Host+"/result", "application/json", bytes.NewReader(data))
			if err != nil {
				return err
			}
			resp.Body.Close()
			if resp.StatusCode != tc.status {
				return errors.New("an error with the wrong or consumed state was accepted")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = srv.run(ctx, webauthn.CeremonyRequest{
		Mode: webauthn.CeremonyGet, Challenge: bytes.Repeat([]byte{1}, 32), CredentialID: []byte{2},
	})
	var ceremonyErr *webauthn.CeremonyError
	if !errors.As(err, &ceremonyErr) || ceremonyErr.State == "" || ceremonyErr.State == "foreign-state" {
		t.Fatalf("error = %v", err)
	}
}

func TestCeremonyServerRejectsLateErrorAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var endpoint, state string
	srv, err := newCeremonyServer(func(raw string) error {
		u, _ := url.Parse(raw)
		fragment, _ := url.ParseQuery(u.Fragment)
		endpoint, state = u.Scheme+"://"+u.Host+"/result", fragment.Get("state")
		cancel()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.close()
	_, err = srv.run(ctx, webauthn.CeremonyRequest{
		Mode: webauthn.CeremonyGet, Challenge: bytes.Repeat([]byte{1}, 32), CredentialID: []byte{2},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	data, _ := json.Marshal(map[string]string{"error": "Canceled", "state": state})
	resp, err := http.Post(endpoint, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("late error: %s", resp.Status)
	}
}
