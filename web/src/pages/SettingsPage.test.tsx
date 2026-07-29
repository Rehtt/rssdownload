import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntApp } from "antd";
import { beforeEach, describe, expect, it, vi } from "vitest";
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
  proxyUrl: "http://127.0.0.1:7890",
  proxyUsername: "proxy-user",
  proxyPassword: "••••",
  hasProxyPassword: true,
  defaultDownloadDir: "/downloads",
  pollIntervalMinutes: 10,
  updatedAt: "2026-07-29T00:00:00Z",
};

describe("SettingsPage", () => {
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

    fireEvent.change(proxyUrl, {
      target: { value: "socks5://127.0.0.1:1080" },
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
    });
  });
});
