"use client";

import { useMemo, useState } from "react";
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
import { KanbanIcon, PlusIcon, SearchIcon } from "lucide-react";
import { toast } from "sonner";
import { useAllCards, useMoveCard } from "@/lib/cards";
import { useMe } from "@/lib/session";
import { formatMoney } from "@/lib/format";
import { CARD_COLUMN_LABELS, CARD_COLUMNS, type AllCard, type CardColumn } from "@/lib/types";
import { cn } from "@/lib/utils";
import { PageHeader } from "@/components/page-header";
import { CardDialog, CardFace, COLUMN_TONE, NewCardDialog, type ProjectOption } from "@/components/projects/project-board";
import { NewRequestDialog } from "@/components/requests/new-request-dialog";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";

const ALL = "all";
const projectLabel = (p: { project_number?: number; number?: number; project_name?: string | null; name?: string | null }) =>
  `#${p.project_number ?? p.number} ${(p.project_name ?? p.name) || "Untitled"}`;

/** What the board needs to know about a card's container: a project, or a client request. */
type Container = {
  id: string;
  isRequest: boolean;
  /** How the card names it: the client first, that being how Sales know their work. */
  label: string;
  members: { ids: string[]; names: string[] };
  canCreate: boolean;
  canMove: boolean;
  canAssign: boolean;
  priceLine?: string;
  client?: string | null;
  contact?: string | null;
  order?: string | null;
  profile?: string | null;
};

/**
 * Every card the user can see in one board: client requests from Sales and the Team
 * Leader's tasks, on projects and on client requests for clients with no project.
 */
