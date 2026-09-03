import { describe, expect, it, vi } from "vitest";
import { changesCatalog, createCatalog } from "./catalog";
import type { SchemaObject } from "./api";

const objects = (...names: string[]): SchemaObject[] =>
  names.map((name) => ({ name, kind: "table" }));

describe("createCatalog", () => {
  it("has nothing before the first load", () => {
    expect(createCatalog(vi.fn()).objects()).toEqual([]);
  });

  it("holds what the namespace returned", async () => {
    const catalog = createCatalog(vi.fn().mockResolvedValue(objects("USERS", "ORDERS")));
    await catalog.load("TEST_DB", "PUBLIC");
    expect(catalog.objects().map((o) => o.name)).toEqual(["USERS", "ORDERS"]);
  });

  it("fetches a namespace once however often it is asked for", async () => {
    const fetchObjects = vi.fn().mockResolvedValue(objects("USERS"));
    const catalog = createCatalog(fetchObjects);

    await catalog.load("TEST_DB", "PUBLIC");
    await catalog.load("TEST_DB", "PUBLIC");

    expect(fetchObjects).toHaveBeenCalledTimes(1);
  });

  it("does not fetch the same namespace twice while the first is in flight", async () => {
    const fetchObjects = vi.fn().mockResolvedValue(objects("USERS"));
    const catalog = createCatalog(fetchObjects);

    // Both started before either resolves: the second must see the namespace
    // as claimed rather than as not yet loaded.
    await Promise.all([catalog.load("TEST_DB", "PUBLIC"), catalog.load("TEST_DB", "PUBLIC")]);

    expect(fetchObjects).toHaveBeenCalledTimes(1);
  });

  it("replaces the cache when the namespace changes", async () => {
    const fetchObjects = vi
      .fn()
      .mockResolvedValueOnce(objects("USERS"))
      .mockResolvedValueOnce(objects("EVENTS"));
    const catalog = createCatalog(fetchObjects);

    await catalog.load("TEST_DB", "PUBLIC");
    await catalog.load("OTHER_DB", "PUBLIC");

    expect(catalog.objects().map((o) => o.name)).toEqual(["EVENTS"]);
  });

  it("ignores a load with no namespace", async () => {
    const fetchObjects = vi.fn();
    await createCatalog(fetchObjects).load("", "PUBLIC");
    expect(fetchObjects).not.toHaveBeenCalled();
  });

  it("empties the cache when the catalog cannot be read", async () => {
    const catalog = createCatalog(
      vi.fn().mockResolvedValueOnce(objects("USERS")).mockRejectedValueOnce(new Error("down")),
    );

    await catalog.load("TEST_DB", "PUBLIC");
    await catalog.load("OTHER_DB", "PUBLIC");

    expect(catalog.objects()).toEqual([]);
  });

  it("retries a namespace whose load failed", async () => {
    const fetchObjects = vi
      .fn()
      .mockRejectedValueOnce(new Error("down"))
      .mockResolvedValueOnce(objects("USERS"));
    const catalog = createCatalog(fetchObjects);

    await catalog.load("TEST_DB", "PUBLIC");
    await catalog.load("TEST_DB", "PUBLIC");

    expect(catalog.objects().map((o) => o.name)).toEqual(["USERS"]);
  });
});

describe("createCatalog().refresh", () => {
  it("re-reads the namespace so a table just created is offered", async () => {
    const fetchObjects = vi
      .fn()
      .mockResolvedValueOnce(objects("USERS"))
      .mockResolvedValueOnce(objects("USERS", "ORDERS"));
    const catalog = createCatalog(fetchObjects);

    await catalog.load("TEST_DB", "PUBLIC");
    await catalog.refresh();

    expect(fetchObjects).toHaveBeenNthCalledWith(2, "TEST_DB", "PUBLIC");
    expect(catalog.objects().map((o) => o.name)).toEqual(["USERS", "ORDERS"]);
  });

  it("does nothing before a namespace has been loaded", async () => {
    const fetchObjects = vi.fn();
    await createCatalog(fetchObjects).refresh();
    expect(fetchObjects).not.toHaveBeenCalled();
  });
});

describe("changesCatalog", () => {
  it.each([
    "CREATE TABLE USERS (id INT)",
    "create or replace table users (id int)",
    "DROP TABLE USERS",
    "ALTER TABLE USERS ADD COLUMN name VARCHAR",
    "UNDROP TABLE USERS",
    "USE SCHEMA ANALYTICS",
  ])("is true for %s", (statement) => {
    expect(changesCatalog(statement)).toBe(true);
  });

  it.each([
    "SELECT * FROM USERS",
    "INSERT INTO USERS VALUES (1)",
    "UPDATE USERS SET id = 2",
    "DELETE FROM USERS",
    "CALL DO_THING()",
    "",
  ])("is false for %s", (statement) => {
    expect(changesCatalog(statement)).toBe(false);
  });

  it("looks past a leading line comment", () => {
    expect(changesCatalog("-- set up the fixture\nCREATE TABLE USERS (id INT)")).toBe(true);
  });

  it("looks past a leading block comment", () => {
    expect(changesCatalog("/* set up */ DROP TABLE USERS")).toBe(true);
  });

  it("is not fooled by a verb appearing later in the statement", () => {
    expect(changesCatalog("SELECT 'CREATE TABLE' AS example")).toBe(false);
  });
});
