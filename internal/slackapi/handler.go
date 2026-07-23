package slackapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"sergeant/internal/i18n"
	"sergeant/internal/ledger"
	"sergeant/internal/parser"
)

const (
	maxBodyBytes             = 1 << 20
	replyTimeout             = 5 * time.Second
	mentionEmoji             = "sergeant"
	unrecognizedCommandEmoji = "sergeant-no"
)

type Responder interface {
	PostInThread(ctx context.Context, channel, threadTs, text string) error
	PostEphemeral(ctx context.Context, channel, user, text string) error
	PostEphemeralBlocks(ctx context.Context, channel, user, fallback string, blocks []slack.Block) error
	OpenView(ctx context.Context, triggerID string, view slack.ModalViewRequest) error
	AddReaction(ctx context.Context, channel, ts, name string) error
}

type SlackResponder struct{ Client *slack.Client }

func (r SlackResponder) PostInThread(ctx context.Context, channel, threadTs, text string) error {
	_, _, err := r.Client.PostMessageContext(ctx, channel,
		slack.MsgOptionText(text, false),
		slack.MsgOptionTS(threadTs),
		slack.MsgOptionDisableLinkUnfurl(),
	)
	return err
}

func (r SlackResponder) PostEphemeral(ctx context.Context, channel, user, text string) error {
	_, err := r.Client.PostEphemeralContext(ctx, channel, user,
		slack.MsgOptionText(text, false),
		slack.MsgOptionDisableLinkUnfurl(),
	)
	return err
}

func (r SlackResponder) PostEphemeralBlocks(ctx context.Context, channel, user, fallback string, blocks []slack.Block) error {
	_, err := r.Client.PostEphemeralContext(ctx, channel, user,
		slack.MsgOptionText(fallback, false),
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionDisableLinkUnfurl(),
	)
	return err
}

func (r SlackResponder) OpenView(ctx context.Context, triggerID string, view slack.ModalViewRequest) error {
	_, err := r.Client.OpenViewContext(ctx, triggerID, view)
	return err
}

func (r SlackResponder) AddReaction(ctx context.Context, channel, ts, name string) error {
	return r.Client.AddReactionContext(ctx, name, slack.NewRefToMessage(channel, ts))
}

type Handler struct {
	SigningSecret string
	BotUserID     string
	Ledger        *ledger.Ledger
	Responder     Responder
	Translator    *i18n.Translator
	Logger        *slog.Logger
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Logger.Debug("incoming", "method", r.Method, "path", r.URL.Path, "from", r.RemoteAddr)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	sv, err := slack.NewSecretsVerifier(r.Header, h.SigningSecret)
	if err == nil {
		_, err = sv.Write(body)
	}
	if err == nil {
		err = sv.Ensure()
	}
	if err != nil {
		h.Logger.Warn("verify failed", "err", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		h.serveInteractivity(w, body)
		return
	}

	event, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	switch event.Type {
	case slackevents.URLVerification:
		var v slackevents.ChallengeResponse
		if err := json.Unmarshal(body, &v); err != nil {
			http.Error(w, "bad challenge", http.StatusBadRequest)
			return
		}
		h.Logger.Info("url verification challenge")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(v.Challenge))

	case slackevents.CallbackEvent:
		w.WriteHeader(http.StatusOK)
		go h.safely("dispatch", func() { h.dispatch(event) })

	default:
		h.Logger.Debug("ignoring event", "type", event.Type)
		w.WriteHeader(http.StatusOK)
	}
}

type incoming struct {
	source                      string // "app_mention" or "im"
	user, channel, ts, threadTs string
	text                        string
}

func (h *Handler) dispatch(event slackevents.EventsAPIEvent) {
	var msg incoming
	switch ev := event.InnerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		msg = incoming{"app_mention", ev.User, ev.Channel, ev.TimeStamp, ev.ThreadTimeStamp, ev.Text}
	case *slackevents.MessageEvent:
		// Only direct messages from real users - skip channels, edits/joins,
		// other bots, and our own outbound messages.
		if ev.ChannelType != "im" || ev.BotID != "" || ev.SubType != "" || ev.User == "" || ev.User == h.BotUserID {
			return
		}
		msg = incoming{"im", ev.User, ev.Channel, ev.TimeStamp, ev.ThreadTimeStamp, ev.Text}
	default:
		h.Logger.Debug("ignoring callback", "inner_type", event.InnerEvent.Type)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), replyTimeout)
	defer cancel()

	acknowledge := func() bool {
		if msg.source != "app_mention" {
			return false
		}
		if err := h.Responder.AddReaction(ctx, msg.channel, msg.ts, mentionEmoji); err != nil {
			h.Logger.Warn("add reaction", "err", err)
			return false
		}
		return true
	}

	stripped := stripBotMention(msg.text, h.BotUserID)
	h.Logger.Info(msg.source,
		"user", msg.user,
		"channel", msg.channel,
		"text", stripped,
	)

	cmd, err := parser.Parse(stripped)
	if err != nil {
		h.Logger.Info("unrecognized command", "text", stripped)
		acknowledge()
		if err := h.Responder.AddReaction(ctx, msg.channel, msg.ts, unrecognizedCommandEmoji); err != nil {
			h.Logger.Warn("add reaction", "err", err)
		}
		h.send(ctx, Reply{channel: msg.channel, user: msg.user, text: h.Translator.T(i18n.HandlerUsage), ephemeral: true})
		return
	}
	h.Logger.Info("dispatch", "kind", cmd.Kind, "target", cmd.Target)
	debtMutation := cmd.Kind == parser.KindAdd || cmd.Kind == parser.KindReset

	if cmd.Kind == parser.KindHelp {
		acknowledge()
		h.send(ctx, Reply{channel: msg.channel, user: msg.user, text: h.Translator.T(i18n.HandlerUsage), ephemeral: true})
		return
	}

	if cmd.Kind == parser.KindHello {
		acknowledge()
		threadTs := msg.threadTs
		if threadTs == "" {
			threadTs = msg.ts
		}
		h.send(ctx, Reply{channel: msg.channel, user: msg.user, threadTs: threadTs, text: h.Translator.T(i18n.HandlerUsage)})
		return
	}

	if cmd.Kind == parser.KindPaySetForm {
		acknowledge()
		if err := h.Responder.PostEphemeralBlocks(
			ctx, msg.channel, msg.user,
			h.Translator.T(i18n.PayOpenerText),
			payOpenerBlocks(h.Translator),
		); err != nil {
			h.Logger.Error("post pay opener", "err", err)
		}
		return
	}

	r, err := h.Ledger.Apply(ctx, msg.user, cmd)
	if err != nil {
		if debtMutation {
			if err := h.Responder.AddReaction(ctx, msg.channel, msg.ts, unrecognizedCommandEmoji); err != nil {
				h.Logger.Warn("add failed debt mutation reaction", "err", err)
			}
			return
		}
		h.Logger.Error("ledger apply", "err", err)
		h.send(ctx, Reply{channel: msg.channel, user: msg.user, text: h.Translator.T(i18n.HandlerError), ephemeral: true})
		return
	}

	if debtMutation {
		if err := h.Responder.AddReaction(ctx, msg.channel, msg.ts, mentionEmoji); err != nil {
			h.Logger.Warn("add debt mutation reaction", "err", err)
		}
		return
	}
	acknowledge()

	threadTs := msg.threadTs
	if threadTs == "" {
		threadTs = msg.ts
	}
	h.send(ctx, Reply{
		channel:   msg.channel,
		user:      msg.user,
		threadTs:  threadTs,
		text:      r.Text,
		ephemeral: r.Ephemeral,
	})
}

