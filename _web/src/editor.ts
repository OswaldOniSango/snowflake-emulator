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
}

export interface Editor {
  /** The selected text when there is one, otherwise the whole buffer. */
  statementToRun(): string;
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
        syntaxHighlighting(highlighting),
        runKeymap,
        keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
        EditorView.lineWrapping,
        theme,
      ],
    }),
  });

  return {
    statementToRun() {
      const { from, to } = view.state.selection.main;
      const selected = from === to ? "" : view.state.sliceDoc(from, to);
      return (selected || view.state.doc.toString()).trim();
    },
    focus() {
      view.focus();
    },
  };
}
