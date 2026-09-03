import { autocompletion, type CompletionSource } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { sql, StandardSQL } from "@codemirror/lang-sql";
import { tags } from "@lezer/highlight";
import { EditorState, Prec, type Extension } from "@codemirror/state";
import {
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
} from "@codemirror/view";

/**
 * Theme driven by the CSS custom properties in style.css, so the editor
 * follows the page's light and dark palettes instead of carrying its own.
 */
const theme = EditorView.theme({
  "&": {
    height: "100%",
    fontSize: "12.5px",
    backgroundColor: "var(--panel)",
    color: "var(--ink)",
  },
  "&.cm-focused": { outline: "none" },
  ".cm-content": {
    fontFamily: "var(--mono)",
    padding: "12px 0",
    caretColor: "var(--accent)",
  },
  ".cm-gutters": {
    backgroundColor: "var(--sunk)",
    color: "var(--ink-3)",
    border: "none",
    borderRight: "1px solid var(--line-soft)",
    fontFamily: "var(--mono)",
  },
  ".cm-activeLine": { backgroundColor: "var(--sunk)" },
  ".cm-activeLineGutter": { backgroundColor: "var(--panel-2)", color: "var(--ink-2)" },
  ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
    backgroundColor: "var(--accent-soft)",
  },
  ".cm-cursor": { borderLeftColor: "var(--accent)" },
  ".cm-scroller": { overflow: "auto", lineHeight: "1.6" },
  ".cm-tooltip-autocomplete": {
    backgroundColor: "var(--panel-2)",
    border: "1px solid var(--line)",
    borderRadius: "6px",
    fontFamily: "var(--mono)",
    fontSize: "12px",
  },
  ".cm-tooltip-autocomplete > ul > li": { padding: "3px 8px", color: "var(--ink-2)" },
  ".cm-tooltip-autocomplete > ul > li[aria-selected]": {
    backgroundColor: "var(--accent-soft)",
    color: "var(--ink)",
  },
  ".cm-completionDetail": { color: "var(--ink-3)", fontStyle: "normal", marginLeft: "1em" },
  ".cm-completionMatchedText": { color: "var(--accent)", textDecoration: "none" },
});

/**
 * Syntax colours drawn from the same tokens as the rest of the page. Building
 * the extension list by hand means no highlighting is applied unless it is
 * asked for explicitly — basicSetup would have included it.
 */
const highlighting = HighlightStyle.define([
  { tag: tags.keyword, color: "var(--speculum)", fontWeight: "600" },
  { tag: tags.string, color: "var(--ok)" },
  { tag: tags.number, color: "var(--warn)" },
  { tag: tags.comment, color: "var(--ink-3)", fontStyle: "italic" },
  { tag: tags.operator, color: "var(--ink-2)" },
  { tag: tags.typeName, color: "var(--accent)" },
  { tag: tags.function(tags.variableName), color: "var(--accent)", fontWeight: "600" },
  { tag: tags.bool, color: "var(--warn)" },
  { tag: tags.null, color: "var(--warn)" },
]);

export interface EditorOptions {
  parent: HTMLElement;
  initialValue: string;
  /** Invoked by the run shortcut, so the keybinding lives with the editor. */
  onRun: () => void;
  /** Invoked when the buffer changes, so the worksheet can be persisted. */
  onChange?: (value: string) => void;
  /**
   * Supplies suggestions. Passed in rather than built here because what is
   * worth suggesting depends on the worksheet's namespace, which the editor
   * knows nothing about. Omitted, the editor simply does not complete.
   */
  completions?: CompletionSource;
}

export interface Editor {
  /** The whole buffer. */
  value(): string;
  /** Replaces the buffer, as when switching worksheets. */
  setValue(text: string): void;
  /** The selected text, or "" when nothing is selected. */
  selection(): string;
  /** Where the cursor sits, for locating the statement around it. */
  cursorOffset(): number;
  /** Replaces the selection with text, or inserts it at the cursor. */
  insert(text: string): void;
  focus(): void;
}

export function createEditor(options: EditorOptions): Editor {
  // defaultKeymap already binds Mod-Enter to insertBlankLine, so this has to
  // outrank it rather than merely be registered first.
  const runKeymap: Extension = Prec.highest(
    keymap.of([
      {
        key: "Mod-Enter",
        preventDefault: true,
        run: () => {
          options.onRun();
          return true;
        },
      },
    ]),
  );

  const view = new EditorView({
    parent: options.parent,
    state: EditorState.create({
      doc: options.initialValue,
      extensions: [
        lineNumbers(),
        highlightActiveLine(),
        highlightActiveLineGutter(),
        history(),
        sql({ dialect: StandardSQL, upperCaseKeywords: true }),
        // Only the given source runs: the SQL language's own completion knows
        // the dialect's keywords but nothing of the emulator's catalog, and
        // offering both would duplicate every keyword.
        ...(options.completions
          ? [autocompletion({ override: [options.completions], icons: false })]
          : []),
        syntaxHighlighting(highlighting),
        runKeymap,
        keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
        EditorView.lineWrapping,
        EditorView.updateListener.of((update) => {
          if (update.docChanged) {
            options.onChange?.(update.state.doc.toString());
          }
        }),
        theme,
      ],
    }),
  });

  return {
    value() {
      return view.state.doc.toString();
    },
    setValue(text: string) {
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: text },
        selection: { anchor: Math.min(text.length, view.state.selection.main.anchor) },
      });
    },
    selection() {
      const { from, to } = view.state.selection.main;
      return from === to ? "" : view.state.sliceDoc(from, to).trim();
    },
    cursorOffset() {
      return view.state.selection.main.head;
    },
    insert(text: string) {
      const { from, to } = view.state.selection.main;
      view.dispatch({
        changes: { from, to, insert: text },
        selection: { anchor: from + text.length },
      });
      view.focus();
    },
    focus() {
      view.focus();
    },
  };
}
