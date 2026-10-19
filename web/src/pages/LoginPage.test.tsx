import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { LoginPage } from "./LoginPage";

const login = vi.fn().mockResolvedValue(undefined);

vi.mock("../auth", () => ({
  useAuth: () => ({ login }),
}));

vi.mock("../api", () => ({ api: vi.fn() }));

beforeEach(() => {
  vi.mocked(api).mockResolvedValue({ password: true, oidc: false });
  window.history.replaceState({}, "", "/login");
  login.mockClear();
});

afterEach(cleanup);

describe("LoginPage", () => {
  it("submits administrator credentials", async () => {
    render(<LoginPage />);
    fireEvent.change(screen.getByLabelText("密码"), {
      target: { value: "password123" },
    });
    fireEvent.click(screen.getByRole("button", { name: /登\s*录/ }));
    await waitFor(() => {
      expect(login).toHaveBeenCalledWith("admin", "password123");
    });
  });

  it("shows an enabled OIDC entry with a full-page navigation link", async () => {
    vi.mocked(api).mockResolvedValue({ password: true, oidc: true });
    render(<LoginPage />);
    const link = await screen.findByRole("link", { name: "OIDC 登录" });
    expect(link).toHaveAttribute("href", "/api/v1/auth/oidc/login");
    expect(api).toHaveBeenCalledWith("/auth/methods");
    expect(screen.getByLabelText("密码")).toBeInTheDocument();
  });

  it("hides OIDC when disabled", async () => {
    render(<LoginPage />);
    await waitFor(() => expect(api).toHaveBeenCalledWith("/auth/methods"));
    expect(screen.queryByRole("link", { name: "OIDC 登录" })).not.toBeInTheDocument();
  });

  it("preserves password login if loading methods fails", async () => {
    vi.mocked(api).mockRejectedValue(new Error("unavailable"));
    render(<LoginPage />);
    fireEvent.change(screen.getByLabelText("密码"), { target: { value: "password123" } });
    fireEvent.click(screen.getByRole("button", { name: /登\s*录/ }));
    await waitFor(() => expect(login).toHaveBeenCalledWith("admin", "password123"));
  });

  it.each([
    ["oidc_invalid_state", "登录请求已失效，请重新发起 OIDC 登录"],
    ["oidc_access_denied", "仅允许邮箱白名单中的已验证账号登录"],
    ["oidc_unavailable", "身份服务暂时不可用，请稍后重试或使用密码登录"],
    ["oidc_rate_limited", "登录尝试过于频繁，请稍后再试"],
    ["oidc_disabled", "OIDC 登录未启用，请使用密码登录"],
    ["oidc_failed", "OIDC 登录失败，请重试或使用密码登录"],
    ["provider-secret-error", "OIDC 登录失败，请重试或使用密码登录"],
    ["__proto__", "OIDC 登录失败，请重试或使用密码登录"],
    ["constructor", "OIDC 登录失败，请重试或使用密码登录"],
  ])("shows a fixed Chinese message for %s", async (code, message) => {
    window.history.replaceState({}, "", `/login?oidc_error=${code}`);
    render(<LoginPage />);
    expect(screen.getByText(message)).toBeInTheDocument();
    expect(screen.queryByText("provider-secret-error")).not.toBeInTheDocument();
    await waitFor(() => expect(api).toHaveBeenCalled());
  });
});
