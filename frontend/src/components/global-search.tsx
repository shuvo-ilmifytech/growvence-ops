"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { FileIcon, FolderKanbanIcon, InboxIcon, KanbanIcon, SearchIcon, UserRoundIcon, type LucideIcon } from "lucide-react";
import { api } from "@/lib/api";
import { relativeTime } from "@/lib/format";
import {
  CARD_COLUMN_LABELS,
  REQUEST_STATUS_LABELS,
  ROLE_LABELS,
  STATUS_LABELS,
  type CardColumn,
  type ProjectStatus,
  type RequestStatus,
  type Role,
} from "@/lib/types";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Skeleton } from "@/components/ui/skeleton";

type Kind = "project" | "request" | "card" | "request_card" | "file" | "person";
type Hit = { kind: Kind; id: string; project_id: string; title: string; subtitle: string; badge: string; at: string };

const ICONS: Record<Kind, LucideIcon> = {
  project: FolderKanbanIcon,
  request: InboxIcon,
  card: KanbanIcon,
  request_card: KanbanIcon,
  file: FileIcon,
  person: UserRoundIcon,
};
const KIND_LABELS: Record<Kind, string> = {
  project: "Projects",
  request: "Client requests",
  card: "Cards",
  request_card: "Request cards",
  file: "Files",
  person: "People",
};
const ORDER: Kind[] = ["project", "request", "card", "request_card", "file", "person"];

function badgeText(h: Hit) {
  if (h.kind === "project") return STATUS_LABELS[h.badge as ProjectStatus] ?? h.badge;
  if (h.kind === "request") return REQUEST_STATUS_LABELS[h.badge as RequestStatus] ?? h.badge;
  if (h.kind === "card" || h.kind === "request_card") return CARD_COLUMN_LABELS[h.badge as CardColumn] ?? h.badge;
  if (h.kind === "person") return ROLE_LABELS[h.badge as Role] ?? h.badge;
  return "";
}

function hrefFor(h: Hit) {
  switch (h.kind) {
    case "project":
      return `/projects/${h.id}`;
    case "request":
      return `/requests/${h.id}`;
    case "card":
      return `/projects/${h.project_id}?tab=board&card=${h.id}`;
    case "request_card":
      return `/requests/${h.project_id}?card=${h.id}`;
    case "file":
      return h.badge ? `/api/files/versions/${h.badge}/download` : `/projects/${h.project_id}`;
    case "person":
      return `/hr/people/${h.id}`;
  }
}

/** One search box over projects, cards, files and people, scoped to what the user may see. */
export function GlobalSearch() {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [debounced, setDebounced] = useState("");
  const [active, setActive] = useState(0);
  const router = useRouter();
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 180);
    return () => clearTimeout(t);
  }, [q]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((v) => !v);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const search = useQuery({
    queryKey: ["search", debounced],
    queryFn: () => api<{ results: Hit[] }>(`/search?q=${encodeURIComponent(debounced)}`).then((r) => r.results),
    enabled: open && debounced.length >= 2,
    staleTime: 15_000,
  });

  const hits = debounced.length >= 2 ? (search.data ?? []) : [];
  const groups = ORDER.map((kind) => ({ kind, items: hits.filter((h) => h.kind === kind) })).filter((g) => g.items.length);
  const flat = groups.flatMap((g) => g.items);

  function go(h: Hit) {
    setOpen(false);
    setQ("");
    if (h.kind === "file") window.open(hrefFor(h), "_blank", "noopener");
    else router.push(hrefFor(h));
  }

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        className="text-muted-foreground max-sm:px-2"
        onClick={() => setOpen(true)}
        aria-label="Search everything"
      >
        <SearchIcon />
        <span className="max-sm:hidden">Search</span>
        <kbd className="ml-6 hidden rounded border px-1 text-[10px] sm:inline">Ctrl K</kbd>
      </Button>
      <Dialog
        open={open}
        onOpenChange={(o) => {
          setOpen(o);
          if (!o) setQ("");
        }}
      >
        <DialogContent className="top-24 max-h-[70vh] translate-y-0 gap-3 overflow-hidden sm:max-w-xl">
          <DialogTitle className="sr-only">Search</DialogTitle>
          <DialogDescription className="sr-only">Projects, cards, files and people you can see</DialogDescription>
          <InputGroup>
            <InputGroupAddon>
              <SearchIcon />
            </InputGroupAddon>
            <InputGroupInput
              autoFocus
              value={q}
              onChange={(e) => {
                setQ(e.target.value);
                setActive(0);
              }}
              onKeyDown={(e) => {
                if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                  e.preventDefault();
                  setActive((i) => Math.min(Math.max(i + (e.key === "ArrowDown" ? 1 : -1), 0), Math.max(flat.length - 1, 0)));
                  listRef.current?.querySelectorAll("[data-hit]")[active]?.scrollIntoView({ block: "nearest" });
                } else if (e.key === "Enter" && flat[active]) {
                  e.preventDefault();
                  go(flat[active]);
                }
              }}
              placeholder="Order ID, client, project, card, file or person"
              aria-label="Search"
            />
          </InputGroup>
          <div ref={listRef} className="-mx-1 min-h-24 overflow-y-auto px-1">
            {debounced.length < 2 ? (
              <p className="py-6 text-center text-sm text-muted-foreground">Type at least two characters.</p>
            ) : search.isPending ? (
              <div className="space-y-2 py-2">
                <Skeleton className="h-9" />
                <Skeleton className="h-9" />
              </div>
            ) : flat.length === 0 ? (
              <p className="py-6 text-center text-sm text-muted-foreground">Nothing matches “{debounced}”.</p>
            ) : (
              groups.map((g) => (
                <div key={g.kind} className="mb-2">
                  <p className="px-2 py-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">{KIND_LABELS[g.kind]}</p>
                  {g.items.map((h) => {
                    const Icon = ICONS[h.kind];
                    const i = flat.indexOf(h);
                    return (
                      <button
                        key={`${h.kind}-${h.id}`}
                        data-hit
                        type="button"
                        onMouseEnter={() => setActive(i)}
                        onClick={() => go(h)}
                        className={cn(
                          "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left",
                          i === active ? "bg-accent text-accent-foreground" : "hover:bg-muted/60",
                        )}
                      >
                        <Icon className="size-4 shrink-0 text-muted-foreground" />
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm font-medium">{h.title}</span>
                          {h.subtitle && <span className="block truncate text-xs text-muted-foreground">{h.subtitle}</span>}
                        </span>
                        {h.kind !== "file" && <span className="shrink-0 text-xs text-muted-foreground">{badgeText(h)}</span>}
                        {h.kind === "project" && <span className="shrink-0 text-xs text-muted-foreground">{relativeTime(h.at)}</span>}
                      </button>
                    );
                  })}
                </div>
              ))
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
