"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./api";
import { uploadProjectFile } from "./uploads";
import type { FileVersion, ProjectFile } from "./types";

export type ProjectFilesResponse = { files: ProjectFile[] };
export type ProjectVersionsResponse = { versions: FileVersion[] };

/** Standalone file list for a project (also available via project detail). */
export function useProjectFiles(projectId: string | null) {
  return useQuery({
    queryKey: ["projects", projectId, "files"],
    queryFn: () => api<ProjectFilesResponse>(`/projects/${projectId}/files`),
    enabled: !!projectId,
  });
}

/** All versions of all files on a project. */
export function useProjectFileVersions(projectId: string | null) {
  return useQuery({
    queryKey: ["projects", projectId, "file-versions"],
    queryFn: () => api<ProjectVersionsResponse>(`/projects/${projectId}/files/versions`),
    enabled: !!projectId,
  });
}

export type UploadProgress = { name: string; progress: number; error?: string };

/**
 * Upload a file to a project via presigned S3 URL.
 * If replaceFileId is given, the upload becomes a new version of that file.
 */
export function useUploadProjectFile(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      file,
      replaceFileId,
      onProgress,
    }: {
      file: File;
      replaceFileId?: string;
      onProgress?: (fraction: number) => void;
    }) => uploadProjectFile(projectId, file, { replaceFileId, onProgress }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["projects", projectId] });
      qc.invalidateQueries({ queryKey: ["projects", projectId, "files"] });
      qc.invalidateQueries({ queryKey: ["projects", projectId, "file-versions"] });
    },
  });
}

/** Delete a file (all versions). */
export function useDeleteProjectFile(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (fileId: string) =>
      api(`/projects/${projectId}/files/${fileId}`, { method: "DELETE" }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["projects", projectId] });
      qc.invalidateQueries({ queryKey: ["projects", projectId, "files"] });
      qc.invalidateQueries({ queryKey: ["projects", projectId, "file-versions"] });
    },
  });
}

/** Download URL for a specific file version (redirects to presigned S3 URL). */
export function fileVersionDownloadUrl(versionId: string): string {
  return `/api/files/versions/${versionId}/download`;
}
