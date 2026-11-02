import { test } from "node:test";
import assert from "node:assert/strict";
import { answerFields, buildAnswer, dialogKind, fieldKind, hasCloseReason, splitProperties } from "../src/lib/questions.ts";

test("the Java type of a field decides the control", () => {
  assert.equal(fieldKind("boolean"), "bool");
  assert.equal(fieldKind("class java.lang.Boolean"), "bool");
  assert.equal(fieldKind("int"), "number");
  assert.equal(fieldKind("class java.lang.Long"), "number");
  assert.equal(fieldKind("class java.lang.String"), "text");
  assert.equal(fieldKind("class org.appwork.uio.CloseReason"), "text");
  assert.equal(fieldKind(""), "text");
});

const answer = { closereason: "class org.appwork.uio.CloseReason", dontShowAgain: "boolean", newName: "class java.lang.String", count: "int" };

test("the close reason is carried by the buttons, the other fields are shown", () => {
  assert.deepEqual(answerFields(answer), [
    { name: "count", kind: "number" }, { name: "dontShowAgain", kind: "bool" }, { name: "newName", kind: "text" },
  ]);
  assert.ok(hasCloseReason(answer));
  assert.ok(!hasCloseReason({ x: "int" }));
  assert.deepEqual(answerFields(undefined), []);
});

test("an answer holds the close reason and the filled fields with their types", () => {
  const fields = answerFields(answer);
  const data = buildAnswer("OK", fields, { dontShowAgain: true, newName: "file (1).mp4", count: " 3 " }, answer);
  assert.deepEqual(data, { closereason: "OK", dontShowAgain: true, newName: "file (1).mp4", count: 3 });
});

test("empty and invalid numbers are left out, untouched fields too", () => {
  const fields = answerFields(answer);
  assert.deepEqual(buildAnswer("CANCEL", fields, { count: "", newName: "x" }, answer), { closereason: "CANCEL", newName: "x" });
  assert.deepEqual(buildAnswer("OK", fields, { count: "abc" }, answer), { closereason: "OK" });
  assert.deepEqual(buildAnswer("OK", [], {}, undefined), { closereason: "OK" });
});

test("the spelling of the close reason key of JDownloader is kept", () => {
  assert.deepEqual(buildAnswer("OK", [], {}, { closeReason: "x" }), { closeReason: "OK" });
});

test("title and message lead, the rest is a list, markup stays text", () => {
  const p = splitProperties({ title: "File exists", message: "Replace <b>it</b>?", path: "/a/b", size: "1 GB", empty: "" });
  assert.equal(p.title, "File exists");
  assert.equal(p.message, "Replace <b>it</b>?");
  assert.deepEqual(p.rest, [["path", "/a/b"], ["size", "1 GB"]]);
  assert.deepEqual(splitProperties({}), { title: "", message: "", rest: [] });
  assert.equal(splitProperties({ Caption: "C", Text: "T" }).title, "C");
  assert.equal(splitProperties({ Caption: "C", Text: "T" }).message, "T");
});

test("the interface name is shortened for the label", () => {
  assert.equal(dialogKind("jd.controlling.downloadcontroller.IfFileExistsDialogInterface"), "IfFileExists");
  assert.equal(dialogKind("org.appwork.uio.ConfirmDialogInterface"), "Confirm");
  assert.equal(dialogKind("Plain"), "Plain");
});
