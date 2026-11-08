"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircleIcon, SendIcon } from "lucide-react";
import { toast } from "sonner";
import { toLocalInput, formatMoney } from "@/lib/format";
import { api, ApiError } from "@/lib/api";
import { PRIORITIES, PRIORITY_LABELS, type Profile, type ProjectDetail, type ProjectType } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { InputGroup, InputGroupAddon, InputGroupInput, InputGroupText } from "@/components/ui/input-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";

type FormState = {
  name: string;
  profile_id: string;
  client_name: string;
  order_id: string;
  client_contact: string;
  project_type_id: string;
  scope: string;
  agreed_price: string;
  currency: string;
  start_date: string;
  deadline: string;
  priority: string;
  special_notes: string;
};

const LABELS: Record<keyof FormState, string> = {
  name: "Project name",
  profile_id: "Profile",
  client_name: "Client name",
  order_id: "Order ID",
  client_contact: "Client contact",
  project_type_id: "Project type",
  scope: "Scope & requirements",
  agreed_price: "Agreed price",
  currency: "Currency",
  start_date: "Start date",
  deadline: "Delivery deadline",
  priority: "Priority",
  special_notes: "Special notes",
};

const REQUIRED = new Set<keyof FormState>([
  "name", "profile_id", "client_name", "order_id", "project_type_id", "scope", "agreed_price", "deadline",
]);

const CURRENCIES = ["USD", "EUR", "GBP", "BDT", "AUD", "CAD", "INR"];

function fromProject(d?: ProjectDetail): FormState {
  const p = d?.project;
  return {
    name: p?.name ?? "",
    profile_id: p?.profile_id ?? "",
    client_name: p?.client_name ?? "",
    order_id: p?.order_id ?? "",
    client_contact: p?.client_contact ?? "",
    project_type_id: p?.project_type_id ?? "",
    scope: p?.scope ?? "",
    agreed_price: p?.agreed_price ?? "",
    currency: p?.currency ?? "USD",
    start_date: p?.start_date ?? "",
    deadline: toLocalInput(p?.deadline),
    priority: p?.priority ?? "medium",
    special_notes: p?.special_notes ?? "",
  };
}

