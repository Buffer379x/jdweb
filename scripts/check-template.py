#!/usr/bin/env python3
"""Checks the Unraid template: well-formed XML, the fields Unraid needs, and the
promises of the README (port, data path, required JDWEB_JD_URL, hardened run
options, links on raw.githubusercontent.com).

Usage: check-template.py [deploy/jdweb.xml]
"""
import re
import sys
import xml.etree.ElementTree as ET

path = sys.argv[1] if len(sys.argv) > 1 else "deploy/jdweb.xml"
errors = []


def need(cond, msg):
    if not cond:
        errors.append(msg)


try:
    root = ET.parse(path).getroot()
except ET.ParseError as e:
    sys.exit(f"FAIL: {path} is not well-formed XML: {e}")

need(root.tag == "Container" and root.get("version") == "2", "root must be <Container version=\"2\">")


def text(tag):
    el = root.find(tag)
    return (el.text or "").strip() if el is not None else ""


for tag in ("Name", "Repository", "Registry", "Network", "Support", "Project", "Overview",
            "Category", "WebUI", "TemplateURL", "Icon", "ExtraParams"):
    need(text(tag), f"<{tag}> is missing or empty")

need(text("Repository") == "ghcr.io/buffer379x/jdweb:latest", "Repository must be ghcr.io/buffer379x/jdweb:latest")
need(text("Network") == "bridge", "Network must be bridge")
need(text("WebUI") == "http://[IP]:[PORT:7468]/", "WebUI must be http://[IP]:[PORT:7468]/")
need(text("Category") == "Downloaders: Network:Web:", "Category must be 'Downloaders: Network:Web:'")
base = "https://raw.githubusercontent.com/Buffer379x/jdweb/main/deploy/"
need(text("TemplateURL") == base + "jdweb.xml", "TemplateURL must point to raw.githubusercontent.com .../deploy/jdweb.xml")
need(text("Icon") == base + "jdweb-icon.png", "Icon must point to raw.githubusercontent.com .../deploy/jdweb-icon.png")
for flag in ("--user 0:0", "--read-only", "--security-opt no-new-privileges:true", "--memory=256m"):
    need(flag in text("ExtraParams"), f"ExtraParams must contain {flag}")

ov = text("Overview").lower()
for phrase, why in (("unofficial", "say that it is unofficial"),
                    ("appwork", "say that it is not affiliated with AppWork"),
                    ("deprecated api enabled", "name the setting Deprecated Api Enabled"),
                    ("deprecated api localhost only", "name the setting Deprecated Api Localhost Only"),
                    ("setup token", "mention the setup token in the log"),
                    ("no login", "say that JDownloader's API has no login")):
    need(phrase in ov, f"Overview must {why}")

configs = {}
for c in root.findall("Config"):
    need(c.get("Type") in ("Port", "Path", "Variable"), f"Config {c.get('Name')!r}: bad Type {c.get('Type')!r}")
    need(c.get("Name"), "a Config has no Name")
    configs[c.get("Target")] = c

for target, default, required, display in (("7468", "7468", "true", "always"),
                                            ("/data", "/mnt/cache/appdata/jdweb", "true", "always"),
                                            ("JDWEB_JD_URL", "", "true", "always"),
                                            ("TZ", None, "false", "always"),
                                            ("PUID", "99", "true", "advanced"),
                                            ("PGID", "100", "true", "advanced"),
                                            ("JDWEB_TRUSTED_PROXIES", "", "false", "advanced"),
                                            ("JDWEB_LOG_LEVEL", "info", "false", "advanced")):
    c = configs.get(target)
    if c is None:
        errors.append(f"Config with Target {target} is missing")
        continue
    need(c.get("Required") == required, f"{target}: Required must be {required}")
    need(c.get("Display") == display, f"{target}: Display must be {display}")
    if default is not None:
        need(c.get("Default") == default, f"{target}: Default must be {default!r}")
        need((c.text or "").strip() == default, f"{target}: the value must equal the default {default!r}")
    need(c.get("Description"), f"{target}: Description is empty")
jd = configs.get("JDWEB_JD_URL")
if jd is not None:
    d = jd.get("Description", "")
    need("3128" in d and "Network Type" in d, "JDWEB_JD_URL description must explain :3128 and the choice of network")
need(len(set(c.get("Target") for c in root.findall("Config"))) == len(root.findall("Config")), "duplicate Config Target")

if errors:
    print(f"FAIL: {path}")
    for e in errors:
        print("  -", e)
    sys.exit(1)
print(f"OK: {path} ({len(configs)} settings)")
