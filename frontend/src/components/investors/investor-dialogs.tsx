"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { money, parseAmount } from "@/lib/payments";
import { usePaymentMethods } from "@/lib/milestones";
import {
  EXPENSE_CATEGORIES,
  investorAvailable,
  useCreateExpense,
  useCreateInvestor,
  useCreateRate,
  useDistribute,
  useLatestRate,
  useRecordInvestment,
  useRequestWithdrawal,
  useSetShare,
  useUpdateInvestor,
  useUpdateReserve,
  type ExpenseCategory,
  type Investor,
} from "@/lib/investors";

function today() {
  return new Date().toISOString().slice(0, 10);
}

function Shell({
  title,
  description,
  onClose,
  children,
}: {
  title: string;
  description: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        {children}
      </DialogContent>
    </Dialog>
  );
}

function RateHint({ currency, date, onUse }: { currency: string; date: string; onUse: (r: string) => void }) {
  const cur = currency.trim().toUpperCase();
  const latest = useLatestRate(cur);
  if (cur === "BDT" || cur.length !== 3) return null;
  const set = latest.data?.rate;
  return (
    <p className="text-xs text-muted-foreground">
      {set ? (
        <>
          Latest {cur} rate: {set} BDT.{" "}
          <button type="button" className="underline" onClick={() => onUse(set)}>
            Use it
          </button>
        </>
      ) : (
        `No ${cur} rate is set for ${date || "today"} — enter the rate you received.`
      )}
    </p>
  );
}

function CurrencyField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <Field>
      <FieldLabel>Currency</FieldLabel>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="BDT">BDT</SelectItem>
          <SelectItem value="USD">USD</SelectItem>
          <SelectItem value="EUR">EUR</SelectItem>
          <SelectItem value="GBP">GBP</SelectItem>
        </SelectContent>
      </Select>
    </Field>
  );
}

