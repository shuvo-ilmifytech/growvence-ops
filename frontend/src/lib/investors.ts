"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "./api";
import { parseAmount } from "./payments";

export type InvestorStatus = "active" | "inactive";

export type Investor = {
  id: string;
  user_id: string | null;
  name: string;
  email: string | null;
  phone: string | null;
  join_date: string;
  status: InvestorStatus;
  notes: string;
  created_at: string;
  updated_at: string;
  invested_bdt: string;
  earned_bdt: string;
  withdrawn_bdt: string;
  pending_withdrawals_bdt: string;
  share_percent: string;
};

export type Investment = {
  id: string;
  investor_id: string;
  amount: string;
  currency: string;
  rate_to_bdt: string;
  amount_bdt: string;
  method: string | null;
  invested_at: string;
  note: string;
  created_by: string | null;
  created_at: string;
};

export type Distribution = {
  id: string;
  source_type: string;
  source_id: string | null;
  investor_id: string | null;
  amount: string;
  currency: string;
  rate_to_bdt: string;
  amount_bdt: string;
  share_percent: string;
  distributed_at: string;
  note: string;
};

export type WithdrawalStatus = "pending" | "approved" | "paid" | "rejected";

export type Withdrawal = {
  id: string;
  investor_id: string;
  investor_name: string;
  amount: string;
  currency: string;
  rate_to_bdt: string;
  amount_bdt: string;
  method: string;
  status: WithdrawalStatus;
  requested_at: string;
  requested_by: string | null;
  reviewed_by: string | null;
  reviewed_at: string | null;
  paid_at: string | null;
  note: string;
  created_at: string;
  updated_at: string;
};

export type ShareRow = {
  id: string;
  investor_id: string;
  share_percent: string;
  effective_from: string;
  created_by_name: string | null;
  created_at: string;
};

export type InvestorDetail = {
  investor: Investor;
  investments: Investment[];
  distributions: Distribution[];
  withdrawals: Withdrawal[];
  shares: ShareRow[];
};

export type ExpenseCategory = "Tools" | "Software" | "Office" | "Marketing" | "Other";

export type InvestorExpense = {
  id: string;
  category: ExpenseCategory;
  description: string;
  amount: string;
  currency: string;
  rate_to_bdt: string;
  amount_bdt: string;
  expense_date: string;
  receipt_url: string | null;
  added_by_name: string | null;
  created_at: string;
};

export type ExpenseTotal = { category: string; total_bdt: string; count: number };

export type CompanyTotals = {
  total_income_bdt: string;
  total_expenses_bdt: string;
  total_distributed_bdt: string;
  reserve_bdt: string;
  total_withdrawn_bdt: string;
};

export type CompanyAccount = {
  totals: CompanyTotals;
  investors: Investor[];
  expense_totals: ExpenseTotal[];
  net_profit_bdt: number;
  balance_bdt: number;
};

export type CurrencyRate = {
  id: string;
  currency: string;
  rate: string;
  effective_from: string;
  created_by_name: string;
  created_at: string;
};

export type MonthlyEarning = { month: string; earned_bdt: string };

export type InvestorMe = {
  investor: Investor;
  investments: Investment[];
  distributions: Distribution[];
  by_month: MonthlyEarning[];
  withdrawals: Withdrawal[];
  reserve_bdt: string;
};

export const EXPENSE_CATEGORIES: ExpenseCategory[] = ["Tools", "Software", "Office", "Marketing", "Other"];

/** Earned minus paid out. Pending/approved withdrawals don't reduce it yet. */
export const investorBalance = (inv: Investor) =>
  parseAmount(inv.earned_bdt) - parseAmount(inv.withdrawn_bdt);

/** What can still be requested: balance minus withdrawals already in flight. */
export const investorAvailable = (inv: Investor) =>
  investorBalance(inv) - parseAmount(inv.pending_withdrawals_bdt);

function invalidateAll(qc: ReturnType<typeof useQueryClient>, investorId?: string) {
  qc.invalidateQueries({ queryKey: ["investors"] });
  qc.invalidateQueries({ queryKey: ["company-account"] });
  qc.invalidateQueries({ queryKey: ["investor-expenses"] });
  qc.invalidateQueries({ queryKey: ["withdrawals"] });
  qc.invalidateQueries({ queryKey: ["investor-me"] });
  if (investorId) qc.invalidateQueries({ queryKey: ["investors", investorId] });
}

export function useInvestors() {
  return useQuery({
    queryKey: ["investors"],
    queryFn: () =>
      api<{ investors: Investor[]; reserve_percent: number; total_shares: number }>("/investors"),
  });
}

export function useInvestor(id: string) {
  return useQuery({
    queryKey: ["investors", id],
    queryFn: () => api<InvestorDetail>(`/investors/${id}`),
    enabled: !!id,
  });
}

export function useCompanyAccount() {
  return useQuery({
    queryKey: ["company-account"],
    queryFn: () => api<CompanyAccount>("/company-account"),
  });
}

export function useInvestorExpenses(category = "") {
  return useQuery({
    queryKey: ["investor-expenses", category],
    queryFn: () =>
      api<{ expenses: InvestorExpense[]; totals: ExpenseTotal[] }>(
        `/investor-expenses${category ? `?category=${category}` : ""}`,
      ),
  });
}

