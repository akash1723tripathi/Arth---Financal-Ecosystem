import { createArthClient } from "@arth/api-client";

import { API_URL } from "@/lib/config";
import { authedFetch } from "@/lib/api/session";

/**
 * The one typed API client for the app (types generated from api/openapi.yaml).
 * Use it only inside TanStack Query hooks (hooks/queries, hooks/mutations), wrapped in `unwrap(...)`.
 */
export const api = createArthClient({ baseUrl: API_URL, fetch: authedFetch });

export { API_URL };
export { unwrap, ApiError, isApiError } from "@arth/api-client";
