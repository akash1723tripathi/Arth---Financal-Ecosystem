import { api } from "@/lib/api/client";
import { toApiError } from "@arth/api-client";
import { format, subDays } from "date-fns";
import { Directory, File, Paths } from "expo-file-system";
import * as Sharing from "expo-sharing";

// Days of history in the export (same window as before). The server allows up to 366 days.
const EXPORT_WINDOW_DAYS = 30;

/**
 * Download the CSV from GET /v1/transactions/export (built by the server: UTF-8, header row, amounts as decimal
 * strings in each transaction's currency), save it to the cache and open the share sheet.
 * The account filter is applied; the type filter and search are not (the endpoint has no such filters).
 */
export async function exportTransactionsToCsv(accountId?: string | null) {
  const today = new Date();
  const from = format(subDays(today, EXPORT_WINDOW_DAYS), "yyyy-MM-dd");
  const to = format(today, "yyyy-MM-dd");

  const { data, error, response } = await api.GET("/v1/transactions/export", {
    params: { query: { format: "csv", from, to, ...(accountId ? { account_id: accountId } : {}) } },
    parseAs: "text",
  });
  if (error !== undefined || !response.ok || typeof data !== "string") {
    throw toApiError(response, error);
  }

  const lines = data.split(/\r?\n/).filter((l) => l.length > 0);
  const count = Math.max(lines.length - 1, 0);
  if (count === 0) return { count, uri: null };

  const disposition = response.headers.get("Content-Disposition") ?? "";
  const fileName = /filename="([^"]+)"/.exec(disposition)?.[1] ?? `arth-transactions-${from}-${to}.csv`;
  const file = new File(new Directory(Paths.cache), fileName);
  if (file.exists) file.delete();
  file.create();
  file.write(data);

  if (await Sharing.isAvailableAsync()) {
    await Sharing.shareAsync(file.uri, {
      mimeType: "text/csv",
      dialogTitle: "Export transactions",
      UTI: "public.comma-separated-values-text",
    });
  }

  return { count, uri: file.uri };
}
