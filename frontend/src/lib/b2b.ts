"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "./api";
import { parseAmount } from "./payments";

// B2B project features (Phase 5): contracts, retainers and the B2B report.

export type ContractStatus = "draft" | "active" | "completed" | "terminated" | "expired";

export type Contract = {
  id: string;
  contract_number: string;
  client_id: string;
  project_id: string | null;
  title: string;
  value: string;
  currency: string;
  start_date: string;
  end_date: string | null;
  terms: string;
  document_url: string | null;
  status: ContractStatus;
  signed_at: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  client_name: string;
  client_code: string;
  project_name: string | null;
};

export type ContractInput = {
  client_id: string;
  project_id: string;
  title: string;
  value: number;
  currency: string;
  start_date: string;
  end_date: string;
  terms: string;
  document_url: string;
  status: ContractStatus;
};

export type ExpiringContract = {
  id: string;
  contract_number: string;
  client_id: string;
  title: string;
  value: string;
  currency: string;
  start_date: string;
  end_date: string | null;
  status: ContractStatus;
  client_name: string;
};

export type RetainerStatus = "active" | "paused" | "cancelled";

export type Retainer = {
  id: string;
  client_id: string;
  title: string;
  monthly_fee: string;
  currency: string;
  start_date: string;
  billing_day: number;
  status: RetainerStatus;
  auto_renew: boolean;
  next_billing_date: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  client_name: string;
  client_code: string;
  client_email?: string | null;
};

export type RetainerInput = {
  client_id: string;
  title: string;
  monthly_fee: number;
  currency: string;
  start_date: string;
  billing_day: number;
  auto_renew: boolean;
  status: RetainerStatus;
};

export type B2BRevenueRow = {
  client_id: string;
  client_code: string;
  company_name: string;
  revenue_bdt: number;
  paid_count: number;
};

export type B2BOutstandingRow = {
  client_id: string;
  client_code: string;
  company_name: string;
  outstanding_bdt: number;
  open_count: number;
};

export type B2BProfitRow = {
  client_id: string;
  client_code: string;
  company_name: string;
  agreed_bdt: number;
  received_bdt: number;
  outstanding_bdt: number;
};

export type B2BHealthRow = {
  client_id: string;
  client_code: string;
  company_name: string;
  score: number;
  on_time: number;
  response_rate: number;
  active_score: number;
  paid_total: number;
  overdue_count: number;
  approvals_asked: number;
  active_projects: number;
};

export type B2BReport = {
  revenue_by_client: B2BRevenueRow[];
  outstanding: B2BOutstandingRow[];
  profitability: B2BProfitRow[];
  health: B2BHealthRow[];
  retainers_due: Retainer[];
  contracts_expiring: ExpiringContract[];
};

export const contractValue = (c: Contract) => parseAmount(c.value);
export const retainerFee = (r: Retainer) => parseAmount(r.monthly_fee);

export function useContracts(status = "") {
  return useQuery({
    queryKey: ["contracts", status],
    queryFn: () =>
      api<{ contracts: Contract[] }>(`/contracts${status ? `?status=${status}` : ""}`),
  });
}

export function useContract(id: string | null) {
  return useQuery({
    queryKey: ["contracts", "one", id],
    queryFn: () => api<{ contract: Contract }>(`/contracts/${id}`),
    enabled: !!id,
  });
}

function invalidateContracts(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ["contracts"] });
  qc.invalidateQueries({ queryKey: ["b2b-report"] });
}

export function useCreateContract() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ContractInput) =>
      api<{ id: string; contract_number: string }>("/contracts", { method: "POST", body: input }),
    onSuccess: (r) => {
      invalidateContracts(qc);
      toast.success(`Contract ${r.contract_number} created`);
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useUpdateContract() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: ContractInput }) =>
      api<{ ok: boolean }>(`/contracts/${id}`, { method: "PUT", body: input }),
    onSuccess: () => {
      invalidateContracts(qc);
      toast.success("Contract updated");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useRenewContract() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, start_date, end_date }: { id: string; start_date: string; end_date: string }) =>
      api<{ id: string; contract_number: string }>(`/contracts/${id}/renew`, {
        method: "POST",
        body: { start_date, end_date },
      }),
    onSuccess: (r) => {
      invalidateContracts(qc);
      toast.success(`Renewed as ${r.contract_number}`);
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useRetainers(status = "") {
  return useQuery({
    queryKey: ["retainers", status],
    queryFn: () =>
      api<{ retainers: Retainer[] }>(`/retainers${status ? `?status=${status}` : ""}`),
  });
}

export function useDueRetainers() {
  return useQuery({
    queryKey: ["retainers", "due"],
    queryFn: () => api<{ retainers: Retainer[] }>("/retainers/due"),
  });
}

function invalidateRetainers(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ["retainers"] });
  qc.invalidateQueries({ queryKey: ["b2b-report"] });
}

export function useCreateRetainer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: RetainerInput) =>
      api<{ id: string; next_billing_date: string }>("/retainers", { method: "POST", body: input }),
    onSuccess: (r) => {
      invalidateRetainers(qc);
      toast.success(`Retainer created — first billing ${r.next_billing_date}`);
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useUpdateRetainer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: RetainerInput }) =>
      api<{ ok: boolean }>(`/retainers/${id}`, { method: "PUT", body: input }),
    onSuccess: () => {
      invalidateRetainers(qc);
      toast.success("Retainer updated");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useB2BReport() {
  return useQuery({
    queryKey: ["b2b-report"],
    queryFn: () => api<B2BReport>("/b2b/report"),
  });
}