export function useWithdrawals(status = "") {
  return useQuery({
    queryKey: ["withdrawals", status],
    queryFn: () =>
      api<{ withdrawals: Withdrawal[] }>(`/withdrawals${status ? `?status=${status}` : ""}`),
  });
}

export function useCurrencyRates() {
  return useQuery({
    queryKey: ["currency-rates"],
    queryFn: () => api<{ rates: CurrencyRate[] }>("/currency-rates"),
  });
}

export function useLatestRate(currency: string) {
  return useQuery({
    queryKey: ["currency-rates", "latest", currency],
    queryFn: () => api<{ currency: string; rate: string }>(`/currency-rates/latest?currency=${currency}`),
    enabled: !!currency && currency !== "BDT",
    staleTime: 60_000,
  });
}

export function useInvestorMe() {
  return useQuery({
    queryKey: ["investor-me"],
    queryFn: () => api<InvestorMe>("/investor/me"),
  });
}

export type InvestorInput = {
  name: string;
  email: string;
  phone: string;
  join_date: string;
  notes: string;
  user_id: string;
};

export function useCreateInvestor() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: InvestorInput) =>
      api<{ id: string }>("/investors", { method: "POST", body: input }),
    onSuccess: () => {
      invalidateAll(qc);
      toast.success("Investor added");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useUpdateInvestor(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: InvestorInput & { status: InvestorStatus }) =>
      api<{ ok: boolean }>(`/investors/${id}`, { method: "PUT", body: input }),
    onSuccess: () => {
      invalidateAll(qc, id);
      toast.success("Investor updated");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export type InvestmentInput = {
  amount: number;
  currency: string;
  rate_to_bdt?: string;
  method: string;
  invested_at: string;
  note: string;
};

export function useRecordInvestment(investorId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: InvestmentInput) =>
      api<{ id: string }>(`/investors/${investorId}/investments`, { method: "POST", body: input }),
    onSuccess: () => {
      invalidateAll(qc, investorId);
      toast.success("Investment recorded");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useSetShare(investorId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { share_percent: number; effective_from: string }) =>
      api<{ id: string }>(`/investors/${investorId}/share`, { method: "PUT", body: input }),
    onSuccess: () => {
      invalidateAll(qc, investorId);
      toast.success("Share updated");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useUpdateReserve() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (reserve_percent: number) =>
      api<{ ok: boolean }>("/profit-settings", { method: "PUT", body: { reserve_percent } }),
    onSuccess: () => {
      invalidateAll(qc);
      toast.success("Reserve percent updated");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export type DistributeInput = {
  source_type: string;
  source_id?: string;
  amount: number;
  currency: string;
  rate_to_bdt?: string;
  note: string;
};

export function useDistribute(investorId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: DistributeInput) =>
      api<{ ok: boolean; rows_created: number }>(`/investors/${investorId}/distribute`, {
        method: "POST",
        body: input,
      }),
    onSuccess: (r) => {
      invalidateAll(qc, investorId);
      toast.success(`Distributed to ${r.rows_created} ${r.rows_created === 1 ? "row" : "rows"}`);
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export type WithdrawalInput = {
  amount: number;
  currency: string;
  rate_to_bdt?: string;
  method: string;
  note: string;
};

export function useRequestWithdrawal(investorId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: WithdrawalInput) =>
      api<{ id: string }>(`/investors/${investorId}/withdrawals`, { method: "POST", body: input }),
    onSuccess: () => {
      invalidateAll(qc, investorId);
      toast.success("Withdrawal requested");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

function useReviewWithdrawal(investorId: string | undefined, action: "approve" | "reject" | "mark-paid", label: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      api<{ ok: boolean }>(`/withdrawals/${id}/${action}`, { method: "PUT" }),
    onSuccess: () => {
      invalidateAll(qc, investorId);
      toast.success(label);
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export const useApproveWithdrawal = (investorId?: string) =>
  useReviewWithdrawal(investorId, "approve", "Withdrawal approved");
export const useRejectWithdrawal = (investorId?: string) =>
  useReviewWithdrawal(investorId, "reject", "Withdrawal rejected");
export const useMarkWithdrawalPaid = (investorId?: string) =>
  useReviewWithdrawal(investorId, "mark-paid", "Withdrawal marked as paid");

export type ExpenseInput = {
  category: ExpenseCategory;
  description: string;
  amount: number;
  currency: string;
  rate_to_bdt?: string;
  expense_date: string;
  receipt_url: string;
};

export function useCreateExpense() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ExpenseInput) =>
      api<{ id: string }>("/investor-expenses", { method: "POST", body: input }),
    onSuccess: () => {
      invalidateAll(qc);
      toast.success("Expense added");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useDeleteExpense() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api<{ ok: boolean }>(`/investor-expenses/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      invalidateAll(qc);
      toast.success("Expense deleted");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useCreateRate() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { currency: string; rate: number; effective_from: string }) =>
      api<{ id: string }>("/currency-rates", { method: "POST", body: input }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["currency-rates"] });
      toast.success("Rate saved");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}
