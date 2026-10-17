# Mallard console reference reconstruction

The seven user-supplied screenshots are visual references, not specifications of
undocumented Snowflake behavior. Mallard retains its duck mark and independent
identity. All markup, styles and interaction code are implemented locally.

## Observed structure

| Reference | Visible structure | Mallard implementation |
| --- | --- | --- |
| 1: recent projects | Expanded left navigation, grouped destinations, heading, tabs, file table | Home lists actual browser-local SQL worksheets, opens an existing worksheet or creates one. Database/schema replace unavailable viewed/updated timestamps. |
| 2: worksheet | Compact navigation, horizontal file tabs, breadcrumb, execution controls, editor above output | Compact rail, active-tab accent, worksheet breadcrumb, run controls on the left, context on the right, CodeMirror editor and existing result grid. |
| 3: run menu | Split execution button and run-all option | Split play/chevron control with Run all menu action; execution semantics are retained. |
| 4: namespace | Searchable database/schema columns, active checks | Existing live catalog picker retained and positioned below the right-side context controls. |
| 5: compute | Searchable role/warehouse columns, warehouse state and size | Existing authorized-role and warehouse picker retained. |
| 6: ingestion | Large two-column action cards | Table CSV preparation and internal stage upload are available. Unsupported cloud connectors are explained rather than presented as working actions. |
| 7: load dialog | Centered modal, file chooser, destination controls, bottom actions | Accessible native dialog, file picker, database/schema/stage fields and optional existing table. Upload errors are visible and retryable. |

## Visual rules

Neutral charcoal surfaces, blue selection and primary actions, thin gray borders,
compact outlined controls, horizontal SQL tabs with a top selection stripe,
monospace code, and dense result rows. The green mallard mark is retained.
Light, dark and system theme preferences remain available. Expanded navigation is
used on general pages, compact navigation on worksheets, with a manual toggle.
Focus hides the object explorer while retaining the navigation and context.

## Implemented interaction boundaries

- Worksheets remain browser-local; the home table does not invent modification
  times, server-side project persistence, notebooks or Streamlit support.
- Namespace and role/warehouse changes keep the existing authentication and
  execution paths. The redesign does not alter permissions or SQL translation.
- Upload destinations must already exist. The dialog uses the authenticated
  internal-stage upload API; it does not create databases, schemas or tables.
- UI uploads are limited to 63 MiB to leave multipart overhead below the server's
  64 MiB request limit. The reference's 250 MB limit is not applicable.
- Table preparation accepts CSV with one header row. It uploads first, then opens
  a worksheet with COPY INTO for explicit review and execution. Select a warehouse
  using the existing context picker before executing COPY. It is not an
  atomic import wizard, does not infer columns, and does not deduplicate loads.
- COPY preparation supports simple unquoted identifiers and filenames containing
  letters, digits, underscores, hyphens and dots. It uses an exact filename glob PATTERN to
  select exactly the uploaded file; the backend does not implement FILES lists.
- Stage upload can store other file types, but storage does not imply COPY support
  for those formats. Cloud connectors and the reference's advertised ORC, Avro,
  Parquet and XML table-import options are not implemented by this UI.

## Remaining reference details

The screenshots do not establish the behavior of search, sharing, settings,
notifications, chart/pivot configuration, ingestion preview/schema mapping,
external integrations, or most left-navigation destinations. These require more
references and separate backend work where appropriate. Chart and Pivot controls
are not displayed as functioning features. The legacy worksheet migration banner
is account-specific and has no equivalent migration in Mallard.

The implementation intentionally does not claim complete Snowsight or Snowflake
compatibility. No Snowflake logos, extracted assets or production account data
are included.

## Account placement

The account avatar is anchored to the lower-left navigation area. Expanded
navigation also shows the username and active role. Its upward-opening account
panel contains the username, current role, the existing appearance toggle and
Sign out. Role selection remains in the worksheet context picker. Account
switching, downloads, support and other unavailable product options are omitted.
The navigation expand/collapse button is above the navigation destinations.
The account panel closes on Escape or an outside click; Escape returns focus to
the avatar. Session termination removes the account controls.

## Database Explorer

The database sidebar uses original outline SVG icons for databases, schemas and
the seven object kinds returned by the current catalog. Schema contents are
grouped by type, with collapsible groups, live counts of matching loaded objects,
indentation guides and an outlined selected object. Groups initially open to
preserve direct access to existing insert/upload actions. Search only filters
loaded objects and opens matching groups. No functions, sequences or Data Products
are displayed without catalog support. Workspaces opens the browser-local project
list; Databases marks the active explorer view. The existing lazy catalog loading,
refresh, SQL insertion and stage upload flows remain in use.


## Execution control

The primary play button executes the selected SQL, or the statement at the cursor
when no text is selected. Its adjacent chevron opens a keyboard-accessible menu
with only Run all and its platform-specific shortcut on a second line. The menu
is aligned under the darker chevron segment, matching the supplied reference. The
menu closes on selection, Escape, outside clicks or navigation. Execution controls
are disabled while running or when there are no statements. Run all uses the
existing sequential executor and stops on failure; choosing it does not change
the primary button's default action. The editor's existing current-query shortcut
is retained.

Run all also supports Cmd+Shift+Return on macOS and Ctrl+Shift+Enter elsewhere.


## Result panel

The output header keeps its collapse chevron at the far right. A draggable
horizontal separator above it adjusts the editor/result proportions continuously;
Up/Down adjust by five percentage points, Home/End choose the bounds. The ratio
is remembered locally, and folding/restoring retains it. Both panes have minimum
heights. Resizing works with mouse, touch and keyboard input.

