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

/** Public privacy policy URL (required for the Play listing). Placeholder until it is published (D-OPEN-11). */
export const PRIVACY_POLICY_URL = process.env.EXPO_PUBLIC_PRIVACY_POLICY_URL || "";
