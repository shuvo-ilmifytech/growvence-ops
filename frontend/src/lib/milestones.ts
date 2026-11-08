"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "./api";
import { parseAmount } from "./payments";

export type MilestoneStatus =
  | "scheduled"
  | "invoiced"
  | "partially_paid"
  | "paid"
  | "overdue";

export type Milestone = {
  id: string;
  project_id: string;
  label: string;
  amount: string;
  currency: string;
  due_date: string;
  status: MilestoneStatus;
  invoice_id: string | null;
  paid_amount: string;
  paid_at: string | null;
  payment_method: string | null;
  payment_reference: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  /** Multi-team milestone (Phase 5): the team working this payment. */
  team_id?: string | null;
  team_name?: string | null;
  /** SLA: expected response time in hours for this milestone. */
  sla_response_hours?: number | null;
  invoice_number: string | null;
  invoice_status: string | null;
  /** Team dashboard only. */
  project_name?: string | null;
  project_number?: number;
};

export type MilestoneSummary = {
  total: number;
  paid: number;
  pending: number;
  overdue_amount: number;
  overdue_count: number;
  count: number;
  project_value: number | null;
  project_name: string;
  project_currency: string;
};

export type MilestoneReminder = {
  id: string;
  milestone_id: string;
  sent_at: string;
  sent_by: string | null;
  channel: string;
  note: string;
  sent_by_name: string | null;
};

export type MilestoneInput = {
  label: string;
  amount: number;
  currency: string;
  due_date: string;
  team_id: string | null;
  sla_response_hours: number | null;
};

export type RecordPaymentInput = {
  amount: number;
  method: string;
  reference: string;
  paid_at: string;
  rate_to_bdt?: string;
  note: string;
};

export const milestoneAmount = (m: Milestone) => parseAmount(m.amount);
export const milestonePaid = (m: Milestone) => parseAmount(m.paid_amount);
export const milestoneRemaining = (m: Milestone) => milestoneAmount(m) - milestonePaid(m);

function invalidate(qc: ReturnType<typeof useQueryClient>, projectId?: string) {
  qc.invalidateQueries({ queryKey: ["milestones"] });
  if (projectId) qc.invalidateQueries({ queryKey: ["milestones", "project", projectId] });
}

export function useMilestones(projectId: string) {
  return useQuery({
    queryKey: ["milestones", "project", projectId],
    queryFn: () =>
      api<{ milestones: Milestone[]; summary: MilestoneSummary }>(
        `/projects/${projectId}/milestones`,
      ),
    enabled: !!projectId,
  });
}

export function useMilestone(id: string | null) {
  return useQuery({
    queryKey: ["milestones", "one", id],
    queryFn: () =>
      api<{ milestone: Milestone; reminders: MilestoneReminder[] }>(`/milestones/${id}`),
    enabled: !!id,
  });
}

export function useDueMilestones(days = 7) {
  return useQuery({
    queryKey: ["milestones", "due", days],
    queryFn: () => api<{ milestones: Milestone[] }>(`/milestones/due?days=${days}`),
  });
}

export function usePaymentMethods() {
  return useQuery({
    queryKey: ["payment-methods"],
    queryFn: () => api<{ methods: string[] }>("/payment-methods"),
    staleTime: 60_000,
  });
}

export function useCreateMilestone(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: MilestoneInput) =>
      api<{ id: string }>(`/projects/${projectId}/milestones`, { method: "POST", body: input }),
    onSuccess: () => {
      invalidate(qc, projectId);
      toast.success("Milestone added");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useUpdateMilestone(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: MilestoneInput }) =>
      api<{ ok: boolean }>(`/projects/${projectId}/milestones/${id}`, { method: "PUT", body: input }),
    onSuccess: () => {
      invalidate(qc, projectId);
      toast.success("Milestone updated");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useDeleteMilestone(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      api<{ ok: boolean }>(`/projects/${projectId}/milestones/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      invalidate(qc, projectId);
      toast.success("Milestone deleted");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useRecordMilestonePayment(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, input }: { id: string; input: RecordPaymentInput }) =>
      api<{ ok: boolean; status: string; paid_amount: number }>(
        `/projects/${projectId}/milestones/${id}/record-payment`,
        { method: "POST", body: input },
      ),
    onSuccess: (r) => {
      invalidate(qc, projectId);
      toast.success(r.status === "paid" ? "Milestone fully paid" : "Payment recorded");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useRemindMilestone(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, note }: { id: string; note: string }) =>
      api<{ ok: boolean }>(`/projects/${projectId}/milestones/${id}/remind`, {
        method: "POST",
        body: { note },
      }),
    onSuccess: () => {
      invalidate(qc, projectId);
      toast.success("Reminder sent to the client");
    },
    onError: (e: Error) => toast.error(e.message),
  });
}

export function useClientMilestones(projectId: string) {
  return useQuery({
    queryKey: ["client", "milestones", projectId],
    queryFn: () =>
      api<{ milestones: Milestone[] }>(`/client/projects/${projectId}/milestones`),
    enabled: !!projectId,
  });
}
