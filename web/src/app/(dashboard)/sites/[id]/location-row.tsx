"use client";

import { useActionState, useState } from "react";

import { archiveLocation, renameLocation } from "@/app/actions/sites";
import { InlineAction, SubmitButton } from "@/components/client";
import { Badge, Input, buttonClass } from "@/components/ui";
import type { Location } from "@/lib/types";

export function LocationRow({
  location,
  siteId,
  canManage,
  hasChildren,
}: {
  location: Location;
  siteId: string;
  canManage: boolean;
  hasChildren: boolean;
}) {
  const [editing, setEditing] = useState(false);
  const [state, action] = useActionState(async (prev: Parameters<typeof renameLocation>[0], form: FormData) => {
    const result = await renameLocation(prev, form);
    if (result?.ok) setEditing(false);
    return result;
  }, null);

  if (editing) {
    return (
      <form action={action} className="flex flex-wrap items-center gap-2">
        <input type="hidden" name="site_id" value={siteId} />
        <input type="hidden" name="location_id" value={location.id} />
        <Input
          name="name"
          required
          maxLength={100}
          autoFocus
          defaultValue={state?.values?.name ?? location.name}
          className="h-8 max-w-64"
          aria-label={`New name for ${location.name}`}
        />
        <SubmitButton size="sm" variant="secondary">Save</SubmitButton>
        <button type="button" className={buttonClass("ghost", "sm")} onClick={() => setEditing(false)}>
          Cancel
        </button>
        {state?.error && <span className="w-full text-xs text-danger">{state.error}</span>}
      </form>
    );
  }

  return (
    <div className="flex min-h-8 items-center justify-between gap-3">
      <span className="flex min-w-0 items-center gap-2">
        <span className="truncate text-sm">{location.name}</span>
        <Badge>{location.kind}</Badge>
      </span>
      {canManage && (
        <span className="flex shrink-0 items-start gap-1">
          <button type="button" className={buttonClass("ghost", "sm")} onClick={() => setEditing(true)}>
            Rename
          </button>
          {!hasChildren && (
            <InlineAction
              action={archiveLocation}
              fields={{ site_id: siteId, location_id: location.id }}
              label="Archive"
              confirmLabel="Archive"
              variant="ghost"
            />
          )}
        </span>
      )}
    </div>
  );
}
