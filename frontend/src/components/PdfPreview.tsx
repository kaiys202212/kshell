// PDF 预览：动态加载 pdfjs-dist@6，worker 经 Vite `?url` 打包。
// workerSrc 对应 pdfjs-dist/build/pdf.worker.min.mjs（与主包大版本锁定）。
import { useCallback, useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { PDFDocumentLoadingTask, PDFDocumentProxy } from 'pdfjs-dist';
import pdfWorkerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url';
import { Button } from './ui/button';
import { readFileBytes } from '../lib/api';

export interface PdfPreviewProps {
  wsPath: string;
  path: string;
}

function base64ToUint8Array(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function clampPage(page: number, numPages: number): number {
  if (numPages < 1) return 1;
  return Math.min(Math.max(1, page), numPages);
}

export default function PdfPreview({ wsPath, path }: PdfPreviewProps) {
  const { t } = useTranslation();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const docRef = useRef<PDFDocumentProxy | null>(null);
  const renderGenRef = useRef(0);
  const pageRenderTaskRef = useRef<{ cancel: () => void } | null>(null);
  const [numPages, setNumPages] = useState(0);
  const [page, setPage] = useState(1);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    let task: PDFDocumentLoadingTask | null = null;
    setLoading(true);
    setError(null);
    setNumPages(0);
    setPage(1);
    docRef.current = null;

    (async () => {
      try {
        const pdfjs = await import('pdfjs-dist');
        pdfjs.GlobalWorkerOptions.workerSrc = pdfWorkerUrl;

        const bytes = await readFileBytes(wsPath, path);
        if (cancelled) return;
        if (!bytes) {
          setError(t('ui.files.pdf_read_failed'));
          setLoading(false);
          return;
        }

        const data = base64ToUint8Array(bytes.Base64);
        task = pdfjs.getDocument({ data });
        const doc = await task.promise;
        if (cancelled) return;
        docRef.current = doc;
        setNumPages(doc.numPages);
        setPage(1);
        setLoading(false);
      } catch (e) {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
        setLoading(false);
      }
    })();

    return () => {
      cancelled = true;
      renderGenRef.current += 1;
      pageRenderTaskRef.current?.cancel();
      pageRenderTaskRef.current = null;
      docRef.current = null;
      if (task) void task.destroy();
    };
  }, [wsPath, path, t]);

  const renderPage = useCallback(async (pageNum: number, gen: number) => {
    const doc = docRef.current;
    const canvas = canvasRef.current;
    if (!doc || !canvas) return;

    const pdfPage = await doc.getPage(pageNum);
    if (gen !== renderGenRef.current) return;

    const viewport = pdfPage.getViewport({ scale: 1.25 });
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    canvas.height = viewport.height;
    canvas.width = viewport.width;
    const renderTask = pdfPage.render({ canvas, canvasContext: ctx, viewport });
    pageRenderTaskRef.current = renderTask;
    try {
      await renderTask.promise;
    } finally {
      if (pageRenderTaskRef.current === renderTask) {
        pageRenderTaskRef.current = null;
      }
      if (gen !== renderGenRef.current) return;
    }
  }, []);

  useEffect(() => {
    if (loading || error || numPages < 1) return;
    const safe = clampPage(page, numPages);
    if (safe !== page) {
      setPage(safe);
      return;
    }
    const gen = ++renderGenRef.current;
    void renderPage(safe, gen).catch((e) => {
      if (gen !== renderGenRef.current) return;
      setError(e instanceof Error ? e.message : String(e));
    });

    return () => {
      renderGenRef.current += 1;
      pageRenderTaskRef.current?.cancel();
      pageRenderTaskRef.current = null;
    };
  }, [page, numPages, loading, error, renderPage]);

  if (error) {
    return <p className="p-3 text-sm text-destructive">{error}</p>;
  }
  if (loading) {
    return <p className="p-3 text-sm text-muted-foreground">{t('ui.files.loading')}</p>;
  }

  const atFirst = page <= 1;
  const atLast = page >= numPages;

  return (
    <div className="flex h-full flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b border-border px-2 py-1.5">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={atFirst}
          onClick={() => setPage((p) => clampPage(p - 1, numPages))}
        >
          {t('ui.files.prev_page')}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={atLast}
          onClick={() => setPage((p) => clampPage(p + 1, numPages))}
        >
          {t('ui.files.next_page')}
        </Button>
        <span className="text-xs text-muted-foreground">
          {page} / {numPages}
        </span>
      </div>
      <div className="flex flex-1 items-start justify-center overflow-auto p-2">
        <canvas ref={canvasRef} className="max-w-full" />
      </div>
    </div>
  );
}