function MethodField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const methods = usePaymentMethods();
  return (
    <Field>
      <FieldLabel>Method</FieldLabel>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger>
          <SelectValue placeholder="Select a method" />
        </SelectTrigger>
        <SelectContent>
          {(methods.data?.methods ?? []).map((m) => (
            <SelectItem key={m} value={m}>
              {m}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
}

export function InvestorDialog({ investor, onClose }: { investor?: Investor; onClose: () => void }) {
  const create = useCreateInvestor();
  const update = useUpdateInvestor(investor?.id ?? "");
  const [name, setName] = useState(investor?.name ?? "");
  const [email, setEmail] = useState(investor?.email ?? "");
  const [phone, setPhone] = useState(investor?.phone ?? "");
  const [joinDate, setJoinDate] = useState(investor?.join_date ?? today());
  const [notes, setNotes] = useState(investor?.notes ?? "");
  const [status, setStatus] = useState(investor?.status ?? "active");
  const [userId, setUserId] = useState(investor?.user_id ?? "");
  const pending = create.isPending || update.isPending;

  function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const body = { name, email, phone, join_date: joinDate, notes, user_id: userId, status: status as "active" | "inactive" };
    if (investor) update.mutate(body, { onSuccess: onClose });
    else create.mutate(body, { onSuccess: onClose });
  }

  return (
    <Shell title={investor ? "Edit investor" : "Add investor"} description="A capital partner. Link a login later from Users by giving them the investor role." onClose={onClose}>
      <form onSubmit={onSubmit}>
        <FieldGroup>
          <Field>
            <FieldLabel>Name</FieldLabel>
            <Input value={name} onChange={(e) => setName(e.target.value)} required maxLength={120} />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel>Email</FieldLabel>
              <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
            </Field>
            <Field>
              <FieldLabel>Phone</FieldLabel>
              <Input value={phone} onChange={(e) => setPhone(e.target.value)} />
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel>Join date</FieldLabel>
              <Input type="date" value={joinDate} onChange={(e) => setJoinDate(e.target.value)} required />
            </Field>
            {investor && (
              <Field>
                <FieldLabel>Status</FieldLabel>
                <Select value={status} onValueChange={(v) => setStatus(v as "active" | "inactive")}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="active">Active</SelectItem>
                    <SelectItem value="inactive">Inactive</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
            )}
          </div>
          <Field>
            <FieldLabel>Linked login user ID (optional)</FieldLabel>
            <Input value={userId} onChange={(e) => setUserId(e.target.value)} placeholder="UUID of an investor-role user" />
          </Field>
          <Field>
            <FieldLabel>Notes</FieldLabel>
            <Textarea value={notes} onChange={(e) => setNotes(e.target.value)} />
          </Field>
        </FieldGroup>
        <DialogFooter className="mt-4">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={pending}>
            {investor ? "Save changes" : "Add investor"}
          </Button>
        </DialogFooter>
      </form>
    </Shell>
  );
}

export function InvestmentDialog({ investor, onClose }: { investor: Investor; onClose: () => void }) {
  const record = useRecordInvestment(investor.id);
  const [amount, setAmount] = useState("");
  const [currency, setCurrency] = useState("BDT");
  const [rate, setRate] = useState("");
  const [method, setMethod] = useState("");
  const [investedAt, setInvestedAt] = useState(today());
  const [note, setNote] = useState("");
  const foreign = currency !== "BDT";
  const converted = foreign && rate.trim() !== "" ? parseAmount(amount) * parseAmount(rate) : null;

  function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    record.mutate(
      { amount: parseAmount(amount), currency, rate_to_bdt: foreign ? rate.trim() : undefined, method, invested_at: investedAt, note },
      { onSuccess: onClose },
    );
  }

  return (
    <Shell title={`Record investment — ${investor.name}`} description="Capital the investor put in. The BDT value is fixed at the rate recorded here." onClose={onClose}>
      <form onSubmit={onSubmit}>
        <FieldGroup>
          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel>Amount</FieldLabel>
              <Input type="number" min="0" step="0.01" value={amount} onChange={(e) => setAmount(e.target.value)} required />
            </Field>
            <CurrencyField value={currency} onChange={setCurrency} />
          </div>
          {foreign && (
            <Field>
              <FieldLabel>Rate to BDT</FieldLabel>
              <Input type="number" min="0" step="0.0001" value={rate} onChange={(e) => setRate(e.target.value)} placeholder="e.g. 120" />
              <RateHint currency={currency} date={investedAt} onUse={setRate} />
              {converted !== null && converted > 0 && (
                <p className="text-xs text-muted-foreground">≈ {money(converted, "BDT")}</p>
              )}
            </Field>
          )}
          <div className="grid grid-cols-2 gap-3">
            <MethodField value={method} onChange={setMethod} />
            <Field>
              <FieldLabel>Invested on</FieldLabel>
              <Input type="date" value={investedAt} onChange={(e) => setInvestedAt(e.target.value)} required />
            </Field>
          </div>
          <Field>
            <FieldLabel>Note</FieldLabel>
            <Textarea value={note} onChange={(e) => setNote(e.target.value)} />
          </Field>
        </FieldGroup>
        <DialogFooter className="mt-4">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={record.isPending}>
            Record investment
          </Button>
        </DialogFooter>
      </form>
    </Shell>
  );
}

export function ShareDialog({ investor, onClose }: { investor: Investor; onClose: () => void }) {
  const setShare = useSetShare(investor.id);
  const [share, setShareValue] = useState(String(parseAmount(investor.share_percent)));
  const [effectiveFrom, setEffectiveFrom] = useState(today());

  function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setShare.mutate({ share_percent: parseAmount(share), effective_from: effectiveFrom }, { onSuccess: onClose });
  }

  return (
    <Shell
      title={`Set share — ${investor.name}`}
      description={`Current share: ${parseAmount(investor.share_percent)}%. A new row is recorded; past shares stay in history.`}
      onClose={onClose}
    >
      <form onSubmit={onSubmit}>
        <FieldGroup>
          <Field>
            <FieldLabel>Share percent</FieldLabel>
            <Input type="number" min="0" max="100" step="0.01" value={share} onChange={(e) => setShareValue(e.target.value)} required />
          </Field>
          <Field>
            <FieldLabel>Effective from</FieldLabel>
            <Input type="date" value={effectiveFrom} onChange={(e) => setEffectiveFrom(e.target.value)} required />
          </Field>
        </FieldGroup>
        <DialogFooter className="mt-4">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={setShare.isPending}>
            Save share
          </Button>
        </DialogFooter>
      </form>
    </Shell>
  );
}