type Reply struct {
	channel, user, threadTs, text string
	ephemeral                     bool
}

func (h *Handler) send(ctx context.Context, r Reply) {
	text := ":cop: " + r.text
	var err error
	if r.ephemeral {
		err = h.Responder.PostEphemeral(ctx, r.channel, r.user, text)
	} else {
		err = h.Responder.PostInThread(ctx, r.channel, r.threadTs, text)
	}
	if err != nil {
		h.Logger.Error("post reply", "err", err)
	}
}

func (h *Handler) safely(label string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			h.Logger.Error("panic in goroutine", "where", label, "panic", r)
		}
	}()
	fn()
}

var leadingMentionRE = regexp.MustCompile(`^\s*<@([A-Z0-9]+)(?:\|[^>]*)?>\s*`)

func stripBotMention(text, botID string) string {
	m := leadingMentionRE.FindStringSubmatch(text)
	if m == nil || m[1] != botID {
		return text
	}
	return text[len(m[0]):]
}

func (h *Handler) serveInteractivity(w http.ResponseWriter, body []byte) {
	form, err := url.ParseQuery(string(body))
	if err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	raw := form.Get("payload")
	if raw == "" {
		http.Error(w, "no payload", http.StatusBadRequest)
		return
	}
	var cb slack.InteractionCallback
	if err := json.Unmarshal([]byte(raw), &cb); err != nil {
		h.Logger.Warn("interaction parse", "err", err)
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	h.Logger.Info("interaction", "type", cb.Type, "user", cb.User.ID, "callback_id", cb.View.CallbackID)

	switch cb.Type {
	case slack.InteractionTypeBlockActions:
		w.WriteHeader(http.StatusOK)
		go h.safely("handleBlockAction", func() { h.handleBlockAction(cb) })

	case slack.InteractionTypeViewSubmission:
		// view_submission must respond synchronously: empty 200 closes the
		// modal, a JSON `errors` body reopens it with field-level errors.
		if cb.View.CallbackID == callbackPayForm {
			h.handlePayFormSubmission(w, cb)
			return
		}
		w.WriteHeader(http.StatusOK)

	default:
		w.WriteHeader(http.StatusOK)
	}
}

func (h *Handler) handleBlockAction(cb slack.InteractionCallback) {
	ctx, cancel := context.WithTimeout(context.Background(), replyTimeout)
	defer cancel()

	for _, a := range cb.ActionCallback.BlockActions {
		if a.ActionID == actionOpenPayForm {
			if err := h.Responder.OpenView(ctx, cb.TriggerID, payModalView(h.Translator)); err != nil {
				h.Logger.Error("open view", "err", err)
			}
			return
		}
	}
}

func (h *Handler) handlePayFormSubmission(w http.ResponseWriter, cb slack.InteractionCallback) {
	values := cb.View.State.Values

	method := values[blockMethod][actionMethod].SelectedOption.Value
	value := strings.TrimSpace(values[blockValue][actionValue].Value)

	if method == "" || value == "" {
		// Re-open the modal with field-level errors.
		errs := map[string]string{}
		if method == "" {
			errs[blockMethod] = h.Translator.T(i18n.PayModalMethod)
		}
		if value == "" {
			errs[blockValue] = h.Translator.T(i18n.PayModalValue)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(slack.NewErrorsViewSubmissionResponse(errs))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), replyTimeout)
	defer cancel()
	if _, err := h.Ledger.Apply(ctx, cb.User.ID, parser.Command{
		Kind:      parser.KindPaySet,
		PayMethod: method,
		PayValue:  value,
	}); err != nil {
		h.Logger.Error("ledger apply (modal)", "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	saved := paySavedView(h.Translator, method)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(slack.NewUpdateViewSubmissionResponse(&saved))
}
