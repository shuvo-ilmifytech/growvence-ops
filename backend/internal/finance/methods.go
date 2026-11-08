// Package finance is the ledger, its categories and rates, payroll and
// expense invoices. PaymentMethods is the canonical list of ways money can
// arrive (milestone payments today; investor withdrawals and payouts later):
// one list, so every form and report spells a method the same way.
package finance

// PaymentMethods lists every accepted payment method, most-used first.
var PaymentMethods = []string{
	"PayPal",
	"Stripe",
	"Payoneer",
	"Xoom",
	"Wise",
	"Remitly",
	"Western Union",
	"MoneyGram",
	"Skrill",
	"Bank Transfer",
	"bKash",
	"Nagad",
	"Rocket",
	"Cash",
	"Crypto (USDT)",
	"Other",
}

// ValidPaymentMethod reports whether method is one of the accepted methods.
func ValidPaymentMethod(method string) bool {
	for _, m := range PaymentMethods {
		if m == method {
			return true
		}
	}
	return false
}