export function ProjectForm({ initial }: { initial?: ProjectDetail }) {
  const router = useRouter();
  const qc = useQueryClient();
  const [form, setForm] = useState<FormState>(() => fromProject(initial));
  const [errors, setErrors] = useState<Partial<Record<keyof FormState, string>>>({});
  const [banner, setBanner] = useState("");
  const [pending, setPending] = useState(false);
  const projectId = initial?.project.id;
  const [lockVersion, setLockVersion] = useState(initial?.project.lock_version);

  const canSetPriority = !initial || initial.permissions.can_set_priority;

  const profiles = useQuery({
    queryKey: ["profiles"],
    queryFn: () => api<{ profiles: Profile[] }>("/profiles").then((r) => r.profiles),
  });
  const types = useQuery({
    queryKey: ["project-types"],
    queryFn: () => api<{ project_types: ProjectType[] }>("/project-types").then((r) => r.project_types),
  });

  // Keep a deactivated profile/type selectable on an existing project.
  const profileOptions = withCurrent(profiles.data, initial?.project.profile_id, initial?.project.profile_name);
  // The marketplace's share is automatic: the chosen profile's, or what the project already records.
  const selectedProfile = profiles.data?.find((x) => x.id === form.profile_id);
  const feePercent =
    selectedProfile && selectedProfile.id !== initial?.project.profile_id
      ? Number(selectedProfile.fee_percent)
      : Number(initial?.project.platform_fee_percent ?? selectedProfile?.fee_percent ?? 20);
  const typeOptions = withCurrent(types.data, initial?.project.project_type_id, initial?.project.project_type_name);

  const set = (k: keyof FormState) => (v: string) => {
    setForm((f) => ({ ...f, [k]: v }));
    if (errors[k]) setErrors((e) => ({ ...e, [k]: undefined }));
  };

  function payload() {
    const body: Record<string, unknown> = { ...form };
    // The picker shows the viewer's own clock; send the exact moment so it means the same everywhere.
    if (form.deadline) {
      const at = new Date(form.deadline);
      if (!Number.isNaN(at.getTime())) body.deadline = at.toISOString();
    }
    if (!canSetPriority) delete body.priority;
    if (lockVersion !== undefined && projectId) body.lock_version = lockVersion;
    return body;
  }

  function showError(err: unknown) {
    if (err instanceof ApiError && Object.keys(err.fields).length) {
      const next: typeof errors = {};
      for (const [k, msg] of Object.entries(err.fields)) {
        if (k in LABELS) next[k as keyof FormState] = `${LABELS[k as keyof FormState]} ${msg}`;
      }
      setErrors(next);
      setBanner("Please fill in or fix the highlighted fields.");
    } else {
      setBanner(err instanceof Error ? err.message : "Something went wrong");
    }
  }

  // A new project goes straight to its type's team; there are no drafts.
  async function save() {
    setPending(true);
    setErrors({});
    setBanner("");
    try {
      const detail = projectId
        ? await api<ProjectDetail>(`/projects/${projectId}`, { method: "PATCH", body: payload() })
        : await api<ProjectDetail>("/projects", { method: "POST", body: payload() });
      const id = detail.project.id;
      setLockVersion(detail.project.lock_version);
      qc.setQueryData(["projects", "detail", id], detail);
      qc.invalidateQueries({ queryKey: ["projects", "list"] });
      toast.success(projectId ? "Changes saved" : "Project created and sent to the team");
      router.push(`/projects/${id}`);
    } catch (err) {
      showError(err);
    } finally {
      setPending(false);
    }
  }

  const fieldProps = (k: keyof FormState) => ({
    "data-invalid": !!errors[k] || undefined,
  });
  const label = (k: keyof FormState) => (
    <FieldLabel htmlFor={k}>
      {LABELS[k]}
      {REQUIRED.has(k) && <span className="text-primary" aria-hidden>*</span>}
    </FieldLabel>
  );

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
      className="space-y-6 pb-24"
    >
      {banner && (
        <div role="alert" className="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive">
          <AlertCircleIcon className="mt-0.5 size-4 shrink-0" />
          {banner}
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Client & order</CardTitle>
          <CardDescription>Where the order came from and who it&apos;s for.</CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup className="grid grid-cols-1 gap-5 md:grid-cols-2">
            <Field {...fieldProps("profile_id")}>
              {label("profile_id")}
              <Select value={form.profile_id} onValueChange={set("profile_id")}>
                <SelectTrigger id="profile_id" className="w-full" aria-invalid={!!errors.profile_id}>
                  <SelectValue placeholder="Choose a marketplace profile" />
                </SelectTrigger>
                <SelectContent>
                  {profileOptions.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name}
                      {"platform" in p && p.platform ? <span className="text-muted-foreground"> · {p.platform}</span> : null}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldError>{errors.profile_id}</FieldError>
            </Field>
            <Field {...fieldProps("order_id")}>
              {label("order_id")}
              <Input id="order_id" value={form.order_id} onChange={(e) => set("order_id")(e.target.value)} placeholder="e.g. FO72A1B9C" aria-invalid={!!errors.order_id} />
              <FieldError>{errors.order_id}</FieldError>
            </Field>
            <Field {...fieldProps("client_name")}>
              {label("client_name")}
              <Input id="client_name" value={form.client_name} onChange={(e) => set("client_name")(e.target.value)} aria-invalid={!!errors.client_name} />
              <FieldError>{errors.client_name}</FieldError>
            </Field>
            <Field {...fieldProps("client_contact")}>
              {label("client_contact")}
              <Input id="client_contact" value={form.client_contact} onChange={(e) => set("client_contact")(e.target.value)} placeholder="Username, email or phone" />
              <FieldError>{errors.client_contact}</FieldError>
            </Field>
          </FieldGroup>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Project</CardTitle>
          <CardDescription>What needs to be delivered.</CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <div className="grid grid-cols-1 gap-5 md:grid-cols-2">
              <Field {...fieldProps("name")}>
                {label("name")}
                <Input id="name" value={form.name} onChange={(e) => set("name")(e.target.value)} placeholder="e.g. Acme Shopify store" aria-invalid={!!errors.name} />
                <FieldError>{errors.name}</FieldError>
              </Field>
              <Field {...fieldProps("project_type_id")}>
                {label("project_type_id")}
                <Select value={form.project_type_id} onValueChange={set("project_type_id")}>
                  <SelectTrigger id="project_type_id" className="w-full" aria-invalid={!!errors.project_type_id}>
                    <SelectValue placeholder="Choose a type" />
                  </SelectTrigger>
                  <SelectContent>
                    {typeOptions.map((t) => (
                      <SelectItem key={t.id} value={t.id}>
                        {t.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FieldError>{errors.project_type_id}</FieldError>
              </Field>
            </div>
            <Field {...fieldProps("scope")}>
              {label("scope")}
              <Textarea
                id="scope"
                value={form.scope}
                onChange={(e) => set("scope")(e.target.value)}
                rows={7}
                placeholder="Pages, features, integrations, references, anything the team needs to know."
                aria-invalid={!!errors.scope}
              />
              <FieldError>{errors.scope}</FieldError>
            </Field>
          </FieldGroup>
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Commercials</CardTitle>
          </CardHeader>
          <CardContent>
            <FieldGroup>
              <Field {...fieldProps("currency")}>
                {label("currency")}
                <Select value={form.currency} onValueChange={set("currency")}>
                  <SelectTrigger id="currency" className="w-32">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {Array.from(new Set([form.currency, ...CURRENCIES])).map((c) => (
                      <SelectItem key={c} value={c}>
                        {c}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
              <Field {...fieldProps("agreed_price")}>
                {label("agreed_price")}
                <InputGroup>
                  <InputGroupInput
                    id="agreed_price"
                    inputMode="decimal"
                    value={form.agreed_price}
                    onChange={(e) => set("agreed_price")(e.target.value)}
                    placeholder="0.00"
                    aria-invalid={!!errors.agreed_price}
                  />
                  <InputGroupAddon align="inline-end">
                    <InputGroupText>{form.currency}</InputGroupText>
                  </InputGroupAddon>
                </InputGroup>
                <FeePreview
                  price={form.agreed_price}
                  currency={form.currency}
                  percent={feePercent}
                  platform={selectedProfile?.platform || "The marketplace"}
                />
                <FieldError>{errors.agreed_price}</FieldError>
              </Field>
            </FieldGroup>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Schedule & priority</CardTitle>
          </CardHeader>
          <CardContent>
            <FieldGroup>
              <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                {(["start_date", "deadline"] as const).map((k) => (
                  <Field key={k} {...fieldProps(k)}>
                    {label(k)}
                    {/* The delivery deadline is a moment: marketplace orders are due at a time. */}
                    <Input
                      id={k}
                      type={k === "deadline" ? "datetime-local" : "date"}
                      value={form[k]}
                      onChange={(e) => set(k)(e.target.value)}
                      aria-invalid={!!errors[k]}
                    />
                    <FieldError>{errors[k]}</FieldError>
                  </Field>
                ))}
              </div>
              <Field {...fieldProps("priority")}>
                {label("priority")}
                <Select value={form.priority} onValueChange={set("priority")} disabled={!canSetPriority}>
                  <SelectTrigger id="priority" className="w-40">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {PRIORITIES.map((p) => (
                      <SelectItem key={p} value={p}>
                        {PRIORITY_LABELS[p]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FieldDescription>
                  {canSetPriority ? "Your suggestion. The Team Leader confirms it when assigning." : "Set by the Team Leader once the project is assigned."}
                </FieldDescription>
                <FieldError>{errors.priority}</FieldError>
              </Field>
            </FieldGroup>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Special notes</CardTitle>
          <CardDescription>Client preferences, access details, anything unusual.</CardDescription>
        </CardHeader>
        <CardContent>
          <Field {...fieldProps("special_notes")}>
            <Textarea id="special_notes" value={form.special_notes} onChange={(e) => set("special_notes")(e.target.value)} rows={4} />
            <FieldError>{errors.special_notes}</FieldError>
          </Field>
        </CardContent>
      </Card>

      <div className="fixed inset-x-0 bottom-0 z-10 border-t bg-background/90 backdrop-blur md:left-(--sidebar-width) peer-data-[state=collapsed]:md:left-(--sidebar-width-icon)">
        <div className="mx-auto flex max-w-6xl items-center justify-between gap-3 px-4 py-3 md:px-8">
          <p className="hidden text-xs text-muted-foreground sm:block">
            <span className="text-primary">*</span> Required
          </p>
          <div className="ml-auto flex gap-2">
            <Button type="button" variant="ghost" asChild>
              <Link href={projectId ? `/projects/${projectId}` : "/projects"}>Cancel</Link>
            </Button>
            <Button type="submit" disabled={pending}>
              {pending ? <Spinner /> : projectId ? null : <SendIcon />}
              {projectId ? "Save changes" : "Create project"}
            </Button>
          </div>
        </div>
      </div>
    </form>
  );
}

function withCurrent<T extends { id: string; name: string }>(list: T[] | undefined, id?: string | null, name?: string | null) {
  const items: { id: string; name: string; platform?: string }[] = list ? [...list] : [];
  if (id && name && !items.some((i) => i.id === id)) items.push({ id, name: `${name} (inactive)` });
  return items;
}

/** What the marketplace keeps of the agreed price and what reaches Growvence, worked out as you type. */
function FeePreview({ price, currency, percent, platform }: { price: string; currency: string; percent: number; platform: string }) {
  const amount = Number(price);
  if (!price.trim() || !Number.isFinite(amount) || amount <= 0) {
    return (
      <FieldDescription>
        {platform} keeps {percent}%; what you receive is worked out automatically.
      </FieldDescription>
    );
  }
  const fee = Math.round(amount * percent) / 100;
  return (
    <FieldDescription>
      {platform} keeps {percent}% ({formatMoney(fee.toFixed(2), currency)}) · You receive{" "}
      <span className="font-medium text-foreground">{formatMoney((amount - fee).toFixed(2), currency)}</span>
    </FieldDescription>
  );
}
