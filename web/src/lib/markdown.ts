// Lightweight safe Markdown -> HTML renderer styled for daumini whiteboard prose.

function escapeHTML(s: string): string {
  return s
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function renderInline(raw: string): string {
  const codeSpans: string[] = [];
  let s = raw.replace(/`([^`\n]+)`/g, (_, code: string) => {
    const idx = codeSpans.length;
    codeSpans.push(`<code>${escapeHTML(code)}</code>`);
    return `\u0000CODE${idx}\u0000`;
  });

  s = escapeHTML(s);

  // Links [label](https://...)
  s = s.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, (_, label: string, url: string) => {
    return `<a href="${url}" target="_blank" rel="noreferrer">${label}</a>`;
  });

  // Bold **text** -> daumini highlighter mark (no faux bold smear in Xiaolai)
  s = s.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  // Italic *text*
  s = s.replace(/(^|[^*])\*([^*\n]+)\*/g, '$1<em>$2</em>');
  // Strikethrough ~~text~~
  s = s.replace(/~~([^~]+)~~/g, '<del>$1</del>');

  return s.replace(/\u0000CODE(\d+)\u0000/g, (_, i: string) => codeSpans[Number(i)] ?? '');
}

export function renderMarkdown(md: string): string {
  if (!md) return '';
  const lines = md.replace(/\r\n/g, '\n').split('\n');
  const out: string[] = [];
  let inCode = false;
  let codeLang = '';
  let codeLines: string[] = [];
  let listType: 'ul' | 'ol' | null = null;
  let paraLines: string[] = [];

  const flushPara = () => {
    if (paraLines.length === 0) return;
    out.push(`<p>${paraLines.map((l) => renderInline(l)).join('<br>')}</p>`);
    paraLines = [];
  };

  const flushList = () => {
    if (!listType) return;
    out.push(`</${listType}>`);
    listType = null;
  };

  for (const line of lines) {
    const fence = line.match(/^```([a-zA-Z0-9_+-]*)\s*$/);
    if (fence) {
      if (inCode) {
        const codeText = codeLines.join('\n');
        const langLabel = codeLang ? `<span class="md-code-lang">${escapeHTML(codeLang)}</span>` : '<span></span>';
        out.push(
          `<div class="md-code-wrap border-line"><div class="md-code-head">${langLabel}<button type="button" class="md-copy-btn" data-copy="${encodeURIComponent(codeText)}">复制</button></div><pre class="md-code-pre"><code>${escapeHTML(codeText)}</code></pre></div>`
        );
        inCode = false;
        codeLang = '';
        codeLines = [];
      } else {
        flushPara();
        flushList();
        inCode = true;
        codeLang = fence[1] || '';
      }
      continue;
    }

    if (inCode) {
      codeLines.push(line);
      continue;
    }

    if (line.trim() === '') {
      flushPara();
      flushList();
      continue;
    }

    const heading = line.match(/^(#{1,4})\s+(.+)$/);
    if (heading) {
      flushPara();
      flushList();
      const lvl = heading[1].length;
      out.push(`<h${lvl}>${renderInline(heading[2])}</h${lvl}>`);
      continue;
    }

    if (/^[-*_]{3,}\s*$/.test(line)) {
      flushPara();
      flushList();
      out.push('<hr class="md-hr">');
      continue;
    }

    const bq = line.match(/^>\s?(.*)$/);
    if (bq) {
      flushPara();
      flushList();
      out.push(`<blockquote>${renderInline(bq[1])}</blockquote>`);
      continue;
    }

    const ul = line.match(/^\s*[-*+]\s+(.+)$/);
    if (ul) {
      flushPara();
      if (listType !== 'ul') {
        flushList();
        listType = 'ul';
        out.push('<ul>');
      }
      out.push(`<li>${renderInline(ul[1])}</li>`);
      continue;
    }

    const ol = line.match(/^\s*\d+\.\s+(.+)$/);
    if (ol) {
      flushPara();
      if (listType !== 'ol') {
        flushList();
        listType = 'ol';
        out.push('<ol>');
      }
      out.push(`<li>${renderInline(ol[1])}</li>`);
      continue;
    }

    flushList();
    paraLines.push(line);
  }

  if (inCode) {
    const codeText = codeLines.join('\n');
    out.push(`<div class="md-code-wrap border-line"><pre class="md-code-pre"><code>${escapeHTML(codeText)}</code></pre></div>`);
  }
  flushPara();
  flushList();
  return out.join('\n');
}
