import { test } from "node:test";
import assert from "node:assert/strict";
import { isSafeQrSvg, qrDataUrl } from "../src/lib/qr.ts";

const good = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 33 33" shape-rendering="crispEdges"><rect width="33" height="33" fill="#fff"/><path fill="#000" d="M1 1h7v7h-7z"/></svg>';

test("the SVG of the server passes", () => {
  assert.ok(isSafeQrSvg(good));
  assert.ok(isSafeQrSvg(`\n${good}\n`));
  const url = qrDataUrl(good);
  assert.ok(url?.startsWith("data:image/svg+xml;charset=utf-8,"));
  assert.ok(!url!.includes("<"), "the markup is escaped in the URL");
});

test("scripts, handlers, links and foreign content are refused", () => {
  const bad = [
    '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><rect/></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><rect onclick="x()"/></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><a href="javascript:alert(1)"><rect/></a></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><div/></foreignObject></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><image href="http://evil/x.png"/></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><rect style="fill:red"/></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><rect href="x"/></svg>',
    '<svg xmlns="http://www.w3.org/1999/xhtml"><rect/></svg>',
    '<!DOCTYPE svg><svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>',
    '<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><!-- c --><rect/></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><![CDATA[x]]></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg">text<rect/></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><rect fill="&#106;avascript:x"/></svg>',
    '<svg xmlns="http://www.w3.org/2000/svg"><rect/>',
    '<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg></svg>',
    '<rect/>',
    '<svg xmlns="http://www.w3.org/2000/svg"><rect fill=\'x\'/></svg>',
    "",
    "not svg",
  ];
  for (const b of bad) {
    assert.ok(!isSafeQrSvg(b), b);
    assert.equal(qrDataUrl(b), null, b);
  }
});

test("a huge text is refused before it is looked at", () => {
  assert.ok(!isSafeQrSvg(`<svg xmlns="http://www.w3.org/2000/svg">${" ".repeat(300_000)}</svg>`));
});
