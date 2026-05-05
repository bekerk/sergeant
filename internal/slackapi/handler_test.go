package slackapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/slack-go/slack"

	"sergeant/internal/i18n"
	"sergeant/internal/ledger"
	"sergeant/internal/parser"
	"sergeant/internal/store"
)

const (
	testSecret = "test-signing-secret"
	botID      = "USERGEANT"
)

type post struct {
	channel, target, text string
	ephemeral             bool
}

type spy struct {
	mu       sync.Mutex
	calls    []post
	views    []slack.ModalViewRequest
	triggers []string
	done     chan struct{}
}

func newSpy() *spy { return &spy{done: make(chan struct{}, 8)} }

func (s *spy) PostInThread(_ context.Context, ch, ts, text string) error {
	return s.add(post{ch, ts, text, false})
}
func (s *spy) PostEphemeral(_ context.Context, ch, user, text string) error {
	return s.add(post{ch, user, text, true})
}
func (s *spy) PostEphemeralBlocks(_ context.Context, ch, user, fallback string, _ []slack.Block) error {
	return s.add(post{ch, user, fallback, true})
}
func (s *spy) OpenView(_ context.Context, triggerID string, view slack.ModalViewRequest) error {
	s.mu.Lock()
	s.triggers = append(s.triggers, triggerID)
	s.views = append(s.views, view)
	s.mu.Unlock()
	s.done <- struct{}{}
	return nil
}
func (s *spy) add(p post) error {
	s.mu.Lock()
	s.calls = append(s.calls, p)
	s.mu.Unlock()
	s.done <- struct{}{}
	return nil
}
func (s *spy) wait(t *testing.T) post {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for responder")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[len(s.calls)-1]
}

func newHandler(t *testing.T) (*Handler, *spy, *ledger.Ledger) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	tr := i18n.New("en")
	led := ledger.New(st, "PLN", tr)
	sp := newSpy()
	h := &Handler{
		SigningSecret: testSecret,
		BotUserID:     botID,
		Ledger:        led,
		Responder:     sp,
		Translator:    tr,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return h, sp, led
}

func sign(t *testing.T, body []byte, ts int64) *http.Request {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(testSecret))
	_, _ = fmt.Fprintf(mac, "v0:%d:", ts)
	mac.Write(body)
	req := httptest.NewRequest(http.MethodPost, "/slack/events", bytes.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	return req
}

func appMention(text string) []byte {
	return []byte(fmt.Sprintf(`{"type":"event_callback","event":{"type":"app_mention","user":"UAAA","text":%q,"channel":"C1","ts":"123.456"}}`, text))
}

func TestVerification(t *testing.T) {
	h, _, _ := newHandler(t)
	body := []byte(`{"type":"event_callback"}`)

	t.Run("bad signature", func(t *testing.T) {
		req := sign(t, body, time.Now().Unix())
		req.Header.Set("X-Slack-Signature", "v0=deadbeef")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("got %d", rec.Code)
		}
	})

	t.Run("stale timestamp", func(t *testing.T) {
		req := sign(t, body, time.Now().Add(-10*time.Minute).Unix())
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("got %d", rec.Code)
		}
	})
}

