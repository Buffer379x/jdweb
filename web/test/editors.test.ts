import { test } from "node:test";
import assert from "node:assert/strict";
import {
  MASK, editorFor, filterIndexes, holdsMask, isMask, parseText, sameValue, searchLine, secretIsSet, shortInterface, valueToText,
} from "../src/lib/editors.ts";

const opts = [["A", "First"], ["B", ""]];

test("the type of a setting picks the editor", () => {
  const k = (abstractType: string, enumOptions?: string[][]) => editorFor({ abstractType, enumOptions });
  assert.equal(k("BOOLEAN"), "bool");
  assert.equal(k("INT"), "int");
  assert.equal(k("LONG"), "long");
  assert.equal(k("DOUBLE"), "double");
  assert.equal(k("STRING"), "string");
  assert.equal(k("HEX_COLOR"), "string");
  assert.equal(k("ENUM", opts), "enum");
  assert.equal(k("ENUM"), "string", "an enum without options is typed in");
  assert.equal(k("ENUM", []), "string");
  assert.equal(k("STRING_LIST"), "list");
  assert.equal(k("INT_LIST"), "list");
  assert.equal(k("ENUM_LIST", opts), "list");
  assert.equal(k("OBJECT"), "json");
  assert.equal(k("OBJECT_LIST"), "json");
  assert.equal(k("SOMETHING_NEW"), "json");
});

const E = { abstractType: "STRING", enumOptions: undefined };

test("integers must be integers inside the range of an int", () => {
  assert.deepEqual(parseText("int", " 42 ", E), { ok: true, value: 42 });
  assert.deepEqual(parseText("int", "-7", E), { ok: true, value: -7 });
  assert.deepEqual(parseText("int", "2147483647", E), { ok: true, value: 2147483647 });
  for (const bad of ["", "1.5", "abc", "2147483648", "-2147483649", "1e3", "0x1"]) assert.deepEqual(parseText("int", bad, E), { ok: false, error: "int" }, bad);
});

test("longs stop where a JSON number stops being exact", () => {
  assert.deepEqual(parseText("long", "1800000", E), { ok: true, value: 1800000 });
  assert.deepEqual(parseText("long", "9007199254740991", E), { ok: true, value: 9007199254740991 });
  assert.deepEqual(parseText("long", "9007199254740993", E), { ok: false, error: "long" });
  assert.deepEqual(parseText("long", "1.5", E), { ok: false, error: "long" });
});

test("doubles take a comma", () => {
  assert.deepEqual(parseText("double", "0,5", E), { ok: true, value: 0.5 });
  assert.deepEqual(parseText("double", "-2.25", E), { ok: true, value: -2.25 });
  assert.deepEqual(parseText("double", "", E), { ok: false, error: "double" });
  assert.deepEqual(parseText("double", "NaN", E), { ok: false, error: "double" });
});

test("a text stays as typed, spaces included", () => {
  assert.deepEqual(parseText("string", "  a b  ", E), { ok: true, value: "  a b  " });
  assert.deepEqual(parseText("string", "", E), { ok: true, value: "" });
});

test("an enum value must be one of the options", () => {
  const e = { abstractType: "ENUM", enumOptions: opts };
  assert.deepEqual(parseText("enum", "A", e), { ok: true, value: "A" });
  assert.deepEqual(parseText("enum", "C", e), { ok: false, error: "enum" });
});

test("lists are JSON arrays of the right items", () => {
  const sl = { abstractType: "STRING_LIST", enumOptions: undefined };
  assert.deepEqual(parseText("list", '["a","b"]', sl), { ok: true, value: ["a", "b"] });
  assert.deepEqual(parseText("list", "", sl), { ok: true, value: [] });
  assert.deepEqual(parseText("list", "[]", sl), { ok: true, value: [] });
  assert.deepEqual(parseText("list", '{"a":1}', sl), { ok: false, error: "list" });
  assert.deepEqual(parseText("list", "[1,2]", sl), { ok: false, error: "listItems" });
  assert.deepEqual(parseText("list", '["a",', sl), { ok: false, error: "list" });
  const il = { abstractType: "INT_LIST", enumOptions: undefined };
  assert.deepEqual(parseText("list", "[80, 10101]", il), { ok: true, value: [80, 10101] });
  assert.deepEqual(parseText("list", '["80"]', il), { ok: false, error: "listInts" });
  assert.deepEqual(parseText("list", "[1.5]", il), { ok: false, error: "listInts" });
});

