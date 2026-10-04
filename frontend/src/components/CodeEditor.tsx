// CodeMirror 6 封装：按路径推断语言，支持只读/主题/行号与受控 value。
import { useEffect, useRef } from 'react';
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
import { css } from '@codemirror/lang-css';
import { go } from '@codemirror/lang-go';
import { html } from '@codemirror/lang-html';
import { javascript } from '@codemirror/lang-javascript';
import { json } from '@codemirror/lang-json';
import { markdown } from '@codemirror/lang-markdown';
import { python } from '@codemirror/lang-python';
import { EditorState, type Extension } from '@codemirror/state';
import { oneDark } from '@codemirror/theme-one-dark';
import { EditorView, keymap, lineNumbers } from '@codemirror/view';
import { codeLanguage } from '../lib/fileKind';

export interface CodeEditorProps {
  value: string;
  onChange?: (value: string) => void;
  readOnly?: boolean;
  path: string;
  theme: 'light' | 'dark';
}

function languageExtensions(path: string): Extension[] {
  switch (codeLanguage(path)) {
    case 'javascript':
      return [javascript()];
    case 'typescript':
      return [javascript({ typescript: true })];
    case 'tsx':
      return [javascript({ typescript: true, jsx: true })];
    case 'json':
      return [json()];
    case 'markdown':
      return [markdown()];
    case 'html':
      return [html()];
    case 'css':
      return [css()];
    case 'python':
      return [python()];
    case 'go':
      return [go()];
    default:
      return [];
  }
}

export default function CodeEditor({
  value,
  onChange,
  readOnly = false,
  path,
  theme,
}: CodeEditorProps) {
  const parentRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  useEffect(() => {
    if (!parentRef.current) return;

    const extensions: Extension[] = [
      lineNumbers(),
      history(),
      keymap.of([...defaultKeymap, ...historyKeymap]),
      EditorView.theme({
        '&': { height: '100%' },
        '.cm-scroller': { overflow: 'auto' },
      }),
      ...languageExtensions(path),
      EditorView.updateListener.of((update) => {
        if (update.docChanged) {
          onChangeRef.current?.(update.state.doc.toString());
        }
      }),
    ];
    if (readOnly) {
      extensions.push(EditorState.readOnly.of(true));
    }
    if (theme === 'dark') {
      extensions.push(oneDark);
    }

    const view = new EditorView({
      state: EditorState.create({
        doc: value,
        extensions,
      }),
      parent: parentRef.current,
    });
    viewRef.current = view;

    return () => {
      view.destroy();
      viewRef.current = null;
    };
    // value 由下方 effect 同步，避免每次按键重建编辑器
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, theme, readOnly]);

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    const current = view.state.doc.toString();
    if (current === value) return;
    view.dispatch({
      changes: { from: 0, to: current.length, insert: value },
    });
  }, [value]);

  return (
    <div
      ref={parentRef}
      data-testid="code-editor"
      className="h-full w-full min-h-0 overflow-hidden"
    />
  );
}
