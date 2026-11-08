"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import {
  LayoutTemplateIcon,
  PlusIcon,
  PencilIcon,
  TrashIcon,
  PlayIcon,
  CheckIcon,
  XIcon,
} from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import {
  useTemplates,
  useCreateTemplate,
  useUpdateTemplate,
  useDeleteTemplate,
  useApplyTemplate,
  type ProjectTemplate,
  type TemplateTask,
  type TemplateInput,
} from "@/lib/templates";
import { useMe } from "@/lib/session";

const ALL = "all";

function canEdit(me: { role?: string; is_admin?: boolean } | null | undefined) {
  if (!me) return false;
  return me.is_admin || ["tl", "hr", "ceo"].includes(me.role ?? "");
}

export default function TemplatesPage() {
  const { data: me } = useMe();
  const router = useRouter();
  const [category, setCategory] = useState(ALL);
  const [editOpen, setEditOpen] = useState(false);
  const [editing, setEditing] = useState<ProjectTemplate | null>(null);
  const [applyOpen, setApplyOpen] = useState(false);
  const [applying, setApplying] = useState<ProjectTemplate | null>(null);

  const list = useTemplates(category === ALL ? undefined : category);
  const del = useDeleteTemplate();
  const editable = canEdit(me);
  const categories = list.data?.categories ?? [];
  const templates = list.data?.templates ?? [];

  function openNew() {
    setEditing(null);
    setEditOpen(true);
  }

  function openEdit(t: ProjectTemplate) {
    setEditing(t);
    setEditOpen(true);
  }

  function openApply(t: ProjectTemplate) {
    setApplying(t);
    setApplyOpen(true);
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Project templates"
        description="Reusable blueprints: apply one to spin up a project with its task cards pre-created."
        actions={
          editable && (
            <Button onClick={openNew}>
              <PlusIcon /> New template
            </Button>
          )
        }
      />

      <div className="flex items-center gap-3">
        <Select value={category} onValueChange={setCategory}>
          <SelectTrigger className="w-44" aria-label="Category">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All categories</SelectItem>
            {categories.map((c) => (
              <SelectItem key={c} value={c}>
                {c}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {list.isPending ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-56" />
          ))}
        </div>
      ) : templates.length === 0 ? (
        <Empty className="min-h-[40vh]">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <LayoutTemplateIcon />
            </EmptyMedia>
            <EmptyTitle>No templates yet</EmptyTitle>
            <EmptyDescription>
              {editable
                ? "Create your first template to speed up project setup."
                : "No templates in this category yet."}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {templates.map((t) => (
            <Card key={t.id} className="flex flex-col">
              <CardHeader>
                <div className="flex items-start justify-between gap-2">
                  <CardTitle className="text-base">{t.name}</CardTitle>
                  <Badge variant="secondary">{t.category}</Badge>
                </div>
                {t.description && <CardDescription>{t.description}</CardDescription>}
              </CardHeader>
              <CardContent className="flex-1">
                <p className="mb-2 text-xs font-medium text-muted-foreground uppercase">
                  {t.tasks.length} tasks
                </p>
                <ul className="space-y-1.5">
                  {t.tasks.slice(0, 5).map((task, i) => (
                    <li key={i} className="flex items-start gap-2 text-sm">
                      <CheckIcon className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                      <span className="truncate">{task.title}</span>
                      {task.estimated_days > 0 && (
                        <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                          ~{task.estimated_days}d
                        </span>
                      )}
                    </li>
                  ))}
                  {t.tasks.length > 5 && (
                    <li className="text-xs text-muted-foreground">
                      +{t.tasks.length - 5} more
                    </li>
                  )}
                </ul>
              </CardContent>
              <CardFooter className="gap-2">
                <Button size="sm" onClick={() => openApply(t)}>
                  <PlayIcon /> Use template
                </Button>
                {editable && (
                  <>
                    <Button size="sm" variant="outline" onClick={() => openEdit(t)}>
                      <PencilIcon />
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        if (confirm(`Delete template "${t.name}"?`)) del.mutate(t.id);
                      }}
                    >
                      <TrashIcon />
                    </Button>
                  </>
                )}
              </CardFooter>
            </Card>
          ))}
        </div>
      )}

      <TemplateDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        template={editing}
      />
      <ApplyDialog
        open={applyOpen}
        onOpenChange={setApplyOpen}
        template={applying}
        onApplied={(projectId) => router.push(`/projects/${projectId}`)}
      />
    </div>
  );
}

