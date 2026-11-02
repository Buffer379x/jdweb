// The QR code of the two-factor enrollment comes from the server as an SVG text. It is shown as an
// image with a data: URL, where no script can run; this check makes sure it is nothing but a drawing anyway.

const ALLOWED_TAGS = new Set(["svg", "path", "rect"]);
const ALLOWED_ATTRS = new Set(["xmlns", "viewbox", "shape-rendering", "width", "height", "fill", "d", "x", "y"]);
const TAG = /<(\/?)([a-zA-Z][\w:-]*)((?:\s+[a-zA-Z][\w:-]*="[^"<>]*")*)\s*(\/?)>/y;
const ATTR = /\s+([a-zA-Z][\w:-]*)="([^"<>]*)"/g;

/** True when the text holds only svg, path and rect elements with plain drawing attributes. */
export function isSafeQrSvg(svg: string): boolean {
  if (typeof svg !== "string" || svg.length === 0 || svg.length > 200_000) return false;
  let pos = 0;
  let depth = 0;
  let sawRoot = false;
  while (pos < svg.length) {
    if (svg[pos] !== "<") {
      if (!/\s/.test(svg[pos]!)) return false; // text between elements is no drawing
      pos++;
      continue;
    }
    TAG.lastIndex = pos;
    const m = TAG.exec(svg);
    if (!m) return false; // comments, doctype, processing instructions, broken tags
    const [all, close, name, attrs, selfClose] = m;
    const tag = name!.toLowerCase();
    if (!ALLOWED_TAGS.has(tag)) return false;
    if (close) {
      if (attrs || selfClose || depth === 0) return false;
      depth--;
    } else {
      if (!sawRoot && tag !== "svg") return false;
      sawRoot = true;
      ATTR.lastIndex = 0;
      let a: RegExpExecArray | null;
      while ((a = ATTR.exec(attrs!))) {
        const an = a[1]!.toLowerCase();
        if (!ALLOWED_ATTRS.has(an)) return false;
        if (an === "xmlns" && a[2] !== "http://www.w3.org/2000/svg") return false;
        if (/javascript:|data:|&#|&/i.test(a[2]!)) return false;
      }
      if (!selfClose) depth++;
    }
    pos += all!.length;
  }
  return sawRoot && depth === 0;
}

/** A data: URL for an <img>, or null when the SVG is not safe. */
export function qrDataUrl(svg: string): string | null {
  if (!isSafeQrSvg(svg)) return null;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}
