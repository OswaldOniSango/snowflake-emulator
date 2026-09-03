import { describe, expect, it } from "vitest";
import { completionsFor, createCompletionSource } from "./completion";
import type { Catalog } from "./catalog";
import type { SchemaObject } from "./api";
import type { CompletionContext } from "@codemirror/autocomplete";

const catalogOf = (objects: SchemaObject[]): Catalog => ({
  objects: () => objects,
  load: async () => {},
  refresh: async () => {},
});

const labels = (word: string, objects: SchemaObject[] = []) =>
  completionsFor(word, objects).map((option) => option.label);

describe("completionsFor", () => {
  it("offers catalog objects before the fixed vocabulary", () => {
    const options = completionsFor("S", [{ name: "SALES", kind: "table" }]);

    expect(options[0]?.label).toBe("SALES");
    expect(options[0]?.boost).toBe(2);
    expect(options.some((option) => option.label === "SELECT")).toBe(true);
  });

  it("matches a lowercase prefix against the uppercase names the emulator stores", () => {
    expect(labels("us", [{ name: "USERS", kind: "table" }])).toContain("USERS");
  });

  it("offers only what the prefix starts, not what merely contains it", () => {
    expect(labels("ORD", [{ name: "USERS", kind: "table" }, { name: "ORDERS", kind: "table" }]))
      .toEqual(["ORDERS", "ORDER BY"]);
  });

  it("labels each object with its kind", () => {
    const [table, procedure] = completionsFor("X", [
      { name: "X_TABLE", kind: "table" },
      { name: "X_PROC", kind: "procedure" },
    ]);

    expect(table?.detail).toBe("table");
    expect(procedure?.detail).toBe("procedure");
    expect(procedure?.type).toBe("function");
  });

  it("appends the detail the catalog supplied", () => {
    const [option] = completionsFor("U", [
      { name: "USERS", kind: "stream", detail: "on TEST_DB.PUBLIC.EVENTS" },
    ]);

    expect(option?.detail).toBe("stream · on TEST_DB.PUBLIC.EVENTS");
  });

  it("still names a kind it has no label for", () => {
    const [option] = completionsFor("V", [{ name: "V1", kind: "view" }]);
    expect(option?.detail).toBe("view");
  });

  it("suggests the Snowflake functions the emulator translates", () => {
    const [option] = completionsFor("IFF", []);
    expect(option?.label).toBe("IFF");
    expect(option?.detail).toBe("translated for DuckDB");
  });

  it("suggests multi-word statements as one entry", () => {
    expect(labels("MERGE")).toEqual(["MERGE INTO"]);
  });

  it("offers everything it knows for an empty prefix", () => {
    expect(labels("", [{ name: "USERS", kind: "table" }]).length).toBeGreaterThan(30);
  });

  it("offers nothing for a prefix that matches neither catalog nor vocabulary", () => {
    expect(labels("ZZZQ", [{ name: "USERS", kind: "table" }])).toEqual([]);
  });
});

/** Builds the parts of a CompletionContext the source actually reads. */
function contextAt(text: string, explicit = false): CompletionContext {
  return {
    explicit,
    matchBefore: (expr: RegExp) => {
      const match = new RegExp(`(?:${expr.source})$`).exec(text);
      if (!match) {
        return null;
      }
      return { from: text.length - match[0].length, to: text.length, text: match[0] };
    },
  } as unknown as CompletionContext;
}

describe("createCompletionSource", () => {
  it("completes from the start of the word being typed", () => {
    const source = createCompletionSource(catalogOf([{ name: "USERS", kind: "table" }]));
    const result = source(contextAt("SELECT * FROM USE"));

    expect(result?.from).toBe("SELECT * FROM ".length);
    expect(result?.options.map((option) => option.label)).toContain("USERS");
  });

  it("stays closed after a space unless it was asked for", () => {
    const source = createCompletionSource(catalogOf([]));
    expect(source(contextAt("SELECT * FROM "))).toBeNull();
  });

  it("opens on an empty word when explicitly requested", () => {
    const source = createCompletionSource(catalogOf([{ name: "USERS", kind: "table" }]));
    const result = source(contextAt("SELECT * FROM ", true));

    expect(result?.options.map((option) => option.label)).toContain("USERS");
  });

  it("reads the catalog on each request rather than capturing it once", () => {
    let objects: SchemaObject[] = [];
    const source = createCompletionSource({
      objects: () => objects,
      load: async () => {},
      refresh: async () => {},
    });

    expect(source(contextAt("USE"))?.options.map((o) => o.label)).not.toContain("USERS");
    objects = [{ name: "USERS", kind: "table" }];
    expect(source(contextAt("USE"))?.options.map((o) => o.label)).toContain("USERS");
  });
});
