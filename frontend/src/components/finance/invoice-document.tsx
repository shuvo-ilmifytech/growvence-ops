"use client";

import Link from "next/link";
import { use, useState } from "react";
import { ArrowLeftIcon, CheckIcon, LockIcon, PencilIcon, PrinterIcon, XIcon } from "lucide-react";
import { formatDate } from "@/lib/format";
import {
  formatAmount,
  formatBdt,
  invoiceRef,
  INVOICE_STATUS,
  itemQuantity,
  takaInWords,
  useInvoice,
  useInvoices,
  type Invoice,
} from "@/lib/finance";
import { useMe } from "@/lib/session";
import { LogoMark } from "@/components/brand";
import { InvoiceDialog, InvoiceReviewDialog, type ReviewTarget } from "@/components/finance/invoice-dialog";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";

// The company's own letterhead, as it appears on the printed voucher. Change it here.
const COMPANY = {
  name: "Growvence Tech",
  tagline: "LEAD THE NEXT",
  address: ["Address: House No: 27, Road No: 01", "Block: A, Banasree, Dhaka-1219"],
};

/** dd/mm/yyyy, the way the voucher writes a date. */
function voucherDate(day: string) {
  const [y, m, d] = day.split("-");
  return d && m && y ? `${d}/${m}/${y}` : day;
}

/**
 * One invoice as the company's debit voucher, laid out to match the paper form: letterhead,
 * the DEBIT VOUCHER banner, the numbered details table, the total in words, and the three
 * signature lines. This is the page the browser prints (`print-document`), so the app's
 * sidebar, header and buttons are left off and it comes out black on white.
 */
