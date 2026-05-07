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

type reaction struct{ channel, ts, name string }

type spy struct {
	mu        sync.Mutex
	calls     []post
	views     []slack.ModalViewRequest
	triggers  []string
	reactions []reaction
	done      chan struct{}
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
func (s *spy) AddReaction(_ context.Context, channel, ts, name string) error {
	s.mu.Lock()
	s.reactions = append(s.reactions, reaction{channel, ts, name})
	s.mu.Unlock()
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
	usageReply := ":cop: " + i18n.New("en").T(i18n.HandlerUsage)
	tests := []struct {
		name      string
		text      string
		ephemeral bool
		want      string
		// where the reply must go: thread ts ("123.456") for non-ephemeral,
		// user ID ("UAAA") for ephemeral
		target string
	}{
		{name: "add", text: "<@USERGEANT> <@UBBB> +20 PLN", ephemeral: false, want: ":cop: <@UBBB> now owes you 20.00 PLN.", target: "123.456"},
		{name: "status all", text: "<@USERGEANT> status", ephemeral: false, want: ":cop: Nobody owes you and you owe nothing.", target: "123.456"},
		{name: "unrecognized", text: "<@USERGEANT> ¿qué?", ephemeral: true, want: usageReply, target: "UAAA"},
		{name: "bare mention", text: "<@USERGEANT>", ephemeral: true, want: usageReply, target: "UAAA"},
		{name: "bad amount", text: "<@USERGEANT> <@UBBB> +abc", ephemeral: true, want: usageReply, target: "UAAA"},
		{name: "bad currency", text: "<@USERGEANT> <@UBBB> +20 PLNS", ephemeral: true, want: usageReply, target: "UAAA"},
		{name: "unknown pay subcommand", text: "<@USERGEANT> pay nonsense", ephemeral: true, want: usageReply, target: "UAAA"},
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
			if p.text != tc.want {
				t.Errorf("text:\n  got:  %q\n  want: %q", p.text, tc.want)
			}
		})
	}
}

func TestAppMentionAddsReaction(t *testing.T) {
	h, sp, _ := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, appMention("<@USERGEANT> status"), time.Now().Unix()))
	sp.wait(t)
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if len(sp.reactions) != 1 {
		t.Fatalf("reactions = %v, want 1", sp.reactions)
	}
	got := sp.reactions[0]
	if got.channel != "C1" || got.ts != "123.456" || got.name != "sergeant" {
		t.Errorf("reaction = %+v", got)
	}
}

func TestDirectMessageNoReaction(t *testing.T) {
	h, sp, _ := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, directMessage("<@UBBB> +20 PLN"), time.Now().Unix()))
	sp.wait(t)
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if len(sp.reactions) != 0 {
		t.Errorf("DM should not trigger a reaction, got %v", sp.reactions)
	}
}

