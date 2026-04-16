// Bundled CodeMirror 6 setup for debeasy.
// Exposed as window.DebeasyCM after bundling with esbuild.
import { EditorView, basicSetup } from "codemirror";
import { keymap } from "@codemirror/view";
import { sql, PostgreSQL, MySQL, SQLite } from "@codemirror/lang-sql";
import { Compartment, EditorState } from "@codemirror/state";

const dialectByKind = {
  postgres: PostgreSQL,
  mysql: MySQL,
  sqlite: SQLite,
};

export function attach(textarea, opts) {
  opts = opts || {};
  const kind = opts.kind || "postgres";
  const onRun = opts.onRun || (() => {});

  const view = new EditorView({
    doc: textarea.value,
    extensions: [
      basicSetup,
      sql({ dialect: dialectByKind[kind] || PostgreSQL, upperCaseKeywords: true }),
      keymap.of([
        {
          key: "Mod-Enter",
          run: () => { onRun(view.state.doc.toString()); return true; },
        },
      ]),
      EditorView.theme({
        "&": { fontSize: "13.5px", border: "2px solid #0a0a0a", boxShadow: "3px 3px 0 #0a0a0a", background: "#fffdf2" },
        ".cm-scroller": { fontFamily: "JetBrains Mono, ui-monospace, Menlo, monospace" },
        ".cm-content": { padding: "10px 0" },
        ".cm-gutters": { background: "#ece9df", borderRight: "2px solid #0a0a0a" },
      }, { dark: false }),
      EditorView.updateListener.of((u) => {
        if (u.docChanged) {
          textarea.value = u.state.doc.toString();
        }
      }),
    ],
  });

  textarea.style.display = "none";
  textarea.parentNode.insertBefore(view.dom, textarea);

  return {
    view,
    getValue: () => view.state.doc.toString(),
    setValue: (s) => view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: s } }),
    destroy: () => view.destroy(),
  };
}

export { EditorView, EditorState };
