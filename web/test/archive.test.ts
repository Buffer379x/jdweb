import { test } from "node:test";
import assert from "node:assert/strict";
import { buildPatch, hasSecret, splitPasswords, type ArchiveEdit } from "../src/lib/archive.ts";
import { MASK } from "../src/lib/editors.ts";

const orig = {
  archiveId: "a.rar", extractPath: "/x", finalPassword: MASK, passwords: [MASK], autoExtract: true,
  removeDownloadLinksAfterExtraction: false, removeFilesAfterExtraction: false, secret: true,
};
const same: ArchiveEdit = { extractPath: "/x", autoExtract: true, removeDownloadLinksAfterExtraction: false, removeFilesAfterExtraction: false, finalPassword: null, passwords: null };

test("nothing changed, nothing sent", () => {
  assert.deepEqual(buildPatch(orig, same), {});
});

test("only the changed fields go", () => {
  assert.deepEqual(buildPatch(orig, { ...same, extractPath: "/y", removeFilesAfterExtraction: true }), { extractPath: "/y", removeFilesAfterExtraction: true });
});

test("untouched secrets are not sent, so the stored ones stay", () => {
  const p = buildPatch(orig, { ...same, extractPath: "/y" });
  assert.ok(!("finalPassword" in p));
  assert.ok(!("passwords" in p));
});

test("a changed secret is sent, the mask never", () => {
  assert.deepEqual(buildPatch(orig, { ...same, finalPassword: "new" }), { finalPassword: "new" });
  assert.deepEqual(buildPatch(orig, { ...same, finalPassword: MASK }), {});
  assert.deepEqual(buildPatch(orig, { ...same, passwords: `a\n${MASK}\nb` }), {});
});

test("passwords are one per line, clean, and replace the list", () => {
  assert.deepEqual(splitPasswords(" a \n\nb\r\na\n  \n"), ["a", "b"]);
  assert.deepEqual(buildPatch(orig, { ...same, passwords: "one\ntwo" }), { passwords: ["one", "two"] });
  assert.deepEqual(buildPatch(orig, { ...same, passwords: "" }), { passwords: [] }, "an empty text clears the list");
});

test("revealed values that were not edited are not sent again", () => {
  const revealed = { ...orig, finalPassword: "real", passwords: ["p1", "p2"] };
  assert.deepEqual(buildPatch(revealed, { ...same, finalPassword: "real", passwords: "p1\np2" }), {});
});

test("secret presence", () => {
  assert.ok(hasSecret(MASK));
  assert.ok(hasSecret([MASK]));
  assert.ok(!hasSecret(""));
  assert.ok(!hasSecret([]));
});
