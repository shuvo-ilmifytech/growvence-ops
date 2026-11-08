"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BotIcon, CheckIcon, CopyIcon, KeyRoundIcon, PlugIcon, Trash2Icon } from "lucide-react";
import { toast } from "sonner";
import { api, ApiError } from "@/lib/api";
import { relativeTime } from "@/lib/format";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

type Token = {
  id: string;
  kind: "personal" | "oauth";
  name: string;
  created_at: string;
  last_used_at: string | null;
};

const KEY = ["account", "assistant-tokens"];

function Copy({ text, label }: { text: string; label: string }) {
  const [done, setDone] = useState(false);
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      aria-label={label}
      onClick={() => {
        navigator.clipboard.writeText(text).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        });
      }}
    >
      {done ? <CheckIcon /> : <CopyIcon />}
    </Button>
  );
}

function Snippet({ code }: { code: string }) {
  return (
    <div className="relative">
      <pre className="overflow-x-auto rounded-lg border bg-muted/50 py-3 pr-11 pl-3 text-xs leading-relaxed">
        <code>{code}</code>
      </pre>
      <div className="absolute top-1.5 right-1.5">
        <Copy text={code} label="Copy" />
      </div>
    </div>
  );
}

/**
 * Connect AI assistants (Claude, ChatGPT, Codex) to OpsFlow over MCP. They act as
 * this person, with exactly their access. Connectors sign in with OAuth; command-line
 * tools use a personal token, shown once.
 */
export function AssistantsCard() {
  const qc = useQueryClient();
  const tokens = useQuery({ queryKey: KEY, queryFn: () => api<{ tokens: Token[]; endpoint: string }>("/account/assistant-tokens") });
  const [name, setName] = useState("");
  const [fresh, setFresh] = useState<string | null>(null);
  const endpoint = tokens.data?.endpoint ?? "https://portal.growvence.com/mcp";
  const secret = fresh ?? "YOUR_TOKEN";

  const create = useMutation({
    mutationFn: (n: string) => api<{ token: string }>("/account/assistant-tokens", { method: "POST", body: { name: n } }),
    onSuccess: (r) => {
      setFresh(r.token);
      setName("");
      qc.invalidateQueries({ queryKey: KEY });
    },
    onError: (err) => toast.error(err instanceof ApiError ? err.message : "Could not create the token"),
  });
  const revoke = useMutation({
    mutationFn: (id: string) => api(`/account/assistant-tokens/${id}`, { method: "DELETE" }),
    onSuccess: () => {
      toast.success("Disconnected");
      qc.invalidateQueries({ queryKey: KEY });
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "Could not disconnect"),
  });

  const list = tokens.data?.tokens ?? [];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <BotIcon className="size-4 text-primary" /> AI assistants
        </CardTitle>
        <CardDescription>
          Let Claude, ChatGPT or Codex see your projects and keep them up to date for you. They act as you, with exactly your
          access. Client updates they write are saved as drafts for you to check, and everything they change is marked in
          the activity log.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <Tabs defaultValue="claude">
          <TabsList className="flex-wrap">
            <TabsTrigger value="claude">Claude.ai</TabsTrigger>
            <TabsTrigger value="chatgpt">ChatGPT</TabsTrigger>
            <TabsTrigger value="claude-code">Claude Code</TabsTrigger>
            <TabsTrigger value="codex">Codex</TabsTrigger>
          </TabsList>
          <TabsContent value="claude" className="space-y-2 pt-2 text-sm">
            <p>
              In Claude, open <strong>Settings → Connectors → Add custom connector</strong>, name it OpsFlow and paste this URL. Claude
              opens OpsFlow so you can sign in and allow it.
            </p>
            <Snippet code={endpoint} />
          </TabsContent>
          <TabsContent value="chatgpt" className="space-y-2 pt-2 text-sm">
            <p>
              In ChatGPT, open <strong>Settings → Apps &amp; Connectors</strong>, turn on developer mode if asked, then{" "}
              <strong>Create</strong> a connector with this URL and OAuth sign-in. ChatGPT opens OpsFlow so you can allow it.
            </p>
            <Snippet code={endpoint} />
          </TabsContent>
          <TabsContent value="claude-code" className="space-y-2 pt-2 text-sm">
            <p>Create a token below, then run this once in a terminal:</p>
            <Snippet code={`claude mcp add --transport http opsflow ${endpoint} --header "Authorization: Bearer ${secret}"`} />
          </TabsContent>
          <TabsContent value="codex" className="space-y-2 pt-2 text-sm">
            <p>
              Create a token below, put it in an environment variable called <code>OPSFLOW_TOKEN</code>, and add this to{" "}
              <code>~/.codex/config.toml</code>:
            </p>
            <Snippet code={`[mcp_servers.opsflow]\nurl = "${endpoint}"\nbearer_token_env_var = "OPSFLOW_TOKEN"`} />
          </TabsContent>
        </Tabs>

        {fresh && (
          <div className="space-y-2 rounded-lg border border-emerald-500/40 bg-emerald-500/5 p-3">
            <p className="text-sm font-medium">Your new token. Copy it now: it won&apos;t be shown again.</p>
            <div className="flex items-center gap-1 rounded-md border bg-background px-2">
              <code className="min-w-0 flex-1 truncate py-2 text-xs">{fresh}</code>
              <Copy text={fresh} label="Copy token" />
            </div>
            <p className="text-xs text-muted-foreground">Treat it like a password. The commands above now include it.</p>
          </div>
        )}

        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (name.trim()) create.mutate(name.trim());
          }}
        >
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Token name, e.g. Claude Code on my laptop" maxLength={100} aria-label="Token name" />
          <Button type="submit" disabled={!name.trim() || create.isPending}>
            <KeyRoundIcon /> Create token
          </Button>
        </form>

        <div className="space-y-2">
          <p className="text-sm font-medium">Connected</p>
          {list.length === 0 ? (
            <p className="text-sm text-muted-foreground">Nothing connected yet.</p>
          ) : (
            <ul className="divide-y rounded-lg border">
              {list.map((t) => (
                <li key={t.id} className="flex items-center gap-3 px-3 py-2.5">
                  {t.kind === "oauth" ? <PlugIcon className="size-4 text-muted-foreground" /> : <KeyRoundIcon className="size-4 text-muted-foreground" />}
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-medium">{t.name}</span>
                      <Badge variant="outline" className="shrink-0 text-[10px]">
                        {t.kind === "oauth" ? "Connector" : "Token"}
                      </Badge>
                    </div>
                    <div className="text-xs text-muted-foreground">
                      Added {relativeTime(t.created_at)} · {t.last_used_at ? `last used ${relativeTime(t.last_used_at)}` : "not used yet"}
                    </div>
                  </div>
                  <Button variant="ghost" size="sm" disabled={revoke.isPending} onClick={() => revoke.mutate(t.id)}>
                    <Trash2Icon /> Disconnect
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
