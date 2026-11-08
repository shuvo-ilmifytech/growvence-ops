"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError, api } from "@/lib/api";
import { useEditRequest } from "@/lib/requests";
import type { ClientRequest, Profile } from "@/lib/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";

/** Sales correct what the client asked for. The team it is with is changed by its leader. */
export function EditRequestDialog({
  open,
  onOpenChange,
  request,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  request: ClientRequest;
}) {
  const edit = useEditRequest(request.id);
  const profileList = useQuery({
    queryKey: ["profiles"],
    queryFn: () => api<{ profiles: Profile[] }>("/profiles").then((r) => r.profiles),
  });
  const [profile, setProfile] = useState(request.profile_id);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const profiles = (profileList.data ?? []).filter((p) => p.is_active || p.id === request.profile_id);

  return (
    <Dialog open={open} onOpenChange={(o) => (onOpenChange(o), o || (setErrors({}), setProfile(request.profile_id)))}>
      <DialogContent className="sm:max-w-lg">
        <form
          className="grid gap-5"
          onSubmit={(e) => {
            e.preventDefault();
            const d = new FormData(e.currentTarget);
            setErrors({});
            edit.mutate(
              {
                profile_id: profile,
                client_name: String(d.get("client_name")).trim(),
                order_id: String(d.get("order_id")).trim() || null,
                title: String(d.get("title")).trim(),
                details: String(d.get("details")).trim(),
              },
              {
                onSuccess: () => {
                  toast.success("Saved. The team has been told it changed.");
                  onOpenChange(false);
                },
                onError: (err) => (err instanceof ApiError && Object.keys(err.fields).length ? setErrors(err.fields) : toast.error(err.message)),
              },
            );
          }}
        >
          <DialogHeader>
            <DialogTitle>Edit CR-{request.number}</DialogTitle>
            <DialogDescription>The team is already working from this, so they&apos;ll be told it changed.</DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field data-invalid={!!errors.profile_id || undefined}>
              <FieldLabel htmlFor="edit-profile">Profile the update goes out on</FieldLabel>
              <Select value={profile} onValueChange={setProfile}>
                <SelectTrigger id="edit-profile" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {profiles.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.name} · {p.platform}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldError>{errors.profile_id && `Profile ${errors.profile_id}`}</FieldError>
            </Field>
            <Field data-invalid={!!errors.client_name || undefined}>
              <FieldLabel htmlFor="edit-client">Client</FieldLabel>
              <Input id="edit-client" name="client_name" required maxLength={200} defaultValue={request.client_name} />
              <FieldError>{errors.client_name && `Client ${errors.client_name}`}</FieldError>
            </Field>
            <Field data-invalid={!!errors.order_id || undefined}>
              <FieldLabel htmlFor="edit-order">Order ID</FieldLabel>
              <Input id="edit-order" name="order_id" maxLength={100} defaultValue={request.order_id ?? ""} className="sm:w-60" />
              <FieldDescription>Optional.</FieldDescription>
              <FieldError>{errors.order_id && `Order ID ${errors.order_id}`}</FieldError>
            </Field>
            <Field data-invalid={!!errors.title || undefined}>
              <FieldLabel htmlFor="edit-title">What the client asked for</FieldLabel>
              <Input id="edit-title" name="title" required maxLength={200} defaultValue={request.title} />
              <FieldError>{errors.title && `Title ${errors.title}`}</FieldError>
            </Field>
            <Field>
              <FieldLabel htmlFor="edit-details">Details</FieldLabel>
              <Textarea id="edit-details" name="details" rows={4} maxLength={20000} defaultValue={request.details} />
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={edit.isPending}>
              {edit.isPending && <Spinner />} Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
