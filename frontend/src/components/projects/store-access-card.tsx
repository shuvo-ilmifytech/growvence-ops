"use client";

import { useState } from "react";
import { CheckIcon, CopyIcon, ExternalLinkIcon, KeyRoundIcon, PencilIcon } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api";
import { relativeTime } from "@/lib/format";
import { useStoreAccess } from "@/lib/projects";
import { STORE_ACCESS_LABELS, type ProjectDetail, type StoreAccess } from "@/lib/types";
import { cn } from "@/lib/utils";
import { StoreAccessBadge } from "@/components/project-badges";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";

const STEPS: StoreAccess[] = ["not_requested", "requested", "given"];

/**
 * Access to the client's store: has it been asked for, has it been given, and the
 * link the client gave. Not requested shows red until someone asks.
 */
export function StoreAccessCard({ detail }: { detail: ProjectDetail }) {
  const p = detail.project;
  const can = detail.permissions.can_set_access;
  const save = useStoreAccess(p.id);
  const [editing, setEditing] = useState(false);
  const [link, setLink] = useState(p.store_access_link ?? "");
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);

  function update(v: { status: StoreAccess; link?: string }, done?: () => void) {
    setError("");
    save.mutate(v, {
      onSuccess: () => {
        toast.success(`Store access: ${STORE_ACCESS_LABELS[v.status].toLowerCase()}`);
        done?.();
      },
      onError: (err) => {
        if (err instanceof ApiError && err.fields.link) setError(`Link ${err.fields.link}`);
        else toast.error(err.message);
      },
    });
  }

  return (
    <Card className={cn(p.store_access === "not_requested" && p.status !== "cancelled" && "border-red-300 dark:border-red-900")}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRoundIcon className="size-4 text-muted-foreground" /> Store access
        </CardTitle>
        <CardDescription>
          {p.store_access_changed_at ? `Updated ${relativeTime(p.store_access_changed_at)}` : "Ask the client for access to their store before work starts."}
        </CardDescription>
        <CardAction>
          <StoreAccessBadge access={p.store_access} />
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-3">
        {can && (
          <div className="grid grid-cols-3 gap-1 rounded-lg bg-muted p-1" role="radiogroup" aria-label="Store access">
            {STEPS.map((s) => (
              <button
                key={s}
                type="button"
                role="radio"
                aria-checked={p.store_access === s}
                disabled={save.isPending}
                onClick={() => p.store_access !== s && update({ status: s })}
                className={cn(
                  "rounded-md px-2 py-1.5 text-xs font-medium transition-colors",
                  p.store_access === s
                    ? s === "not_requested"
                      ? "bg-red-600 text-white shadow-xs"
                      : s === "requested"
                        ? "bg-amber-500 text-white shadow-xs"
                        : "bg-emerald-600 text-white shadow-xs"
                    : "text-muted-foreground hover:bg-background hover:text-foreground",
                )}
              >
                {STORE_ACCESS_LABELS[s]}
              </button>
            ))}
          </div>
        )}

        {editing ? (
          <form
            className="space-y-2"
            onSubmit={(e) => {
              e.preventDefault();
              update({ status: p.store_access, link: link.trim() }, () => setEditing(false));
            }}
          >
            <Input
              value={link}
              onChange={(e) => setLink(e.target.value)}
              placeholder="https://store.myshopify.com/admin"
              aria-label="Client access link"
              aria-invalid={!!error}
              autoFocus
            />
            {error && <p className="text-xs text-destructive">{error}</p>}
            <p className="text-xs text-muted-foreground">A link only: never put passwords here.</p>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" size="sm" onClick={() => setEditing(false)}>
                Cancel
              </Button>
              <Button type="submit" size="sm" disabled={save.isPending}>
                Save link
              </Button>
            </div>
          </form>
        ) : p.store_access_link ? (
          <div className="flex items-center gap-1 rounded-lg border px-2 py-1.5">
            <a
              href={p.store_access_link}
              target="_blank"
              rel="noreferrer noopener"
              className="min-w-0 flex-1 truncate text-sm text-primary hover:underline"
              title={p.store_access_link}
            >
              {p.store_access_link.replace(/^https?:\/\//, "")}
            </a>
            <Button variant="ghost" size="icon-sm" asChild aria-label="Open the client's access link">
              <a href={p.store_access_link} target="_blank" rel="noreferrer noopener">
                <ExternalLinkIcon />
              </a>
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Copy link"
              onClick={() =>
                navigator.clipboard.writeText(p.store_access_link!).then(() => {
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1500);
                })
              }
            >
              {copied ? <CheckIcon /> : <CopyIcon />}
            </Button>
            {can && (
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Edit link"
                onClick={() => {
                  setLink(p.store_access_link ?? "");
                  setEditing(true);
                }}
              >
                <PencilIcon />
              </Button>
            )}
          </div>
        ) : can ? (
          <Button
            variant="outline"
            size="sm"
            className="w-full"
            onClick={() => {
              setLink("");
              setEditing(true);
            }}
          >
            Add the client&apos;s access link
          </Button>
        ) : (
          <p className="text-sm text-muted-foreground">No access link yet.</p>
        )}
      </CardContent>
    </Card>
  );
}
