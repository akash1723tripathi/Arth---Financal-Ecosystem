import type { EntryType, Extraction } from "@arth/api-client";
import { ApiError, api, unwrap } from "@/lib/api/client";
import { minorToInput } from "@/lib/money";

/** A transaction draft from an AI extraction, normalised for the form (amount as an input string, never a float). */
export type ExtractionDraft = {
  type: EntryType | null;
  amountInput: string | null;
  /** Currency of `amountInput` (the receipt's, or the user's when the receipt doesn't say). */
  currency: string;
  categoryId: string | null;
  description: string | null;
  occurredOn: string | null; // yyyy-MM-dd
  transcript: string | null;
};

export function extractionToDraft(e: Extraction): ExtractionDraft {
  return {
    type: e.type,
    amountInput: e.amount_minor != null ? minorToInput(e.amount_minor, e.currency) : null,
    currency: e.currency,
    categoryId: e.category_id,
    description: e.description,
    occurredOn: e.occurred_on,
    transcript: e.transcript,
  };
}

/**
 * POST /v1/ai/receipt-extractions (multipart `file`, ≤ 5 MB). The server runs the AI provider behind its
 * interface (the fake driver in dev returns canned data). Nothing is stored; the user reviews and saves.
 * React Native's FormData takes a file as {uri, name, type}.
 */
export async function extractReceipt(uri: string, mimeType: string): Promise<ExtractionDraft> {
  const ext = mimeType.split("/")[1]?.replace("jpeg", "jpg") ?? "jpg";
  const form = new FormData();
  form.append("file", { uri, name: `receipt.${ext}`, type: mimeType } as unknown as Blob);
  const extraction = await unwrap(
    api.POST("/v1/ai/receipt-extractions", {
      body: form as unknown as { file: string },
      bodySerializer: (body) => body as unknown as FormData,
    })
  );
  return extractionToDraft(extraction);
}

/**
 * Voice entry has no server endpoint yet (post-MVP; LLM provider D-OPEN-1). The voice UI is only visible in
 * development builds, where this explains the situation instead of calling a client-side AI key.
 */
export async function extractVoice(_uri: string): Promise<ExtractionDraft> {
  throw new ApiError({
    status: 0,
    code: "not_available",
    message: "Voice entry has no server endpoint yet (post-MVP).",
  });
}
