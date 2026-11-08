"use client";

import { use, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  pointerWithin,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  ArrowLeftIcon,
  ArrowRightLeftIcon,
  CheckCircle2Icon,
  HashIcon,
  PlusIcon,
  RotateCcwIcon,
  StoreIcon,
  UserIcon,
  UserPlusIcon,
  UsersIcon,
  XCircleIcon,
  type LucideIcon,
} from "lucide-react";
import { toast } from "sonner";
import { useCardAssignees, useMoveCard } from "@/lib/cards";
import { formatDateTime, relativeTime } from "@/lib/format";
import { useMoveRequestTeam, useRequest, useSetRequestStatus } from "@/lib/requests";
import { useMe } from "@/lib/session";
import { useTeams } from "@/lib/teams";
import { CARD_COLUMN_LABELS, CARD_COLUMNS, type Card as BoardCard, type CardColumn, type RequestDetail } from "@/lib/types";
import { cn } from "@/lib/utils";
import { CardDialog, CardFace, COLUMN_TONE, NewCardDialog } from "@/components/projects/project-board";
import { EditRequestDialog } from "@/components/requests/edit-request-dialog";
import { RequestStatusBadge } from "@/components/requests/request-badges";
import { UpdatesPanel } from "@/components/updates/project-updates";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";

/**
 * One client request: a client with no project in the portal, what they asked for, the
 * team it went to, the cards the team works and the client updates written on it.
 */
