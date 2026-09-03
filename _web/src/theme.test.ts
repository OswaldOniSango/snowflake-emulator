import { describe, expect, it } from "vitest";
import { applyTheme, loadTheme, nextTheme, saveTheme, themeLabel } from "./theme";

function fakeStorage(initial: Record<string, string> = {}, failing = false): Storage {
  const data = new Map(Object.entries(initial));
  const boom = () => {
    throw new Error("storage unavailable");
  };
  return {
    get length() {
      return data.size;
    },
    clear: () => data.clear(),
    key: (index: number) => [...data.keys()][index] ?? null,
    getItem: (key: string) => (failing ? boom() : (data.get(key) ?? null)),
    setItem: (key: string, value: string) => {
      if (failing) {
        boom();
      }
      data.set(key, value);
    },
    removeItem: (key: string) => void data.delete(key),
  } as Storage;
}

/** A stand-in for documentElement that records what was set. */
function fakeRoot(): HTMLElement {
  const attributes = new Map<string, string>();
  return {
    setAttribute: (name: string, value: string) => void attributes.set(name, value),
    removeAttribute: (name: string) => void attributes.delete(name),
    getAttribute: (name: string) => attributes.get(name) ?? null,
  } as unknown as HTMLElement;
}

describe("loadTheme", () => {
  it("follows the system when nothing is stored", () => {
    expect(loadTheme(fakeStorage())).toBe("system");
  });

  it("restores a stored choice", () => {
    expect(loadTheme(fakeStorage({ "mallard.theme": "dark" }))).toBe("dark");
  });

  it("ignores a value that is not a theme", () => {
    expect(loadTheme(fakeStorage({ "mallard.theme": "neon" }))).toBe("system");
  });

  it("follows the system when storage is missing or refuses", () => {
    expect(loadTheme(null)).toBe("system");
    expect(loadTheme(fakeStorage({}, true))).toBe("system");
  });
});

describe("applyTheme", () => {
  it("stamps an explicit choice so it beats the system preference", () => {
    const root = fakeRoot();

    applyTheme("dark", root);
    expect(root.getAttribute("data-theme")).toBe("dark");

    applyTheme("light", root);
    expect(root.getAttribute("data-theme")).toBe("light");
  });

  it("stamps nothing for the system theme", () => {
    // The stylesheet reads prefers-color-scheme only when no attribute is set.
    const root = fakeRoot();

    applyTheme("dark", root);
    applyTheme("system", root);

    expect(root.getAttribute("data-theme")).toBeNull();
  });
});

describe("saveTheme", () => {
  it("round-trips", () => {
    const storage = fakeStorage();
    saveTheme("light", storage);
    expect(loadTheme(storage)).toBe("light");
  });

  it("says nothing when storage refuses", () => {
    expect(() => saveTheme("dark", fakeStorage({}, true))).not.toThrow();
    expect(() => saveTheme("dark", null)).not.toThrow();
  });
});

describe("nextTheme", () => {
  it("cycles system, light, dark and back", () => {
    expect(nextTheme("system")).toBe("light");
    expect(nextTheme("light")).toBe("dark");
    expect(nextTheme("dark")).toBe("system");
  });
});

describe("themeLabel", () => {
  it("names each theme for a screen reader", () => {
    expect(themeLabel("system")).toBe("System theme");
    expect(themeLabel("light")).toBe("Light theme");
    expect(themeLabel("dark")).toBe("Dark theme");
  });
});
