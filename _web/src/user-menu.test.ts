import { afterEach, describe, expect, it } from "vitest";
import { renderIdentity } from "./user-menu";
import type { AuthSession } from "./auth";

const user: AuthSession = { username: "<img src=x>", role: "PUBLIC", token: "secret", masterToken: "private", database: "TEST_DB", schema: "PUBLIC", warehouse: "", expiresAt: 0 };
afterEach(() => { document.body.replaceChildren(); document.documentElement.removeAttribute("data-theme"); localStorage.clear(); });
describe("account menu", () => {
  it("shows the user and current role as text, without exposing credentials", () => {
    const parent = document.createElement("div"); document.body.append(parent);
    renderIdentity(parent, user);
    expect(parent.querySelector("img")).toBeNull();
    expect(parent.querySelector(".user-heading")!.textContent).toBe(user.username);
    expect(parent.textContent).toContain("Current role · PUBLIC");
    expect(parent.textContent).not.toContain("secret");
    expect(parent.textContent).not.toContain("private");
    expect(parent.querySelector("details")!.open).toBe(false);
    expect(parent.textContent).toContain("Appearance");
    expect(parent.querySelector(".identity-signout")!.textContent).toBe("Sign out");
  });
  it("closes on Escape and returns focus to the avatar", () => {
    const parent = document.createElement("div"); document.body.append(parent);
    renderIdentity(parent, user);
    const details = parent.querySelector("details")!;
    details.open = true;
    details.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    expect(details.open).toBe(false);
    expect(document.activeElement).toBe(parent.querySelector("summary"));
  });
  it("clears the menu when the session ends", () => {
    const parent = document.createElement("div"); renderIdentity(parent, user);
    renderIdentity(parent, null);
    expect(parent.childElementCount).toBe(0);
  });
});
