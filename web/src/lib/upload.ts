// Uploading a document file (docs/api/files.md): certificates, medical
// certificates, appointment letters, H&S file and vault documents.
//
// The browser sends the file to a Server Action, and this runs on the
// Next.js server:
//   1. hash the bytes (SHA-256);
//   2. declare the file to the API under its id;
//   3. PUT the bytes to the signed storage link, with every header the API
//      listed;
//   4. complete it: the API re-reads the stored bytes and seals them.
// Then the caller sends its record with the file id.
//
// Doing it here rather than in the browser keeps the token on the server and
// needs no CORS rule on the bucket. next.config.ts raises the Server Action
// and proxy body limits so a 20 MiB file gets through whole.

import "server-only";

import { createHash } from "node:crypto";

import { ApiError, api } from "./api";
import type { FileContentType, FileDeclareRequest, FilePurpose, StoredFile } from "./types";

// files.md: PDF, JPEG or PNG, up to 20 MiB.
export const MAX_FILE_BYTES = 20 * 1024 * 1024;
const TYPES: FileContentType[] = ["application/pdf", "image/jpeg", "image/png"];

// The file id comes from the record it belongs to and the file's own hash.
// Submitting the same form again with the same file reuses the declared,
// already sealed file (the API answers 200), and choosing a different file
// after an error gets a new id instead of "file id already used with
// different details". Formatted as a version 4 UUID.
function fileIdFor(recordId: string, sha256: string) {
  const h = createHash("sha256").update(`${recordId}:${sha256}`).digest();
  h[6] = (h[6] & 0x0f) | 0x40;
  h[8] = (h[8] & 0x3f) | 0x80;
  const x = h.subarray(0, 16).toString("hex");
  return `${x.slice(0, 8)}-${x.slice(8, 12)}-${x.slice(12, 16)}-${x.slice(16, 20)}-${x.slice(20)}`;
}

// Returns the verified file's id. Throws ApiError with a readable message,
// so a Server Action can show it like any other API error.
export async function uploadFile(
  file: FormDataEntryValue | null,
  opts: { recordId: string; purpose: FilePurpose; siteId?: string },
): Promise<string> {
  if (!(file instanceof File) || file.size === 0) {
    throw new ApiError(400, "Choose the file to upload.");
  }
  if (file.size > MAX_FILE_BYTES) {
    throw new ApiError(400, "The file is over 20 MB. Upload a smaller scan or PDF.");
  }
  const type = TYPES.find((t) => t === file.type);
  if (!type) throw new ApiError(400, "Upload a PDF, JPEG or PNG file.");

  const bytes = new Uint8Array(await file.arrayBuffer());
  const sha256 = createHash("sha256").update(bytes).digest("hex");
  const id = fileIdFor(opts.recordId, sha256);
  const name = file.name.trim().slice(0, 200);

  const body: FileDeclareRequest = {
    purpose: opts.purpose,
    content_sha256: sha256,
    size_bytes: bytes.byteLength,
    content_type: type,
    // site_id only for the purposes that take one: the API refuses it on
    // the others.
    ...(opts.purpose === "appointment" || opts.purpose === "site_document" ? { site_id: opts.siteId } : {}),
    ...(name ? { file_name: name } : {}),
  };
  const path = `/v1/files/${encodeURIComponent(id)}`;
  const declared = await api<StoredFile>(path, { method: "PUT", body });

  if (declared.status === "verified") return declared.id;
  if (declared.status === "rejected") {
    throw new ApiError(409, "That file was refused by the storage check before. Save it again (as a PDF or image) and upload the new copy.");
  }

  if (declared.upload) {
    let res: Response;
    try {
      res = await fetch(declared.upload.url, {
        method: declared.upload.method,
        headers: declared.upload.headers,
        body: bytes,
        cache: "no-store",
      });
    } catch {
      throw new ApiError(503, "Couldn't reach file storage. Try again in a moment.");
    }
    // 412: an earlier attempt already stored these bytes (the link carries
    // If-None-Match: *), so go straight on to completing it.
    if (!res.ok && res.status !== 412) {
      throw new ApiError(502, "File storage refused the upload. Try again.");
    }
  }

  await api<StoredFile>(`${path}/complete`, { method: "POST" });
  return declared.id;
}
