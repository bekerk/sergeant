package slackapi

import (
	"github.com/slack-go/slack"

	"sergeant/internal/i18n"
)

const (
	actionOpenPayForm = "open_pay_form"
	callbackPayForm   = "pay_form"
	blockMethod       = "pay_method_block"
	blockValue        = "pay_value_block"
	actionMethod      = "pay_method"
	actionValue       = "pay_value"
)

func payOpenerBlocks(t *i18n.Translator) []slack.Block {
	intro := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, t.T(i18n.PayOpenerText), false, false),
		nil, nil,
	)
	button := slack.NewButtonBlockElement(
		actionOpenPayForm, "",
		slack.NewTextBlockObject(slack.PlainTextType, t.T(i18n.PayOpenerButton), false, false),
	)
	button.Style = slack.StylePrimary
	return []slack.Block{intro, slack.NewActionBlock("pay_opener", button)}
}

func payModalView(t *i18n.Translator) slack.ModalViewRequest {
	plain := func(s string) *slack.TextBlockObject {
		return slack.NewTextBlockObject(slack.PlainTextType, s, false, false)
	}
	option := func(value, label string) *slack.OptionBlockObject {
		return slack.NewOptionBlockObject(value, plain(label), nil)
	}

	methodSelect := slack.NewOptionsSelectBlockElement(
		slack.OptTypeStatic, plain(t.T(i18n.PayModalMethod)), actionMethod,
		option("bank", t.T(i18n.PayOptBank)),
		option("blik", t.T(i18n.PayOptBlik)),
		option("revolut", t.T(i18n.PayOptRevolut)),
		option("paypal", t.T(i18n.PayOptPaypal)),
		option("wise", t.T(i18n.PayOptWise)),
	)
	methodInput := slack.NewInputBlock(blockMethod, plain(t.T(i18n.PayModalMethod)), nil, methodSelect)

	valueElem := &slack.PlainTextInputBlockElement{
		Type:      slack.METPlainTextInput,
		ActionID:  actionValue,
		MaxLength: 200,
	}
	valueInput := slack.NewInputBlock(blockValue, plain(t.T(i18n.PayModalValue)), plain(t.T(i18n.PayModalHint)), valueElem)

	return slack.ModalViewRequest{
		Type:       slack.VTModal,
		Title:      plain(t.T(i18n.PayModalTitle)),
		Submit:     plain(t.T(i18n.PayModalSubmit)),
		Close:      plain(t.T(i18n.PayModalCancel)),
		CallbackID: callbackPayForm,
		Blocks: slack.Blocks{
			BlockSet: []slack.Block{methodInput, valueInput},
		},
	}
}

func paySavedView(t *i18n.Translator, method string) slack.ModalViewRequest {
	plain := func(s string) *slack.TextBlockObject {
		return slack.NewTextBlockObject(slack.PlainTextType, s, false, false)
	}
	body := slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, t.T(i18n.PaySaved, method), false, false),
		nil, nil,
	)
	return slack.ModalViewRequest{
		Type:  slack.VTModal,
		Title: plain(t.T(i18n.PayModalTitle)),
		Close: plain(t.T(i18n.PayModalDone)),
		Blocks: slack.Blocks{
			BlockSet: []slack.Block{body},
		},
	}
}
