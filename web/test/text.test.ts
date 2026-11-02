import { test } from "node:test";
import assert from "node:assert/strict";
import { displayText } from "../src/lib/text.ts";

test("bidi override characters are replaced, everything else stays", () => {
  assert.equal(displayText("‮txt.exe-gnp.‭"), "�txt.exe-gnp.�");
  assert.equal(displayText("a⁦b⁩c"), "a�b�c");
  assert.equal(displayText("<script>alert(1)</script> ملف \u{1F3AC}"), "<script>alert(1)</script> ملف \u{1F3AC}");
  assert.equal(displayText(""), "");
});