function TemplateDialog({
  open,
  onOpenChange,
  template,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  template: ProjectTemplate | null;
}) {
  const create = useCreateTemplate();
  const update = useUpdateTemplate();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [category, setCategory] = useState("general");
  const [tasks, setTasks] = useState<TemplateTask[]>([]);

  // Reset when dialog opens / template changes.
  useEffect(() => {
    if (open) {
      setName(template?.name ?? "");
      setDescription(template?.description ?? "");
      setCategory(template?.category ?? "general");
      setTasks(template?.tasks ? [...template.tasks] : []);
    }
  }, [open, template]);

  function addTask() {
    setTasks([...tasks, { title: "", description: "", estimated_days: 1 }]);
  }

  function updateTask(i: number, patch: Partial<TemplateTask>) {
    setTasks(tasks.map((t, j) => (j === i ? { ...t, ...patch } : t)));
  }

  function removeTask(i: number) {
    setTasks(tasks.filter((_, j) => j !== i));
  }

  function onSubmit() {
    const input: TemplateInput = {
      name: name.trim(),
      description: description.trim(),
      category: category.trim() || "general",
      tasks: tasks.filter((t) => t.title.trim()),
    };
    if (!input.name) return;
    if (template) {
      update.mutate(
        { id: template.id, ...input },
        { onSuccess: () => onOpenChange(false) },
      );
    } else {
      create.mutate(input, { onSuccess: () => onOpenChange(false) });
    }
  }

  const pending = create.isPending || update.isPending;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{template ? "Edit template" : "New template"}</DialogTitle>
          <DialogDescription>
            Define the task list. Applying the template creates a project plus one
            board card per task.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <Field>
            <FieldLabel>Name</FieldLabel>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Shopify Store" />
          </Field>
          <Field>
            <FieldLabel>Description</FieldLabel>
            <Textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What is this template for?"
              rows={2}
            />
          </Field>
          <Field>
            <FieldLabel>Category</FieldLabel>
            <Input
              value={category}
              onChange={(e) => setCategory(e.target.value)}
              placeholder="e.g. shopify, web, branding"
            />
          </Field>
          <div>
            <div className="mb-2 flex items-center justify-between">
              <FieldLabel>Tasks</FieldLabel>
              <Button size="sm" variant="outline" onClick={addTask}>
                <PlusIcon /> Add task
              </Button>
            </div>
            <div className="space-y-3">
              {tasks.map((t, i) => (
                <div key={i} className="rounded-lg border p-3 space-y-2">
                  <div className="flex gap-2">
                    <Input
                      value={t.title}
                      onChange={(e) => updateTask(i, { title: e.target.value })}
                      placeholder="Task title"
                      className="flex-1"
                    />
                    <Input
                      type="number"
                      min={0}
                      value={t.estimated_days}
                      onChange={(e) =>
                        updateTask(i, { estimated_days: parseInt(e.target.value) || 0 })
                      }
                      className="w-20"
                      title="Estimated days"
                    />
                    <Button size="icon" variant="ghost" onClick={() => removeTask(i)}>
                      <XIcon />
                    </Button>
                  </div>
                  <Textarea
                    value={t.description}
                    onChange={(e) => updateTask(i, { description: e.target.value })}
                    placeholder="Task description (optional)"
                    rows={2}
                  />
                </div>
              ))}
              {tasks.length === 0 && (
                <p className="text-sm text-muted-foreground">
                  No tasks yet. Add the steps this template should create.
                </p>
              )}
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={onSubmit} disabled={pending || !name.trim()}>
            {template ? "Save changes" : "Create template"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ApplyDialog({
  open,
  onOpenChange,
  template,
  onApplied,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  template: ProjectTemplate | null;
  onApplied: (projectId: string) => void;
}) {
  const apply = useApplyTemplate();
  const [name, setName] = useState("");
  const [clientName, setClientName] = useState("");
  const [orderId, setOrderId] = useState("");

  function onSubmit() {
    if (!template || !name.trim()) return;
    apply.mutate(
      {
        id: template.id,
        project: {
          name: name.trim(),
          client_name: clientName.trim(),
          order_id: orderId.trim(),
        },
      },
      {
        onSuccess: (r) => {
          onOpenChange(false);
          onApplied(r.project_id);
        },
      },
    );
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Use template: {template?.name}</DialogTitle>
          <DialogDescription>
            This will create a new project with {template?.tasks.length ?? 0} task
            cards from the template. Fill in any missing project details on the
            project page afterwards.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <Field>
            <FieldLabel>Project name</FieldLabel>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Acme Shopify Store"
            />
          </Field>
          <Field>
            <FieldLabel>Client name</FieldLabel>
            <Input
              value={clientName}
              onChange={(e) => setClientName(e.target.value)}
              placeholder="Client or company"
            />
          </Field>
          <Field>
            <FieldLabel>Order ID</FieldLabel>
            <Input
              value={orderId}
              onChange={(e) => setOrderId(e.target.value)}
              placeholder="Fiverr order ID (optional)"
            />
          </Field>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={onSubmit} disabled={apply.isPending || !name.trim()}>
            <PlayIcon /> Create project
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