func TestAppMentionWritesToLedger(t *testing.T) {
	h, sp, l := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, appMention("<@USERGEANT> <@UBBB> +20 PLN"), time.Now().Unix()))
	sp.wait(t)

	r, _ := l.Apply(context.Background(), "UAAA", parser.Command{Kind: parser.KindStatusFor, Target: "UBBB"})
	if want := "<@UBBB> owes you 20.00 PLN (just now)."; r.Text != want {
		t.Fatalf("ledger view: got %q, want %q", r.Text, want)
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
	if want := "Add a payment method privately"; p.text != want {
		t.Errorf("text: got %q, want %q", p.text, want)
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
	var resp struct {
		ResponseAction string `json:"response_action"`
		View           struct {
			Blocks []struct {
				Type string `json:"type"`
				Text struct {
					Text string `json:"text"`
				} `json:"text"`
			} `json:"blocks"`
		} `json:"view"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body not JSON: %s", rec.Body.String())
	}
	if resp.ResponseAction != "update" {
		t.Fatalf("response_action = %q, want update", resp.ResponseAction)
	}
	if len(resp.View.Blocks) == 0 {
		t.Fatalf("update view has no blocks: %s", rec.Body.String())
	}
	if want := "Saved your `bank`."; resp.View.Blocks[0].Text.Text != want {
		t.Errorf("update view body: got %q, want %q", resp.View.Blocks[0].Text.Text, want)
	}

	// Confirm the row was actually written.
	pms, err := l.Apply(context.Background(), "UAAA", parser.Command{Kind: parser.KindPayShowSelf})
	if err != nil {
		t.Fatal(err)
	}
	if want := "<@UAAA> :money_with_wings: \n- `bank` - PL61 1090 0000"; pms.Text != want {
		t.Errorf("ledger view: got %q, want %q", pms.Text, want)
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

func TestAppMentionPaySetInline(t *testing.T) {
	h, sp, l := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, appMention("<@USERGEANT> pay set bank PL61 1090 0000 1234"), time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	p := sp.wait(t)
	if p.ephemeral {
		t.Error("pay set reply should not be ephemeral")
	}
	if want := ":cop: Saved your `bank`."; p.text != want {
		t.Errorf("text: got %q, want %q", p.text, want)
	}
	r, _ := l.Apply(context.Background(), "UAAA", parser.Command{Kind: parser.KindPayShowSelf})
	if want := "<@UAAA> :money_with_wings: \n- `bank` - PL61 1090 0000 1234"; r.Text != want {
		t.Errorf("ledger view: got %q, want %q", r.Text, want)
	}
}

func TestAppMentionPayRemove(t *testing.T) {
	h, sp, l := newHandler(t)
	if _, err := l.Apply(context.Background(), "UAAA", parser.Command{Kind: parser.KindPaySet, PayMethod: "bank", PayValue: "PL61"}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, appMention("<@USERGEANT> pay rm bank"), time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	sp.wait(t)
	r, _ := l.Apply(context.Background(), "UAAA", parser.Command{Kind: parser.KindPayShowSelf})
	want := "You haven't added any payment methods yet. Try `@sergeant pay set bank PL61 ...` or `@sergeant pay set blik 555 555 555`."
	if r.Text != want {
		t.Errorf("payment methods should be empty: got %q, want %q", r.Text, want)
	}
}

func TestAppMentionPayClear(t *testing.T) {
	h, sp, l := newHandler(t)
	for _, m := range []string{"bank", "blik"} {
		if _, err := l.Apply(context.Background(), "UAAA", parser.Command{Kind: parser.KindPaySet, PayMethod: m, PayValue: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, appMention("<@USERGEANT> pay clear"), time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	sp.wait(t)
	r, _ := l.Apply(context.Background(), "UAAA", parser.Command{Kind: parser.KindPayShowSelf})
	want := "You haven't added any payment methods yet. Try `@sergeant pay set bank PL61 ...` or `@sergeant pay set blik 555 555 555`."
	if r.Text != want {
		t.Errorf("expected empty payment list: got %q, want %q", r.Text, want)
	}
}

func TestAppMentionReset(t *testing.T) {
	h, sp, l := newHandler(t)
	ctx := context.Background()
	if _, err := l.Apply(ctx, "UAAA", parser.Command{Kind: parser.KindAdd, Target: "UBBB", Sign: 1, Minor: 2000, Currency: "PLN"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(ctx, "UAAA", parser.Command{Kind: parser.KindAdd, Target: "UBBB", Sign: 1, Minor: 1000, Currency: "EUR"}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, appMention("<@USERGEANT> <@UBBB> reset"), time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	sp.wait(t)
	r, _ := l.Apply(ctx, "UAAA", parser.Command{Kind: parser.KindStatusFor, Target: "UBBB"})
	if want := "<@UBBB> owes you nothing."; r.Text != want {
		t.Errorf("both currencies should be cleared: got %q, want %q", r.Text, want)
	}
}

func TestAppMentionResetCurrency(t *testing.T) {
	h, sp, l := newHandler(t)
	ctx := context.Background()
	if _, err := l.Apply(ctx, "UAAA", parser.Command{Kind: parser.KindAdd, Target: "UBBB", Sign: 1, Minor: 2000, Currency: "PLN"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Apply(ctx, "UAAA", parser.Command{Kind: parser.KindAdd, Target: "UBBB", Sign: 1, Minor: 1000, Currency: "EUR"}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, appMention("<@USERGEANT> <@UBBB> reset PLN"), time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	sp.wait(t)
	r, _ := l.Apply(ctx, "UAAA", parser.Command{Kind: parser.KindStatusFor, Target: "UBBB"})
	if want := "<@UBBB> owes you 10.00 EUR (just now)."; r.Text != want {
		t.Errorf("EUR should remain, PLN cleared: got %q, want %q", r.Text, want)
	}
}

func TestRejectsNonPost(t *testing.T) {
	h, _, _ := newHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/slack/events", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d, want 405", rec.Code)
	}
}

func TestRejectsBadJSON(t *testing.T) {
	h, _, _ := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, []byte("not-json"), time.Now().Unix()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestIgnoresUnknownOuterEventType(t *testing.T) {
	h, sp, _ := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, []byte(`{"type":"app_rate_limited"}`), time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	select {
	case <-sp.done:
		t.Fatal("responder should not have been called")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestServeInteractivityNoPayload(t *testing.T) {
	h, sp, _ := newHandler(t)
	body := url.Values{"foo": []string{"bar"}}.Encode()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signForm(t, body, time.Now().Unix()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
	select {
	case <-sp.done:
		t.Fatal("responder should not have been called")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestServeInteractivityBadPayloadJSON(t *testing.T) {
	h, _, _ := newHandler(t)
	body := url.Values{"payload": []string{"not-json"}}.Encode()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signForm(t, body, time.Now().Unix()))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
}

func TestServeInteractivityIgnoresUnknownInteractionType(t *testing.T) {
	h, sp, _ := newHandler(t)
	payload := slack.InteractionCallback{
		Type: slack.InteractionTypeShortcut,
		User: slack.User{ID: "UAAA"},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	body := url.Values{"payload": []string{string(raw)}}.Encode()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, signForm(t, body, time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	select {
	case <-sp.done:
		t.Fatal("responder should not have been called")
	case <-time.After(50 * time.Millisecond):
	}
}

func directMessage(text string) []byte {
	return []byte(fmt.Sprintf(`{"type":"event_callback","event":{"type":"message","channel_type":"im","user":"UAAA","text":%q,"channel":"D1","ts":"123.456"}}`, text))
}

func directMessageWithBot(text string) []byte {
	return []byte(fmt.Sprintf(`{"type":"event_callback","event":{"type":"message","channel_type":"im","user":"UAAA","bot_id":"B1","text":%q,"channel":"D1","ts":"123.456"}}`, text))
}

func TestDirectMessageDispatch(t *testing.T) {
	h, sp, l := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, directMessage("<@UBBB> +20 PLN"), time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	p := sp.wait(t)
	if p.ephemeral {
		t.Errorf("DM reply should not be ephemeral")
	}
	if want := ":cop: <@UBBB> now owes you 20.00 PLN."; p.text != want {
		t.Errorf("text: got %q, want %q", p.text, want)
	}
	r, _ := l.Apply(context.Background(), "UAAA", parser.Command{Kind: parser.KindStatusFor, Target: "UBBB"})
	if want := "<@UBBB> owes you 20.00 PLN (just now)."; r.Text != want {
		t.Fatalf("ledger view: got %q, want %q", r.Text, want)
	}
}

func TestDirectMessageIgnoresBots(t *testing.T) {
	h, sp, _ := newHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, directMessageWithBot("<@UBBB> +20 PLN"), time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	select {
	case <-sp.done:
		t.Fatal("bot DM should be ignored")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDispatchIgnoresUnknownInnerEvent(t *testing.T) {
	h, sp, _ := newHandler(t)
	body := []byte(`{"type":"event_callback","event":{"type":"reaction_added","user":"UAAA","reaction":"thumbsup"}}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, sign(t, body, time.Now().Unix()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	select {
	case <-sp.done:
		t.Fatal("unknown inner event should not invoke responder")
	case <-time.After(100 * time.Millisecond):
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