test("json settings need valid JSON", () => {
  assert.deepEqual(parseText("json", '{"a":[1]}', E), { ok: true, value: { a: [1] } });
  assert.deepEqual(parseText("json", "null", E), { ok: true, value: null });
  assert.deepEqual(parseText("json", "", E), { ok: false, error: "empty" });
  assert.deepEqual(parseText("json", "{a:1}", E), { ok: false, error: "json" });
});

test("values become the text of the editor and back", () => {
  assert.equal(valueToText("int", 5), "5");
  assert.equal(valueToText("string", null), "");
  assert.equal(valueToText("list", null), "[]");
  assert.equal(valueToText("list", ["a"]), '[\n  "a"\n]');
  const il = { abstractType: "INT_LIST", enumOptions: undefined };
  assert.deepEqual(parseText("list", valueToText("list", [80, 1]), il), { ok: true, value: [80, 1] });
  assert.equal(valueToText("json", { a: 1 }), '{\n  "a": 1\n}');
});

test("values compare as JSON, null and missing are one", () => {
  assert.ok(sameValue(["a"], ["a"]));
  assert.ok(!sameValue(["a"], ["b"]));
  assert.ok(sameValue(null, undefined));
  assert.ok(!sameValue(0, null));
  assert.ok(!sameValue("", null));
});

test("the mask is recognized alone and inside a list", () => {
  assert.equal(MASK, "••••••");
  assert.ok(isMask(MASK));
  assert.ok(!isMask("x"));
  assert.ok(holdsMask(MASK));
  assert.ok(holdsMask(["ok", MASK]));
  assert.ok(!holdsMask(["ok"]));
  assert.ok(!holdsMask(5));
  assert.ok(!holdsMask(null));
});

test("a secret is set when the server did not leave it empty", () => {
  assert.ok(secretIsSet(MASK));
  assert.ok(secretIsSet([MASK]));
  assert.ok(!secretIsSet(""));
  assert.ok(!secretIsSet(null));
  assert.ok(!secretIsSet([]));
});

test("the filter finds every word in interface, key, plugin and docs", () => {
  const entries = [
    { interface: "org.jdownloader.settings.GeneralSettings", storage: "", key: "MaxSimultaneDownloads", docs: "How many at once" },
    { interface: "org.jdownloader.plugins.YoutubeConfig", storage: "youtube.com", key: "SubtitlesEnabled", docs: "Grab subtitles" },
    { interface: "x.Other", storage: "", key: "Zebra", docs: "" },
  ];
  const lines = entries.map(searchLine);
  assert.deepEqual(filterIndexes(lines, ""), [0, 1, 2]);
  assert.deepEqual(filterIndexes(lines, "simultane"), [0]);
  assert.deepEqual(filterIndexes(lines, "YOUTUBE subtitles"), [1]);
  assert.deepEqual(filterIndexes(lines, "generalsettings max"), [0]);
  assert.deepEqual(filterIndexes(lines, "org.jdownloader"), [], "the package part of an interface is not searched");
  assert.deepEqual(filterIndexes(lines, "nothing here"), []);
  assert.equal(shortInterface("a.b.C"), "C");
  assert.equal(shortInterface("C"), "C");
});

test("filtering 2,200 settings is far below 150 ms", () => {
  const lines: string[] = [];
  for (let i = 0; i < 2200; i++) {
    lines.push(searchLine({ interface: `org.jdownloader.generated.Config${i % 40}`, storage: i % 3 ? "plugin.example" : "", key: `Setting${i}Enabled`, docs: "Generated setting. ".repeat(10) }));
  }
  const t0 = performance.now();
  for (let r = 0; r < 20; r++) filterIndexes(lines, "setting12 enabled");
  const each = (performance.now() - t0) / 20;
  assert.ok(each < 20, `one filter took ${each.toFixed(2)} ms`);
});
