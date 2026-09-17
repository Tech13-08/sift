"use client";

import { useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { RequireAuth } from "@/components/require-auth";
import { apiSend, COMMON_TIMEZONES, type Me } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";

function ScheduleForm({ me }: { me: Me }) {
  const router = useRouter();
  const browserTz = useMemo(() => {
    try {
      return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
    } catch {
      return "UTC";
    }
  }, []);

  const [timezone, setTimezone] = useState(me.timezone || browserTz);
  const [time, setTime] = useState(me.digest_local_time || "09:00");
  const [query, setQuery] = useState("");
  const [saving, setSaving] = useState(false);

  const zones = useMemo(() => {
    const all = new Set([...COMMON_TIMEZONES, browserTz, me.timezone].filter(Boolean));
    try {
      for (const z of Intl.supportedValuesOf("timeZone")) all.add(z);
    } catch {
      /* older runtimes */
    }
    const list = [...all].sort();
    const q = query.trim().toLowerCase();
    if (!q) return list.slice(0, 40);
    return list.filter((z) => z.toLowerCase().includes(q)).slice(0, 40);
  }, [browserTz, me.timezone, query]);

  async function onSave(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    const res = await apiSend("/api/settings", "POST", {
      timezone,
      digest_local_time: time,
    });
    setSaving(false);
    if (!res.ok) {
      toast.error(res.error === "timezone" ? "Pick a valid timezone." : "Could not save time.");
      return;
    }
    toast.success("Schedule saved");
    router.push("/app");
    router.refresh();
  }

  return (
    <form onSubmit={onSave} className="flex flex-col gap-8">
      <div>
        <h1 className="font-display text-3xl tracking-tight">Schedule</h1>
        <p className="mt-2 text-muted-foreground">
          Every day at{" "}
          <span className="text-amber">
            {time} in {timezone}
          </span>
        </p>
      </div>

      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="tz-search">Timezone</FieldLabel>
          <Input
            id="tz-search"
            value={query || timezone}
            onChange={(e) => {
              setQuery(e.target.value);
              setTimezone(e.target.value);
            }}
            list="tz-list"
            placeholder="Search timezones"
            required
          />
          <datalist id="tz-list">
            {zones.map((z) => (
              <option key={z} value={z} />
            ))}
          </datalist>
          <FieldDescription>Start typing a city - e.g. Los_Angeles or London.</FieldDescription>
        </Field>
        <Field>
          <FieldLabel htmlFor="digest-time">Local time</FieldLabel>
          <Input
            id="digest-time"
            type="time"
            value={time}
            onChange={(e) => setTime(e.target.value)}
            required
          />
        </Field>
      </FieldGroup>

      <Button type="submit" disabled={saving} className="w-fit">
        {saving ? "Saving…" : "Save schedule"}
      </Button>
    </form>
  );
}

export default function SchedulePage() {
  return (
    <RequireAuth>
      {(me) => <ScheduleForm me={me} />}
    </RequireAuth>
  );
}