export function InvoiceDocument({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const { data: me } = useMe();
  const allowed = !!me && me.permissions.includes("invoice.raise");
  const invoice = useInvoice(id, allowed);
  // The categories for the edit dialog come with the list, which is already cached.
  const list = useInvoices({}, allowed);
  const [editing, setEditing] = useState(false);
  const [review, setReview] = useState<ReviewTarget | null>(null);

  if (me && !allowed) {
    return (
      <Empty className="min-h-[60vh]">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <LockIcon />
          </EmptyMedia>
          <EmptyTitle>Not your part of the app</EmptyTitle>
          <EmptyDescription>Invoices are raised by the Office Admin and approved by HR and the CEO.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }
  if (invoice.isError) {
    return (
      <Empty className="min-h-[60vh]">
        <EmptyHeader>
          <EmptyTitle>Invoice not found</EmptyTitle>
          <EmptyDescription>{invoice.error.message}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }
  const inv = invoice.data;
  if (!inv) return <Skeleton className="h-[70vh] w-full" />;

  const mine = inv.requested_by === me?.id;
  const open = inv.status === "pending" || inv.status === "awaiting_ceo";
  const canReview = list.data?.can_review ?? false;
  const myStep = canReview && !mine && (me?.role === "ceo" ? open : inv.status === "pending");
  const canEdit = open && (inv.status === "pending" ? mine || canReview : canReview);
  const taka = inv.currency === "BDT";
  // Enough blank rows that a short voucher still fills the form, as the paper one does.
  const blanks = Math.max(0, 6 - inv.items_list.length);

  return (
    <div className="space-y-6">
      <div data-print-hide className="flex flex-wrap items-center gap-2">
        <Button variant="ghost" size="sm" asChild>
          <Link href="/invoices">
            <ArrowLeftIcon /> All invoices
          </Link>
        </Button>
        <div className="ml-auto flex flex-wrap gap-2">
          {myStep && (
            <>
              <Button variant="outline" size="sm" onClick={() => setReview({ invoice: inv, approve: false, final: true })}>
                <XIcon /> Reject
              </Button>
              <Button size="sm" onClick={() => setReview({ invoice: inv, approve: true, final: me?.role === "ceo" })}>
                <CheckIcon /> Approve
              </Button>
            </>
          )}
          {canEdit && (
            <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
              <PencilIcon /> Edit
            </Button>
          )}
          <Button variant="outline" size="sm" onClick={() => window.print()}>
            <PrinterIcon /> Print
          </Button>
        </div>
      </div>

      {/* The voucher itself. Black on white, fixed borders: it is meant for paper. */}
      <article className="print-document mx-auto max-w-3xl border border-black bg-white p-6 text-black sm:p-10">
        <header className="flex flex-wrap items-start justify-between gap-4">
          <div className="flex items-center gap-2">
            <LogoMark className="h-10" />
            <div className="leading-tight">
              <div className="text-xl font-bold">{COMPANY.name}</div>
              <div className="text-[0.6rem] tracking-[0.3em] text-neutral-600">{COMPANY.tagline}</div>
            </div>
          </div>
          <div className="text-xs font-semibold">
            {COMPANY.address.map((line) => (
              <div key={line}>{line}</div>
            ))}
          </div>
        </header>

        <div className="mt-6 bg-neutral-200 py-1 text-center text-sm font-bold tracking-wide">DEBIT VOUCHER</div>

        <div className="mt-4 flex flex-wrap justify-between gap-2 text-sm">
          <div>
            <span className="font-semibold">No.:</span> {invoiceRef(inv.number)}
          </div>
          <div>
            <span className="font-semibold">Date:</span> {voucherDate(inv.expense_date)}
          </div>
        </div>
        <div className="mt-2 text-sm">
          <span className="font-semibold">Purpose:</span> {inv.description}
        </div>
        {inv.status !== "approved" && (
          // Never let an unapproved voucher print as though it had been signed off.
          <div className="mt-2 border border-black px-2 py-1 text-xs font-semibold uppercase">
            {INVOICE_STATUS[inv.status].label} — not recorded in the ledger
          </div>
        )}

        <table className="print-keep mt-3 w-full border-collapse text-sm">
          <thead>
            <tr className="bg-neutral-200 text-center font-semibold">
              <th className="w-12 border border-black px-2 py-1">No</th>
              <th className="border border-black px-2 py-1">Details</th>
              <th className="w-16 border border-black px-2 py-1">Qty</th>
              <th className="w-32 border border-black px-2 py-1">{taka ? "Taka" : inv.currency}</th>
            </tr>
          </thead>
          <tbody>
            {inv.items_list.map((it, i) => (
              <tr key={it.id}>
                <td className="border-x border-black px-2 py-0.5 text-center tabular-nums">{String(i + 1).padStart(2, "0")}</td>
                <td className="border-x border-black px-2 py-0.5">
                  {it.description}
                  <span className="text-neutral-600"> · {it.category_name}</span>
                </td>
                <td className="border-x border-black px-2 py-0.5 text-center tabular-nums">{itemQuantity(it.quantity)}</td>
                <td className="border-x border-black px-2 py-0.5 text-center tabular-nums">{formatAmount(it.amount, inv.currency).replace(/^Tk\s/, "")}</td>
              </tr>
            ))}
            {Array.from({ length: blanks }, (_, i) => (
              <tr key={`blank-${i}`}>
                <td className="border-x border-black px-2 py-0.5">&nbsp;</td>
                <td className="border-x border-black px-2 py-0.5" />
                <td className="border-x border-black px-2 py-0.5" />
                <td className="border-x border-black px-2 py-0.5" />
              </tr>
            ))}
          </tbody>
          <tfoot>
            <tr>
              <td colSpan={3} className="border border-black px-2 py-1">
                <span className="font-semibold">In Words:</span>{" "}
                {taka ? takaInWords(inv.amount) : `${formatAmount(inv.amount, inv.currency)} (${formatBdt(inv.amount_bdt)})`}
              </td>
              <td className="border border-black bg-neutral-200 px-2 py-1 text-right font-bold tabular-nums">
                TOTAL: {formatAmount(inv.amount, inv.currency).replace(/^Tk\s/, "")}
              </td>
            </tr>
            {!taka && (
              <tr>
                <td colSpan={3} className="border-x border-b border-black px-2 py-1 text-xs">
                  Converted at {Number(inv.rate_to_bdt)} BDT per 1 {inv.currency}
                </td>
                <td className="border-x border-b border-black px-2 py-1 text-right text-xs font-semibold tabular-nums">
                  {formatBdt(inv.amount_bdt)}
                </td>
              </tr>
            )}
          </tfoot>
        </table>

        <Signatures invoice={inv} />
      </article>

      {editing && list.data && (
        <InvoiceDialog invoice={inv} categories={list.data.categories} today={list.data.today} onClose={() => setEditing(false)} />
      )}
      <InvoiceReviewDialog target={review} onOpenChange={(o) => !o && setReview(null)} />
    </div>
  );
}

/**
 * The three seats on the paper form. Whoever has already decided is typed above their line,
 * the way a typed name sits over a signature; the rest stay blank to be signed by hand.
 */
function Signatures({ invoice: inv }: { invoice: Invoice }) {
  const seats = [
    { label: "Received By", name: inv.requested_by_name, when: inv.created_at },
    { label: "Verified By HR & Compliance", name: inv.hr_reviewed_by_name, when: inv.hr_reviewed_at },
    { label: "Approved By CEO", name: inv.ceo_reviewed_by_name, when: inv.ceo_reviewed_at },
  ];
  return (
    <div className="print-keep mt-16 grid grid-cols-1 gap-8 sm:grid-cols-3">
      {seats.map((s) => (
        <div key={s.label} className="text-center text-xs">
          <div className="h-8 leading-tight">
            {s.name && (
              <>
                <div className="font-medium">{s.name}</div>
                <div className="text-neutral-600">{s.when && formatDate(s.when)}</div>
              </>
            )}
          </div>
          <div className="border-t border-black pt-1 font-semibold">{s.label}</div>
        </div>
      ))}
    </div>
  );
}
