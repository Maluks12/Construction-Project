// Who is signed in and which site the phone is working on.

import * as SecureStore from "expo-secure-store";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { AppState, Platform } from "react-native";

import { api, clearTokens, hasSession, saveTokens, setSignedOutHandler } from "./api";
import type { Capability, Me, Site, TokenPair } from "./types";

const SITE_KEY = "sp_site";

type Session = {
  status: "loading" | "out" | "in";
  sites: Site[];
  site: Site | null;
  // GET /v1/me (M12): role, names and capabilities. Null until loaded, or
  // when the phone was offline at launch.
  me: Me | null;
  // Display hints only: hide what the role can't do. The API decides, and
  // for a supervisor each capability means "on your own sites".
  can: (capability: Capability) => boolean;
  signIn: (login: string, password: string) => Promise<void>;
  signOut: () => Promise<void>;
  chooseSite: (id: string) => Promise<void>;
};

const Ctx = createContext<Session | null>(null);

export function useSession() {
  const value = useContext(Ctx);
  if (!value) throw new Error("useSession must be used inside SessionProvider");
  return value;
}

// SA numbers are often typed as 082 123 4567; the API wants +27821234567.
function toE164(input: string) {
  const digits = input.replace(/[\s()-]/g, "");
  if (/^0\d{9}$/.test(digits)) return "+27" + digits.slice(1);
  if (/^27\d{9}$/.test(digits)) return "+" + digits;
  return digits;
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<Session["status"]>("loading");
  const [sites, setSites] = useState<Site[]>([]);
  const [siteId, setSiteId] = useState<string | null>(null);
  const [me, setMe] = useState<Me | null>(null);

  // Supervisors only get the sites they've been added to.
  const loadSites = useCallback(async () => {
    const list = (await api<{ sites: Site[] }>("/v1/sites")).sites;
    setSites(list);
    setSiteId((await SecureStore.getItemAsync(SITE_KEY)) ?? null);
  }, []);

  // Read on every call by the API, so a role change shows the next time.
  // A suspended member gets 401, which signs the phone out (api.ts).
  const loadMe = useCallback(async () => {
    setMe(await api<Me>("/v1/me"));
  }, []);

  useEffect(() => {
    setSignedOutHandler(() => {
      setMe(null);
      setStatus("out");
    });
    (async () => {
      if (!(await hasSession())) return setStatus("out");
      setStatus("in");
      // Offline at launch: stay signed in; screens show their own error.
      await Promise.all([loadSites().catch(() => undefined), loadMe().catch(() => undefined)]);
    })();
    return () => setSignedOutHandler(null);
  }, [loadSites, loadMe]);

  // Back in the foreground: read the role again (M12 handover).
  useEffect(() => {
    if (status !== "in") return;
    const sub = AppState.addEventListener("change", (state) => {
      if (state === "active") void loadMe().catch(() => undefined);
    });
    return () => sub.remove();
  }, [status, loadMe]);

  const value = useMemo<Session>(() => {
    const site = sites.find((s) => s.id === siteId) ?? sites.find((s) => s.status === "active") ?? sites[0] ?? null;
    return {
      status,
      sites,
      site,
      me,
      can: (capability) => me?.capabilities.includes(capability) ?? false,
      async signIn(login, password) {
        const id = login.trim();
        const pair = await api<TokenPair>("/v1/auth/login", {
          method: "POST",
          auth: false,
          body: {
            ...(id.includes("@") ? { email: id } : { mobile: toE164(id) }),
            password,
            device_name: Platform.OS === "ios" ? "iPhone app" : "Android app",
          },
        });
        await saveTokens(pair);
        setStatus("in");
        await Promise.all([loadSites().catch(() => undefined), loadMe().catch(() => undefined)]);
      },
      async signOut() {
        // Tell the API so the session is revoked there too; sign out locally
        // even when that can't be reached.
        await api<void>("/v1/auth/logout", { method: "POST" }).catch(() => undefined);
        await clearTokens();
        setSites([]);
        setMe(null);
        setStatus("out");
      },
      async chooseSite(id) {
        setSiteId(id);
        await SecureStore.setItemAsync(SITE_KEY, id);
      },
    };
  }, [status, sites, siteId, me, loadSites, loadMe]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}
