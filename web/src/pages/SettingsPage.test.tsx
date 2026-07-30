import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntApp } from "antd";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Settings } from "../types";
import { SettingsPage } from "./SettingsPage";

const { apiMock } = vi.hoisted(() => ({
  apiMock: vi.fn(),
}));

vi.mock("../api", () => ({
  api: apiMock,
}));

const loadedSettings: Settings = {
  aria2Url: "http://127.0.0.1:6800/jsonrpc",
  aria2Secret: "••••",
  hasAria2Secret: true,
  seedTimeMinutes: 120.5,
  seedRatio: 1.5,
  proxyUrl: "http://127.0.0.1:7890",
  proxyUsername: "proxy-user",
  proxyPassword: "••••",
  hasProxyPassword: true,
  defaultDownloadDir: "/downloads",
  pollIntervalMinutes: 10,
  updatedAt: "2026-07-29T00:00:00Z",
};

describe("SettingsPage", () => {
  afterEach(() => {
    cleanup();
  });

  beforeEach(() => {
    apiMock.mockReset();
    apiMock.mockResolvedValue(loadedSettings);
  });

  it("loads and submits proxy settings without echoing the stored password", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <AntApp>
        <QueryClientProvider client={queryClient}>
          <SettingsPage />
        </QueryClientProvider>
      </AntApp>,
    );

    const proxyUrl = await screen.findByLabelText("代理地址");
    expect(proxyUrl).toHaveValue("http://127.0.0.1:7890");
    expect(screen.getByLabelText("代理账号")).toHaveValue("proxy-user");
    expect(screen.getByLabelText("代理密码")).toHaveValue("");
    expect(screen.getByText("已保存密码；留空表示保持不变。")).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByLabelText("做种时间（分钟）")).toHaveValue("120.5");
      expect(screen.getByLabelText("做种比例（分享率）")).toHaveValue("1.5");
    });

    fireEvent.change(proxyUrl, {
      target: { value: "socks5://127.0.0.1:1080" },
    });
    fireEvent.change(screen.getByLabelText("做种时间（分钟）"), {
      target: { value: "180.5" },
    });
    fireEvent.change(screen.getByLabelText("做种比例（分享率）"), {
      target: { value: "2" },
    });
    fireEvent.change(screen.getByLabelText("代理密码"), {
      target: { value: "replacement-pass" },
    });
    fireEvent.click(screen.getByRole("button", { name: /保存设置/ }));

    await waitFor(() => {
      expect(
        apiMock.mock.calls.some(
          ([path, init]) =>
            path === "/settings" &&
            typeof init === "object" &&
            init !== null &&
            init.method === "PUT",
        ),
      ).toBe(true);
    });
    const saveCall = apiMock.mock.calls.find(
      ([path, init]) =>
        path === "/settings" &&
        typeof init === "object" &&
        init !== null &&
        init.method === "PUT",
    );
    expect(saveCall).toBeDefined();
    expect(JSON.parse(saveCall?.[1].body as string)).toMatchObject({
      proxyUrl: "socks5://127.0.0.1:1080",
      proxyUsername: "proxy-user",
      proxyPassword: "replacement-pass",
      clearProxyPassword: false,
      seedTimeMinutes: 180.5,
      seedRatio: 2,
    });
  });

  it("submits blank seeding options as null", async () => {
    apiMock.mockResolvedValue({
      ...loadedSettings,
      seedTimeMinutes: null,
      seedRatio: null,
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <AntApp>
        <QueryClientProvider client={queryClient}>
          <SettingsPage />
        </QueryClientProvider>
      </AntApp>,
    );

    await screen.findByLabelText("做种时间（分钟）");
    fireEvent.click(screen.getByRole("button", { name: /保存设置/ }));

    await waitFor(() => {
      expect(
        apiMock.mock.calls.some(
          ([path, init]) =>
            path === "/settings" &&
            typeof init === "object" &&
            init !== null &&
            init.method === "PUT",
        ),
      ).toBe(true);
    });
    const saveCall = apiMock.mock.calls.find(
      ([path, init]) =>
        path === "/settings" &&
        typeof init === "object" &&
        init !== null &&
        init.method === "PUT",
    );
    expect(JSON.parse(saveCall?.[1].body as string)).toMatchObject({
      seedTimeMinutes: null,
      seedRatio: null,
    });
  });
});
