package i18n

import (
	"fmt"
	"sort"
)

const DefaultLocale = "en"

const (
	LedgerNowOwes             = "ledger.now_owes"               // args: target, amount, currency
	LedgerTabCleared          = "ledger.tab_cleared"            // args: target, currency
	LedgerResetAll            = "ledger.reset_all"              // args: target
	LedgerResetCurrency       = "ledger.reset_currency"         // args: target, currency
	LedgerStatusForEmpty      = "ledger.status_for_empty"       // args: target
	LedgerStatusFor           = "ledger.status_for"             // args: target, joined amounts
	LedgerStatusAllEmpty      = "ledger.status_all_empty"       //
	LedgerStatusAllOwedHeader = "ledger.status_all_owed_header" //
	LedgerStatusAllOweHeader  = "ledger.status_all_owe_header"  //
	LedgerStatusAllLine       = "ledger.status_all_line"        // args: target, joined amounts

	PaySaved         = "pay.saved"            // args: method
	PayRemoved       = "pay.removed"          // args: method
	PayCleared       = "pay.cleared"          //
	PayShowSelfEmpty = "pay.show_self_empty"  //
	PayShowSelfHead  = "pay.show_self_header" //
	PayShowForEmpty  = "pay.show_for_empty"   // args: target
	PayShowForHead   = "pay.show_for_header"  // args: target
	PayLine          = "pay.line"             // args: method, value

	// Modal flow:
	PayOpenerText   = "pay.opener_text"
	PayOpenerButton = "pay.opener_button"
	PayModalTitle   = "pay.modal_title"
	PayModalSubmit  = "pay.modal_submit"
	PayModalCancel  = "pay.modal_cancel"
	PayModalDone    = "pay.modal_done"
	PayModalMethod  = "pay.modal_method_label"
	PayModalValue   = "pay.modal_value_label"
	PayModalHint    = "pay.modal_value_hint"
	PayOptBank      = "pay.option_bank"
	PayOptBlik      = "pay.option_blik"
	PayOptRevolut   = "pay.option_revolut"
	PayOptPaypal    = "pay.option_paypal"
	PayOptWise      = "pay.option_wise"

	HandlerUsage      = "handler.usage"
	HandlerSelfTarget = "handler.self_target"
	HandlerError      = "handler.error"
)

