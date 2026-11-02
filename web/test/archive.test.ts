import { test } from "node:test";
import assert from "node:assert/strict";
import { buildPatch, hasSecret, revealArchive, splitPasswords, type ArchiveEdit } from "../src/lib/archive.ts";
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
  assert.deepEqual(buildPatch(orig, { ...same, passwords: "" }), {}, "an empty text is no edit: deleting is explicit");
  assert.deepEqual(buildPatch(orig, { ...same, passwords: "  \n " }), {});
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

test("deleting a password is the explicit remove, never an empty field", () => {
  assert.deepEqual(buildPatch(orig, { ...same, finalPassword: "" }), {});
  assert.deepEqual(buildPatch(orig, { ...same, clearFinalPassword: true }), { finalPassword: "" });
  assert.deepEqual(buildPatch(orig, { ...same, clearPasswords: true }), { passwords: [] });
  assert.deepEqual(buildPatch(orig, { ...same, clearFinalPassword: true, clearPasswords: true, extractPath: "/z" }), { extractPath: "/z", finalPassword: "", passwords: [] });
  const none = { ...orig, finalPassword: "", passwords: [] as string[] };
  assert.deepEqual(buildPatch(none, { ...same, clearFinalPassword: true, clearPasswords: true }), {}, "nothing stored, nothing to remove");
});

// The flow of the dialog: the form opens with the masked settings, "Show" fetches the real values and puts them
// in the fields, the user saves (or not). The helper does what the component does.
function serve(real: { finalPassword: string; passwords: string[] }) {
  const calls: string[] = [];
  globalThis.fetch = (async (url: string) => {
    calls.push(url);
    return new Response(JSON.stringify(real), { status: 200, headers: { "Content-Type": "application/json" } });
  }) as typeof fetch;
  return calls;
}

test("after Show nothing is dirty, and the stored list is never cleaned unasked", async () => {
  // Doubles, blanks and spaces in the stored list: sending it back would silently change it.
  const stored = { finalPassword: "real", passwords: ["p1", " p2 ", "p1", ""] };
  const calls = serve(stored);
  const r = await revealArchive("a/b.rar", orig);
  assert.deepEqual(calls, ["/api/v1/extraction/archives/a%2Fb.rar/reveal"]);
  const shown: ArchiveEdit = { ...same, finalPassword: r.finalPassword, passwords: r.passwords };
  assert.deepEqual(buildPatch(r.base, shown), {}, "showing is no edit");
  assert.notDeepEqual(buildPatch(orig, shown), {}, "against the mask it would look like one: that was the defect");
  // One real edit sends that field only.
  assert.deepEqual(buildPatch(r.base, { ...shown, extractPath: "/y" }), { extractPath: "/y" });
  assert.deepEqual(buildPatch(r.base, { ...shown, finalPassword: "other" }), { finalPassword: "other" });
  // A list edit goes as the cleaned list, as the hint says.
  assert.deepEqual(buildPatch(r.base, { ...shown, passwords: r.passwords + "\nnew" }), { passwords: ["p1", "p2", "new"] });
  // Putting the text back to what it was is no edit.
  assert.deepEqual(buildPatch(r.base, { ...shown, finalPassword: "x" }), { finalPassword: "x" });
  assert.deepEqual(buildPatch(r.base, { ...shown, finalPassword: "real" }), {});
  // And remove works against the revealed baseline too.
  assert.deepEqual(buildPatch(r.base, { ...shown, clearPasswords: true }), { passwords: [] });
});

test("revealing an archive without secrets gives empty fields", async () => {
  serve({ finalPassword: "", passwords: [] });
  const r = await revealArchive("x", { ...orig, finalPassword: "", passwords: [] });
  assert.deepEqual(buildPatch(r.base, { ...same, finalPassword: r.finalPassword, passwords: r.passwords }), {});
});
