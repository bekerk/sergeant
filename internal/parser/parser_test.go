package parser

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want Command
		bad  bool
	}{
		{in: "<@U1> +20 PLN", want: Command{Kind: KindAdd, Target: "U1", Sign: 1, Minor: 2000, Currency: "PLN"}},
		{in: "<@U1> 20 PLN", want: Command{Kind: KindAdd, Target: "U1", Sign: 1, Minor: 2000, Currency: "PLN"}},
		{in: "<@U1> 2137", want: Command{Kind: KindAdd, Target: "U1", Sign: 1, Minor: 213700}},
		{in: "<@U1> +20", want: Command{Kind: KindAdd, Target: "U1", Sign: 1, Minor: 2000}},
		{in: "<@U1> +20.50 EUR", want: Command{Kind: KindAdd, Target: "U1", Sign: 1, Minor: 2050, Currency: "EUR"}},
		{in: "<@U1> +0.01", want: Command{Kind: KindAdd, Target: "U1", Sign: 1, Minor: 1}},
		{in: "<@U1> -5 PLN", want: Command{Kind: KindAdd, Target: "U1", Sign: -1, Minor: 500, Currency: "PLN"}},
		{in: "<@U1|name> +1", want: Command{Kind: KindAdd, Target: "U1", Sign: 1, Minor: 100}},
		{in: "<@U1> reset", want: Command{Kind: KindReset, Target: "U1"}},
		{in: "<@U1> reset pln", want: Command{Kind: KindReset, Target: "U1", Currency: "PLN"}},
		{in: "<@U1> status", want: Command{Kind: KindStatusFor, Target: "U1"}},
		{in: "<@U1> ?", want: Command{Kind: KindStatusFor, Target: "U1"}},
		{in: "status", want: Command{Kind: KindStatusAll}},
		{in: "?", want: Command{Kind: KindStatusAll}},
		{in: "  ?  ", want: Command{Kind: KindStatusAll}},
		{in: "  STATUS  ", want: Command{Kind: KindStatusAll}},
		{in: "  <@U1>   +20   PLN  ", want: Command{Kind: KindAdd, Target: "U1", Sign: 1, Minor: 2000, Currency: "PLN"}},

		// Payment-method forms.
		{in: "pay me", want: Command{Kind: KindPayShowSelf}},
		{in: "  PAY  ME  ", want: Command{Kind: KindPayShowSelf}},
		{in: "pay set bank PL61 1090 0000 1234 5678", want: Command{Kind: KindPaySet, PayMethod: "bank", PayValue: "PL61 1090 0000 1234 5678"}},
		{in: "pay set BLIK 555 555 555", want: Command{Kind: KindPaySet, PayMethod: "blik", PayValue: "555 555 555"}},
		{in: "pay rm bank", want: Command{Kind: KindPayRemove, PayMethod: "bank"}},
		{in: "pay remove bank", want: Command{Kind: KindPayRemove, PayMethod: "bank"}},
		{in: "pay clear", want: Command{Kind: KindPayClear}},
		{in: "pay set", want: Command{Kind: KindPaySetForm}},
		{in: "<@U1> pay", want: Command{Kind: KindPayShowFor, Target: "U1"}},

		{in: "help", want: Command{Kind: KindHelp}},
		{in: "  HELP  ", want: Command{Kind: KindHelp}},

		{in: "hello", want: Command{Kind: KindHello}},
		{in: "  HELLO  ", want: Command{Kind: KindHello}},

		{in: "", bad: true},
		{in: "<@U1> +abc", bad: true},         // bad amount
		{in: "<@U1> +20 PLNS", bad: true},     // bad currency
		{in: "<@U1> +20.123", bad: true},      // too many fraction digits
		{in: "<@U1> pay 20", bad: true},       // pay takes no args after target
		{in: "pay set bank", bad: true},       // missing value
		{in: "pay set BAD! value", bad: true}, // invalid method chars
		{in: "pay rm", bad: true},
		{in: "pay clear extra", bad: true},
		{in: "pay nonsense", bad: true},
		{in: "pay", bad: true}, // bare pay no longer recognized
		{in: "pay me extra", bad: true},
		{in: "<@U1> status now", bad: true},
		{in: "<@U1> reset PLN extra", bad: true},
		{in: "<@U1> +99999999999999999999 PLN", bad: true}, // exceeds int64
		{in: "<@U1> +100000000000000000 PLN", bad: true},   // 1e17, would overflow on *100
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := Parse(tc.in)
			if tc.bad {
				if !errors.Is(err, ErrUnrecognized) {
					t.Fatalf("want ErrUnrecognized, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
