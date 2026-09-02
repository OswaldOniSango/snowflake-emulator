/**
 * Light, dark, or whatever the system says.
 *
 * The stylesheet defines its palette on :root and redefines it under both
 * prefers-color-scheme and [data-theme], so following the system means setting
 * no attribute at all — a choice has to win in either direction.
 */

const STORAGE_KEY = "mallard.theme";

export type Theme = "system" | "light" | "dark";

const ORDER: Theme[] = ["system", "light", "dark"];

const LABELS: Record<Theme, string> = {
  system: "System theme",
  light: "Light theme",
  dark: "Dark theme",
};

const ICONS: Record<Theme, string> = {
  system: "◐",
  light: "☀",
  dark: "☾",
};

export function loadTheme(storage: Storage | null = safeStorage()): Theme {
  try {
    const stored = storage?.getItem(STORAGE_KEY);
    return isTheme(stored) ? stored : "system";
  } catch {
    return "system";
  }
}

export function applyTheme(theme: Theme, root: HTMLElement = document.documentElement): void {
  if (theme === "system") {
    root.removeAttribute("data-theme");
    return;
  }
  root.setAttribute("data-theme", theme);
}

export function saveTheme(theme: Theme, storage: Storage | null = safeStorage()): void {
  try {
    storage?.setItem(STORAGE_KEY, theme);
  } catch {
    // The theme simply will not be remembered.
  }
}

export function nextTheme(theme: Theme): Theme {
  return ORDER[(ORDER.indexOf(theme) + 1) % ORDER.length] as Theme;
}

export function themeLabel(theme: Theme): string {
  return LABELS[theme];
}

export function themeIcon(theme: Theme): string {
  return ICONS[theme];
}

/** A button that cycles system → light → dark. */
export function createThemeToggle(): HTMLButtonElement {
  let theme = loadTheme();
  applyTheme(theme);

  const button = document.createElement("button");
  button.className = "icon-btn";
  button.type = "button";

  const paint = (): void => {
    button.textContent = themeIcon(theme);
    button.setAttribute("aria-label", `${themeLabel(theme)}. Click to change.`);
    button.title = themeLabel(theme);
  };

  button.addEventListener("click", () => {
    theme = nextTheme(theme);
    applyTheme(theme);
    saveTheme(theme);
    paint();
  });

  paint();
  return button;
}

function isTheme(value: unknown): value is Theme {
  return value === "system" || value === "light" || value === "dark";
}

function safeStorage(): Storage | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}