Results use single-line headers with original outline datatype icons, thin cell
borders, compact rows, a fixed row-number gutter, numeric alignment and muted
lowercase null values. Full declared types remain available in header tooltips
and accessible text. Decimal values misreported as TEXT retain numeric inference.
The Table toolbar holds real execution status and a download icon for CSV/JSON.
Chart, pivot, profiling and query-phase timing indicators are not implemented and
are not displayed as working features. Translation remains available as its own
output tab. No Snowflake engine behavior is implied by the visual styling.


## Workspace list and result inspection

Workspaces now switches the sidebar to the actual browser-local worksheet list,
with active-file highlighting and Add new. Databases switches back to the catalog;
Home continues to open the existing recent-project page. Focus sits next to the
worksheet tabs. Run's primary segment aligns with the editor's line-number gutter.

Result search filters loaded rows by displayed values in visible columns. The
column picker supports name search, select all, checkboxes and explicit Apply.
These are view-only operations; exports continue to contain the original result.
Counts show visible loaded rows with the total available in the info tooltip.
Duration displays the actual client-measured milliseconds separately from the row
count. Its details identify compilation, provisioning and execution as Not measured:
the current API does not expose these phase timings. The blue timing bar denotes
the measured total; no synthetic per-phase percentages are supplied.


## Ingestion context controls

The load dialog places a warehouse selector beside its title. It lists warehouse
names with their actual state and normalized size, searches by name and marks the
selected entry. A green light means ACTIVE; suspended, resuming and unknown states
are gray, with the actual state retained in the tooltip. Selecting a warehouse
updates the browser session so the prepared COPY runs in that warehouse; selecting
one does not resume it. Catalog metadata refreshes when the dropdown opens.
The same state/size presentation is used in the worksheet compute context.

The database/schema destination uses the existing searchable two-column catalog
picker with a qualified Schema DATABASE.SCHEMA label. The table dropdown lists existing tables or Create new table. Name is shown only
for a new table. Stage names remain editable text. Warehouse/catalog errors
are visible or retain the existing context fallback; uploads still validate their
destination and report server failures. Existing table/stage requirements and CSV
preparation semantics are unchanged.


## Table creation and Query History

CSV loading prepares SQL for explicit execution in a worksheet. Existing tables
receive COPY INTO only. Create new table uses Name as the destination and prepares
CREATE TABLE followed by COPY INTO; use Run all after reviewing both statements.
The CSV header supplies column names, all typed VARCHAR (no automatic type
inference). Empty, duplicate or non-simple headers are rejected; quoted identifiers with
punctuation or spaces are not supported by this preparation flow. An existing internal stage
is still required; the dialog does not execute destination SQL automatically.

Monitoring presents Query History with SQL text, query ID, status, recorded
warehouse, total duration, started timestamp and rows. Status, time period and
SQL/ID search filter the retained history; Columns controls visibility. Clicking
a row reopens it in a worksheet. USER is the authenticated username captured at
submission. WAREHOUSE SIZE is captured atomically at warehouse admission, including
queries admitted after waiting in the queue. Both snapshots persist with each
statement and do not change when a user is renamed or a warehouse is resized.
Older entries without snapshots show an em dash. Statements that never acquire a
warehouse (including failures before admission and warehouse-free commands) also
show an em dash for size. Existing ownership filtering continues to use stable
user IDs, independently of the display name. Grouped queries, per-phase
execution timing and additional Snowflake filters are not supported.

## Local filtering and ordering

Query History's User dropdown filters the recorded usernames in the history the
current principal is allowed to see; it does not expand access to other users'
history. Not recorded selects legacy entries without a username. This filter
combines with status, time period and SQL/ID search.

Click a history or result header (or focus it and press Enter/Space) to sort
ascending; click again for descending. The active header displays an arrow and
exposes aria-sort. Numeric columns use numeric ordering, including large integer
strings and DECIMAL values; history timestamps and durations use raw numbers and
warehouse sizes use their capacity order. Equal values retain their original
order. Nulls sort last ascending and first descending.

Result sorting reorders the loaded rows only. It preserves search and hidden
columns and does not execute SQL, change the stored result, or change exported
rows. Use SQL ORDER BY for server ordering across an entire result beyond the
browser's loaded rows. A new result resets ordering. Reloading Query History
resets its local filters and ordering.

## Sidebar width and compact execution button

The worksheet Run split button is 28px tall. The Workspaces/Databases sidebar
shares a draggable vertical separator with keyboard Left/Right, Home/End and
an accessible width value. Width is limited to 220–560px and further bounded
by available space so the worksheet remains usable. The browser remembers the
preferred width, restoring it after reload and Focus. A double click resets it
to 280px. Both sidebar and separator remain hidden on narrow screens (760px or
less); Focus hides them without changing the saved width.

## Saved worksheets and tab actions

Worksheet SQL, names, namespace, active worksheet and open tabs are saved
automatically in this browser. Saved locally confirms a successful browser
storage write; Not saved reports unavailable/full storage and suggests a SQL
download. This is local persistence, not server synchronization or cross-device
storage; clearing browser site data removes saved worksheets.

Hover or keyboard focus reveals a tab's three-dot menu with Rename, Download and
Delete. Rename also works by double-clicking the name (Enter commits, Escape
cancels). Download saves the worksheet's current SQL as a .sql file. Delete asks
for confirmation and removes the saved file. The close X closes only the tab;
its worksheet remains in My Workspace and Recent projects and can be reopened.
Closing/deleting the last open tab creates a blank worksheet to keep the editor
usable. Existing saved worksheets are retained when loading older browser data.
