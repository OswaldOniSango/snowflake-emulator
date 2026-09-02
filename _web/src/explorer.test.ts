import { describe, expect, it } from "vitest";
import { nameToInsert } from "./explorer";

const CONTEXT = { database: "TEST_DB", schema: "PUBLIC" };

describe("nameToInsert", () => {
  it("inserts a bare name for an object in the current namespace", () => {
    // The emulator only resolves unqualified names, so this is the form that
    // actually runs.
    expect(nameToInsert("TEST_DB", "PUBLIC", "USERS", CONTEXT)).toBe("USERS");
  });

  it("keeps the full name for another database", () => {
    expect(nameToInsert("OTHER_DB", "PUBLIC", "USERS", CONTEXT)).toBe("OTHER_DB.PUBLIC.USERS");
  });

  it("keeps the full name for another schema", () => {
    expect(nameToInsert("TEST_DB", "STAGING", "USERS", CONTEXT)).toBe("TEST_DB.STAGING.USERS");
  });

  it("qualifies when there is no context to compare against", () => {
    expect(nameToInsert("TEST_DB", "PUBLIC", "USERS")).toBe("TEST_DB.PUBLIC.USERS");
  });
});