func TestURLVerification(t *testing.T) {
	h, _, _ := newHandler(t)
	body := []byte(`{"type":"url_verification","challenge":"abc123"}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, body, time.Now().Unix()))
	if rec.Code != http.StatusOK || rec.Body.String() != "abc123" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestAppMentionDispatch(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		ephemeral bool
		// substrings the responder text MUST contain
		wants []string
		// where the reply must go: thread ts ("123.456") for non-ephemeral,
		// user ID ("UAAA") for ephemeral
		target string
	}{
		{name: "add", text: "<@USERGEANT> <@UBBB> +20 PLN", ephemeral: false, wants: []string{"<@UBBB>", "20.00 PLN"}, target: "123.456"},
		{name: "status all", text: "<@USERGEANT> status", ephemeral: true, wants: []string{"Nobody owes you"}, target: "UAAA"},
		{name: "unrecognized", text: "<@USERGEANT> ¿qué?", ephemeral: true, wants: []string{"Usage"}, target: "UAAA"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, sp, _ := newHandler(t)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, sign(t, appMention(tc.text), time.Now().Unix()))
			if rec.Code != http.StatusOK {
				t.Fatalf("got %d", rec.Code)
			}
			p := sp.wait(t)
			if p.ephemeral != tc.ephemeral {
				t.Errorf("ephemeral=%v, want %v", p.ephemeral, tc.ephemeral)
			}
			if p.target != tc.target {
				t.Errorf("target=%q, want %q", p.target, tc.target)
			}
			for _, w := range tc.wants {
				if !strings.Contains(p.text, w) {
					t.Errorf("text=%q missing %q", p.text, w)
				}
			}
		})
	}
}

func TestAppMentionWritesToLedger(t *testing.T) {
	h, sp, l := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, appMention("<@USERGEANT> <@UBBB> +20 PLN"), time.Now().Unix()))
	sp.wait(t)

	r, _ := l.Apply(context.Background(), "UAAA", parser.Command{Kind: parser.KindStatusFor, Target: "UBBB"})
	if !strings.Contains(r.Text, "20.00 PLN") {
		t.Fatalf("ledger view: %q", r.Text)
	}
}

// signForm signs a form-urlencoded interactivity body the same way Slack does.
func signForm(t *testing.T, body string, ts int64) *http.Request {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(testSecret))
	_, _ = fmt.Fprintf(mac, "v0:%d:", ts)
	mac.Write([]byte(body))
	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", fmt.Sprintf("%d", ts))
	req.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	return req
}

func TestPaySetFormPostsOpenerButton(t *testing.T) {
	h, sp, _ := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, appMention("<@USERGEANT> pay set"), time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	p := sp.wait(t)
	if !p.ephemeral {
		t.Errorf("opener should be ephemeral")
	}
	if !strings.Contains(p.text, "Add a payment method") {
		t.Errorf("text=%q missing fallback", p.text)
	}
}

func TestBlockActionOpensModal(t *testing.T) {
	h, sp, _ := newHandler(t)

	payload := slack.InteractionCallback{
		Type:      slack.InteractionTypeBlockActions,
		User:      slack.User{ID: "UAAA"},
		TriggerID: "trig.123",
		ActionCallback: slack.ActionCallbacks{
			BlockActions: []*slack.BlockAction{
				{ActionID: actionOpenPayForm},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	body := url.Values{"payload": []string{string(raw)}}.Encode()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signForm(t, body, time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}

	// The dispatch is in a goroutine; wait for OpenView.
	select {
	case <-sp.done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for OpenView")
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if len(sp.triggers) != 1 || sp.triggers[0] != "trig.123" {
		t.Fatalf("triggers = %v", sp.triggers)
	}
	if sp.views[0].CallbackID != callbackPayForm {
		t.Fatalf("modal callback = %q", sp.views[0].CallbackID)
	}
}

func TestViewSubmissionStoresAndConfirms(t *testing.T) {
	h, _, l := newHandler(t)

	// Build a minimal view_submission payload by hand because slack.View's
	// State.Values shape isn't a public construction-friendly API.
	payloadJSON := fmt.Sprintf(`{
		"type":"view_submission",
		"user":{"id":"UAAA"},
		"view":{
			"callback_id":%q,
			"state":{"values":{
				%q:{%q:{"selected_option":{"value":"bank"}}},
				%q:{%q:{"value":"PL61 1090 0000"}}
			}}
		}
	}`, callbackPayForm, blockMethod, actionMethod, blockValue, actionValue)

	body := url.Values{"payload": []string{payloadJSON}}.Encode()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signForm(t, body, time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d (body=%s)", rec.Code, rec.Body.String())
	}

	// Confirmation arrives by updating the modal in-place.
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not JSON: %s", rec.Body.String())
	}
	if resp["response_action"] != "update" {
		t.Fatalf("response_action = %v, want update", resp["response_action"])
	}
	view, ok := resp["view"].(map[string]any)
	if !ok {
		t.Fatalf("view missing: %+v", resp)
	}
	rendered, _ := json.Marshal(view)
	if !strings.Contains(string(rendered), "bank") {
		t.Errorf("update view missing method: %s", rendered)
	}

	// Confirm the row was actually written.
	pms, err := l.Apply(context.Background(), "UAAA", parser.Command{Kind: parser.KindPayShowSelf})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"bank", "PL61 1090 0000"} {
		if !strings.Contains(pms.Text, want) {
			t.Errorf("ledger view %q missing %q", pms.Text, want)
		}
	}
}

func TestViewSubmissionEmptyValueReopensWithErrors(t *testing.T) {
	h, _, _ := newHandler(t)

	payloadJSON := fmt.Sprintf(`{
		"type":"view_submission",
		"user":{"id":"UAAA"},
		"channel":{"id":"C1"},
		"view":{
			"callback_id":%q,
			"state":{"values":{
				%q:{%q:{"selected_option":{"value":"bank"}}},
				%q:{%q:{"value":""}}
			}}
		}
	}`, callbackPayForm, blockMethod, actionMethod, blockValue, actionValue)

	body := url.Values{"payload": []string{payloadJSON}}.Encode()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signForm(t, body, time.Now().Unix()))

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not JSON: %s", rec.Body.String())
	}
	if resp["response_action"] != "errors" {
		t.Fatalf("expected response_action=errors, got %+v", resp)
	}
	errs, ok := resp["errors"].(map[string]any)
	if !ok || errs[blockValue] == nil {
		t.Fatalf("expected error on %q, got %+v", blockValue, resp)
	}
}

func TestStripBotMention(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<@USERGEANT> +1", "+1"},
		{"<@USERGEANT|sergeant> status", "status"},
		{"hello", "hello"},
		{"<@OTHER> +1", "<@OTHER> +1"},
	}
	for _, c := range cases {
		if got := stripBotMention(c.in, "USERGEANT"); got != c.want {
			t.Errorf("strip(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
