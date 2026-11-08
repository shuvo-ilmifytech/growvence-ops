"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "./api";
import { formatMoney } from "./format";
import { errorText } from "./hr";

// Finance is HR and the CEO's: the ledger, its categories and rates, and the monthly analytics.
// Every amount below is a decimal string; "_bdt" amounts are converted to BDT.

export const FINANCE_KEY = ["finance"];

export type FinanceKind = "income" | "expense";

export type FinanceCategory = { id: string; name: string; kind: FinanceKind; is_active: boolean; entries: number };

export type FinanceEntry = {
  id: string;
  category_id: string;
  category_name: string;
  kind: FinanceKind;
  amount: string;
  currency: string;
  rate_to_bdt: string;
  amount_bdt: string;
  entry_date: string;
  description: string;
  invoice_id: string | null; // set when the line came from an approved invoice
  invoice_number: number; // 0 when it didn't
  created_by_name: string;
  updated_by_name: string;
  created_at: string;
  updated_at: string;
};

export type FinanceRate = { id: string; currency: string; rate: string; effective_from: string; created_by_name: string; created_at: string };

export type FinanceMonth = {
  month: string; // YYYY-MM
  ledger_income: string;
  ledger_expense: string;
  project_earnings: string;
  payroll: string;
  total_income: string;
  total_expense: string;
  profit: string;
  entries: number;
  projects: number;
  paid_people: number;
  earnings_missing_rates: string[];
};

export type FinanceSummary = {
  from: string;
  to: string;
  current_month: string;
  months: FinanceMonth[];
  categories: { id: string; name: string; kind: FinanceKind; total: string; entries: number }[];
};

export type PayrollAdjustment = { kind: "bonus" | "deduction"; reason: string; amount: string };

export type PayrollPayment = {
  id: string;
  user_id: string;
  base_amount: string;
  currency: string;
  rate_to_bdt: string;
  net_amount: string;
  net_bdt: string;
  paid_on: string;
  note: string;
  updated_by_name: string;
  updated_at: string;
  adjustments: PayrollAdjustment[];
};

export type PayrollRow = {
  user_id: string;
  name: string;
  is_active: boolean;
  designation: string;
  department_name: string;
  salary_amount: string | null; // the fixed monthly salary
  salary_currency: string | null;
  payment: PayrollPayment | null; // null until recorded
  attendance: { working_days: number; absent: number; late: number; unpaid_leave_days: number } | null;
};

export type PayrollSheet = { month: string; today: string; rows: PayrollRow[] };

export type EntryFilter = { from: string; to: string; kind?: string; category_id?: string; q?: string };

// ── Expense invoices ─────────────────────────────────────────
// Every expense starts as an invoice: raised, approved by HR, then by the CEO, whose
// approval posts it to the ledger. Income is still entered straight into the ledger.

export type InvoiceStatus = "pending" | "awaiting_ceo" | "approved" | "rejected" | "cancelled";

/** One line of an invoice: "Office chair, 4 × 1,000 = 4,000", with its own category. */
export type InvoiceItem = {
  id: string;
  invoice_id: string;
  position: number;
  category_id: string;
  category_name: string;
  description: string;
  quantity: string;
  unit_price: string;
  amount: string;
};

export type Invoice = {
  id: string;
  number: number;
  amount: string; // the lines' total, in the invoice's currency
  currency: string;
  rate_to_bdt: string;
  amount_bdt: string;
  items: number; // how many lines
  items_list: InvoiceItem[];
  expense_date: string;
  description: string; // what the whole purchase is for
  status: InvoiceStatus;
  requested_by: string;
  requested_by_name: string;
  requested_by_role: string;
  hr_reviewed_by: string | null;
  hr_reviewed_by_name: string | null;
  hr_reviewed_at: string | null;
  hr_note: string;
  ceo_reviewed_by: string | null;
  ceo_reviewed_by_name: string | null;
  ceo_reviewed_at: string | null;
  ceo_note: string;
  ledger_lines: number; // ledger entries it posted: one per category, 0 until approved
  created_at: string;
  updated_at: string;
};

export type InvoiceListResult = {
  invoices: Invoice[];
  categories: FinanceCategory[]; // the expense categories an invoice may be filed under
  waiting: { pending_hr: number; awaiting_ceo: number; mine_open: number };
  today: string;
  can_review: boolean; // HR and the CEO, who also see everyone else's invoices
};

