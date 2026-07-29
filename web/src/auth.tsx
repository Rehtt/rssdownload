import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { api, loadSession, setCSRFToken } from "./api";
import type { Session } from "./types";

interface AuthContextValue {
  session: Session | null;
  loading: boolean;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null);
  const [loading, setLoading] = useState(true);

  const clear = useCallback(() => {
    setCSRFToken("");
    setSession(null);
  }, []);

  useEffect(() => {
    loadSession()
      .then(setSession)
      .catch(clear)
      .finally(() => setLoading(false));
  }, [clear]);

  useEffect(() => {
    window.addEventListener("rssdownload:unauthorized", clear);
    return () => window.removeEventListener("rssdownload:unauthorized", clear);
  }, [clear]);

  const login = useCallback(async (username: string, password: string) => {
    const result = await api<Session>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    });
    setCSRFToken(result.csrfToken);
    setSession(result);
  }, []);

  const logout = useCallback(async () => {
    try {
      await api<void>("/auth/logout", { method: "POST" });
    } finally {
      clear();
    }
  }, [clear]);

  const value = useMemo(
    () => ({ session, loading, login, logout }),
    [session, loading, login, logout],
  );
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used inside AuthProvider");
  }
  return context;
}

