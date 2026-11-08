import { Suspense } from "react";
import { InvoiceDocument } from "@/components/finance/invoice-document";

export const metadata = { title: "Invoice" };

export default function InvoicePage({ params }: PageProps<"/invoices/[id]">) {
  return (
    <Suspense>
      <InvoiceDocument params={params} />
    </Suspense>
  );
}
