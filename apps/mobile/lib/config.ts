import { Platform } from "react-native";

/**
 * Base URL of the Arth Go API. Everything EXPO_PUBLIC_* is compiled into the bundle (public, no secrets).
 *
 * Default for local development:
 * - Android emulator: http://10.0.2.2:8080 (the emulator's own "localhost" is the emulator; 10.0.2.2 is the PC).
 * - iOS simulator / web: http://localhost:8080
 * Physical phone: set EXPO_PUBLIC_API_URL to http://<your PC's LAN IP>:8080 (docs/PREVIEW_LOCALLY.md).
 */
const DEFAULT_API_URL = Platform.OS === "android" ? "http://10.0.2.2:8080" : "http://localhost:8080";

export const API_URL = (process.env.EXPO_PUBLIC_API_URL || DEFAULT_API_URL).replace(/\/+$/, "");

/**
 * TEMPORARY switch while the app moves from the Welth stack to the Go API (removed in the last repoint step).
 * Clerk/Supabase auth and Go auth cannot be mixed, so the whole app runs on one of them: "api" (default) or "supabase".
 */
export const BACKEND: "api" | "supabase" = process.env.EXPO_PUBLIC_BACKEND === "supabase" ? "supabase" : "api";
