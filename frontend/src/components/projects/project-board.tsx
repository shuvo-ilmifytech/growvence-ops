"use client";

import { useMemo, useState } from "react";
import {
  closestCorners,
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragOverEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { CalendarIcon, CheckIcon, MessageSquareIcon, PaperclipIcon, PlusIcon, SearchIcon, Trash2Icon, UserRoundIcon, XIcon } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/lib/api";
import {
  useAttachToCard,
  useBoard,
  useCard,
  useCardAssignees,
  useCardComment,
  useCreateCard,
  useMarkCardOpened,
  useMoveCard,
  useRemoveCard,
  useRemoveCardAttachment,
  useUpdateCard,
} from "@/lib/cards";
import { useMe } from "@/lib/session";
import { formatBytes, formatDate, formatDateTime, initials, relativeTime } from "@/lib/format";
import { CARD_COLUMN_LABELS, CARD_COLUMNS, type Card as BoardCard, type CardColumn, type ProjectDetail } from "@/lib/types";

/** The people on a project, for giving cards to. */
export type Members = { ids: string[]; names: string[] };
/**
 * What the card window needs to know about the container a card is on. canMove is dragging
 * a card between columns — on a client request the whole team may; canAssign is handing it
 * to someone, which stays with the Team Leader and whoever already works it.
 */
export type CardProject = { members: Members; canMove: boolean; canAssign: boolean };
/**
 * Something the user can add a card to — a project, or a client request for a client with
 * no project — with what Sales recognise it by (client and order).
 */
export type ProjectOption = {
  id: string;
  label: string;
  /** The container is a client request, so the card is added to it instead. */
  isRequest?: boolean;
  members: Members;
  client?: string | null; // client name
  contact?: string | null; // client username / contact
  order?: string | null;
  profile?: string | null;
  price?: string; // formatted
};
import { cn } from "@/lib/utils";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { CardCollab } from "@/components/projects/card-collab";

type Lanes = Record<CardColumn, BoardCard[]>;

function lanesOf(cards: BoardCard[]): Lanes {
  const lanes: Lanes = { todo: [], in_progress: [], review: [], done: [] };
  for (const c of cards) lanes[c.column_key].push(c);
  return lanes;
}

export const COLUMN_TONE: Record<CardColumn, string> = {
  todo: "bg-slate-400",
  in_progress: "bg-sky-500",
  review: "bg-amber-500",
  done: "bg-emerald-500",
};

/** The project board: client requests from Sales and the Team Leader's tasks in four columns. */
export function ProjectBoard({ detail, openCardId, onOpenCard }: { detail: ProjectDetail; openCardId: string | null; onOpenCard: (id: string | null) => void }) {
  const p = detail.project;
  const board = useBoard(p.id);
  const move = useMoveCard();
  const [lanes, setLanes] = useState<Lanes | null>(null);
  const [dragging, setDragging] = useState<BoardCard | null>(null);
  const [adding, setAdding] = useState(false);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  // Server order is the truth; local lanes only hold a drag in progress.
  const serverLanes = useMemo(() => lanesOf(board.data?.cards ?? []), [board.data]);
  const view = lanes ?? serverLanes;
  const canMove = !!board.data?.can_move;
  const canAssign = !!board.data?.can_assign;
  const ownOnly = !!board.data?.own_only;
  const total = board.data?.cards.length ?? 0;

  const columnOf = (id: string): CardColumn | undefined =>
    (CARD_COLUMNS as string[]).includes(id) ? (id as CardColumn) : CARD_COLUMNS.find((c) => view[c].some((x) => x.id === id));

  function onDragStart(e: DragStartEvent) {
    setLanes(serverLanes);
    setDragging(board.data?.cards.find((c) => c.id === e.active.id) ?? null);
  }

  function onDragOver(e: DragOverEvent) {
    const from = columnOf(String(e.active.id));
    const to = e.over ? columnOf(String(e.over.id)) : undefined;
    if (!from || !to || from === to) return;
    setLanes((cur) => {
      const l = cur ?? serverLanes;
      const card = l[from].find((c) => c.id === e.active.id);
      if (!card) return l;
      const overIndex = l[to].findIndex((c) => c.id === e.over?.id);
      const target = [...l[to]];
      target.splice(overIndex < 0 ? target.length : overIndex, 0, { ...card, column_key: to });
      return { ...l, [from]: l[from].filter((c) => c.id !== card.id), [to]: target };
    });
  }

  function onDragEnd(e: DragEndEvent) {
    const id = String(e.active.id);
    const col = columnOf(id);
    setDragging(null);
    if (!col || !e.over) {
      setLanes(null);
      return;
    }
    const list = [...view[col]];
    const from = list.findIndex((c) => c.id === id);
    let to = list.findIndex((c) => c.id === e.over?.id);
    if (to < 0) to = list.length - 1;
    list.splice(to, 0, ...list.splice(from, 1));
    const original = board.data?.cards.find((c) => c.id === id);
    setLanes({ ...view, [col]: list });
    if (original && original.column_key === col && serverLanes[col].findIndex((c) => c.id === id) === to) {
      setLanes(null);
      return;
    }
    move.mutate(
      { id, projectId: p.id, column: col, index: to },
      {
        onError: (err) => toast.error(err.message),
        onSettled: () => setLanes(null),
        onSuccess: () => original && original.column_key !== col && toast.success(`Moved to ${CARD_COLUMN_LABELS[col]}`),
      },
    );
  }

  const addLabel = ownOnly ? "Add client request" : "Add task";

  return (
    <Card>
      <CardHeader>
        <CardTitle>{ownOnly ? "Client requests" : "Board"}</CardTitle>
        <CardDescription>
          {ownOnly
            ? "What the client asked for, logged by any of the Sales team, and where Ops has each request."
            : "Client requests from Sales and the Team Leader's tasks. Drag cards between columns, or open one to move it."}
        </CardDescription>
        {board.data?.can_create && (
          <CardAction>
            <Button size="sm" onClick={() => setAdding(true)}>
              <PlusIcon /> {addLabel}
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent>
        {board.isPending ? (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
            {CARD_COLUMNS.map((c) => (
              <Skeleton key={c} className="h-40" />
            ))}
          </div>
        ) : total === 0 && !board.data?.can_create ? (
          <p className="text-sm text-muted-foreground">No cards yet.</p>
        ) : (
          <DndContext sensors={sensors} collisionDetection={closestCorners} onDragStart={onDragStart} onDragOver={onDragOver} onDragEnd={onDragEnd} onDragCancel={() => (setLanes(null), setDragging(null))}>
            {/* Columns scroll sideways on narrow screens instead of widening the page. */}
            <div className="-mx-1 overflow-x-auto px-1 pb-1">
              <div className="grid min-w-[44rem] grid-cols-4 gap-3">
                {CARD_COLUMNS.map((col) => (
                  <Lane key={col} column={col} cards={view[col]} canMove={canMove} onOpen={onOpenCard} />
                ))}
              </div>
            </div>
            <DragOverlay>{dragging ? <CardFace card={dragging} lifted /> : null}</DragOverlay>
          </DndContext>
        )}
      </CardContent>
      <NewCardDialog
        open={adding}
        onOpenChange={setAdding}
        projects={[{ id: p.id, label: `#${p.number} ${p.name ?? ""}`, members: { ids: p.member_ids, names: p.member_names } }]}
        isLead={!ownOnly}
      />
      <CardDialog id={openCardId} project={() => ({ members: { ids: p.member_ids, names: p.member_names }, canMove, canAssign })} onClose={() => onOpenCard(null)} />
    </Card>
  );
}

function Lane({ column, cards, canMove, onOpen }: { column: CardColumn; cards: BoardCard[]; canMove: boolean; onOpen: (id: string) => void }) {
  const { setNodeRef, isOver } = useDroppable({ id: column, disabled: !canMove });
  return (
    <section aria-label={CARD_COLUMN_LABELS[column]} className={cn("flex min-h-40 flex-col rounded-lg bg-muted/50 p-2", isOver && "ring-2 ring-primary/40")}>
      <h3 className="mb-2 flex items-center gap-2 px-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">
        <span className={cn("size-2 rounded-full", COLUMN_TONE[column])} />
        {CARD_COLUMN_LABELS[column]}
        <span className="ml-auto tabular-nums">{cards.length}</span>
      </h3>
      <SortableContext id={column} items={cards.map((c) => c.id)} strategy={verticalListSortingStrategy}>
        <div ref={setNodeRef} className="flex flex-1 flex-col gap-2">
          {cards.map((c) => (
            <SortableCard key={c.id} card={c} canMove={canMove} onOpen={onOpen} />
          ))}
          {cards.length === 0 && <p className="px-1 py-4 text-center text-xs text-muted-foreground">Nothing here</p>}
        </div>
      </SortableContext>
    </section>
  );
}

function SortableCard({ card, canMove, onOpen }: { card: BoardCard; canMove: boolean; onOpen: (id: string) => void }) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: card.id, disabled: !canMove });
  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
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

export function isLate(card: BoardCard) {
  return !!card.due_date && card.column_key !== "done" && card.due_date < new Date().toISOString().slice(0, 10);
}

export function CardFace({ card, lifted, project }: { card: BoardCard; lifted?: boolean; project?: string }) {
  return (
    <div className={cn("cursor-pointer rounded-md border bg-card p-2.5 text-left shadow-xs transition-colors hover:border-primary/40", lifted && "rotate-1 shadow-lg")}>
      {project && <div className="mb-1 truncate text-[11px] font-medium text-primary">{project}</div>}
      <div className="flex items-start gap-2">
        <span className="min-w-0 flex-1 text-sm leading-snug font-medium break-words">{card.title}</span>
        <span className="font-mono text-[10px] text-muted-foreground">#{card.number}</span>
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
        {card.source === "client" && (
          <Badge variant="outline" className="h-5 border-0 bg-sky-50 px-1.5 text-[10px] text-sky-700 ring-1 ring-sky-600/15 ring-inset dark:bg-sky-950 dark:text-sky-300">
            From client
          </Badge>
        )}
        {card.due_date && (
          <span className={cn("inline-flex items-center gap-0.5", isLate(card) && "font-medium text-red-600")}>
            <CalendarIcon className="size-3" /> {formatDate(card.due_date)}
          </span>
        )}
        {card.comment_count > 0 && (
          <span className="inline-flex items-center gap-0.5">
            <MessageSquareIcon className="size-3" /> {card.comment_count}
          </span>
        )}
        {card.attachment_count > 0 && (
          <span className="inline-flex items-center gap-0.5" title="Attachments">
            <PaperclipIcon className="size-3" /> {card.attachment_count}
          </span>
        )}
        <span className="ml-auto flex -space-x-1.5">
          {card.assignee_names.slice(0, 3).map((n) => (
            <Avatar key={n} className="size-5 ring-2 ring-card">
              <AvatarFallback className="bg-accent text-[9px] font-medium text-accent-foreground" title={n}>
                {initials(n)}
              </AvatarFallback>
            </Avatar>
          ))}
        </span>
      </div>
    </div>
  );
}

// ── New card ───────────────────────────────────────────────

/**
 * Adds a client request (Sales) or a task (Team Leader). On a project's own board the project is
 * fixed; on the all-projects board (chooseProject) the user picks it, pre-filled when obvious.
 */
export function NewCardDialog({
  open,
  onOpenChange,
  projects,
  isLead,
  chooseProject,
  defaultProject,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  projects: ProjectOption[];
  isLead: boolean;
  chooseProject?: boolean;
  defaultProject?: string;
}) {
  const create = useCreateCard();
  const [files, setFiles] = useState<File[]>([]);
  const [assignees, setAssignees] = useState<string[]>([]);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const preset = defaultProject && projects.some((x) => x.id === defaultProject) ? defaultProject : projects.length === 1 ? projects[0].id : "";
  const [choice, setPicked] = useState<string | null>(null);
  const picked = choice ?? preset;
  const project = chooseProject ? projects.find((x) => x.id === picked) : projects[0];
  const label = isLead ? "Add task" : "Add client request";

  return (
    <Dialog open={open} onOpenChange={(o) => (onOpenChange(o), o || (setErrors({}), setAssignees([]), setPicked(null), setFiles([])))}>
      <DialogContent className="sm:max-w-lg">
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            const d = new FormData(e.currentTarget);
            setErrors({});
            if (!project) {
              setErrors({ project: "Pick a project" });
              return;
            }
            create.mutate(
              {
                projectId: project.id,
                isRequest: project.isRequest,
                title: String(d.get("title")).trim(),
                description: String(d.get("description")).trim(),
                due_date: String(d.get("due_date")) || null,
                ...(isLead && assignees.length ? { assignee_ids: assignees } : {}),
                files,
              },
              {
                onSuccess: (res) => {
                  toast.success(isLead ? "Task added to To do" : "Request added. The Team Leader and members have been told.");
                  if (res.failed.length) toast.error(`Couldn't attach ${res.failed.join(", ")}. Open the card and try again.`);
                  onOpenChange(false);
                  setAssignees([]);
                  setPicked(null);
                  setFiles([]);
                },
                onError: (err) => (err instanceof ApiError && Object.keys(err.fields).length ? setErrors(err.fields) : toast.error(err.message)),
              },
            );
          }}
        >
          <DialogHeader>
            <DialogTitle>{label}</DialogTitle>
            <DialogDescription>
              {isLead ? "A task for the team. It starts in To do." : "Write down what the client asked for. Ops picks it up from To do."}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            {chooseProject && (
              <Field data-invalid={!!errors.project || undefined}>
                <FieldLabel htmlFor="project-search">Client / project</FieldLabel>
                <ProjectSearch options={projects} value={picked} onChange={(v) => (setPicked(v), setAssignees([]))} />
                <FieldError>{errors.project}</FieldError>
              </Field>
            )}
            <Field data-invalid={!!errors.title || undefined}>
              <FieldLabel htmlFor="card-title">{isLead ? "Task" : "Request"}</FieldLabel>
              <Input id="card-title" name="title" required maxLength={200} autoFocus={!chooseProject} placeholder={isLead ? "e.g. Set up the theme" : "e.g. Change the header colour to navy"} />
              <FieldError>{errors.title && `Title ${errors.title}`}</FieldError>
            </Field>
            <Field>
              <FieldLabel htmlFor="card-desc">Details</FieldLabel>
              <Textarea id="card-desc" name="description" rows={4} maxLength={20000} placeholder={isLead ? "Anything the member needs to know" : "Paste or summarise the client's message"} />
            </Field>
            <Field data-invalid={!!errors.due_date || undefined}>
              <FieldLabel htmlFor="card-due">Due date</FieldLabel>
              <Input id="card-due" name="due_date" type="date" className="w-44" />
              <FieldError>{errors.due_date && `Due date ${errors.due_date}`}</FieldError>
            </Field>
            <Field>
              <FieldLabel htmlFor="card-files">{isLead ? "Attachments" : "Files from the client"}</FieldLabel>
              <Input
                id="card-files"
                type="file"
                multiple
                onChange={(e) => {
                  // Read the list before clearing the input: the state updater runs later.
                  const picked = Array.from(e.target.files ?? []);
                  e.target.value = "";
                  setFiles((cur) => [...cur, ...picked]);
                }}
              />
              {files.length > 0 && (
                <ul className="space-y-1">
                  {files.map((f, i) => (
                    <li key={`${f.name}-${i}`} className="flex items-center gap-2 text-sm">
                      <PaperclipIcon className="size-3.5 text-muted-foreground" />
                      <span className="min-w-0 flex-1 truncate">{f.name}</span>
                      <span className="text-xs text-muted-foreground">{formatBytes(f.size)}</span>
                      <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove ${f.name}`} onClick={() => setFiles((cur) => cur.filter((_, j) => j !== i))}>
                        <XIcon />
                      </Button>
                    </li>
                  ))}
                </ul>
              )}
            </Field>
            {isLead && project && project.members.ids.length > 0 && (
              <Field data-invalid={!!errors.assignee_ids || undefined}>
                <FieldLabel>Give it to</FieldLabel>
                <MemberToggles members={project.members} value={assignees} onChange={setAssignees} />
                <FieldError>{errors.assignee_ids}</FieldError>
              </Field>
            )}
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending && <Spinner />} {create.isPending && files.length ? "Uploading…" : "Add"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

/** Find a project by what Sales know: the order ID, the client's username or name, or the project name. */
function ProjectSearch({ options, value, onChange }: { options: ProjectOption[]; value: string; onChange: (id: string) => void }) {
  const [q, setQ] = useState("");
  const needle = q.trim().toLowerCase();
  const matches = options
    .filter((o) => !needle || [o.order, o.contact, o.client, o.label, o.profile].some((f) => f?.toLowerCase().includes(needle)))
    .slice(0, 50);
  return (
    <div className="grid gap-2">
      <InputGroup>
        <InputGroupAddon>
          <SearchIcon />
        </InputGroupAddon>
        <InputGroupInput
          id="project-search"
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search by order ID or client username"
          autoComplete="off"
          autoFocus
        />
      </InputGroup>
      <div role="listbox" aria-label="Projects" className="max-h-56 divide-y overflow-y-auto rounded-lg border">
        {matches.map((o) => {
          const on = o.id === value;
          return (
            <button
              key={o.id}
              type="button"
              role="option"
              aria-selected={on}
              onClick={() => onChange(o.id)}
              className={cn("flex w-full items-start gap-2 px-3 py-2 text-left hover:bg-muted/60", on && "bg-primary/10")}
            >
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">
                  {o.contact || o.client || "No client yet"}
                  {o.contact && o.client && o.client !== o.contact && <span className="font-normal text-muted-foreground"> · {o.client}</span>}
                </span>
                <span className="block truncate text-xs text-muted-foreground">{[o.label, o.price, o.profile].filter(Boolean).join(" · ")}</span>
              </span>
              {o.order && <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{o.order}</span>}
              {on && <CheckIcon className="mt-0.5 size-4 shrink-0 text-primary" />}
            </button>
          );
        })}
        {matches.length === 0 && <p className="px-3 py-4 text-center text-sm text-muted-foreground">No project matches “{q}”.</p>}
      </div>
    </div>
  );
}

function MemberToggles({ members, value, onChange, disabled }: { members: Members; value: string[]; onChange: (v: string[]) => void; disabled?: boolean }) {
  return (
    <div className="flex flex-wrap gap-2">
      {members.ids.map((id, i) => {
        const on = value.includes(id);
        return (
          <button
            key={id}
            type="button"
            disabled={disabled}
            aria-pressed={on}
            onClick={() => onChange(on ? value.filter((x) => x !== id) : [...value, id])}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-sm transition-colors disabled:opacity-50",
              on ? "border-primary bg-primary/10 text-foreground" : "text-muted-foreground hover:bg-muted",
            )}
          >
            <UserRoundIcon className="size-3.5" /> {members.names[i]}
          </button>
        );
      })}
    </div>
  );
}

// ── Card detail ────────────────────────────────────────────

export function CardDialog({ id, project, onClose }: { id: string | null; project: (containerId: string) => CardProject | undefined; onClose: () => void }) {
  const view = useCard(id);
  useMarkCardOpened(id);
  return (
    <Dialog open={!!id} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-xl">
        {view.isPending ? (
          <>
            <DialogTitle className="sr-only">Loading card</DialogTitle>
            <Skeleton className="h-48" />
          </>
        ) : view.isError ? (
          <DialogHeader>
            <DialogTitle>Card not found</DialogTitle>
            <DialogDescription>It may have been removed, or you don&apos;t have access to it.</DialogDescription>
          </DialogHeader>
        ) : (
          <CardBody
            key={view.data.card.id}
            view={view.data}
            members={project(view.data.card.container_id)?.members ?? { ids: [], names: [] }}
            canMove={!!project(view.data.card.container_id)?.canMove}
            canAssign={!!project(view.data.card.container_id)?.canAssign}
            onClose={onClose}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function CardBody({ view, members, canMove, canAssign, onClose }: { view: NonNullable<ReturnType<typeof useCard>["data"]>; members: Members; canMove: boolean; canAssign: boolean; onClose: () => void }) {
  const c = view.card;
  // The container the card is on: its project, or the client request it was raised under.
  const pid = c.container_id;
  const update = useUpdateCard(pid, c.id);
  const assign = useCardAssignees(pid, c.id);
  const comment = useCardComment(pid, c.id);
  const move = useMoveCard();
  const remove = useRemoveCard(pid);
  const [editing, setEditing] = useState(false);
  const [body, setBody] = useState("");
  // Show the pick right away while it saves; the fresh card replaces it.
  const assignees = assign.isPending && assign.variables ? assign.variables : c.assignee_ids;

  return (
    <div className="grid gap-5">
      <DialogHeader>
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <span className="font-mono">#{c.number}</span>
          <span className="inline-flex items-center gap-1">
            <span className={cn("size-2 rounded-full", COLUMN_TONE[c.column_key])} /> {CARD_COLUMN_LABELS[c.column_key]}
          </span>
          <span>· {c.source === "client" ? "Client request" : "Task"} added by {c.created_by_name} {relativeTime(c.created_at)}</span>
        </div>
        {editing ? null : <DialogTitle className="text-lg leading-snug">{c.title}</DialogTitle>}
        {!editing && <DialogDescription className="sr-only">Card details</DialogDescription>}
      </DialogHeader>

      {editing ? (
        <form
          className="grid gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            const d = new FormData(e.currentTarget);
            update.mutate(
              { title: String(d.get("title")), description: String(d.get("description")), due_date: String(d.get("due_date")) },
              { onSuccess: () => (setEditing(false), toast.success("Card saved")), onError: (err) => toast.error(err.message) },
            );
          }}
        >
          <DialogTitle className="sr-only">Edit card</DialogTitle>
          <DialogDescription className="sr-only">Change the title, details and due date</DialogDescription>
          <Input name="title" defaultValue={c.title} required maxLength={200} aria-label="Title" />
          <Textarea name="description" defaultValue={c.description} rows={5} maxLength={20000} aria-label="Details" />
          <div className="flex items-center gap-2">
            <Input name="due_date" type="date" defaultValue={c.due_date ?? ""} className="w-44" aria-label="Due date" />
            <Button type="button" variant="outline" className="ml-auto" onClick={() => setEditing(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={update.isPending}>
              {update.isPending && <Spinner />} Save
            </Button>
          </div>
        </form>
      ) : (
        <div className="space-y-3">
          {c.description ? (
            <p className="text-sm leading-relaxed whitespace-pre-wrap">{c.description}</p>
          ) : (
            <p className="text-sm text-muted-foreground">No details.</p>
          )}
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <span className={cn("inline-flex items-center gap-1 text-muted-foreground", isLate(c) && "font-medium text-red-600")}>
              <CalendarIcon className="size-4" /> {c.due_date ? `Due ${formatDate(c.due_date)}` : "No due date"}
            </span>
            {c.can_edit && (
              <Button variant="outline" size="sm" className="ml-auto" onClick={() => setEditing(true)}>
                Edit
              </Button>
            )}
          </div>
        </div>
      )}

      {(canMove || canAssign) && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          {/* The team moves a card between columns; only the leader (or whoever already
              works it) hands it to someone, so the two controls stand on their own. */}
          {canMove && (
            <Field>
              <FieldLabel>Column</FieldLabel>
              <Select
                value={c.column_key}
                onValueChange={(v) =>
                  move.mutate(
                    { id: c.id, projectId: pid, column: v as CardColumn, index: 0 },
                    { onSuccess: () => toast.success(`Moved to ${CARD_COLUMN_LABELS[v as CardColumn]}`), onError: (err) => toast.error(err.message) },
                  )
                }
              >
                <SelectTrigger className="w-full" aria-label="Move to column">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {CARD_COLUMNS.map((col) => (
                    <SelectItem key={col} value={col}>
                      {CARD_COLUMN_LABELS[col]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}
          {canAssign && (
            <Field>
              <FieldLabel>Working on it</FieldLabel>
              <MemberToggles
                members={members}
                value={assignees}
                disabled={assign.isPending}
                onChange={(v) => assign.mutate(v, { onError: (err) => toast.error(err.message) })}
              />
            </Field>
          )}
        </div>
      )}
      {!canAssign && c.assignee_names.length > 0 && (
        <p className="text-sm text-muted-foreground">Working on it: {c.assignee_names.join(", ")}</p>
      )}

      <CardAttachments view={view} />

      <div className="space-y-3 border-t pt-4">
        <h4 className="text-sm font-medium">Comments</h4>
        {view.comments.length === 0 && <p className="text-sm text-muted-foreground">No comments yet.</p>}
        <ul className="space-y-3">
          {view.comments.map((m) => (
            <li key={m.id} className="flex gap-2">
              <Avatar className="size-6">
                <AvatarFallback className="bg-accent text-[10px] font-medium text-accent-foreground">{initials(m.author_name)}</AvatarFallback>
              </Avatar>
              <div className="min-w-0">
                <div className="text-xs text-muted-foreground">
                  <span className="font-medium text-foreground">{m.author_name}</span> · <span title={formatDateTime(m.created_at)}>{relativeTime(m.created_at)}</span>
                </div>
                <p className="text-sm whitespace-pre-wrap">{m.body}</p>
              </div>
            </li>
          ))}
        </ul>
        {c.can_comment && (
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (!body.trim()) return;
              comment.mutate(body.trim(), { onSuccess: () => setBody(""), onError: (err) => toast.error(err.message) });
            }}
          >
            <Textarea value={body} onChange={(e) => setBody(e.target.value)} rows={2} placeholder="Write a comment…" aria-label="Comment" maxLength={10000} />
            <Button type="submit" variant="outline" className="self-end" disabled={comment.isPending || !body.trim()}>
              Send
            </Button>
          </form>
        )}
      </div>

      <CardCollab card={c} projectId={pid} />

      {c.can_remove && (
        <div className="flex justify-end border-t pt-4">
          <Button
            variant="ghost"
            className="text-destructive hover:text-destructive"
            disabled={remove.isPending}
            onClick={() => remove.mutate(c.id, { onSuccess: () => (toast.success("Card removed"), onClose()), onError: (err) => toast.error(err.message) })}
          >
            <Trash2Icon /> Remove card
          </Button>
        </div>
      )}
    </div>
  );
}

function CardAttachments({ view }: { view: NonNullable<ReturnType<typeof useCard>["data"]> }) {
  const c = view.card;
  const { data: me } = useMe();
  const attach = useAttachToCard(c.container_id, c.id);
  const remove = useRemoveCardAttachment(c.container_id, c.id);
  if (!view.attachments.length && !view.can_attach) return null;
  return (
    <div className="space-y-2 border-t pt-4">
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-medium">Attachments</h4>
        {view.can_attach && (
          <Button asChild variant="outline" size="sm" disabled={attach.isPending}>
            <label className="cursor-pointer">
              {attach.isPending ? <Spinner /> : <PaperclipIcon />} Add files
              <input
                type="file"
                multiple
                className="sr-only"
                onChange={async (e) => {
                  const list = Array.from(e.target.files ?? []);
                  e.target.value = "";
                  for (const f of list) {
                    await attach.mutateAsync(f).catch((err: Error) => toast.error(`${f.name}: ${err.message}`));
                  }
                }}
              />
            </label>
          </Button>
        )}
      </div>
      {view.attachments.length === 0 ? (
        <p className="text-sm text-muted-foreground">No files yet.</p>
      ) : (
        <ul className="divide-y rounded-lg border">
          {view.attachments.map((a) => (
            <li key={a.id} className="flex items-center gap-2 px-3 py-2 text-sm">
              <PaperclipIcon className="size-4 shrink-0 text-muted-foreground" />
              <a href={`/api/files/versions/${a.version_id}/download`} className="min-w-0 flex-1 truncate font-medium hover:underline">
                {a.original_name}
              </a>
              <span className="hidden text-xs text-muted-foreground sm:inline">
                {formatBytes(a.size_bytes)} · {a.uploaded_by_name}
              </span>
              {(a.uploaded_by === me?.id || me?.role === "tl") && view.can_attach && (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`Remove ${a.original_name}`}
                  disabled={remove.isPending}
                  onClick={() => remove.mutate(a.id, { onError: (err) => toast.error(err.message) })}
                >
                  <XIcon />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