var bundles = map[string]map[string]string{
	"en": {
		LedgerNowOwes:             "<@%s> now owes you %s %s.",
		LedgerTabCleared:          "Tab cleared: <@%s> owes you nothing in %s.",
		LedgerResetAll:            "Cleared <@%s>'s tab with you.",
		LedgerResetCurrency:       "Cleared <@%s>'s %s tab with you.",
		LedgerStatusForEmpty:      "<@%s> owes you nothing.",
		LedgerStatusFor:           "<@%s> owes you %s.",
		LedgerStatusAllEmpty:      "Nobody owes you and you owe nothing.",
		LedgerStatusAllOwedHeader: "Owed to you:",
		LedgerStatusAllOweHeader:  "\n\nYou owe:",
		LedgerStatusAllLine:       "\n• <@%s> - %s",

		PaySaved:         "Saved your `%s`.",
		PayRemoved:       "Removed your `%s`.",
		PayCleared:       "Cleared all your payment methods.",
		PayShowSelfEmpty: "You haven't added any payment methods yet. Try `@sergeant pay set bank PL61 ...` or `@sergeant pay set blik 555 555 555`.",
		PayShowSelfHead:  "Your payment methods:",
		PayShowForEmpty:  "<@%s> hasn't added any payment methods.",
		PayShowForHead:   "<@%s>'s payment methods:",
		PayLine:          "\n• `%s` - %s",

		PayOpenerText:   "Add a payment method privately",
		PayOpenerButton: "Open form",
		PayModalTitle:   "Add payment method",
		PayModalSubmit:  "Save",
		PayModalCancel:  "Cancel",
		PayModalDone:    "Done",
		PayModalMethod:  "Method",
		PayModalValue:   "Details",
		PayModalHint:    "e.g. PL61 1090 0000 1234 5678, your phone number for BLIK, or your Revolut/PayPal handle.",
		PayOptBank:      "Bank account / IBAN",
		PayOptBlik:      "BLIK (phone)",
		PayOptRevolut:   "Revolut",
		PayOptPaypal:    "PayPal",
		PayOptWise:      "Wise",

		HandlerUsage:      "Usage: `@sergeant <@user> +AMOUNT [CCY]` / `-AMOUNT [CCY]`, `… reset [CCY]`, `… status` (or `?`), `@sergeant status`, `@sergeant pay`, `@sergeant pay set` (opens a form), `@sergeant pay set METHOD VALUE`, `@sergeant pay rm METHOD`, `@sergeant pay clear`, `@sergeant <@user> pay`.",
		HandlerSelfTarget: "You can't owe yourself.",
		HandlerError:      "Something went wrong.",
	},
	"pl": {
		LedgerNowOwes:             "<@%s> jest tobie winny %s %s.",
		LedgerTabCleared:          "Wyzerowano: <@%s> nie ma u Ciebie długu w %s.",
		LedgerResetAll:            "Wyzerowano rachunek <@%s>.",
		LedgerResetCurrency:       "Wyzerowano rachunek <@%s> w %s.",
		LedgerStatusForEmpty:      "<@%s> nie ma u Ciebie długu.",
		LedgerStatusFor:           "<@%s> ma u Ciebie %s.",
		LedgerStatusAllEmpty:      "Nikt nie ma u Ciebie długu i Ty też nie.",
		LedgerStatusAllOwedHeader: "Mają u Ciebie dług:",
		LedgerStatusAllOweHeader:  "\n\nMasz dług u:",
		LedgerStatusAllLine:       "\n• <@%s> - %s",

		PaySaved:         "Zapisano `%s`.",
		PayRemoved:       "Usunięto `%s`.",
		PayCleared:       "Wyczyszczono wszystkie metody płatności.",
		PayShowSelfEmpty: "Nie masz jeszcze żadnych metod płatności. Spróbuj `@sergeant pay set bank PL61 ...` lub `@sergeant pay set blik 555 555 555`.",
		PayShowSelfHead:  "Twoje metody płatności:",
		PayShowForEmpty:  "<@%s> nie dodał jeszcze żadnych metod płatności.",
		PayShowForHead:   "Metody płatności <@%s>:",
		PayLine:          "\n• `%s` - %s",

		PayOpenerText:   "Dodaj metodę płatności.",
		PayOpenerButton: "Otwórz formularz.",
		PayModalTitle:   "Dodaj metodę płatności.",
		PayModalSubmit:  "Zapisz",
		PayModalCancel:  "Anuluj",
		PayModalDone:    "Gotowe",
		PayModalMethod:  "Metoda",
		PayModalValue:   "Szczegóły",
		PayModalHint:    "np. PL61 1090 0000 1234 5678, numer telefonu dla BLIK, albo nazwa użytkownika Revolut/PayPal.",
		PayOptBank:      "Konto bankowe / IBAN",
		PayOptBlik:      "BLIK (telefon)",
		PayOptRevolut:   "Revolut",
		PayOptPaypal:    "PayPal",
		PayOptWise:      "Wise",

		HandlerUsage:      "Użycie: `@sergeant <@user> +KWOTA [WALUTA]` / `-KWOTA [WALUTA]`, `… reset [WALUTA]`, `… status` (lub `?`), `@sergeant status`, `@sergeant pay`, `@sergeant pay set` (otwiera formularz), `@sergeant pay set METODA WARTOŚĆ`, `@sergeant pay rm METODA`, `@sergeant pay clear`, `@sergeant <@user> pay`.",
		HandlerSelfTarget: "Nie możesz zapisać długu na samego siebie.",
		HandlerError:      "Coś poszło nie tak.",
	},
}

type Translator struct {
	locale string
	msgs   map[string]string
}

func New(locale string) *Translator {
	msgs, ok := bundles[locale]
	if !ok {
		locale = DefaultLocale
		msgs = bundles[DefaultLocale]
	}
	return &Translator{locale: locale, msgs: msgs}
}

func (t *Translator) Locale() string { return t.locale }

func (t *Translator) T(id string, args ...any) string {
	f, ok := t.msgs[id]
	if !ok {
		f, ok = bundles[DefaultLocale][id]
		if !ok {
			return id
		}
	}
	if len(args) == 0 {
		return f
	}
	return fmt.Sprintf(f, args...)
}

func Available() []string {
	out := make([]string, 0, len(bundles))
	for k := range bundles {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