export default function BoardPage() {
  const { data: me } = useMe();
  const [project, setProject] = useState(ALL);
  const [source, setSource] = useState(ALL);
  const [mine, setMine] = useState(false);
  const [q, setQ] = useState("");
  const [open, setOpen] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [raising, setRaising] = useState(false);
  const [dragging, setDragging] = useState<AllCard | null>(null);
  // One picker over both containers. Its value says which kind the id is ("p:" or "r:"),
  // so the board can ask for that project's cards or that request's.
  const [kind, pickedId] = project === ALL ? [ALL, undefined] : project.split(":");
  const board = useAllCards({
    project: kind === "p" ? pickedId : undefined,
    request: kind === "r" ? pickedId : undefined,
    source: source === ALL ? undefined : source,
    mine,
    q: q.trim() || undefined,
  });
  const move = useMoveCard();
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }), useSensor(KeyboardSensor));

  const data = board.data;
  const ownOnly = !!data?.own_only;
  const isLead = me?.role === "tl";
  const isSales = me?.role === "sales";
  // Projects and client requests are both containers of cards, so the filter, the labels
  // and the add dialog read one list of them.
  const containers = useMemo(() => {
    const out = new Map<string, Container>();
    for (const p of data?.projects ?? []) {
      const who = p.client_contact || p.client_name;
      out.set(p.id, {
        id: p.id,
        isRequest: false,
        label: who ? `${who} · ${p.name || projectLabel(p)}` : projectLabel(p),
        members: { ids: p.member_ids, names: p.member_names },
        canCreate: p.can_create,
        canMove: p.can_move,
        canAssign: p.can_assign,
        priceLine: p.agreed_price ? formatMoney(p.agreed_price, p.currency) : undefined,
        client: p.client_name,
        contact: p.client_contact,
        order: p.order_id,
        profile: p.profile_name,
      });
    }
    for (const r of data?.requests ?? []) {
      out.set(r.id, {
        id: r.id,
        isRequest: true,
        label: `${r.client_name} · CR-${r.number} (no project)`,
        members: { ids: r.member_ids, names: r.member_names },
        canCreate: r.can_create,
        canMove: r.can_move,
        canAssign: r.can_assign,
        client: r.client_name,
        order: r.order_id,
        profile: r.profile_name,
      });
    }
    return out;
  }, [data]);
  const creatable: ProjectOption[] = [...containers.values()]
    .filter((c) => c.canCreate)
    .map((c) => ({
      id: c.id,
      isRequest: c.isRequest,
      label: c.label,
      members: c.members,
      client: c.client,
      contact: c.contact,
      order: c.order,
      profile: c.profile,
      price: c.priceLine,
    }));
  const clientLabel = (containerId: string, fallback: string) => containers.get(containerId)?.label ?? fallback;
  const lanes = useMemo(() => {
    const out: Record<CardColumn, AllCard[]> = { todo: [], in_progress: [], review: [], done: [] };
    for (const c of data?.cards ?? []) out[c.column_key].push(c);
    return out;
  }, [data]);

  function onDragEnd(e: DragEndEvent) {
    setDragging(null);
    const card = data?.cards.find((c) => c.id === e.active.id);
    const to = e.over?.id as CardColumn | undefined;
    if (!card || !to || card.column_key === to) return;
    move.mutate(
      { id: card.id, projectId: card.container_id, column: to, index: 0 },
      { onSuccess: () => toast.success(`Moved to ${CARD_COLUMN_LABELS[to]}`), onError: (err) => toast.error(err.message) },
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={ownOnly ? "Client requests" : "Board"}
        description={
          ownOnly
            ? "Every client request logged by any of the Sales team, and where Ops has it: on a project, or on a client with no project here. Cover for a colleague by adding a request on their work."
            : "Client requests from Sales and the Team Leader's tasks across everything you work on. Drag a card to another column, or open it."
        }
        actions={
          <div className="flex flex-wrap gap-2">
            {isSales && (
              <Button variant="outline" onClick={() => setRaising(true)}>
                <PlusIcon /> Client with no project
              </Button>
            )}
            {creatable.length > 0 && (
              <Button onClick={() => setAdding(true)}>
                <PlusIcon /> {isLead ? "Add task" : "Add client request"}
              </Button>
            )}
          </div>
        }
      />

      <div className="flex flex-col gap-3 md:flex-row md:items-center">
        <InputGroup className="md:max-w-xs">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search cards, projects or clients" aria-label="Search" />
        </InputGroup>
        <Select value={project} onValueChange={setProject}>
          <SelectTrigger className="w-full md:w-64" aria-label="Project">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>Everything</SelectItem>
            {[...containers.values()].map((c) => (
              <SelectItem key={c.id} value={`${c.isRequest ? "r" : "p"}:${c.id}`}>
                {c.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {!ownOnly && (
          <Select value={source} onValueChange={setSource}>
            <SelectTrigger className="w-full md:w-44" aria-label="Kind">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>Requests and tasks</SelectItem>
              <SelectItem value="client">Client requests</SelectItem>
              <SelectItem value="internal">Tasks</SelectItem>
            </SelectContent>
          </Select>
        )}
        {(me?.role === "member" || me?.role === "tl") && (
          <Button variant={mine ? "default" : "outline"} aria-pressed={mine} onClick={() => setMine((m) => !m)}>
            Only mine
          </Button>
        )}
      </div>

      {board.isPending ? (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
          {CARD_COLUMNS.map((c) => (
            <Skeleton key={c} className="h-64" />
          ))}
        </div>
      ) : data && data.projects.length === 0 && data.requests.length === 0 ? (
        <Empty className="min-h-[40vh]">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <KanbanIcon />
            </EmptyMedia>
            <EmptyTitle>Nothing to work on yet</EmptyTitle>
            <EmptyDescription>Cards appear here once you have projects or client requests to work on.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <DndContext
          sensors={sensors}
          collisionDetection={pointerWithin}
          onDragStart={(e) => setDragging(data?.cards.find((c) => c.id === e.active.id) ?? null)}
          onDragEnd={onDragEnd}
          onDragCancel={() => setDragging(null)}
        >
          {/* Columns scroll sideways on narrow screens instead of widening the page. */}
          <div className="-mx-1 overflow-x-auto px-1 pb-1">
            <div className="grid min-w-[52rem] grid-cols-4 gap-3">
              {CARD_COLUMNS.map((col) => (
                <Lane key={col} column={col} cards={lanes[col]} containers={containers} label={clientLabel} onOpen={setOpen} />
              ))}
            </div>
          </div>
          <DragOverlay>{dragging ? <CardFace card={dragging} project={clientLabel(dragging.container_id, projectLabel(dragging))} lifted /> : null}</DragOverlay>
        </DndContext>
      )}

      <NewCardDialog
        open={adding}
        onOpenChange={setAdding}
        projects={creatable}
        isLead={isLead}
        chooseProject
        defaultProject={pickedId}
      />
      <NewRequestDialog open={raising} onOpenChange={setRaising} />
      <CardDialog
        id={open}
        project={(cid) => {
          const c = containers.get(cid);
          return c && { members: c.members, canMove: c.canMove, canAssign: c.canAssign };
        }}
        onClose={() => setOpen(null)}
      />
    </div>
  );
}

function Lane({
  column,
  cards,
  containers,
  label,
  onOpen,
}: {
  column: CardColumn;
  cards: AllCard[];
  containers: Map<string, Container>;
  label: (containerId: string, fallback: string) => string;
  onOpen: (id: string) => void;
}) {
  const { setNodeRef, isOver } = useDroppable({ id: column });
  return (
    <section
      ref={setNodeRef}
      aria-label={CARD_COLUMN_LABELS[column]}
      className={cn("flex min-h-64 flex-col rounded-lg bg-muted/50 p-2", isOver && "ring-2 ring-primary/40")}
    >
      <h2 className="mb-2 flex items-center gap-2 px-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">
        <span className={cn("size-2 rounded-full", COLUMN_TONE[column])} />
        {CARD_COLUMN_LABELS[column]}
        <span className="ml-auto tabular-nums">{cards.length}</span>
      </h2>
      <div className="flex flex-1 flex-col gap-2">
        {cards.map((c) => (
          <DraggableCard key={c.id} card={c} label={label(c.container_id, projectLabel(c))} canMove={!!containers.get(c.container_id)?.canMove} onOpen={onOpen} />
        ))}
        {cards.length === 0 && <p className="px-1 py-4 text-center text-xs text-muted-foreground">Nothing here</p>}
      </div>
    </section>
  );
}

function DraggableCard({ card, label, canMove, onOpen }: { card: AllCard; label: string; canMove: boolean; onOpen: (id: string) => void }) {
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
      <CardFace card={card} project={label} />
    </div>
  );
}