export function ReserveDialog({ current, onClose }: { current: number; onClose: () => void }) {
  const update = useUpdateReserve();
  const [reserve, setReserve] = useState(String(current));

  function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    update.mutate(parseAmount(reserve), { onSuccess: onClose });
  }

  return (
    <Shell title="Company reserve" description="The percent of every distribution the company keeps as its reserve fund." onClose={onClose}>
      <form onSubmit={onSubmit}>
        <FieldGroup>
          <Field>
            <FieldLabel>Reserve percent</FieldLabel>
            <Input type="number" min="0" max="100" step="0.01" value={reserve} onChange={(e) => setReserve(e.target.value)} required />
          </Field>
        </FieldGroup>
        <DialogFooter className="mt-4">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={update.isPending}>
            Save reserve
          </Button>
        </DialogFooter>
      </form>
    </Shell>
  );
}

export function ExpenseDialog({ onClose }: { onClose: () => void }) {
  const create = useCreateExpense();
  const [category, setCategory] = useState<ExpenseCategory>("Tools");
  const [description, setDescription] = useState("");
  const [amount, setAmount] = useState("");
  const [currency, setCurrency] = useState("BDT");
  const [rate, setRate] = useState("");
  const [expenseDate, setExpenseDate] = useState(today());
  const [receiptUrl, setReceiptUrl] = useState("");
  const foreign = currency !== "BDT";
  const converted = foreign && rate.trim() !== "" ? parseAmount(amount) * parseAmount(rate) : null;

  function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    create.mutate(
      { category, description, amount: parseAmount(amount), currency, rate_to_bdt: foreign ? rate.trim() : undefined, expense_date: expenseDate, receipt_url: receiptUrl },
      { onSuccess: onClose },
    );
  }

  return (
    <Shell title="Add expense" description="Tools, software, office, marketing or other costs. They count against profit before distribution." onClose={onClose}>
      <form onSubmit={onSubmit}>
        <FieldGroup>
          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel>Category</FieldLabel>
              <Select value={category} onValueChange={(v) => setCategory(v as ExpenseCategory)}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {EXPENSE_CATEGORIES.map((c) => (
                    <SelectItem key={c} value={c}>
                      {c}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel>Amount</FieldLabel>
              <Input type="number" min="0" step="0.01" value={amount} onChange={(e) => setAmount(e.target.value)} required />
            </Field>
          </div>
          <Field>
            <FieldLabel>Description</FieldLabel>
            <Input value={description} onChange={(e) => setDescription(e.target.value)} required maxLength={500} />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <CurrencyField value={currency} onChange={setCurrency} />
            <Field>
              <FieldLabel>Date</FieldLabel>
              <Input type="date" value={expenseDate} onChange={(e) => setExpenseDate(e.target.value)} required />
            </Field>
          </div>
          {foreign && (
            <Field>
              <FieldLabel>Rate to BDT</FieldLabel>
              <Input type="number" min="0" step="0.0001" value={rate} onChange={(e) => setRate(e.target.value)} placeholder="e.g. 120" />
              <RateHint currency={currency} date={expenseDate} onUse={setRate} />
              {converted !== null && converted > 0 && (
                <p className="text-xs text-muted-foreground">≈ {money(converted, "BDT")}</p>
              )}
            </Field>
          )}
          <Field>
            <FieldLabel>Receipt URL (optional)</FieldLabel>
            <Input value={receiptUrl} onChange={(e) => setReceiptUrl(e.target.value)} placeholder="https://…" />
          </Field>
        </FieldGroup>
        <DialogFooter className="mt-4">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={create.isPending}>
            Add expense
          </Button>
        </DialogFooter>
      </form>
    </Shell>
  );
}

export function WithdrawalDialog({ investor, onClose }: { investor: Investor; onClose: () => void }) {
  const request = useRequestWithdrawal(investor.id);
  const available = investorAvailable(investor);
  const [amount, setAmount] = useState("");
  const [currency, setCurrency] = useState("BDT");
  const [rate, setRate] = useState("");
  const [method, setMethod] = useState("");
  const [note, setNote] = useState("");
  const foreign = currency !== "BDT";

  function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    request.mutate(
      { amount: parseAmount(amount), currency, rate_to_bdt: foreign ? rate.trim() : undefined, method, note },
      { onSuccess: onClose },
    );
  }

  return (
    <Shell
      title={`Request withdrawal — ${investor.name}`}
      description={`Available to withdraw: ${money(available, "BDT")}. The CEO approves it, then it is marked paid.`}
      onClose={onClose}
    >
      <form onSubmit={onSubmit}>
        <FieldGroup>
          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel>Amount</FieldLabel>
              <Input type="number" min="0" step="0.01" value={amount} onChange={(e) => setAmount(e.target.value)} required />
            </Field>
            <CurrencyField value={currency} onChange={setCurrency} />
          </div>
          {foreign && (
            <Field>
              <FieldLabel>Rate to BDT</FieldLabel>
              <Input type="number" min="0" step="0.0001" value={rate} onChange={(e) => setRate(e.target.value)} placeholder="e.g. 120" />
              <RateHint currency={currency} date={today()} onUse={setRate} />
            </Field>
          )}
          <MethodField value={method} onChange={setMethod} />
          <Field>
            <FieldLabel>Note</FieldLabel>
            <Textarea value={note} onChange={(e) => setNote(e.target.value)} />
          </Field>
        </FieldGroup>
        <DialogFooter className="mt-4">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={request.isPending}>
            Request withdrawal
          </Button>
        </DialogFooter>
      </form>
    </Shell>
  );
}

export function DistributeDialog({ investor, onClose }: { investor: Investor; onClose: () => void }) {
  const distribute = useDistribute(investor.id);
  const [sourceType, setSourceType] = useState("manual");
  const [amount, setAmount] = useState("");
  const [currency, setCurrency] = useState("BDT");
  const [rate, setRate] = useState("");
  const [note, setNote] = useState("");
  // One stable id per dialog: the server requires source_id so a retry of
  // this distribution cannot pay twice. A fresh dialog gets a fresh id.
  const [sourceId] = useState(() => crypto.randomUUID());
  const foreign = currency !== "BDT";

  function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    distribute.mutate(
      { source_type: sourceType, source_id: sourceId, amount: parseAmount(amount), currency, rate_to_bdt: foreign ? rate.trim() : undefined, note },
      { onSuccess: onClose },
    );
  }

  return (
    <Shell
      title="Distribute income"
      description="Splits this income across every active investor by their current shares, plus the company reserve. Milestone payments distribute automatically when they are recorded."
      onClose={onClose}
    >
      <form onSubmit={onSubmit}>
        <FieldGroup>
          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel>Source</FieldLabel>
              <Select value={sourceType} onValueChange={setSourceType}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="manual">Manual income</SelectItem>
                  <SelectItem value="income">Other income</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel>Amount</FieldLabel>
              <Input type="number" min="0" step="0.01" value={amount} onChange={(e) => setAmount(e.target.value)} required />
            </Field>
          </div>
          <CurrencyField value={currency} onChange={setCurrency} />
          {foreign && (
            <Field>
              <FieldLabel>Rate to BDT</FieldLabel>
              <Input type="number" min="0" step="0.0001" value={rate} onChange={(e) => setRate(e.target.value)} placeholder="e.g. 120" />
              <RateHint currency={currency} date={today()} onUse={setRate} />
            </Field>
          )}
          <Field>
            <FieldLabel>Note</FieldLabel>
            <Textarea value={note} onChange={(e) => setNote(e.target.value)} placeholder="What income is this?" />
          </Field>
        </FieldGroup>
        <DialogFooter className="mt-4">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={distribute.isPending}>
            Distribute
          </Button>
        </DialogFooter>
      </form>
    </Shell>
  );
}