export type InvoiceFilter = { status?: string; from?: string; to?: string; q?: string; mine?: string };

/** "STV-0007": the voucher number, the same one printed on the paper voucher. */
export function invoiceRef(n: number) {
  return `STV-${String(n).padStart(4, "0")}`;
}

const ONES = [
  "", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine", "Ten",
  "Eleven", "Twelve", "Thirteen", "Fourteen", "Fifteen", "Sixteen", "Seventeen", "Eighteen", "Nineteen",
];
const TENS = ["", "", "Twenty", "Thirty", "Forty", "Fifty", "Sixty", "Seventy", "Eighty", "Ninety"];

function underThousand(n: number): string {
  if (n === 0) return "";
  if (n < 20) return ONES[n]!;
  if (n < 100) return TENS[Math.floor(n / 10)]! + (n % 10 ? " " + ONES[n % 10] : "");
  return ONES[Math.floor(n / 100)] + " Hundred" + (n % 100 ? " " + underThousand(n % 100) : "");
}

/**
 * An amount written out for the voucher's "In Words" line, in the crore/lakh grouping
 * used in Bangladesh: 150000 → "One Lakh Fifty Thousand Taka only".
 */
export function takaInWords(amount: string | number) {
  const n = Math.round(Number(amount) * 100) / 100;
  if (!Number.isFinite(n) || n < 0) return "—";
  const whole = Math.floor(n);
  const paisa = Math.round((n - whole) * 100);
  const parts: string[] = [];
  const units: [number, string][] = [
    [10000000, "Crore"],
    [100000, "Lakh"],
    [1000, "Thousand"],
  ];
  let left = whole;
  for (const [size, name] of units) {
    const count = Math.floor(left / size);
    if (count > 0) {
      parts.push(`${underThousand(count)} ${name}`);
      left -= count * size;
    }
  }
  if (left > 0) parts.push(underThousand(left));
  const words = parts.join(" ") || "Zero";
  return `${words} Taka${paisa ? ` and ${underThousand(paisa)} Paisa` : ""} only`;
}

export const INVOICE_STATUS: Record<InvoiceStatus, { label: string; waiting?: "hr" | "ceo" }> = {
  pending: { label: "Waiting for HR", waiting: "hr" },
  awaiting_ceo: { label: "Waiting for the CEO", waiting: "ceo" },
  approved: { label: "Approved" },
  rejected: { label: "Rejected" },
  cancelled: { label: "Withdrawn" },
};

/** One invoice with its lines — what the detail and printed copy are built from. */
export function useInvoice(id: string, enabled = true) {
  return useQuery({
    queryKey: [...FINANCE_KEY, "invoice", id],
    queryFn: () => api<Invoice>(`/finance/invoices/${id}`),
    enabled: enabled && !!id,
  });
}

/** "4 × 1,000" with trailing zeros trimmed off the quantity. */
export function itemQuantity(q: string) {
  const n = Number(q);
  return Number.isFinite(n) ? String(n) : q;
}

export function useInvoices(f: InvoiceFilter = {}, enabled = true) {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(f)) if (v) p.set(k, v);
  const qs = p.toString();
  return useQuery({
    queryKey: [...FINANCE_KEY, "invoices", qs],
    queryFn: () => api<InvoiceListResult>(`/finance/invoices${qs ? `?${qs}` : ""}`),
    placeholderData: (prev) => prev,
    enabled,
  });
}

export function entryQuery(f: EntryFilter) {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(f)) if (v) p.set(k, v);
  return p.toString();
}

export function useFinanceSummary(from: string, to: string, enabled = true) {
  return useQuery({
    queryKey: [...FINANCE_KEY, "summary", from, to],
    queryFn: () => api<FinanceSummary>(`/finance/summary?from=${from}&to=${to}`),
    placeholderData: (prev) => prev,
    enabled: enabled && !!from && !!to,
  });
}

export function useFinanceEntries(f: EntryFilter, enabled = true) {
  const qs = entryQuery(f);
  return useQuery({
    queryKey: [...FINANCE_KEY, "entries", qs],
    queryFn: () => api<{ entries: FinanceEntry[] }>(`/finance/entries?${qs}`).then((r) => r.entries),
    placeholderData: (prev) => prev,
    enabled: enabled && !!f.from && !!f.to,
  });
}

