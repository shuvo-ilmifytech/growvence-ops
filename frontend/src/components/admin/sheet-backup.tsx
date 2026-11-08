"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangleIcon, CheckCircle2Icon, RefreshCwIcon, SheetIcon } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { relativeTime } from "@/lib/format";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

export type SheetStatus = {
  configured: boolean;
  spreadsheet_id: string;
  tab: string;
  share_with: string;
  total: number;
  pending: number;
  failing: number;
  last_synced_at: string | null;
  last_error: string;
};

function message(err: unknown) {
  return err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.message) : err instanceof Error ? err.message : "Something went wrong";
}

/** Google Sheet backup: one row per project, rewritten whenever the project changes. */
export function SheetBackup() {
  const qc = useQueryClient();
  const status = useQuery({ queryKey: ["sheet-backup"], queryFn: () => api<SheetStatus>("/admin/sheet"), refetchInterval: 30_000 });
  const refresh = () => qc.invalidateQueries({ queryKey: ["sheet-backup"] });
  const check = useMutation({
    mutationFn: () => api<{ share_with: string }>("/admin/sheet/check", { method: "POST" }),
    onSuccess: (r) => toast.success(`Google answered. The sheet is shared with ${r.share_with}.`),
    onError: (err) => toast.error(message(err)),
  });
  const backfill = useMutation({
    mutationFn: () => api<{ projects: number }>("/admin/sheet/backfill", { method: "POST" }),
    onSuccess: (r) => {
      toast.success(`Backed up ${r.projects} project${r.projects === 1 ? "" : "s"}.`);
      refresh();
    },
    onError: (err) => toast.error(message(err)),
  });
  const s = status.data;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <SheetIcon className="size-4 text-primary" /> Google Sheet backup
        </CardTitle>
        <CardDescription>
          One row per project, found by project number and rewritten whenever anything changes. Drafts stay out; a project joins the
          sheet when Sales submit it.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {!s ? (
          <Skeleton className="h-24" />
        ) : !s.configured ? (
          <div className="space-y-3 rounded-lg border border-dashed p-4 text-sm">
            <p className="font-medium">Not switched on yet.</p>
            <ol className="list-decimal space-y-1 pl-5 text-muted-foreground">
              <li>In Google Cloud, create a service account and enable the Google Sheets API.</li>
              <li>Download its JSON key onto the server.</li>
              <li>Create the spreadsheet and share it with the service account&rsquo;s address as an <strong>Editor</strong>.</li>
              <li>
                Set <code className="font-mono text-xs">SHEETS_SPREADSHEET_ID</code> and{" "}
                <code className="font-mono text-xs">GOOGLE_SERVICE_ACCOUNT_FILE</code>, then restart the API.
              </li>
            </ol>
            <p className="text-muted-foreground">
              {s.total > 0
                ? `${s.total} project${s.total === 1 ? " is" : "s are"} already queued and will be written as soon as a sheet is configured.`
                : "Projects are queued as they're submitted, so nothing is lost before it's switched on."}
            </p>
          </div>
        ) : (
          <>
            <dl className="grid grid-cols-1 gap-x-6 gap-y-2 text-sm sm:grid-cols-[10rem_1fr]">
              <dt className="text-muted-foreground">Spreadsheet</dt>
              <dd className="min-w-0">
                <a
                  href={`https://docs.google.com/spreadsheets/d/${s.spreadsheet_id}/edit`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="font-medium hover:underline"
                >
                  Open the sheet
                </a>
                <span className="text-muted-foreground"> · tab {s.tab}</span>
              </dd>
              <dt className="text-muted-foreground">Shared with</dt>
              <dd className="min-w-0 truncate font-mono text-xs">{s.share_with}</dd>
              <dt className="text-muted-foreground">Projects backed up</dt>
              <dd className="tabular-nums">{s.total - s.pending} of {s.total}</dd>
              <dt className="text-muted-foreground">Last write</dt>
              <dd>{s.last_synced_at ? relativeTime(s.last_synced_at) : "never"}</dd>
            </dl>

            {s.failing > 0 ? (
              <div className="flex gap-2 rounded-lg border border-red-500/40 bg-red-500/5 p-3 text-sm">
                <AlertTriangleIcon className="mt-0.5 size-4 shrink-0 text-red-600" />
                <div className="min-w-0">
                  <p className="font-medium text-red-600">
                    {s.failing} project{s.failing === 1 ? "" : "s"} could not be written. Retrying automatically.
                  </p>
                  {s.last_error && <p className="mt-0.5 break-words text-muted-foreground">{s.last_error}</p>}
                </div>
              </div>
            ) : s.pending > 0 ? (
              <p className="text-sm text-muted-foreground">
                {s.pending} project{s.pending === 1 ? "" : "s"} waiting for the next write.
              </p>
            ) : (
              <p className="flex items-center gap-2 text-sm text-muted-foreground">
                <CheckCircle2Icon className="size-4 text-emerald-600" /> The sheet matches the app.
              </p>
            )}

            <div className="flex flex-wrap gap-2">
              <Button variant="outline" onClick={() => check.mutate()} disabled={check.isPending}>
                Test the connection
              </Button>
              <Button variant="outline" onClick={() => backfill.mutate()} disabled={backfill.isPending}>
                <RefreshCwIcon /> Back up everything now
              </Button>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