export function RequestView({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const detail = useRequest(id);
  const { data: me } = useMe();
  const sp = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const openCard = sp.get("card");
  const setCard = (cid: string | null) => {
    const q = new URLSearchParams(sp);
    if (cid) q.set("card", cid);
    else q.delete("card");
    router.replace(q.size ? `${pathname}?${q}` : pathname, { scroll: false });
  };

  if (detail.isPending) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-full max-w-md" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }
  if (detail.isError || !detail.data) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Client request not found</CardTitle>
          <CardDescription>It may have been cancelled, or you don&apos;t have access to it.</CardDescription>
        </CardHeader>
        <CardContent>
          <Button variant="outline" asChild>
            <Link href="/requests">
              <ArrowLeftIcon /> Back to client requests
            </Link>
          </Button>
        </CardContent>
      </Card>
    );
  }

  const d = detail.data;
  const r = d.request;
  // The work is given out on a card. On a request there is usually just the one, so the
  // client block can open it rather than leaving people to find it on the board.
  const firstCard = (d.cards ?? []).find((c) => c.column_key !== "done") ?? (d.cards ?? [])[0];
  // Read every list through a fallback: an older API, or a cached page from before a
  // deploy, can hand back a null where a list is expected, and that must not blank the page.
  const roster = d.roster ?? [];
  const activity = d.activity ?? [];

  return (
    <div className="space-y-6">
      <div className="space-y-3">
        <Button variant="ghost" size="sm" asChild className="-ml-2">
          <Link href="/requests">
            <ArrowLeftIcon /> Client requests
          </Link>
        </Button>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="space-y-1.5">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-mono text-sm text-muted-foreground">CR-{r.number}</span>
              <RequestStatusBadge status={r.status} />
              <Badge variant="outline">{r.team_name}</Badge>
              <Badge variant="secondary">No project</Badge>
            </div>
            <h1 className="text-2xl font-semibold tracking-tight">{r.title}</h1>
            <p className="text-sm text-muted-foreground">
              Raised by {r.created_by_name} · {relativeTime(r.created_at)}
              {r.closed_at && ` · closed by ${r.closed_by_name} ${relativeTime(r.closed_at)}`}
            </p>
          </div>
          <RequestActions detail={d} />
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>What the client asked for</CardTitle>
          </CardHeader>
          <CardContent>
            {r.details ? (
              <p className="text-sm whitespace-pre-wrap">{r.details}</p>
            ) : (
              <p className="text-sm text-muted-foreground italic">No details beyond the title.</p>
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>The client</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3 text-sm">
            <Row icon={UserIcon} label="Client" value={r.client_name} />
            <Row icon={HashIcon} label="Order ID" value={r.order_id ?? "None"} />
            <Row icon={StoreIcon} label="Updates go out on" value={r.profile_name} />
            <Separator />
            <Row icon={UsersIcon} label="Team" value={r.team_name} />
            {/* Whoever holds the work writes the client updates, so it is given out here,
                on the request, rather than only inside the card. Handing it out is the
                Team Leader's; the rest of the team only reads this row. */}
            {d.permissions.can_assign && firstCard ? (
              <AssignHere requestId={r.id} card={firstCard} roster={roster} />
            ) : (
              <Row icon={UsersIcon} label="Working on it" value={r.member_names?.length ? r.member_names.join(", ") : "Nobody yet"} />
            )}
          </CardContent>
        </Card>
      </div>

      <RequestBoard detail={d} onOpenCard={setCard} />

      <UpdatesPanel
        containerId={r.id}
        isRequest
        canWrite={d.permissions.can_write_note}
        role={me?.role}
        emptyHint={
          d.permissions.can_write_note
            ? undefined
            : d.permissions.can_assign && !r.member_names?.length
              ? "Nobody holds the card yet. Open it and give it to someone (yourself included) — whoever has it writes the updates."
              : undefined
        }
      />

      <Card>
        <CardHeader>
          <CardTitle>Timeline</CardTitle>
          <CardDescription>Everything that has happened on this request.</CardDescription>
        </CardHeader>
        <CardContent>
          {activity.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nothing yet.</p>
          ) : (
            <ul className="space-y-2">
              {activity.map((a) => (
                <li key={a.id} className="flex flex-wrap items-baseline gap-2 text-sm">
                  <span className="font-medium">{a.actor_name ?? "The system"}</span>
                  <span className="text-muted-foreground">{a.action.replace(/[._]/g, " ")}</span>
                  <span className="ml-auto text-xs text-muted-foreground">{formatDateTime(a.created_at)}</span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <CardDialog
        id={openCard}
        project={() => ({
          members: { ids: roster.map((m) => m.id), names: roster.map((m) => m.name) },
          canMove: d.can_move_card,
          canAssign: d.permissions.can_assign,
        })}
        onClose={() => setCard(null)}
      />
    </div>
  );
}

/**
 * Who has the work, set right here. It writes the assignees of the request's card (that is
 * where the board keeps them), so the team's board and this page always agree.
 */
function AssignHere({
  requestId,
  card,
  roster,
}: {
  requestId: string;
  card: BoardCard;
  roster: { id: string; name: string }[];
}) {
  const assign = useCardAssignees(requestId, card.id);
  const held = new Set(card.assignee_ids);
  const toggle = (id: string) => {
    const next = held.has(id) ? card.assignee_ids.filter((x) => x !== id) : [...card.assignee_ids, id];
    assign.mutate(next, { onError: (err) => toast.error(err.message) });
  };
  return (
    <div className="flex items-start gap-2">
      <UserPlusIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      <div className="min-w-0 space-y-1.5">
        <p className="text-xs text-muted-foreground">Working on it</p>
        {roster.length === 0 ? (
          <p className="text-sm">
            Nobody is on this team yet. Add members under <span className="font-medium">Teams</span>, then give them the work.
          </p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {roster.map((m) => (
              <Button
                key={m.id}
                type="button"
                size="sm"
                variant={held.has(m.id) ? "default" : "outline"}
                aria-pressed={held.has(m.id)}
                disabled={assign.isPending}
                onClick={() => toggle(m.id)}
              >
                {m.name}
              </Button>
            ))}
          </div>
        )}
        {held.size === 0 && roster.length > 0 && (
          <p className="text-xs text-muted-foreground">Nobody yet — pick someone (yourself included) and they can write the client updates.</p>
        )}
      </div>
    </div>
  );
}

function Row({ icon: Icon, label, value }: { icon: LucideIcon; label: string; value: string }) {
  return (
    <div className="flex items-start gap-2">
      <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      <div className="min-w-0">
        <p className="text-xs text-muted-foreground">{label}</p>
        <p className="break-words">{value}</p>
      </div>
    </div>
  );
}

/** Sales keep the request right and close it; the Team Leader may pass it to another team. */
function RequestActions({ detail }: { detail: RequestDetail }) {
  const r = detail.request;
  const p = detail.permissions;
  const status = useSetRequestStatus(r.id);
  const [editing, setEditing] = useState(false);
  const [moving, setMoving] = useState(false);
  const [cancelling, setCancelling] = useState(false);

  return (
    <div className="flex flex-wrap items-center gap-2">
      {p.can_edit && (
        <Button variant="outline" onClick={() => setEditing(true)}>
          Edit
        </Button>
      )}
      {p.can_move_team && (
        <Button variant="outline" onClick={() => setMoving(true)}>
          <ArrowRightLeftIcon /> Move to another team
        </Button>
      )}
      {p.can_cancel && (
        <Button variant="outline" onClick={() => setCancelling(true)}>
          <XCircleIcon /> Cancel
        </Button>
      )}
      {p.can_close && (
        <Button
          disabled={status.isPending}
          onClick={() =>
            status.mutate(
              { status: "done" },
              { onSuccess: () => toast.success("Closed. The team has been told."), onError: (e) => toast.error(e.message) },
            )
          }
        >
          {status.isPending ? <Spinner /> : <CheckCircle2Icon />} Mark done
        </Button>
      )}
      {p.can_reopen && (
        <Button
          variant="outline"
          disabled={status.isPending}
          onClick={() =>
            status.mutate({ status: "open" }, { onSuccess: () => toast.success("Reopened."), onError: (e) => toast.error(e.message) })
          }
        >
          {status.isPending ? <Spinner /> : <RotateCcwIcon />} Reopen
        </Button>
      )}
      <EditRequestDialog open={editing} onOpenChange={setEditing} request={r} />
      <MoveTeamDialog open={moving} onOpenChange={setMoving} detail={detail} />
      <CancelDialog open={cancelling} onOpenChange={setCancelling} id={r.id} />
    </div>
  );
}

function MoveTeamDialog({ open, onOpenChange, detail }: { open: boolean; onOpenChange: (o: boolean) => void; detail: RequestDetail }) {
  const router = useRouter();
  const teams = useTeams();
  const move = useMoveRequestTeam(detail.request.id);
  const [team, setTeam] = useState("");
  const others = (teams.data ?? []).filter((t) => t.is_active && t.id !== detail.request.team_id);
  return (
    <Dialog open={open} onOpenChange={(o) => (onOpenChange(o), o || setTeam(""))}>
      <DialogContent>
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            const reason = String(new FormData(e.currentTarget).get("reason")).trim();
            move.mutate(
              { team_id: team, reason },
              {
                onSuccess: (res) => {
                  toast.success("Moved. The new team's leader has been told.");
                  onOpenChange(false);
                  // It is with a team this leader doesn't run, so it is no longer theirs.
                  if ("visible" in res && res.visible === false) router.push("/requests");
                },
                onError: (err) => toast.error(err.message),
              },
            );
          }}
        >
          <DialogHeader>
            <DialogTitle>Move to another team</DialogTitle>
            <DialogDescription>
              Whoever is holding its cards comes off it, so the new team&apos;s leader hands the work out again.
            </DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="move-team">Team</FieldLabel>
            <Select value={team} onValueChange={setTeam}>
              <SelectTrigger id="move-team" className="w-full">
                <SelectValue placeholder="Pick the team" />
              </SelectTrigger>
              <SelectContent>
                {others.map((t) => (
                  <SelectItem key={t.id} value={t.id}>
                    {t.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel htmlFor="move-reason">Why</FieldLabel>
            <Textarea id="move-reason" name="reason" rows={3} required minLength={3} maxLength={1000} placeholder="e.g. This is a WordPress site, not Shopify" />
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!team || move.isPending}>
              {move.isPending && <Spinner />} Move
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function CancelDialog({ open, onOpenChange, id }: { open: boolean; onOpenChange: (o: boolean) => void; id: string }) {
  const status = useSetRequestStatus(id);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            const reason = String(new FormData(e.currentTarget).get("reason")).trim();
            status.mutate(
              { status: "cancelled", reason },
              {
                onSuccess: () => {
                  toast.success("Cancelled. The team has been told.");
                  onOpenChange(false);
                },
                onError: (err) => toast.error(err.message),
              },
            );
          }}
        >
          <DialogHeader>
            <DialogTitle>Cancel this request</DialogTitle>
            <DialogDescription>The team stops work on it. It stays here with its trail, and can be reopened.</DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="cancel-reason">Why</FieldLabel>
            <Textarea id="cancel-reason" name="reason" rows={3} required minLength={1} maxLength={1000} placeholder="e.g. The client changed their mind" />
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Keep it open
            </Button>
            <Button type="submit" variant="destructive" disabled={status.isPending}>
              {status.isPending && <Spinner />} Cancel the request
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** The request's cards in the four board columns, dragged the same way as a project's. */
function RequestBoard({ detail, onOpenCard }: { detail: RequestDetail; onOpenCard: (id: string | null) => void }) {
  const { data: me } = useMe();
  const move = useMoveCard();
  const [adding, setAdding] = useState(false);
  const [dragging, setDragging] = useState<BoardCard | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }), useSensor(KeyboardSensor));
  const isLead = me?.role === "tl";
  const cards = detail.cards ?? [];
  const roster = detail.roster ?? [];
  const lanes: Record<CardColumn, BoardCard[]> = { todo: [], in_progress: [], review: [], done: [] };
  for (const c of cards) lanes[c.column_key].push(c);

  function onDragEnd(e: DragEndEvent) {
    setDragging(null);
    const card = cards.find((c) => c.id === e.active.id);
    const to = e.over?.id as CardColumn | undefined;
    if (!card || !to || card.column_key === to) return;
    move.mutate(
      { id: card.id, projectId: detail.request.id, column: to, index: 0 },
      { onSuccess: () => toast.success(`Moved to ${CARD_COLUMN_LABELS[to]}`), onError: (err) => toast.error(err.message) },
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>The work</CardTitle>
        <CardDescription>
          {detail.permissions.can_assign
            ? "Drag a card to another column, or open it to assign, comment and attach."
            : detail.can_move_card
              ? "Drag a card to another column as the work moves along."
              : "Where the team has got to with it."}
        </CardDescription>
        {detail.permissions.can_add_card && (
          <div className="ml-auto">
            <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
              <PlusIcon /> {isLead ? "Add task" : "Add another ask"}
            </Button>
          </div>
        )}
      </CardHeader>
      <CardContent>
        <DndContext
          sensors={sensors}
          collisionDetection={pointerWithin}
          onDragStart={(e) => setDragging(cards.find((c) => c.id === e.active.id) ?? null)}
          onDragEnd={onDragEnd}
          onDragCancel={() => setDragging(null)}
        >
          <div className="-mx-1 overflow-x-auto px-1 pb-1">
            <div className="grid min-w-[52rem] grid-cols-4 gap-3">
              {CARD_COLUMNS.map((col) => (
                <Lane key={col} column={col} cards={lanes[col]} canMove={detail.can_move_card} onOpen={onOpenCard} />
              ))}
            </div>
          </div>
          <DragOverlay>{dragging ? <CardFace card={dragging} lifted /> : null}</DragOverlay>
        </DndContext>
      </CardContent>
      <NewCardDialog
        open={adding}
        onOpenChange={setAdding}
        projects={[
          {
            id: detail.request.id,
            isRequest: true,
            label: `CR-${detail.request.number} · ${detail.request.client_name}`,
            members: { ids: roster.map((m) => m.id), names: roster.map((m) => m.name) },
            client: detail.request.client_name,
            order: detail.request.order_id,
            profile: detail.request.profile_name,
          },
        ]}
        isLead={isLead}
      />
    </Card>
  );
}

function Lane({
  column,
  cards,
  canMove,
  onOpen,
}: {
  column: CardColumn;
  cards: BoardCard[];
  canMove: boolean;
  onOpen: (id: string) => void;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: column });
  return (
    <section
      ref={setNodeRef}
      aria-label={CARD_COLUMN_LABELS[column]}
      className={cn("flex min-h-48 flex-col rounded-lg bg-muted/50 p-2", isOver && "ring-2 ring-primary/40")}
    >
      <h2 className="mb-2 flex items-center gap-2 px-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">
        <span className={cn("size-2 rounded-full", COLUMN_TONE[column])} />
        {CARD_COLUMN_LABELS[column]}
        <span className="ml-auto tabular-nums">{cards.length}</span>
      </h2>
      <div className="flex flex-1 flex-col gap-2">
        {cards.map((c) => (
          <DraggableCard key={c.id} card={c} canMove={canMove} onOpen={onOpen} />
        ))}
        {cards.length === 0 && <p className="px-1 py-4 text-center text-xs text-muted-foreground">Nothing here</p>}
      </div>
    </section>
  );
}

function DraggableCard({ card, canMove, onOpen }: { card: BoardCard; canMove: boolean; onOpen: (id: string) => void }) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({ id: card.id, disabled: !canMove });
  return (
    <div
      ref={setNodeRef}
      className={cn(isDragging && "opacity-40")}
      {...attributes}
      {...listeners}
      role="button"
      aria-label={`Card ${card.number}: ${card.title}`}
      onClick={() => onOpen(card.id)}
      onKeyDown={(e) => {
        if (e.key === "Enter") onOpen(card.id);
        listeners?.onKeyDown?.(e);
      }}
    >
      <CardFace card={card} />
    </div>
  );
}