export function usePayroll(month: string, enabled = true) {
  return useQuery({
    queryKey: [...FINANCE_KEY, "payroll", month],
    queryFn: () => api<PayrollSheet>(`/finance/payroll?month=${month}`),
    placeholderData: (prev) => prev,
    enabled: enabled && /^\d{4}-\d{2}$/.test(month),
  });
}

/** A payroll line's total: bonuses added, deductions taken away. */
export function adjustmentTotal(lines: PayrollAdjustment[], kind: PayrollAdjustment["kind"]) {
  return lines.filter((a) => a.kind === kind).reduce((s, a) => s + (Number(a.amount.replace(/,/g, "")) || 0), 0);
}

export function useFinanceCategories(enabled = true) {
  return useQuery({
    queryKey: [...FINANCE_KEY, "categories"],
    queryFn: () => api<{ categories: FinanceCategory[] }>("/finance/categories").then((r) => r.categories),
    enabled,
  });
}

export function useFinanceRates(enabled = true) {
  return useQuery({
    queryKey: [...FINANCE_KEY, "rates"],
    queryFn: () => api<{ rates: FinanceRate[] }>("/finance/rates").then((r) => r.rates),
    enabled,
  });
}

/** The rate the server will use for a currency on a day ("" when none is set). */
export function useRateOn(currency: string, date: string) {
  const ok = /^[A-Z]{3}$/.test(currency) && currency !== "BDT" && /^\d{4}-\d{2}-\d{2}$/.test(date);
  return useQuery({
    queryKey: [...FINANCE_KEY, "rate-on", currency, date],
    queryFn: () => api<{ rate: string }>(`/finance/rates/on?currency=${currency}&date=${date}`).then((r) => r.rate),
    enabled: ok,
  });
}

export function useFinanceAction<T>(fn: (vars: T) => Promise<unknown>, success: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      toast.success(success);
      qc.invalidateQueries({ queryKey: FINANCE_KEY });
    },
    onError: (err) => toast.error(errorText(err)),
  });
}

const bdt = new Intl.NumberFormat("en-IN", { maximumFractionDigits: 0 });
const bdtCompact = new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 });
const bdtExact = new Intl.NumberFormat("en-IN", { maximumFractionDigits: 2 });

// "Tk" rather than "৳": the app font has no taka sign and many devices have no fallback for it.

/** "Tk 1,23,456" in the lakh grouping people in Bangladesh read; negatives as "−Tk …". */
export function formatBdt(amount: string | number) {
  const n = Number(amount);
  return `${n < 0 ? "−" : ""}Tk ${bdt.format(Math.abs(n))}`;
}

/**
 * An amount in its own currency, exact to the paisa: "Tk 1,000.50", "$50". Invoices use
 * this rather than formatBdt, because the lines on a printed invoice have to add up.
 */
export function formatAmount(amount: string | number, currency: string) {
  const n = Number(amount);
  if (!Number.isFinite(n)) return `${amount} ${currency}`;
  if (currency === "BDT") return `${n < 0 ? "−" : ""}Tk ${bdtExact.format(Math.abs(n))}`;
  return formatMoney(String(n), currency);
}

/** "Tk 1.2M" for axis labels. */
export function formatBdtShort(n: number) {
  return `${n < 0 ? "−" : ""}Tk ${bdtCompact.format(Math.abs(n))}`;
}

const monthFmt = new Intl.DateTimeFormat("en", { month: "short", year: "numeric" });
const monthShortFmt = new Intl.DateTimeFormat("en", { month: "short" });

/** "2026-10" → "Oct 2026" (or "Oct" when short). */
export function formatMonth(m: string, short = false) {
  const [y, mo] = m.split("-").map(Number);
  return (short ? monthShortFmt : monthFmt).format(new Date(y!, mo! - 1, 1));
}

/** "2026-10" shifted by n months. */
export function addMonths(m: string, n: number) {
  const [y, mo] = m.split("-").map(Number);
  const d = new Date(y!, mo! - 1 + n, 1);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

/** First and last day of a month, "2026-02" → ["2026-02-01", "2026-02-28"]. */
export function monthDays(m: string): [string, string] {
  const [y, mo] = m.split("-").map(Number);
  const last = new Date(y!, mo!, 0).getDate();
  return [`${m}-01`, `${m}-${String(last).padStart(2, "0")}`];
}