export function RateDialog({ onClose }: { onClose: () => void }) {
  const create = useCreateRate();
  const [currency, setCurrency] = useState("USD");
  const [rate, setRate] = useState("");
  const [effectiveFrom, setEffectiveFrom] = useState(today());

  function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    create.mutate({ currency, rate: parseAmount(rate), effective_from: effectiveFrom }, { onSuccess: onClose });
  }

  return (
    <Shell title="Set currency rate" description="One rate per currency per date. Milestone payments, investments and expenses all read this rate." onClose={onClose}>
      <form onSubmit={onSubmit}>
        <FieldGroup>
          <div className="grid grid-cols-2 gap-3">
            <CurrencyField value={currency} onChange={setCurrency} />
            <Field>
              <FieldLabel>Rate to BDT</FieldLabel>
              <Input type="number" min="0" step="0.0001" value={rate} onChange={(e) => setRate(e.target.value)} placeholder="e.g. 120" required />
            </Field>
          </div>
          <Field>
            <FieldLabel>Effective from</FieldLabel>
            <Input type="date" value={effectiveFrom} onChange={(e) => setEffectiveFrom(e.target.value)} required />
          </Field>
        </FieldGroup>
        <DialogFooter className="mt-4">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" disabled={create.isPending}>
            Save rate
          </Button>
        </DialogFooter>
      </form>
    </Shell>
  );
}
