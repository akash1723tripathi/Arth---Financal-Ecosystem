# Run the mobile app locally (Windows, Android emulator)

Steps for **Windows PowerShell**. Variable **names** only; get values from your own dashboards and never commit them.

> Since the repoint (2026-11-01) the app talks only to the local **Go API**. No Clerk, Supabase or AI keys are needed.
> The quickest full path (DB → API → demo data → emulator) is **`docs/PREVIEW_LOCALLY.md`**; this page covers the Android tooling.

## 1. One-time prerequisites

1. **Node.js** LTS ≥ 20.19.4 (`node --version`). Node 24 is tested here.
2. **Git** (`git --version`).
3. **pnpm 10** (the repo pins `pnpm@10.34.9` in `package.json`). Pick one option:

   ```powershell
   # Option A, in an *Administrator* PowerShell (writes shims next to node.exe):
   corepack enable pnpm
   # Option B, no admin rights:
   npm install -g pnpm@10.34.9
   pnpm --version   # expect 10.34.9
   ```

4. **Android Studio** (latest stable). In *Settings → Languages & Frameworks → Android SDK* install:
   - SDK Platforms: **Android 16 (API 36)** (Expo SDK 54 compiles against API 36). An API 35 image is fine for the emulator.
   - SDK Tools: Android SDK Platform-Tools, Android Emulator, Android SDK Build-Tools.
5. **Environment variables** (user scope), then restart PowerShell:

   ```powershell
   [Environment]::SetEnvironmentVariable("ANDROID_HOME", "$env:LOCALAPPDATA\Android\Sdk", "User")
   $p = [Environment]::GetEnvironmentVariable("Path", "User")
   [Environment]::SetEnvironmentVariable("Path", "$p;$env:LOCALAPPDATA\Android\Sdk\platform-tools;$env:LOCALAPPDATA\Android\Sdk\emulator", "User")
   # new window:
   adb --version
   emulator -list-avds
   ```

6. **Create an emulator:** Android Studio → *Device Manager* → *Create device* → Pixel 8 (or similar) → a Google Play system image (API 35 or 36) → Finish.

## 2. Install the project

```powershell
cd D:\codes\Projects\Arth-react-native-finance-platform
pnpm install          # from the repo root; uses nodeLinker: hoisted
```

## 3. Environment file (names only)

None is needed on the emulator: the app defaults to `http://10.0.2.2:8080` (the PC's localhost as seen from the emulator).
Optional `apps\mobile\.env` (copy `apps\mobile\.env.example`):

| Name | What |
| --- | --- |
| `EXPO_PUBLIC_API_URL` | API base URL, e.g. `http://192.168.1.23:8080` for a physical phone (`docs/PREVIEW_LOCALLY.md` §6) |
| `EXPO_PUBLIC_PRIVACY_POLICY_URL` | Privacy policy page; empty = placeholder message |

Everything `EXPO_PUBLIC_*` is public (compiled into the bundle). The API itself must be running: `docs/PREVIEW_LOCALLY.md` §2.

## 4. Run on the emulator

```powershell
# start the emulator (or start it from Android Studio's Device Manager)
emulator -avd <your_avd_name>
adb devices                     # should list emulator-5554 as "device"

# from the repo root:
pnpm mobile                     # = expo start (Metro on port 8081)
# then press  a  in the terminal → Expo CLI installs a matching Expo Go on the emulator and opens the app
```

Alternatively, `pnpm mobile:android` starts Metro and opens Android in one step.

**If Expo Go refuses the project** (the Expo Go in the store supports only the newest SDK), let Expo CLI install the SDK-54-compatible
Expo Go (pressing `a` does this). Or build a dev client instead, which needs **JDK 17** and creates `apps/mobile/android/` (gitignored):

```powershell
cd apps\mobile
npx expo run:android
```

## 5. Quick checks without an emulator

```powershell
pnpm verify:mobile     # tsc --noEmit + eslint + expo export --platform android
```

## 6. Troubleshooting

| Symptom | Fix |
| --- | --- |
| `pnpm` not recognised | Section 1 step 3. Then open a new terminal |
| "Can't reach the server" in the app | The API isn't running or isn't reachable from the emulator: `docs/PREVIEW_LOCALLY.md` §5 |
| Stale bundle / odd module errors | `pnpm --filter @arth/mobile start -- --clear` |
| `Unable to resolve module react-native-css-interop/jsx-runtime` | Someone removed `nodeLinker: hoisted` from `pnpm-workspace.yaml`. Restore it, delete `node_modules`, `pnpm install` |
| Emulator can't reach Metro | Allow Node through Windows Firewall (private networks). Check `adb reverse tcp:8081 tcp:8081` |
| `adb` sees no device | Cold-boot the AVD from Device Manager. `adb kill-server; adb start-server` |

## 7. Local Postgres (for the Go backend, next session)

```powershell
Copy-Item .env.example .env      # set POSTGRES_PASSWORD (and optionally user/db/port)
pnpm db:up                       # docker compose up -d db  → postgres:16 on 127.0.0.1:5432
pnpm db:down
```
