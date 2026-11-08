"use client";

import { api } from "./api";
import type { ProjectDetail } from "./types";

type Ticket = { upload_id: string; url: string; method: string; headers: Record<string, string> };

/**
 * Three-step upload: ask the API for a presigned URL, PUT the bytes straight to
 * storage (with progress), then ask the API to verify and record it.
 */
export async function uploadProjectFile(
  projectId: string,
  file: File,
  opts: { replaceFileId?: string; onProgress?: (fraction: number) => void } = {},
): Promise<ProjectDetail> {
  const contentType = file.type || "application/octet-stream";
  const ticket = await api<Ticket>(`/projects/${projectId}/files/uploads`, {
    method: "POST",
    body: { filename: file.name, size: file.size, content_type: contentType, replace_file_id: opts.replaceFileId ?? null },
  });

  await new Promise<void>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open(ticket.method, ticket.url);
    for (const [k, v] of Object.entries(ticket.headers)) xhr.setRequestHeader(k, v);
    xhr.upload.onprogress = (e) => e.lengthComputable && opts.onProgress?.(e.loaded / e.total);
    xhr.onload = () => (xhr.status >= 200 && xhr.status < 300 ? resolve() : reject(new Error(`Upload failed (${xhr.status})`)));
    xhr.onerror = () => reject(new Error("Upload failed. Check your connection and try again."));
    xhr.send(file);
  });

  return api<ProjectDetail>(`/projects/${projectId}/files/uploads/${ticket.upload_id}/complete`, { method: "POST" });
}
