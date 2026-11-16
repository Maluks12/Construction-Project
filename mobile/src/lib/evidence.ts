// Taking a photo and sealing it as evidence (docs/api/evidence.md).
//
//   1. take the photo
//   2. shrink it to at most 2048px on the long edge, as a JPEG
//   3. hash exactly those bytes (SHA-256)
//   4. declare it to the API under an id made up here
//   5. upload the bytes straight to storage with the signed link
//   6. complete it: the server re-checks the file and seals it
//
// This version needs signal for steps 4 to 6. Queueing photos offline comes
// with the sync milestone (SYNC).

import * as Crypto from "expo-crypto";
import { File } from "expo-file-system";
import { ImageManipulator, SaveFormat } from "expo-image-manipulator";
import * as ImagePicker from "expo-image-picker";

import { ApiError, api, sitePath } from "./api";
import { now } from "./format";
import type { DeclareRequest, Evidence } from "./types";

const MAX_EDGE = 2048;

export type Photo = { id: string; uri: string; capturedAt: string };

const hex = (buf: ArrayBuffer) =>
  Array.from(new Uint8Array(buf), (b) => b.toString(16).padStart(2, "0")).join("");

// Returns null when the person cancels or refuses the camera.
export async function takeEvidencePhoto(siteId: string): Promise<Photo | null> {
  const permission = await ImagePicker.requestCameraPermissionsAsync();
  if (!permission.granted) {
    throw new ApiError(0, "SiteProof needs the camera to take evidence photos. Allow it in the phone's settings.");
  }
  const shot = await ImagePicker.launchCameraAsync({ mediaTypes: ["images"], quality: 1 });
  if (shot.canceled || shot.assets.length === 0) return null;
  const asset = shot.assets[0];
  const capturedAt = now();

  // Shrink before hashing: the server rejects anything over 2048px, and the
  // hash must be of the exact bytes uploaded.
  const context = ImageManipulator.manipulate(asset.uri);
  if (Math.max(asset.width, asset.height) > MAX_EDGE) {
    context.resize(asset.width >= asset.height ? { width: MAX_EDGE } : { height: MAX_EDGE });
  }
  const image = await context.renderAsync();
  const saved = await image.saveAsync({ format: SaveFormat.JPEG, compress: 0.8 });

  const bytes = await new File(saved.uri).arrayBuffer();
  const sha256 = hex(await Crypto.digest(Crypto.CryptoDigestAlgorithm.SHA256, bytes));

  const id = Crypto.randomUUID();
  const path = sitePath(siteId, `/evidence/${id}`);
  const declared = await api<Evidence>(path, {
    method: "PUT",
    body: {
      content_sha256: sha256,
      size_bytes: bytes.byteLength,
      content_type: "image/jpeg",
      client_captured_at: capturedAt,
    } satisfies DeclareRequest,
  });

  if (declared.upload) {
    let res: Response;
    try {
      // Every header the API listed, unchanged, and exactly these bytes.
      res = await fetch(declared.upload.url, { method: declared.upload.method, headers: declared.upload.headers, body: bytes });
    } catch {
      throw new ApiError(0, "The photo couldn't be uploaded. Check your signal and take it again.");
    }
    if (!res.ok) throw new ApiError(res.status, "Storage refused the photo. Take it again.");
  }

  // A rejected file uses its id up for good; the next photo gets a new one.
  await api<Evidence>(`${path}/complete`, { method: "POST" });
  return { id, uri: saved.uri, capturedAt };
}
