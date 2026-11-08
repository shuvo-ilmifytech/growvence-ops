// Payment methods + money helpers shared by milestone payments (Phase 2),
// investor withdrawals and payouts later. Keep in sync with
// backend/internal/finance/methods.go.

export const PAYMENT_METHODS = [
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
] as const;

export type PaymentMethod = (typeof PAYMENT_METHODS)[number];

export function money(n: number, currency: string) {
  try {
    return new Intl.NumberFormat("en", { style: "currency", currency }).format(n);
  } catch {
    return `${n.toFixed(2)} ${currency}`;
  }
}

export function parseAmount(v: string | number | null | undefined): number {
  if (v === null || v === undefined || v === "") return 0;
  const n = typeof v === "number" ? v : parseFloat(v);
  return Number.isFinite(n) ? n : 0;
}
