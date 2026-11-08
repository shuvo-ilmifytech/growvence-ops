"use client";

import { useRef } from "react";
import { useRouter } from "next/navigation";
import { ArrowLeftIcon, MessageSquareIcon } from "lucide-react";
import { toast } from "sonner";
import { useMarkThreadRead, useSendMessage, useThread } from "@/lib/chat";
import { ROLE_LABELS, type Role } from "@/lib/types";
import { CallButton } from "@/components/calls/call-button";
import { Composer, MessageList } from "@/components/messages/message-list";
import { PersonAvatar } from "@/components/messages/person-avatar";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";

/** One-to-one: the person, what was said and the calls between you, and a way to talk instead. */
export function Conversation({ userId, unread }: { userId: string; unread: number }) {
  const router = useRouter();
  const thread = useThread(userId);
  const send = useSendMessage(userId);
  const pane = useRef<HTMLElement>(null);
  useMarkThreadRead(userId, unread);

  const person = thread.data?.user;
  const gone = person?.is_active === false;

  return (
    <section ref={pane} className="relative flex min-h-0 min-w-0 flex-1 flex-col">
      <ChatHeader onBack={() => router.push("/messages")}>
        {thread.isPending ? (
          <Skeleton className="h-10 w-48" />
        ) : (
          <>
            <PersonAvatar name={person?.name ?? ""} />
            <div className="min-w-0 flex-1">
              <div className="truncate font-semibold">{person?.name}</div>
              <div className="truncate text-xs text-muted-foreground">
                {ROLE_LABELS[(person?.role ?? "") as Role] ?? person?.role}
                {gone && " · no longer has access"}
              </div>
            </div>
            <CallButton userId={userId} name={person?.name ?? ""} disabled={gone} />
          </>
        )}
      </ChatHeader>

      <MessageList
        messages={thread.data?.messages ?? []}
        calls={thread.data?.calls ?? []}
        pending={thread.isPending}
        empty={
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <MessageSquareIcon />
              </EmptyMedia>
              <EmptyTitle className="text-sm">Start the conversation</EmptyTitle>
              <EmptyDescription>Say hello to {person?.name?.split(" ")[0] ?? "them"}, send a file, or give them a call.</EmptyDescription>
            </EmptyHeader>
          </Empty>
        }
      />

      <Composer
        pending={send.isPending}
        disabled={gone}
        dropTarget={pane}
        onSend={(m, restore) =>
          send.mutate(m, {
            onError: (err) => {
              restore();
              toast.error(err.message);
            },
          })
        }
      />
    </section>
  );
}

/** The bar across the top of a conversation; on a phone it leads back to the list. */
export function ChatHeader({ onBack, children }: { onBack: () => void; children: React.ReactNode }) {
  return (
    <header className="flex min-h-16 items-center gap-3 border-b bg-background/95 px-3 py-2.5 backdrop-blur md:px-5">
      <Button variant="ghost" size="icon" className="-ml-1 lg:hidden" aria-label="Back to conversations" onClick={onBack}>
        <ArrowLeftIcon />
      </Button>
      {children}
    </header>
  );
}
