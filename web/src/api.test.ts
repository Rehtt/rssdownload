import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";

describe("api", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("accepts a successful response with an empty body", async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValue(new Response(null, { status: 202 }));
    vi.stubGlobal("fetch", fetchMock);

    await expect(
      api<void>("/subscriptions/1/sync", { method: "POST" }),
    ).resolves.toBeUndefined();
  });

  it("parses a successful JSON response", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ status: "ok" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(api<{ status: string }>("/status")).resolves.toEqual({
      status: "ok",
    });
  });
});
