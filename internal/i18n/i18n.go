package i18n

import (
	"fmt"
	"sort"
)

const DefaultLocale = "en"

const (
	LedgerNowOwes             = "ledger.now_owes"               // args: target, amount, currency
	LedgerYouOwe              = "ledger.you_owe"                // args: target, amount, currency
	LedgerTabCleared          = "ledger.tab_cleared"            // args: target, currency
	LedgerResetAll            = "ledger.reset_all"              // args: target
	LedgerResetCurrency       = "ledger.reset_currency"         // args: target, currency
	LedgerStatusForEmpty      = "ledger.status_for_empty"       // args: target
	LedgerStatusFor           = "ledger.status_for"             // args: target, joined amounts
	LedgerStatusAllEmpty      = "ledger.status_all_empty"       //
	LedgerStatusAllOwedHeader = "ledger.status_all_owed_header" //
	LedgerStatusAllOweHeader  = "ledger.status_all_owe_header"  //
	LedgerStatusAllLine       = "ledger.status_all_line"        // args: target, joined amounts

	LedgerSummaryOwedHeader = "ledger.summary_owed_header" // Who owes you money:
	LedgerSummaryPayHeader  = "ledger.summary_pay_header"  // Pay here:
	LedgerSummaryEmpty      = "ledger.summary_empty"       // Nobody owes you anything.

	PaySaved          = "pay.saved"           // args: method
	PayRemoved        = "pay.removed"         // args: method
	PayCleared        = "pay.cleared"         //
	PayDefaultSet     = "pay.default_set"     // args: method
	PayDefaultMissing = "pay.default_missing" // args: method
	PayShowSelfEmpty  = "pay.show_self_empty" //
	PayShowForEmpty   = "pay.show_for_empty"  // args: target

	PayLine        = "pay.line"         // args: method, value
	PayLineDefault = "pay.line_default" // args: method, value

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
		LedgerYouOwe:              "You now owe <@%s> %s %s.",
		LedgerTabCleared:          "Tab cleared: <@%s> owes you nothing in %s.",
		LedgerResetAll:            "Cleared <@%s>'s tab with you.",
		LedgerResetCurrency:       "Cleared <@%s>'s %s tab with you.",
		LedgerStatusForEmpty:      "<@%s> owes you nothing.",
		LedgerStatusFor:           "<@%s> owes you %s.",
		LedgerStatusAllEmpty:      "Nobody owes you and you owe nothing.",
		LedgerStatusAllOwedHeader: "Owed to you:",
		LedgerStatusAllOweHeader:  "You owe:",
		LedgerStatusAllLine:       "\n- <@%s> - %s",

		LedgerSummaryOwedHeader: "Summary",
		LedgerSummaryPayHeader:  ":money_with_wings:",
		LedgerSummaryEmpty:      "Nobody owes you anything.",

		PaySaved:          "Saved your `%s`.",
		PayRemoved:        "Removed your `%s`.",
		PayCleared:        "Cleared all your payment methods.",
		PayDefaultSet:     "Set `%s` as your default payment method.",
		PayDefaultMissing: "You haven't saved `%s` yet — add it first with `pay set %[1]s ...`.",
		PayShowSelfEmpty:  "You haven't added any payment methods yet. Try `@sergeant pay set bank PL61 ...` or `@sergeant pay set blik 555 555 555`.",
		PayShowForEmpty:   "<@%s> hasn't added any payment methods.",

		PayLine:        "\n- `%s` - %s",
		PayLineDefault: "\n- `%s` - %s (default)",

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

		HandlerUsage: "*Tabs*\n" +
			"- `@sergeant <@user> +AMOUNT [CCY]` - add to their tab\n" +
			"- `@sergeant <@user> AMOUNT [CCY]` - plus is optional!\n" +
			"- `@sergeant <@user> -AMOUNT [CCY]` - subtract from their tab\n" +
			"- `@sergeant <@user> reset [CCY]` - clear their tab (one currency or all)\n" +
			"- `@sergeant <@user> status` (or `?`) - what they owe you\n" +
			"- `@sergeant status` (or `?`) - everything you owe and are owed\n\n" +
			"*Payment methods*\n" +
			"- `@sergeant pay me` - show your saved methods\n" +
			"- `@sergeant pay set` - open a private form\n" +
			"- `@sergeant pay set METHOD VALUE` - save inline (e.g. `bank PL61 ...`)\n" +
			"- `@sergeant pay set-default METHOD` - mark a saved method as default\n" +
			"- `@sergeant pay rm METHOD` - remove one method\n" +
			"- `@sergeant pay clear` - remove all methods\n" +
			"- `@sergeant <@user> pay` - show someone else's methods\n\n" +
			"*Other*\n" +
			"- `@sergeant help` - ??? you are already seeing it!",
		HandlerSelfTarget: "You can't owe yourself.",
		HandlerError:      "Something went wrong.",
	},
	"pl": {
		LedgerNowOwes:             "<@%s> jest tobie winny %s %s.",
		LedgerYouOwe:              "Jesteś winny <@%s> %s %s.",
		LedgerTabCleared:          "Wyzerowano: <@%s> nie ma u Ciebie długu w %s.",
		LedgerResetAll:            "Wyzerowano rachunek <@%s>.",
		LedgerResetCurrency:       "Wyzerowano rachunek <@%s> w %s.",
		LedgerStatusForEmpty:      "<@%s> nie ma u Ciebie długu.",
		LedgerStatusFor:           "<@%s> ma u Ciebie %s długu.",
		LedgerStatusAllEmpty:      "Nikt nie ma u Ciebie długu i Ty też nie.",
		LedgerStatusAllOwedHeader: "Mają u Ciebie dług:",
		LedgerStatusAllOweHeader:  "Masz dług u:",
		LedgerStatusAllLine:       "\n- <@%s> - %s",

		LedgerSummaryOwedHeader: "Podsumowanie",
		LedgerSummaryPayHeader:  ":money_with_wings:",
		LedgerSummaryEmpty:      "Nikt nie ma u ciebie długu.",

		PaySaved:          "Zapisano `%s`.",
		PayRemoved:        "Usunięto `%s`.",
		PayCleared:        "Wyczyszczono wszystkie metody płatności.",
		PayDefaultSet:     "Ustawiono `%s` jako domyślną metodę płatności.",
		PayDefaultMissing: "Nie masz jeszcze zapisanej metody `%s` — najpierw dodaj ją przez `pay set %[1]s ...`.",
		PayShowSelfEmpty:  "Nie masz jeszcze żadnych metod płatności. Spróbuj `@sergeant pay set bank PL61 ...` lub `@sergeant pay set blik 555 555 555`.",
		PayShowForEmpty:   "<@%s> nie dodał jeszcze żadnych metod płatności.",

		PayLine:        "\n- `%s` - %s",
		PayLineDefault: "\n- `%s` - %s (default)",

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

		HandlerUsage: "*Rachunki*\n" +
			"- `@sergeant <@user> +KWOTA [WALUTA]` - dolicz do jego rachunku\n" +
			"- `@sergeant <@user> KWOTA [WALUTA]` - plus jest opcjonalny!\n" +
			"- `@sergeant <@user> -KWOTA [WALUTA]` - odejmij od jego rachunku\n" +
			"- `@sergeant <@user> reset [WALUTA]` - wyzeruj rachunek (jedna waluta lub wszystko)\n" +
			"- `@sergeant <@user> status` (lub `?`) - ile jest ci winny\n" +
			"- `@sergeant status` (lub `?`) - wszystko, co jesteś winny i co tobie są winni\n\n" +
			"*Metody płatności*\n" +
			"- `@sergeant pay me` - pokaż twoje zapisane metody\n" +
			"- `@sergeant pay set` - otwórz prywatny formularz\n" +
			"- `@sergeant pay set METODA WARTOŚĆ` - zapisz w wierszu (np. `bank PL61 ...`)\n" +
			"- `@sergeant pay set-default METODA` - oznacz metodę jako domyślną\n" +
			"- `@sergeant pay rm METODA` - usuń jedną metodę\n" +
			"- `@sergeant pay clear` - usuń wszystkie metody\n" +
			"- `@sergeant <@user> pay` - pokaż metody innej osoby\n\n" +
			"*Pozostałe*\n" +
			"- `@sergeant help` - ??? to co wlasnie widzisz!",
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
