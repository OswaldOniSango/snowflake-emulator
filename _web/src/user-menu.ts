import { logout, type AuthSession } from "./auth";
import { createThemeToggle } from "./theme";

/** Small account menu containing only working local-console actions. */
export function renderIdentity(parent: HTMLElement, value: AuthSession | null): void {
  parent.replaceChildren();
  if (!value) return;
  const menu = document.createElement("details");
  menu.className = "user-menu";
  const trigger = document.createElement("summary");
  trigger.setAttribute("role", "button");
  trigger.setAttribute("aria-label", "User menu");
  trigger.setAttribute("aria-expanded", "false");
  trigger.className = "user-trigger";
  const avatar = document.createElement("span");
  avatar.className = "identity-user";
  avatar.textContent = value.username.slice(0, 1).toUpperCase();
  const identity = document.createElement("span");
  identity.className = "user-caption";
  const name = document.createElement("strong");
  name.textContent = value.username;
  const role = document.createElement("small");
  role.textContent = value.role;
  identity.append(name, role);
  trigger.append(avatar, identity);
  const panel = document.createElement("div");
  panel.className = "user-popover";
  const heading = document.createElement("strong");
  heading.className = "user-heading";
  heading.textContent = value.username;
  const currentRole = document.createElement("p");
  currentRole.className = "user-role";
  currentRole.textContent = `Current role · ${value.role}`;
  const appearance = document.createElement("div");
  appearance.className = "user-appearance";
  const label = document.createElement("span");
  label.textContent = "Appearance";
  appearance.append(label, createThemeToggle());
  const documentation = document.createElement("a");
  documentation.className = "identity-documentation";
  documentation.href = "https://oswaldonisango.github.io/snowflake-emulator-documentation/";
  documentation.target = "_blank";
  documentation.rel = "noopener noreferrer";
  documentation.textContent = "Documentation";
  documentation.setAttribute("aria-label", "Documentation (opens in a new tab)");
  const external = document.createElement("span");
  external.textContent = "↗";
  external.setAttribute("aria-hidden", "true");
  documentation.append(external);
  const out = document.createElement("button");
  out.className = "identity-signout";
  out.textContent = "Sign out";
  out.addEventListener("click", () => void logout());
  panel.append(heading, currentRole, appearance, documentation, out);
  menu.append(trigger, panel);
  menu.addEventListener("toggle", () => trigger.setAttribute("aria-expanded", String(menu.open)));
  menu.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      menu.open = false;
      trigger.focus();
      event.stopPropagation();
    }
  });
  parent.append(menu);
}
