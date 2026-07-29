import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LoginPage } from "./LoginPage";

const login = vi.fn().mockResolvedValue(undefined);

vi.mock("../auth", () => ({
  useAuth: () => ({ login }),
}));

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
});
