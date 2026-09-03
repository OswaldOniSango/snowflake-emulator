import { listSchemaObjects, type SchemaObject } from "./api";

/**
 * A cache of what a schema contains, for completion.
 *
 * The object explorer fetches the same list, but it loads lazily as branches
 * are opened. Completion has to answer while someone is typing, so it keeps
 * its own copy of the active namespace and refreshes when that changes.
 */

export interface Catalog {
  /** Objects in the namespace last loaded, empty until the first load lands. */
  objects(): SchemaObject[];
  /** Loads a namespace, replacing whatever was cached. */
  load(database: string, schema: string): Promise<void>;
  /** Re-reads the namespace already loaded, after a statement changed it. */
  refresh(): Promise<void>;
}

/**
 * Whether running this statement can change what a schema contains.
 *
 * A SELECT leaves the catalog alone, so refreshing after one would spend a
 * request per run to learn nothing. Only the verbs that add, remove or rename
 * an object are worth the round trip.
 */
const CATALOG_VERBS = ["CREATE", "DROP", "ALTER", "UNDROP", "RENAME", "USE"];

export function changesCatalog(statement: string): boolean {
  // Leading comments and blank lines sit between the statement and its verb.
  const code = statement
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/--[^\n]*/g, " ")
    .trimStart();
  const verb = /^[A-Za-z]+/.exec(code)?.[0].toUpperCase() ?? "";
  return CATALOG_VERBS.includes(verb);
}

export function createCatalog(fetchObjects = listSchemaObjects): Catalog {
  let objects: SchemaObject[] = [];
  let loaded = "";

  async function load(database: string, schema: string): Promise<void> {
    const key = `${database}.${schema}`;
    if (!database || !schema || key === loaded) {
      return;
    }

    // Claim the namespace before awaiting: a second call for the same one
    // while the first is in flight would otherwise fetch it twice.
    loaded = key;
    try {
      objects = await fetchObjects(database, schema);
    } catch {
      // Completion is a convenience. A catalog that cannot be read leaves the
      // editor working, just without suggestions.
      objects = [];
      loaded = "";
    }
  }

  return {
    objects: () => objects,
    load,

    async refresh(): Promise<void> {
      const [database, schema] = loaded.split(".");
      if (!database || !schema) {
        return;
      }
      loaded = "";
      await load(database, schema);
    },
  };
}
