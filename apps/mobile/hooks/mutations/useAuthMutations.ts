import { api, unwrap } from "@/lib/api/client";
import { getStoredRefreshToken, session } from "@/lib/api/session";
import { useMutation } from "@tanstack/react-query";

export function useLogin() {
  return useMutation({
    mutationFn: async (values: { email: string; password: string }) => {
      const pair = await unwrap(
        api.POST("/v1/auth/login", {
          body: { email: values.email.trim(), password: values.password, client: "MOBILE" },
        })
      );
      await session.start(pair);
    },
  });
}

export function useRegister() {
  return useMutation({
    mutationFn: async (values: { email: string; password: string; name: string }) => {
      const pair = await unwrap(
        api.POST("/v1/auth/register", {
          body: { email: values.email.trim(), password: values.password, name: values.name.trim(), client: "MOBILE" },
        })
      );
      await session.start(pair);
    },
  });
}

/** Sign out of this device: revoke the session on the server (best effort), then clear the tokens locally. */
export function useLogout() {
  return useMutation({
    mutationFn: async () => {
      try {
        // Refresh first if needed, so the refresh token we send is the current one (a rotated-away token would
        // make logout a silent no-op on the server, D-23).
        await session.ensureFreshAccessToken();
        const refreshToken = await getStoredRefreshToken();
        if (refreshToken) await unwrap(api.POST("/v1/auth/logout", { body: { refresh_token: refreshToken } }));
      } catch {
        // Offline or already revoked: still sign out locally.
      } finally {
        await session.end();
      }
    },
  });
}

/** Sign out everywhere: revokes every session of the user, including this one. */
export function useLogoutAll() {
  return useMutation({
    mutationFn: async () => {
      await unwrap(api.POST("/v1/auth/logout-all"));
      await session.end();
    },
  });
}

/** Change password. The server revokes all other sessions; this one stays signed in. */
export function useChangePassword() {
  return useMutation({
    mutationFn: (values: { currentPassword: string; newPassword: string }) =>
      unwrap(
        api.POST("/v1/auth/password", {
          body: { current_password: values.currentPassword, new_password: values.newPassword },
        })
      ),
  });
}

/** DELETE /v1/me {password}: deletes the user and all their data on the server, then clears the session. */
export function useDeleteAccount() {
  return useMutation({
    mutationFn: async (password: string) => {
      await unwrap(api.DELETE("/v1/me", { body: { password } }));
      await session.end();
    },
  });
}
