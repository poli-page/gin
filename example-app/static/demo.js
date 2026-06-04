(() => {
  // Document lifecycle state. The doc-* buttons read this; doc-create
  // sets it, doc-delete clears it.
  const state = { docId: null, lastBlobUrl: null };

  const $ = sel => document.querySelector(sel);
  const escapeHtml = s => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

  const setDocId = id => {
    state.docId = id;
    const el = $('#doc-id');
    const text = $('#doc-id-text');
    if (id) {
      el.classList.add('has-id');
      text.innerHTML = `documentId: ${escapeHtml(id)} <button class="copy" data-copy="${escapeHtml(id)}">copy</button>`;
    } else {
      el.classList.remove('has-id');
      text.textContent = 'no document stored';
    }
    document.querySelectorAll('button[data-needs-doc]').forEach(b => { b.disabled = !id; });
  };
  setDocId(null);

  const paneHead = (label, status) =>
    `<div class="pane-label"><span>${escapeHtml(label)}</span><span class="meta">${escapeHtml(status)}</span></div>`;

  // revokeLastBlob releases the previous Blob URL before assigning a
  // new one — without this the browser leaks the PDF buffer for the
  // page lifetime (plan §11.4 gotcha).
  const revokeLastBlob = () => {
    if (state.lastBlobUrl) {
      URL.revokeObjectURL(state.lastBlobUrl);
      state.lastBlobUrl = null;
    }
  };

  const renderBlobIframe = (target, blob, label, meta) => {
    revokeLastBlob();
    const url = URL.createObjectURL(blob);
    state.lastBlobUrl = url;
    target.classList.remove('is-error');
    target.classList.add('is-ok');
    target.innerHTML = paneHead(label, meta) + `<iframe src="${url}"></iframe>`;
  };

  const renderIframeUrl = (target, src, label, meta) => {
    revokeLastBlob();
    target.classList.remove('is-error');
    target.classList.add('is-ok');
    target.innerHTML = paneHead(label, meta) + `<iframe src="${src}"></iframe>`;
  };

  const renderInlineHtml = (target, html, label, meta) => {
    revokeLastBlob();
    target.classList.remove('is-error');
    target.classList.add('is-ok');
    const safe = html.replace(/"/g, '&quot;');
    target.innerHTML = paneHead(label, meta) + `<iframe srcdoc="${safe}"></iframe>`;
  };

  const renderJson = (target, status, body, label) => {
    target.classList.remove('is-error', 'is-ok');
    const ok = status >= 200 && status < 400;
    target.classList.add(ok ? 'is-ok' : 'is-error');
    let pretty = body;
    try { pretty = JSON.stringify(JSON.parse(body), null, 2); } catch (e) { /* leave raw */ }
    target.innerHTML = paneHead(label, `${status} ${ok ? 'ok' : 'error'}`) +
      `<pre>${escapeHtml(pretty)}</pre>`;
  };

  const renderThumbnails = (target, thumbs, label) => {
    revokeLastBlob();
    target.classList.remove('is-error');
    target.classList.add('is-ok');
    const grid = thumbs.map(t => {
      const src = `data:${t.contentType};base64,${t.data}`;
      return `<figure><img src="${src}" alt="page ${t.page}"><figcaption>page ${t.page}</figcaption></figure>`;
    }).join('');
    target.innerHTML = paneHead(label, `${thumbs.length} thumbnails`) +
      `<div class="thumb-grid">${grid}</div>`;
  };

  const renderError = (target, msg) => {
    target.classList.remove('is-ok');
    target.classList.add('is-error');
    target.innerHTML = paneHead('output', 'error') + `<pre>${escapeHtml(msg)}</pre>`;
  };

  const withLoading = async (btn, fn) => {
    btn.classList.add('is-loading');
    btn.disabled = true;
    try { await fn(); }
    finally {
      btn.classList.remove('is-loading');
      document.querySelectorAll('button[data-needs-doc]').forEach(b => { b.disabled = !state.docId; });
      if (!btn.hasAttribute('data-needs-doc')) btn.disabled = false;
    }
  };

  // Routes match the Gin handlers in handlers.go. Keep these in sync.
  const actions = {
    pdf: async target => {
      const r = await fetch('/api/render/pdf');
      if (!r.ok) return renderError(target, `HTTP ${r.status}`);
      const blob = await r.blob();
      renderBlobIframe(target, blob, 'pdf', `${blob.size.toLocaleString()} bytes`);
    },
    stream: async target => {
      const r = await fetch('/api/render/stream');
      if (!r.ok) return renderError(target, `HTTP ${r.status}`);
      const blob = await r.blob();
      renderBlobIframe(target, blob, 'pdf (stream)', `${blob.size.toLocaleString()} bytes`);
    },
    preview: async target => {
      const r = await fetch('/api/render/preview');
      const html = await r.text();
      if (!r.ok) return renderError(target, `HTTP ${r.status}`);
      renderInlineHtml(target, html, 'html preview', `${html.length.toLocaleString()} chars`);
    },
    'doc-create': async target => {
      const r = await fetch('/api/documents', { method: 'POST' });
      const body = await r.text();
      renderJson(target, r.status, body, 'document descriptor');
      if (r.ok) {
        try { setDocId(JSON.parse(body).documentId); } catch (e) { /* leave state untouched */ }
      }
    },
    'doc-get': async target => {
      if (!state.docId) return;
      // /api/documents/:id returns a 302 to a presigned S3 URL. Fetching
      // and following the redirect from JS hits CORS (S3 doesn't expose
      // Access-Control-Allow-Origin). An <iframe> navigation isn't a
      // fetch, so the browser follows the redirect natively.
      renderIframeUrl(target, `/api/documents/${state.docId}`, 'stored pdf', 'served via 302 → presigned URL');
    },
    'doc-preview': async target => {
      if (!state.docId) return;
      const r = await fetch(`/api/documents/${state.docId}/preview`);
      const html = await r.text();
      if (!r.ok) return renderError(target, `HTTP ${r.status}`);
      renderInlineHtml(target, html, 'stored html preview', `${html.length.toLocaleString()} chars`);
    },
    'doc-thumbnails': async target => {
      if (!state.docId) return;
      const r = await fetch(`/api/documents/${state.docId}/thumbnails`);
      const body = await r.text();
      if (!r.ok) {
        renderJson(target, r.status, body, 'thumbnails');
        return;
      }
      try {
        const data = JSON.parse(body);
        renderThumbnails(target, data.thumbnails || [], 'thumbnails');
      } catch (e) {
        renderJson(target, r.status, body, 'thumbnails');
      }
    },
    'doc-delete': async target => {
      if (!state.docId) return;
      const r = await fetch(`/api/documents/${state.docId}`, { method: 'DELETE' });
      const body = await r.text();
      renderJson(target, r.status, body || '"(204 No Content)"', 'delete');
      if (r.ok) setDocId(null);
    },
    'render-file': async target => {
      const r = await fetch('/api/render/file', { method: 'POST' });
      const body = await r.text();
      renderJson(target, r.status, body, 'wrote to disk');
    },
    'bad-version': async target => {
      const r = await fetch('/api/render/error');
      const body = await r.text();
      renderJson(target, r.status, body, 'error payload');
    },
  };

  document.querySelectorAll('button.run').forEach(btn => {
    btn.addEventListener('click', async () => {
      const action = btn.dataset.action;
      const target = document.getElementById(btn.dataset.target);
      const fn = actions[action];
      if (!fn) return;
      target.classList.remove('is-error', 'is-ok');
      target.innerHTML = paneHead('output', 'running…') + `<div class="empty">Working…</div>`;
      await withLoading(btn, async () => {
        try { await fn(target); }
        catch (e) { renderError(target, e.message || String(e)); }
      });
    });
  });

  // Generic clipboard-copy delegated handler for any button.copy
  // (the doc-id badge and CLI blocks both render these dynamically).
  document.addEventListener('click', e => {
    const btn = e.target.closest('button.copy');
    if (!btn) return;
    const text = btn.dataset.copy;
    navigator.clipboard?.writeText(text);
    const orig = btn.textContent;
    btn.textContent = 'copied';
    btn.classList.add('copied');
    setTimeout(() => { btn.textContent = orig; btn.classList.remove('copied'); }, 1200);
  });
})();
