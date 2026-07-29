import { describe, expect, it } from "vitest";
import { formatBytes, progressPercent } from "./format";

describe("format helpers", () => {
  it("formats byte values", () => {
    expect(formatBytes(1024)).toBe("1.00 KiB");
    expect(formatBytes(536_870_912)).toBe("512 MiB");
    expect(formatBytes(0)).toBe("—");
  });

  it("clamps progress", () => {
    expect(progressPercent(50, 100)).toBe(50);
    expect(progressPercent(200, 100)).toBe(100);
    expect(progressPercent(1, 0)).toBe(0);
  });
});

