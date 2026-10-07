// CodeMirror 6 封装：按路径推断语言，支持只读/主题/行号与受控 value；
// 始终启用语法折叠；空白字符高亮由 showWhitespace 控制（Compartment 热切换）。
import { useEffect, useRef } from 'react';
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
import { css } from '@codemirror/lang-css';
import { go } from '@codemirror/lang-go';
import { html } from '@codemirror/lang-html';
import { javascript } from '@codemirror/lang-javascript';
import { json } from '@codemirror/lang-json';
import { markdown } from '@codemirror/lang-markdown';
import { StreamLanguage, foldGutter, foldKeymap } from '@codemirror/language';
import { powerShell } from '@codemirror/legacy-modes/mode/powershell';
import { shell } from '@codemirror/legacy-modes/mode/shell';
import { python } from '@codemirror/lang-python';
import { Compartment, EditorState, type Extension } from '@codemirror/state';
import { oneDark } from '@codemirror/theme-one-dark';
import { EditorView, highlightWhitespace, keymap, lineNumbers } from '@codemirror/view';
import { codeLanguage } from '../lib/fileKind';

export interface CodeEditorProps {
  value: string;
  onChange?: (value: string) => void;
  readOnly?: boolean;
  path: string;
  theme: 'light' | 'dark';
  /** 显示空格/Tab 等高亮；默认 false。 */
  showWhitespace?: boolean;
}

/** 折叠始终开启；空白高亮按开关。供组件与单测复用。 */
export function buildWhitespaceFoldExtensions(showWhitespace: boolean): Extension[] {
  const exts: Extension[] = [foldGutter(), keymap.of(foldKeymap)];
  if (showWhitespace) {
    exts.push(highlightWhitespace());
  }
  return exts;
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
    case 'shell':
      return [StreamLanguage.define(shell)];
    case 'powershell':
      return [StreamLanguage.define(powerShell)];
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
  showWhitespace = false,
}: CodeEditorProps) {
  const parentRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const whitespaceCompRef = useRef(new Compartment());
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  useEffect(() => {
    if (!parentRef.current) return;

    const wsComp = whitespaceCompRef.current;
    const extensions: Extension[] = [
      lineNumbers(),
      history(),
      keymap.of([...defaultKeymap, ...historyKeymap]),
      foldGutter(),
      keymap.of(foldKeymap),
      wsComp.of(showWhitespace ? highlightWhitespace() : []),
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
    // value / showWhitespace 由下方 effect 同步，避免每次按键或开关重建编辑器
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

  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;
    view.dispatch({
      effects: whitespaceCompRef.current.reconfigure(
        showWhitespace ? highlightWhitespace() : [],
      ),
    });
  }, [showWhitespace]);

  return (
    <div
      ref={parentRef}
      data-testid="code-editor"
      className="h-full w-full min-h-0 overflow-hidden"
    />
  );
}
