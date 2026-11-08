"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "./api";

export type InvoiceItem = {
  description: string;
  quantity: number;
  unit_price: number;
};

export type Invoice = {
  id: string;
  invoice_number: string;
  project_id: string | null;
  project_name: string | null;
  client_name: string;
  client_email: string;
  items: InvoiceItem[];
  subtotal: number;
  tax_rate: number;
  tax_amount: number;
  total: number;
  currency: string;
  status: "draft" | "sent" | "paid" | "overdue";
  due_date: string | null;
  notes: string;
  created_by: string | null;
  created_by_name: string | null;
  created_at: string;
  updated_at: string;
};

export type InvoiceInput = {
  project_id?: string;
  client_name: string;
  client_email: string;
  items: InvoiceItem[];
  tax_rate: number;
  currency: string;
  due_date?: string;
  notes: string;
};

const LIST_KEY = ["invoices", "list"];

export function useInvoices(status?: string) {
  const params = new URLSearchParams();
  if (status) params.set("status", status);
  const qs = params.toString();
  return useQuery({
    queryKey: [...LIST_KEY, status ?? "all"],
    queryFn: () => api<{ invoices: Invoice[] }>(`/billing${qs ? `?${qs}` : ""}`),
  });
}

export function useInvoice(id: string | null) {
  return useQuery({
    queryKey: ["invoices", "one", id],
    queryFn: () => api<Invoice>(`/billing/${id}`),
    enabled: !!id,
  });
}

function invalidate(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ["invoices"] });
}

export function useCreateInvoice() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: InvoiceInput) => api<{ id: string; invoice_number: string }>("/billing", { method: "POST", body: input }),
    onSuccess: (r) => {
      invalidate(qc);
      toast.success(`Invoice ${r.invoice_number} created`);
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useUpdateInvoice() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: InvoiceInput }) =>
      api<{ ok: boolean }>(`/billing/${id}`, { method: "PUT", body: input }),
    onSuccess: () => {
      invalidate(qc);
      toast.success("Invoice updated");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useDeleteInvoice() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api<{ ok: boolean }>(`/billing/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      invalidate(qc);
      toast.success("Invoice deleted");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useSendInvoice() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api<{ ok: boolean }>(`/billing/${id}/send`, { method: "POST" }),
    onSuccess: () => {
      invalidate(qc);
      toast.success("Invoice marked as sent");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useMarkInvoicePaid() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api<{ ok: boolean }>(`/billing/${id}/mark-paid`, { method: "POST" }),
    onSuccess: () => {
      invalidate(qc);
      toast.success("Invoice marked as paid");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}
